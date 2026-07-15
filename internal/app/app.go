package app

import (
	"context"
	"net/http"

	"AgentScope/internal/auth"
	"AgentScope/internal/config"
	"AgentScope/internal/httpapi"
)

// NewServer creates an *http.Server configured with the application router.
// It does not call ListenAndServe — the caller controls the lifecycle.
// Services are wired through httpapi.Dependencies; unset fields cause panics
// at request time.
func NewServer(ctx context.Context, cfg config.Config) (*http.Server, error) {
	sessionSigner := auth.NewSessionSigner([]byte(cfg.SessionSecret))

	deps := httpapi.Dependencies{
		SessionSigner: sessionSigner,
		// TODO: Wire real AuthService and ProjectsService with store.Queries
		// once the store layer is connected.
	}

	handler := httpapi.NewRouter(deps)

	server := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: handler,
	}

	return server, nil
}
