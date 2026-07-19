package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// handleProjectIngestStats handles GET /api/projects/{projectID}/ingest-stats
// Query params: minutes=15 (default), 5, or 60
func handleProjectIngestStats(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")
		if projectID == "" {
			writeError(w, http.StatusBadRequest, "missing_project", "Project ID is required")
			return
		}

		minutes := parseMinutesParam(r, 15)

		// If an IngestStatsQuerier is wired, use it; otherwise return placeholder.
		if deps.IngestStats != nil {
			result := deps.IngestStats.QueryProjectStats(r.Context(), projectID, minutes)
			writeJSON(w, http.StatusOK, result)
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"project_id":   projectID,
			"minutes":      minutes,
			"collected_at": time.Now().UTC(),
			"message":      "Ingest stats query requires ClickHouse tracestore wiring.",
		})
	}
}

// handleProjectIngestAgentStats handles GET /api/projects/{projectID}/ingest-stats/agents
func handleProjectIngestAgentStats(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")
		if projectID == "" {
			writeError(w, http.StatusBadRequest, "missing_project", "Project ID is required")
			return
		}

		minutes := parseMinutesParam(r, 15)

		if deps.IngestStats != nil {
			result := deps.IngestStats.QueryAgentStats(r.Context(), projectID, minutes)
			writeJSON(w, http.StatusOK, result)
			return
		}

		writeJSON(w, http.StatusOK, map[string]interface{}{
			"project_id": projectID,
			"minutes":    minutes,
			"message":    "Agent stats query requires ClickHouse tracestore wiring.",
		})
	}
}

// IngestStatsQuerier is the interface for ingest statistics queries.
// Implementations are wired through the adapter in internal/app.
type IngestStatsQuerier interface {
	QueryProjectStats(ctx interface{}, projectID string, minutes int) interface{}
	QueryAgentStats(ctx interface{}, projectID string, minutes int) interface{}
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
