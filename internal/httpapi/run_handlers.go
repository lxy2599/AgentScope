package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// handleListProjectRuns handles GET /api/projects/{projectID}/runs.
func handleListProjectRuns(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		projectID := chi.URLParam(r, "projectID")
		if projectID == "" {
			writeError(w, http.StatusBadRequest, "missing_project", "Project ID is required")
			return
		}

		runs, err := deps.RunsService.ListProjectRuns(r.Context(), projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", "Failed to list runs")
			return
		}

		writeJSON(w, http.StatusOK, runs)
	}
}

// handleGetRunDetail handles GET /api/runs/{runID}.
func handleGetRunDetail(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		runID := chi.URLParam(r, "runID")
		if runID == "" {
			writeError(w, http.StatusBadRequest, "missing_run", "Run ID is required")
			return
		}

		detail, err := deps.RunsService.GetRunDetail(r.Context(), runID)
		if err != nil {
			if err.Error() == "run not found" || err.Error() == "get run: run not found" {
				writeError(w, http.StatusNotFound, "not_found", "Run not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "detail_failed", "Failed to get run detail")
			return
		}

		writeJSON(w, http.StatusOK, detail)
	}
}
