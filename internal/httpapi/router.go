package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// Dependencies holds the dependencies injected into the router.
// Fields will be populated in later tasks as services are added.
type Dependencies struct{}

// NewRouter creates a chi router with all application routes mounted.
func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	r.Get("/healthz", handleHealthz)

	return r
}

// handleHealthz responds with {"ok":true} to signal the server is running.
func handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
