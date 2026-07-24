package httpapi

import (
	"net/http"

	"AgentScope/internal/quotas"
	"github.com/go-chi/chi/v5"
)

func handleGetProjectQuota(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		projectID := chi.URLParam(r, "projectID")
		if deps.ProjectAuth == nil ||
			deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID) != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a member of this project")
			return
		}
		if deps.QuotaService == nil {
			writeError(w, http.StatusServiceUnavailable, "quota_unavailable", "Project quotas are unavailable")
			return
		}
		status, err := deps.QuotaService.Status(r.Context(), projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "quota_query_failed", "Failed to query project quotas")
			return
		}
		status.CanEdit = deps.ProjectAdminAuth != nil &&
			deps.ProjectAdminAuth.IsProjectAdmin(r.Context(), userID, projectID) == nil
		writeJSON(w, http.StatusOK, status)
	}
}

func handleUpdateProjectQuota(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		projectID := chi.URLParam(r, "projectID")
		if deps.ProjectAdminAuth == nil ||
			deps.ProjectAdminAuth.IsProjectAdmin(r.Context(), userID, projectID) != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Project administrator role is required")
			return
		}
		if deps.QuotaService == nil {
			writeError(w, http.StatusServiceUnavailable, "quota_unavailable", "Project quotas are unavailable")
			return
		}
		var request quotas.Quota
		if err := decodeJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}
		request.ProjectID = projectID
		updated, err := deps.QuotaService.UpdateQuota(r.Context(), projectID, userID, request)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_quota", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, updated)
	}
}

func writeQuotaHeaders(w http.ResponseWriter, decision quotas.Decision) {
	w.Header().Set("X-RateLimit-Limit", formatInt64(decision.Limit))
	w.Header().Set("X-RateLimit-Remaining", formatInt64(decision.Remaining))
	if decision.RetryAfter > 0 {
		seconds := int64(decision.RetryAfter.Seconds())
		if decision.RetryAfter%1e9 != 0 {
			seconds++
		}
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", formatInt64(seconds))
	}
	if !decision.ResetAt.IsZero() {
		w.Header().Set("X-RateLimit-Reset", formatInt64(decision.ResetAt.Unix()))
	}
	if decision.Degraded {
		w.Header().Set("X-AgentScope-Quota-Degraded", "true")
	}
}

func formatInt64(value int64) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		index--
		digits[index] = '-'
	}
	return string(digits[index:])
}
