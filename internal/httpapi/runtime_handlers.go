package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// handleListActiveRuns handles GET /api/projects/{project_id}/active-runs
func handleListActiveRuns(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		projectID := chi.URLParam(r, "projectID")
		if projectID == "" {
			writeError(w, http.StatusBadRequest, "missing_project_id", "Project ID is required")
			return
		}

		// Verify user is a member of the project
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a member of this project")
			return
		}

		limitStr := r.URL.Query().Get("limit")
		offsetStr := r.URL.Query().Get("offset")
		limit := 50
		offset := 0
		if limitStr != "" {
			if v, err := strconv.Atoi(limitStr); err == nil && v > 0 && v <= 200 {
				limit = v
			}
		}
		if offsetStr != "" {
			if v, err := strconv.Atoi(offsetStr); err == nil && v >= 0 {
				offset = v
			}
		}

		// Optional agent_id filter
		agentID := r.URL.Query().Get("agent_id")

		runs, err := deps.RuntimeService.ListActiveRuns(r.Context(), projectID, limit, offset)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", "Failed to list active runs")
			return
		}

		// Apply agent_id filter if specified
		if agentID != "" {
			filtered := make([]ActiveRunResponse, 0, len(runs))
			for _, run := range runs {
				if run.AgentID == agentID {
					filtered = append(filtered, run)
				}
			}
			runs = filtered
		}

		writeJSON(w, http.StatusOK, runs)
	}
}

// handleAgentRecentErrors handles GET /api/v1/agents/{agentID}/recent-errors
func handleAgentRecentErrors(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		agentID := chi.URLParam(r, "agentID")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "missing_agent_id", "Agent ID is required")
			return
		}

		// Get agent to verify project membership and resolve agent name
		agent, err := deps.AgentService.GetAgentByID(r.Context(), agentID)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "Agent not found")
			return
		}

		// Verify user is a member of the agent's project
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, agent.ProjectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a member of this project")
			return
		}

		runs, err := deps.RuntimeService.ListRecentRunsByAgent(r.Context(), agent.ProjectID, agent.Name, 20)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "recent_errors_failed", "Failed to get recent errors")
			return
		}

		writeJSON(w, http.StatusOK, runs)
	}
}

// handleGetRuntimeRunDetail handles GET /api/projects/{project_id}/runs/{run_id}/runtime
func handleGetRuntimeRunDetail(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		projectID := chi.URLParam(r, "projectID")
		if projectID == "" {
			writeError(w, http.StatusBadRequest, "missing_project_id", "Project ID is required")
			return
		}

		runID := chi.URLParam(r, "runID")
		if runID == "" {
			writeError(w, http.StatusBadRequest, "missing_run_id", "Run ID is required")
			return
		}

		// Verify user is a member of the project
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a member of this project")
			return
		}

		detail, err := deps.RuntimeService.GetRuntimeRunDetail(r.Context(), projectID, runID)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "Run not found")
			return
		}

		writeJSON(w, http.StatusOK, detail)
	}
}
