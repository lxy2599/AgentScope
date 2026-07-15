package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// NewRouter creates a chi router with all application routes mounted.
func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	// Public routes
	r.Get("/healthz", handleHealthz)
	r.Post("/api/auth/register", handleRegister(deps))
	r.Post("/api/auth/login", handleLogin(deps))

	// Protected routes (require valid session)
	r.Group(func(r chi.Router) {
		r.Use(requireAuth(deps))
		r.Post("/api/projects", handleCreateProject(deps))
		r.Get("/api/projects", handleListProjects(deps))
		r.Post("/api/projects/{projectID}/tokens", handleCreateToken(deps))
	})

	return r
}

// handleHealthz responds with {"ok":true} to signal the server is running.
func handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
