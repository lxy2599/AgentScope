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

	// OTLP trace ingestion (uses bearer token auth, not session)
	r.Post("/v1/traces", handleIngest(deps))

	// Protected routes (require valid session)
	r.Group(func(r chi.Router) {
		r.Use(requireAuth(deps))
		r.Post("/api/projects", handleCreateProject(deps))
		r.Get("/api/projects", handleListProjects(deps))
		r.Post("/api/projects/{projectID}/tokens", handleCreateToken(deps))
		r.Get("/api/projects/{projectID}/runs", handleListProjectRuns(deps))
		r.Get("/api/runs/{runID}", handleGetRunDetail(deps))
		r.Post("/api/projects/{projectID}/datasets", handleCreateDataset(deps))
		r.Post("/api/datasets/{datasetID}/cases", handleAddCase(deps))
		r.Get("/api/datasets/{datasetID}/cases", handleListCases(deps))
		r.Post("/api/projects/{projectID}/experiments", handleCreateExperiment(deps))
		r.Get("/api/experiments/{experimentID}", handleGetExperiment(deps))
	})

	// Runner API (uses project token bearer auth)
	r.Group(func(r chi.Router) {
		r.Get("/runner/jobs/claim", handleClaimJob(deps))
		r.Post("/runner/jobs/{jobID}/heartbeat", handleHeartbeat(deps))
		r.Post("/runner/jobs/{jobID}/results", handleSubmitResult(deps))
		r.Post("/runner/jobs/{jobID}/fail", handleFailJob(deps))
	})

	return r
}

// handleHealthz responds with {"ok":true} to signal the server is running.
func handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
