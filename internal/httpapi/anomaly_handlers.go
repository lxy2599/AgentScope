package httpapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

func handleListProjectAnomalies(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectID")
		userID := userIDFromCtx(r.Context())
		if deps.ProjectAuth == nil ||
			deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID) != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Project access denied")
			return
		}
		if deps.AnomalyService == nil {
			writeError(
				w,
				http.StatusServiceUnavailable,
				"anomaly_service_unavailable",
				"Operational anomaly data is unavailable",
			)
			return
		}
		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			value, err := strconv.Atoi(raw)
			if err != nil || value <= 0 || value > 200 {
				writeError(w, http.StatusBadRequest, "invalid_limit", "limit must be between 1 and 200")
				return
			}
			limit = value
		}
		records, err := deps.AnomalyService.ListProject(r.Context(), projectID, limit)
		if err != nil {
			writeError(
				w,
				http.StatusInternalServerError,
				"list_anomalies_failed",
				"Failed to list operational anomalies",
			)
			return
		}
		writeJSON(w, http.StatusOK, records)
	}
}
