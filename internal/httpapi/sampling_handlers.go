package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"AgentScope/internal/sampling"
	"github.com/go-chi/chi/v5"
)

// ---- Request types ----

type createSamplingRuleRequest struct {
	Name           string  `json:"name"`
	AgentID        string  `json:"agent_id,omitempty"`
	SampleRate     float32 `json:"sample_rate"`
	Filters        string  `json:"filters,omitempty"`
	Strategy       string  `json:"strategy,omitempty"`
	DailyBudget    int     `json:"daily_budget,omitempty"`
	Stratification string  `json:"stratification,omitempty"`

	sampleRateNull bool
	filtersNull    bool
}

type updateSampleRuleRequest struct {
	Name       *string  `json:"name,omitempty"`
	Enabled    *bool    `json:"enabled,omitempty"`
	SampleRate *float32 `json:"sample_rate,omitempty"`
	Filters    *string  `json:"filters,omitempty"`

	nameNull       bool
	enabledNull    bool
	sampleRateNull bool
	filtersNull    bool
}

func (req *createSamplingRuleRequest) UnmarshalJSON(data []byte) error {
	type requestAlias createSamplingRuleRequest
	var decoded requestAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*req = createSamplingRuleRequest(decoded)
	req.sampleRateNull = rawJSONFieldIsNull(fields, "sample_rate")
	req.filtersNull = rawJSONFieldIsNull(fields, "filters")
	return nil
}

func (req *updateSampleRuleRequest) UnmarshalJSON(data []byte) error {
	type requestAlias updateSampleRuleRequest
	var decoded requestAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*req = updateSampleRuleRequest(decoded)
	req.nameNull = rawJSONFieldIsNull(fields, "name")
	req.enabledNull = rawJSONFieldIsNull(fields, "enabled")
	req.sampleRateNull = rawJSONFieldIsNull(fields, "sample_rate")
	req.filtersNull = rawJSONFieldIsNull(fields, "filters")
	return nil
}

func rawJSONFieldIsNull(fields map[string]json.RawMessage, name string) bool {
	raw, ok := fields[name]
	return ok && bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func samplingValidationMessage(err error) (string, bool) {
	switch {
	case errors.Is(err, sampling.ErrInvalidSampleRate):
		return sampling.ErrInvalidSampleRate.Error(), true
	case errors.Is(err, sampling.ErrInvalidFilters):
		return sampling.ErrInvalidFilters.Error(), true
	case errors.Is(err, sampling.ErrInvalidEnabled):
		return sampling.ErrInvalidEnabled.Error(), true
	case errors.Is(err, sampling.ErrNameRequired):
		return sampling.ErrNameRequired.Error(), true
	case errors.Is(err, sampling.ErrNoUpdateFields):
		return sampling.ErrNoUpdateFields.Error(), true
	default:
		return "", false
	}
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
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "missing_name", "Rule name is required")
			return
		}
		if req.sampleRateNull {
			writeError(w, http.StatusBadRequest, "invalid_sampling_rule", sampling.ErrInvalidSampleRate.Error())
			return
		}
		if req.filtersNull {
			writeError(w, http.StatusBadRequest, "invalid_sampling_rule", sampling.ErrInvalidFilters.Error())
			return
		}

		params, err := sampling.ValidateCreateRuleParams(sampling.CreateRuleParams{
			ProjectID:      projectID,
			AgentID:        req.AgentID,
			Name:           req.Name,
			SampleRate:     req.SampleRate,
			Filters:        req.Filters,
			Strategy:       req.Strategy,
			DailyBudget:    req.DailyBudget,
			Stratification: req.Stratification,
		})
		if err != nil {
			if message, ok := samplingValidationMessage(err); ok {
				writeError(w, http.StatusBadRequest, "invalid_sampling_rule", message)
				return
			}
			writeInternalError(w, "create_failed", "Failed to create sampling rule", "validate sampling rule creation", err)
			return
		}

		rule, err := deps.SamplingService.CreateRule(r.Context(), params)
		if err != nil {
			if message, ok := samplingValidationMessage(err); ok {
				writeError(w, http.StatusBadRequest, "invalid_sampling_rule", message)
				return
			}
			writeInternalError(w, "create_failed", "Failed to create sampling rule", "create sampling rule", err)
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

		var req updateSampleRuleRequest
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		if req.nameNull {
			writeError(w, http.StatusBadRequest, "invalid_sampling_rule", sampling.ErrNameRequired.Error())
			return
		}
		if req.enabledNull {
			writeError(w, http.StatusBadRequest, "invalid_sampling_rule", sampling.ErrInvalidEnabled.Error())
			return
		}
		if req.sampleRateNull {
			writeError(w, http.StatusBadRequest, "invalid_sampling_rule", sampling.ErrInvalidSampleRate.Error())
			return
		}
		if req.filtersNull {
			writeError(w, http.StatusBadRequest, "invalid_sampling_rule", sampling.ErrInvalidFilters.Error())
			return
		}

		params, err := sampling.ValidateUpdateRuleParams(sampling.UpdateRuleParams{
			Name:       req.Name,
			Enabled:    req.Enabled,
			SampleRate: req.SampleRate,
			Filters:    req.Filters,
		})
		if err != nil {
			if message, ok := samplingValidationMessage(err); ok {
				writeError(w, http.StatusBadRequest, "invalid_sampling_rule", message)
				return
			}
			writeInternalError(w, "update_failed", "Failed to update rule", "validate sampling rule update", err)
			return
		}

		rule, err := deps.SamplingService.UpdateRule(r.Context(), existing.ProjectID, ruleID, params)
		if err != nil {
			if message, ok := samplingValidationMessage(err); ok {
				writeError(w, http.StatusBadRequest, "invalid_sampling_rule", message)
				return
			}
			if errors.Is(err, sampling.ErrRuleNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "Rule not found")
				return
			}
			writeInternalError(w, "update_failed", "Failed to update rule", "update sampling rule", err)
			return
		}

		writeJSON(w, http.StatusOK, samplingRuleToResponse(rule))
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
		ID:             rule.ID,
		ProjectID:      rule.ProjectID,
		AgentID:        rule.AgentID,
		Name:           rule.Name,
		Enabled:        rule.Enabled,
		SampleRate:     rule.SampleRate,
		Filters:        rule.Filters,
		Strategy:       rule.Strategy,
		DailyBudget:    rule.DailyBudget,
		Stratification: rule.Stratification,
	}
}

// ---- Ensure SamplingService interface compliance ----

var _ SamplingService = (*sampling.Service)(nil)
