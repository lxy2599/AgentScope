package httpapi

import (
	"errors"
	"net/http"

	domainruns "AgentScope/internal/runs"
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

		// Check project membership
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Access denied")
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

		// First get lightweight run to check project membership before ClickHouse query
		run, err := deps.RunsService.GetRun(r.Context(), runID)
		if err != nil {
			if errors.Is(err, domainruns.ErrRunNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "Run not found")
				return
			}
			writeInternalError(w, "get_run_failed", "Failed to get run", "get run for detail authorization", err)
			return
		}

		// Check project membership
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, run.ProjectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Access denied")
			return
		}

		detail, err := deps.RunsService.GetRunDetail(r.Context(), runID)
		if err != nil {
			if errors.Is(err, domainruns.ErrRunNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "Run not found")
				return
			}
			writeInternalError(w, "detail_failed", "Failed to get run detail", "get run detail", err)
			return
		}

		writeJSON(w, http.StatusOK, detail)
	}
}
