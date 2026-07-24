package app

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"AgentScope/internal/adapter"
	"AgentScope/internal/agents"
	"AgentScope/internal/annotation"
	"AgentScope/internal/auth"
	"AgentScope/internal/config"
	"AgentScope/internal/datasets"
	"AgentScope/internal/evals"
	"AgentScope/internal/experiments"
	"AgentScope/internal/httpapi"
	"AgentScope/internal/ingest"
	"AgentScope/internal/onlinerval"
	"AgentScope/internal/projects"
	"AgentScope/internal/quotas"
	"AgentScope/internal/realtime"
	"AgentScope/internal/runnerapi"
	"AgentScope/internal/runs"
	"AgentScope/internal/sampling"
	"AgentScope/internal/snapshots"
	"AgentScope/internal/store"
	"AgentScope/internal/tracestore"
	"github.com/redis/go-redis/v9"
)

// Compile-time interface checks: real adapters implement the expected interfaces.
var (
	_ httpapi.AuthService           = (*adapter.AuthService)(nil)
	_ httpapi.TokenAuth             = (*tokenAuthAdapter)(nil)
	_ httpapi.ProjectAuth           = (*projectAuthAdapter)(nil)
	_ httpapi.ProjectAdminAuth      = (*projectAuthAdapter)(nil)
	_ httpapi.RunsServiceInterface  = (*runs.Service)(nil)
	_ httpapi.TracestoreInterface   = (*adapter.HTTPTracestore)(nil)
	_ httpapi.RuntimeService        = (*adapter.HTTPRuntimeService)(nil)
	_ httpapi.EvalsService          = (*adapter.HTTPEvalService)(nil)
	_ httpapi.EvalGovernanceService = (*evals.GovernanceService)(nil)
	_ httpapi.EvaluatorCreator      = (*adapter.EvaluatorStoreAdapter)(nil)
	_ httpapi.SamplingService       = (*sampling.Service)(nil)
	_ httpapi.OnlineEvalDispatcher  = (*onlinerval.Dispatcher)(nil)
	_ httpapi.CompareService        = (*adapter.HTTPCompareService)(nil)
	_ realtime.Outbox               = (*adapter.RealtimeOutbox)(nil)
)

// tokenAuthAdapter wraps *projects.Service to implement httpapi.TokenAuth.
type tokenAuthAdapter struct {
	svc *projects.Service
}

func (t *tokenAuthAdapter) AuthenticateToken(ctx context.Context, plainToken, requiredScope string) (string, string, error) {
	auth, err := t.svc.AuthenticateToken(ctx, plainToken, requiredScope)
	if err != nil {
		return "", "", err
	}
	return auth.ProjectID, auth.TokenID, nil
}

// projectAuthAdapter wraps *projects.Service to implement httpapi.ProjectAuth.
type projectAuthAdapter struct {
	svc *projects.Service
}

func (p *projectAuthAdapter) IsProjectMember(ctx context.Context, userID, projectID string) error {
	return p.svc.IsProjectMember(ctx, userID, projectID)
}

func (p *projectAuthAdapter) IsProjectAdmin(ctx context.Context, userID, projectID string) error {
	return p.svc.IsProjectAdmin(ctx, userID, projectID)
}

// NewServer creates an *http.Server configured with the application router.
func NewServer(ctx context.Context, cfg config.Config) (*http.Server, error) {
	sessionSigner := auth.NewSessionSigner([]byte(cfg.SessionSecret))

	// Open PostgreSQL connection
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect to postgresql: %w", err)
	}

	// Open ClickHouse connection
	chStore, err := tracestore.Open(ctx, cfg.ClickHouseDSN)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to clickhouse: %w", err)
	}

	// Create sqlc queries
	q := store.New(pool)

	// Create adapters for the services
	projectQuerier := adapter.NewProjectQuerier(q, pool)
	runQuerier := adapter.NewRunQuerier(q)
	runTracestore := adapter.NewRunTracestore(chStore)
	httpTracestore := adapter.NewHTTPTracestore(chStore)
	datasetQuerier := adapter.NewDatasetQuerier(q)
	experimentQuerier := adapter.NewExperimentQuerier(q, pool)
	runnerQuerier := adapter.NewRunnerQuerier(q, pool)

	// Create real services and adapters
	authService := adapter.NewAuthService(q)
	projectService := projects.NewService(projectQuerier)
	runService := runs.NewService(runQuerier, runTracestore)
	datasetService := datasets.NewService(datasetQuerier)
	experimentService := experiments.NewService(experimentQuerier)
	runnerService := runnerapi.NewService(runnerQuerier)
	agentQuerier := adapter.NewAgentQuerier(q)
	agentService := agents.NewService(agentQuerier)
	runtimeRunQuerier := adapter.NewRuntimeRunQuerier(q)
	runtimeService := runs.NewRuntimeService(runtimeRunQuerier, runTracestore)
	tokenAuth := &tokenAuthAdapter{svc: projectService}
	evalQuerier := adapter.NewEvalQuerier(q).WithPool(pool)
	evalService := evals.NewService(evalQuerier)
	evalGovernance := evals.NewGovernanceService(evalQuerier)
	evalCreator := adapter.NewEvaluatorStoreAdapter(q)
	samplingQuerier := adapter.NewSamplingQuerier(q)
	samplingService := sampling.NewService(samplingQuerier)
	annotationQuerier := adapter.NewAnnotationQuerier(q)
	annotationService := annotation.NewService(annotationQuerier)
	snapshotService := snapshots.NewService(adapter.NewSnapshotRepository(q))
	promoteAdapter := adapter.NewAnnotationPromoteAdapter(datasetService, runService)
	annotationService.SetPromoteDeps(promoteAdapter, promoteAdapter)

	// Create online eval dispatcher
	onlineRunSvc := adapter.NewOnlineRunService(runService)
	onlineEvalSvc := adapter.NewOnlineEvalService(evalService)
	onlineEvalDisp := onlinerval.NewDispatcher(samplingService, onlineRunSvc, onlineEvalSvc)

	// Phase 3.6: Experiment comparison service
	comparisonQuerier := adapter.NewComparisonQuerier(q, pool, experimentQuerier)
	compareService := experiments.NewCompareService(experimentQuerier, comparisonQuerier, experimentService)
	compareService.SetEvalScoreReader(adapter.NewEvalScoreReaderAdapter(evalService))

	// Create and wire the trace-aware online evaluator.
	// This evaluator uses real trace data from ClickHouse to produce
	// multi-dimensional scores (efficiency, final_answer, tool_correctness,
	// trajectory, rag_groundedness, safety). When trace data is unavailable
	// for a specific dimension, it returns an explicit reason.
	runDetailAdapter := adapter.NewOnlineRunDetailAdapter(runService)
	traceEval := onlinerval.NewTraceAwareEvaluator(runDetailAdapter)
	onlineEvalDisp.SetEvaluator(traceEval)

	// Wire the annotation callback: when online evaluation detects a low-score or
	// failed run, automatically create an annotation item in the project's default
	// "线上评测待标注" queue.
	onlineEvalDisp.SetAnnotationCallback(func(ctx context.Context, projectID, runID, traceID string, scores []onlinerval.EvalScore) {
		queue, err := annotationService.EnsureDefaultQueue(ctx, projectID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "online eval: failed to ensure default queue for project %s: %v\n", projectID, err)
			return
		}
		_, err = annotationService.CreateItem(ctx, annotation.CreateItemParams{
			QueueID:  queue.ID,
			RunID:    runID,
			TraceID:  traceID,
			Priority: 5, // default priority for auto-created items
			Scores:   onlinerval.MarshalScores(scores),
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "online eval: failed to create annotation item for run %s: %v\n", runID, err)
		}
	})

	// Phase 2.7: Wire ingest pipeline components (rate limiting, batch writing, stats queries)
	var rateLimiter *ingest.ProjectRateLimiter
	if cfg.Ingest.RateLimitEnabled {
		rateLimiter = ingest.NewProjectRateLimiter(
			cfg.Ingest.RateLimitCapacity,
			cfg.Ingest.RateLimitRefill,
			cfg.Ingest.RateLimitInterval,
		)
	}
	traceWriter := ingest.NewClickHouseTraceWriter(httpTracestore)
	ingestStats := adapter.NewIngestStatsAdapter(chStore)
	ingestHealth := adapter.NewIngestHealthRecorder(chStore)
	var realtimeSubscriber realtime.Subscriber
	var quotaService *quotas.Service
	var redisClient *redis.Client
	if cfg.Realtime.RedisAddr != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:     cfg.Realtime.RedisAddr,
			Password: cfg.Realtime.RedisPassword,
			DB:       cfg.Realtime.RedisDB,
		})
	}
	if cfg.Quotas.Enabled && redisClient != nil {
		quotaService = quotas.NewService(
			quotas.NewPostgresRepository(q),
			quotas.NewRedisLimiter(redisClient, cfg.Quotas.RedisNamespace),
			adapter.NewQuotaBudgetUsage(chStore),
		)
	}

	// Phase 4: Redis is an optional realtime transport. PostgreSQL remains the
	// source of truth, so an unavailable Redis never blocks server startup.
	if cfg.Realtime.Enabled && redisClient != nil {
		stream := realtime.NewRedisStream(redisClient, realtime.RedisStreamOptions{
			MaxLen:    cfg.Realtime.StreamMaxLen,
			DedupeTTL: cfg.Realtime.DedupeTTL,
		})
		realtimeSubscriber = realtime.NewRedisSubscriber(stream, realtime.SubscriberOptions{})
		relay := realtime.NewRelay(adapter.NewRealtimeOutbox(q), stream, realtime.RelayOptions{
			BatchSize:    cfg.Realtime.RelayBatchSize,
			PollInterval: cfg.Realtime.RelayPollInterval,
		})
		go func() {
			_ = relay.Run(ctx)
			_ = stream.Close()
		}()
	}

	deps := httpapi.Dependencies{
		AuthService:        authService,
		ProjectsService:    adapter.NewHTTPProjectsService(projectService),
		TokenAuth:          tokenAuth,
		RunsService:        runService,
		Tracestore:         httpTracestore,
		SessionSigner:      sessionSigner,
		DatasetsService:    adapter.NewHTTPDatasetsService(datasetService),
		ExperimentsService: adapter.NewHTTPExperimentsService(experimentService),
		RunnerService:      adapter.NewHTTPRunnerService(runnerService),
		AgentService:       adapter.NewHTTPAgentService(agentService),
		RuntimeService:     adapter.NewHTTPRuntimeService(runtimeService),
		ProjectAuth:        &projectAuthAdapter{svc: projectService},
		ProjectAdminAuth:   &projectAuthAdapter{svc: projectService},
		EvalsService:       adapter.NewHTTPEvalService(evalService),
		EvalGovernance:     evalGovernance,
		EvaluatorCreator:   evalCreator,
		SamplingService:    samplingService,
		AnnotationService:  adapter.NewAnnotationServiceAdapter(annotationService),
		OnlineEvalDisp:     onlineEvalDisp,
		CompareService:     adapter.NewHTTPCompareService(compareService),
		RateLimiter:        rateLimiter,
		QuotaService:       quotaService,
		TraceWriter:        traceWriter,
		IngestStats:        ingestStats,
		IngestHealth:       ingestHealth,
		RealtimeSubscriber: realtimeSubscriber,
		SnapshotService:    snapshotService,
		MaxBodySize:        cfg.Ingest.MaxBodySize,
		MaxSpans:           cfg.Ingest.MaxSpans,
	}

	handler := httpapi.NewRouter(deps)

	// Try to serve static frontend from web/dist if it exists
	staticDir := "web/dist"
	if info, err := os.Stat(staticDir); err == nil && info.IsDir() {
		deps.StaticFS = os.DirFS(staticDir)
		handler = httpapi.NewRouter(deps)
	}

	server := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: handler,
	}

	return server, nil
}
