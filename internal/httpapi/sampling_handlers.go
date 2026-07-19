package httpapi

import (
	"encoding/json"
	"net/http"

	"AgentScope/internal/sampling"
	"github.com/go-chi/chi/v5"
)

// ---- Request types ----

type createSamplingRuleRequest struct {
	Name       string  `json:"name"`
	AgentID    string  `json:"agent_id,omitempty"`
	SampleRate float32 `json:"sample_rate"`
	Filters    string  `json:"filters,omitempty"`
}

type updateSampleRuleRequest struct {
	Name       *string  `json:"name,omitempty"`
	Enabled    *bool    `json:"enabled,omitempty"`
	SampleRate *float32 `json:"sample_rate,omitempty"`
	Filters    *string  `json:"filters,omitempty"`
}

// ---- Handlers ----

// handleCreateSamplingRule handles POST /api/projects/{projectID}/sampling-rules
func handleCreateSamplingRule(deps Dependencies) http.HandlerFunc {
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
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		var req createSamplingRuleRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "missing_name", "Rule name is required")
			return
		}

		rule, err := deps.SamplingService.CreateRule(r.Context(), sampling.CreateRuleParams{
			ProjectID:  projectID,
			AgentID:    req.AgentID,
			Name:       req.Name,
			SampleRate: req.SampleRate,
			Filters:    req.Filters,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "create_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, samplingRuleToResponse(rule))
	}
}

// handleListSamplingRules handles GET /api/projects/{projectID}/sampling-rules
func handleListSamplingRules(deps Dependencies) http.HandlerFunc {
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
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		rules, err := deps.SamplingService.ListRules(r.Context(), projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", "Failed to list rules")
			return
		}

		results := make([]SamplingRule, 0, len(rules))
		for _, rule := range rules {
			results = append(results, samplingRuleToResponse(rule))
		}
		writeJSON(w, http.StatusOK, results)
	}
}

// handleGetSamplingRule handles GET /api/sampling-rules/{ruleID}
func handleGetSamplingRule(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		ruleID := chi.URLParam(r, "ruleID")
		if ruleID == "" {
			writeError(w, http.StatusBadRequest, "missing_rule_id", "Rule ID is required")
			return
		}

		rule, err := deps.SamplingService.GetRule(r.Context(), ruleID)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "Rule not found")
			return
		}

		// Check project membership
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, rule.ProjectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		writeJSON(w, http.StatusOK, samplingRuleToResponse(rule))
	}
}

// handleUpdateSamplingRule handles PATCH /api/sampling-rules/{ruleID}
func handleUpdateSamplingRule(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		ruleID := chi.URLParam(r, "ruleID")
		if ruleID == "" {
			writeError(w, http.StatusBadRequest, "missing_rule_id", "Rule ID is required")
			return
		}

		// Get existing rule to check project membership
		existing, err := deps.SamplingService.GetRule(r.Context(), ruleID)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "Rule not found")
			return
		}

		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, existing.ProjectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		// For now, only support enabled toggle
		var req updateSampleRuleRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}

		if req.Enabled != nil {
			rule, err := deps.SamplingService.UpdateEnabled(r.Context(), ruleID, *req.Enabled)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "update_failed", "Failed to update rule")
				return
			}
			writeJSON(w, http.StatusOK, samplingRuleToResponse(rule))
			return
		}

		writeJSON(w, http.StatusOK, samplingRuleToResponse(existing))
	}
}

// handleDeleteSamplingRule handles DELETE /api/sampling-rules/{ruleID}
func handleDeleteSamplingRule(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		ruleID := chi.URLParam(r, "ruleID")
		if ruleID == "" {
			writeError(w, http.StatusBadRequest, "missing_rule_id", "Rule ID is required")
			return
		}

		// Get existing rule to check project membership
		existing, err := deps.SamplingService.GetRule(r.Context(), ruleID)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "Rule not found")
			return
		}

		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, existing.ProjectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		if err := deps.SamplingService.DeleteRule(r.Context(), ruleID); err != nil {
			writeError(w, http.StatusInternalServerError, "delete_failed", "Failed to delete rule")
			return
		}

		writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
	}
}

// ---- Response conversion ----

func samplingRuleToResponse(rule sampling.SamplingRule) SamplingRule {
	return SamplingRule{
		ID:         rule.ID,
		ProjectID:  rule.ProjectID,
		AgentID:    rule.AgentID,
		Name:       rule.Name,
		Enabled:    rule.Enabled,
		SampleRate: rule.SampleRate,
		Filters:    rule.Filters,
	}
}

// ---- Ensure SamplingService interface compliance ----

var _ SamplingService = (*sampling.Service)(nil)

// samplingRuleToJSON is a helper for tests
func samplingRuleToJSON(rule sampling.SamplingRule) string {
	b, _ := json.Marshal(samplingRuleToResponse(rule))
	return string(b)
}
