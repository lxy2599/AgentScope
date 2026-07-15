package app

import (
	"context"
	"net/http"

	"AgentScope/internal/config"
	"AgentScope/internal/httpapi"
)

// NewServer creates an *http.Server configured with the application router.
// It does not call ListenAndServe — the caller controls the lifecycle.
func NewServer(_ context.Context, cfg config.Config) (*http.Server, error) {
	deps := httpapi.RouterDependencies{}
	handler := httpapi.NewRouter(deps)

	server := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: handler,
	}

	return server, nil
}
