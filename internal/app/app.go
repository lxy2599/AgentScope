package app

import (
	"context"
	"fmt"
	"net/http"

	"AgentScope/internal/adapter"
	"AgentScope/internal/auth"
	"AgentScope/internal/config"
	"AgentScope/internal/datasets"
	"AgentScope/internal/experiments"
	"AgentScope/internal/httpapi"
	"AgentScope/internal/projects"
	"AgentScope/internal/runnerapi"
	"AgentScope/internal/runs"
	"AgentScope/internal/store"
	"AgentScope/internal/tracestore"
)

// Compile-time interface checks: real adapters implement the expected interfaces.
var (
	_ httpapi.AuthService          = (*adapter.AuthService)(nil)
	_ httpapi.TokenAuth            = (*tokenAuthAdapter)(nil)
	_ httpapi.RunsServiceInterface = (*runs.Service)(nil)
	_ httpapi.TracestoreInterface  = (*adapter.HTTPTracestore)(nil)
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

// NewServer creates an *http.Server configured with the application router.
// It opens PostgreSQL and ClickHouse connections, wires all services, and
// returns an error if any dependency cannot be initialized.
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
	experimentQuerier := adapter.NewExperimentQuerier(q)
	runnerQuerier := adapter.NewRunnerQuerier(q)

	// Create real services and adapters
	authService := adapter.NewAuthService(q)
	projectService := projects.NewService(projectQuerier)
	runService := runs.NewService(runQuerier, runTracestore)
	datasetService := datasets.NewService(datasetQuerier)
	experimentService := experiments.NewService(experimentQuerier)
	runnerService := runnerapi.NewService(runnerQuerier)
	tokenAuth := &tokenAuthAdapter{svc: projectService}

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
	}

	handler := httpapi.NewRouter(deps)

	server := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: handler,
	}

	return server, nil
}
