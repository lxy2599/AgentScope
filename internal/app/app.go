package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"AgentScope/internal/adapter"
	"AgentScope/internal/agents"
	"AgentScope/internal/annotation"
	"AgentScope/internal/anomalies"
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
	_ httpapi.UserSessions          = (*auth.SessionService)(nil)
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
	_ httpapi.AnomalyService        = (*anomalies.Service)(nil)
	_ anomalies.Reader              = (*tracestore.Store)(nil)
	_ anomalies.Repository          = (*adapter.AnomalyRepository)(nil)
	_ realtime.Outbox               = (*adapter.RealtimeOutbox)(nil)
	_ httpapi.RealtimeOutboxWriter  = (*adapter.RealtimeOutbox)(nil)
)

const (
	defaultReadHeaderTimeout = 5 * time.Second
	defaultReadTimeout       = 30 * time.Second
	defaultWriteTimeout      = 60 * time.Second
	defaultIdleTimeout       = 2 * time.Minute
	defaultMaxHeaderBytes    = 1 << 20
)

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadTimeout:       defaultReadTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
		MaxHeaderBytes:    defaultMaxHeaderBytes,
	}
}

// Server owns both the HTTP server and its storage-backed background writers.
// Shutdown first waits for active HTTP handlers, then drains writers before
// closing database connections.
type Server struct {
	*http.Server
	closeResources func() error
	closeOnce      sync.Once
	closeErr       error
}

func (s *Server) Shutdown(ctx context.Context) error {
	if err := s.Server.Shutdown(ctx); err != nil {
		return err
	}
	s.closeOnce.Do(func() {
		if s.closeResources != nil {
			s.closeErr = s.closeResources()
		}
	})
	return s.closeErr
}

// tokenAuthAdapter wraps *projects.Service to implement httpapi.TokenAuth.
type tokenAuthAdapter struct {
	svc *projects.Service
}

func (t *tokenAuthAdapter) AuthenticateToken(ctx context.Context, plainToken, requiredScope string) (string, string, error) {
	auth, err := t.svc.AuthenticateToken(ctx, plainToken, requiredScope)
	if err != nil {
		return "", "", err
	}
	// Best-effort operational metadata; it must never affect a successful ingestion request.
	t.svc.RecordTokenUse(ctx, auth.TokenID, "scope:"+requiredScope, "", "")
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

// NewServer creates a Server configured with the application router and
// ordered resource shutdown.
func NewServer(ctx context.Context, cfg config.Config) (*Server, error) {
	sessionSigner := auth.NewSessionSignerWithTTL([]byte(cfg.SessionSecret), cfg.SessionTTL)

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
	userSessions := auth.NewSessionService(adapter.NewSessionRepository(pool), sessionSigner)
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
	anomalyService := anomalies.NewService(
		chStore,
		adapter.NewAnomalyRepository(pool),
		anomalies.Options{
			BaselineWindow:     cfg.Anomalies.BaselineWindow,
			RecentWindow:       cfg.Anomalies.RecentWindow,
			MinBaselineSamples: cfg.Anomalies.MinBaselineSamples,
			Threshold:          cfg.Anomalies.Threshold,
			OnError: func(err error) {
				log.Printf("operational anomaly detection failed: %v", err)
			},
		},
	)
	if cfg.Anomalies.Enabled {
		go func() {
			if err := anomalyService.Run(ctx, cfg.Anomalies.Interval); err != nil &&
				err != context.Canceled {
				log.Printf("operational anomaly detector stopped: %v", err)
			}
		}()
	}
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
	traceWriter := ingest.NewBufferedTraceWriter(
		ingest.NewClickHouseTraceWriter(httpTracestore),
		ingest.TraceBufferOptions{
			QueueCapacity: cfg.Ingest.TraceQueueCapacity,
			MaxBatchSpans: cfg.Ingest.TraceBatchSpans,
			FlushInterval: cfg.Ingest.TraceFlushInterval,
			WriteTimeout:  cfg.Ingest.TraceWriteTimeout,
			Workers:       cfg.Ingest.TraceWorkers,
		},
	)
	ingestStats := adapter.NewIngestStatsAdapter(chStore)
	ingestHealthSink := adapter.NewIngestHealthRecorder(chStore)
	ingestHealth := ingest.NewBufferedHealthRecorder(ingestHealthSink, ingest.HealthBufferOptions{
		QueueCapacity: cfg.Ingest.HealthQueueCapacity,
		BatchSize:     cfg.Ingest.HealthBatchSize,
		FlushInterval: cfg.Ingest.HealthFlushInterval,
		OnError: func(err error) {
			log.Printf("flush ingest health event batch: %v", err)
		},
	})
	var realtimeSubscriber realtime.Subscriber
	var realtimeOutbox *adapter.RealtimeOutbox
	var quotaService *quotas.Service
	var redisClient *redis.Client
	if cfg.Realtime.RedisAddr != "" {
		redisClient = redis.NewClient(&redis.Options{
			Addr:                  cfg.Realtime.RedisAddr,
			Password:              cfg.Realtime.RedisPassword,
			DB:                    cfg.Realtime.RedisDB,
			ContextTimeoutEnabled: true,
		})
	}
	if cfg.Quotas.Enabled && redisClient != nil {
		quotaService = quotas.NewServiceWithOptions(
			quotas.NewPostgresRepository(q),
			quotas.NewRedisLimiter(redisClient, cfg.Quotas.RedisNamespace),
			adapter.NewQuotaBudgetUsage(chStore),
			quotas.ServiceOptions{LimiterTimeout: cfg.Quotas.LimiterTimeout},
		)
	}

	// Phase 4: Redis is an optional realtime transport. PostgreSQL remains the
	// source of truth, so an unavailable Redis never blocks server startup.
	if cfg.Realtime.Enabled {
		realtimeOutbox = adapter.NewRealtimeOutbox(q)
	}
	if cfg.Realtime.Enabled && redisClient != nil {
		stream := realtime.NewRedisStream(redisClient, realtime.RedisStreamOptions{
			MaxLen:    cfg.Realtime.StreamMaxLen,
			DedupeTTL: cfg.Realtime.DedupeTTL,
		})
		realtimeSubscriber = realtime.NewRedisSubscriber(stream, realtime.SubscriberOptions{})
		relay := realtime.NewRelay(realtimeOutbox, stream, realtime.RelayOptions{
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
		UserSessions:       userSessions,
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
		AnomalyService:     anomalyService,
		TraceWriter:        traceWriter,
		IngestStats:        ingestStats,
		IngestHealth:       ingestHealth,
		RealtimeOutbox:     realtimeOutbox,
		RealtimeSubscriber: realtimeSubscriber,
		SnapshotService:    snapshotService,
		MaxBodySize:        cfg.Ingest.MaxBodySize,
		MaxSpans:           cfg.Ingest.MaxSpans,
		SecureCookies:      cfg.SecureCookies,
	}

	handler := httpapi.NewRouter(deps)

	// Try to serve static frontend from web/dist if it exists
	staticDir := "web/dist"
	if info, err := os.Stat(staticDir); err == nil && info.IsDir() {
		deps.StaticFS = os.DirFS(staticDir)
		handler = httpapi.NewRouter(deps)
	}

	httpServer := newHTTPServer(cfg.HTTPAddr, handler)
	server := &Server{Server: httpServer}
	server.closeResources = func() error {
		var closeErrors []error
		flushCtx, cancelTokenUsage := context.WithTimeout(context.Background(), 5*time.Second)
		if err := projectService.FlushTokenUses(flushCtx); err != nil {
			fmt.Fprintf(os.Stderr, "flush project token usage: %v\n", err)
			closeErrors = append(closeErrors, err)
		}
		cancelTokenUsage()
		if err := traceWriter.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close trace batch writer: %v\n", err)
			closeErrors = append(closeErrors, err)
		}
		traceStats := traceWriter.Stats()
		log.Printf(
			"trace batch writer stopped: enqueued_requests=%d written_requests=%d failed_requests=%d written_spans=%d flushes=%d",
			traceStats.EnqueuedRequests,
			traceStats.WrittenRequests,
			traceStats.FailedRequests,
			traceStats.WrittenSpans,
			traceStats.Flushes,
		)
		if err := ingestHealth.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close ingest health recorder: %v\n", err)
			closeErrors = append(closeErrors, err)
		}
		healthStats := ingestHealth.Stats()
		log.Printf(
			"ingest health recorder stopped: enqueued=%d written=%d dropped=%d failed=%d batches=%d",
			healthStats.Enqueued,
			healthStats.Written,
			healthStats.Dropped,
			healthStats.Failed,
			healthStats.Batches,
		)
		pool.Close()
		if err := chStore.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "close clickhouse: %v\n", err)
			closeErrors = append(closeErrors, err)
		}
		return errors.Join(closeErrors...)
	}

	return server, nil
}
