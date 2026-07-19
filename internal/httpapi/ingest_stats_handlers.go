package httpapi

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// IngestStatsDTO is the response payload for project-level ingest stats.
type IngestStatsDTO struct {
	ProjectID   string    `json:"project_id"`
	Minutes     int       `json:"minutes"`
	CollectedAt time.Time `json:"collected_at"`
	Spans       uint64    `json:"spans"`
	Events      uint64    `json:"events"`
	Errors      uint64    `json:"errors"`
	LLMs        uint64    `json:"llms"`
	Tools       uint64    `json:"tools"`
	Retrievals  uint64    `json:"retrievals"`
	Skills      uint64    `json:"skills"`
	Tokens      uint64    `json:"tokens"`
	Cost        float64   `json:"cost"`
}

// AgentStatsDTO is one row of agent-level error statistics.
type AgentStatsDTO struct {
	AgentName  string  `json:"agent_name"`
	ErrorCount uint64  `json:"error_count"`
	RunCount   uint64  `json:"run_count"`
	P95Latency float64 `json:"p95_latency"`
}

// IngestStatsQuerier provides typed access to ClickHouse aggregation queries.
// Implementations are wired through the adapter in internal/app.
type IngestStatsQuerier interface {
	QueryProjectStats(ctx context.Context, projectID string, minutes int) (*IngestStatsDTO, error)
	QueryAgentStats(ctx context.Context, projectID string, minutes int) ([]AgentStatsDTO, error)
}

// handleProjectIngestStats handles GET /api/projects/{projectID}/ingest-stats
// Query params: minutes=15 (default), 5, or 60
func handleProjectIngestStats(deps Dependencies) http.HandlerFunc {
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

		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a member of this project")
			return
		}

		minutes := parseMinutesParam(r, 15)

		if deps.IngestStats == nil {
			writeJSON(w, http.StatusOK, map[string]interface{}{
				"project_id":   projectID,
				"minutes":      minutes,
				"collected_at": time.Now().UTC(),
				"message":      "Ingest stats query not wired. Configure ClickHouse tracestore.",
			})
			return
		}

		result, err := deps.IngestStats.QueryProjectStats(r.Context(), projectID, minutes)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query_failed", "Failed to query ingest stats")
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// handleProjectIngestAgentStats handles GET /api/projects/{projectID}/ingest-stats/agents
func handleProjectIngestAgentStats(deps Dependencies) http.HandlerFunc {
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

		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a member of this project")
			return
		}

		minutes := parseMinutesParam(r, 15)

		if deps.IngestStats == nil {
			writeJSON(w, http.StatusOK, []AgentStatsDTO{})
			return
		}

		results, err := deps.IngestStats.QueryAgentStats(r.Context(), projectID, minutes)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "query_failed", "Failed to query agent error stats")
			return
		}
		if results == nil {
			results = []AgentStatsDTO{}
		}
		writeJSON(w, http.StatusOK, results)
	}
}

// parseMinutesParam extracts the "minutes" query parameter with a default.
func parseMinutesParam(r *http.Request, defaultMinutes int) int {
	m := r.URL.Query().Get("minutes")
	if m == "" {
		return defaultMinutes
	}
	n, err := strconv.Atoi(m)
	if err != nil || n <= 0 || n > 1440 {
		return defaultMinutes
	}
	return n
}
