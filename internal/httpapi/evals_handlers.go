package httpapi

import (
	"encoding/json"
	"net/http"

	"AgentScope/internal/evals"
	"github.com/go-chi/chi/v5"
)

// ---- Eval scores handlers ----

type createEvalScoreRequest struct {
	RunID          string          `json:"run_id"`
	TraceID        string          `json:"trace_id,omitempty"`
	SpanID         string          `json:"span_id,omitempty"`
	MetricName     string          `json:"metric_name"`
	MetricKind     string          `json:"metric_kind"`
	Score          float64         `json:"score"`
	Passed         bool            `json:"passed"`
	Reason         string          `json:"reason,omitempty"`
	Details        json.RawMessage `json:"details,omitempty"`
	EvaluatorRunID string          `json:"evaluator_run_id,omitempty"`
	Scores         []createEvalScoreRequest `json:"scores,omitempty"`
}

// handleCreateEvalScore handles POST /api/projects/{projectID}/eval-scores
func handleCreateEvalScore(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		projectID := chi.URLParam(r, "projectID")
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		var req createEvalScoreRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}

		if req.RunID == "" {
			writeError(w, http.StatusBadRequest, "missing_run_id", "Run ID is required")
			return
		}
		if req.MetricName == "" {
			writeError(w, http.StatusBadRequest, "missing_metric_name", "Metric name is required")
			return
		}
		if req.MetricKind == "" {
			writeError(w, http.StatusBadRequest, "missing_metric_kind", "Metric kind is required")
			return
		}

		details := ""
		if req.Details != nil {
			details = string(req.Details)
		}

		domainScore := evals.EvalScore{
			ProjectID:      projectID,
			RunID:          req.RunID,
			TraceID:        req.TraceID,
			SpanID:         req.SpanID,
			MetricName:     req.MetricName,
			MetricKind:     req.MetricKind,
			Score:          req.Score,
			Passed:         req.Passed,
			Reason:         req.Reason,
			Details:        details,
			EvaluatorRunID: req.EvaluatorRunID,
		}

		result, err := deps.EvalsService.CreateEvalScore(r.Context(), domainScore)
		if err != nil {
			writeError(w, http.StatusBadRequest, "create_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, evalScoreDomainToResponse(result))
	}
}

// handleBatchCreateEvalScores handles POST /api/projects/{projectID}/eval-scores/batch
func handleBatchCreateEvalScores(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		projectID := chi.URLParam(r, "projectID")
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		var req createEvalScoreRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}

		if len(req.Scores) == 0 {
			writeError(w, http.StatusBadRequest, "no_scores", "At least one score is required in 'scores' array")
			return
		}

		var results []evals.EvalScore
		for _, s := range req.Scores {
			if s.RunID == "" || s.MetricName == "" || s.MetricKind == "" {
				writeError(w, http.StatusBadRequest, "invalid_score", "Each score must have run_id, metric_name, and metric_kind")
				return
			}
			details := ""
			if s.Details != nil {
				details = string(s.Details)
			}

			domainScore := evals.EvalScore{
				ProjectID:      projectID,
				RunID:          s.RunID,
				TraceID:        s.TraceID,
				SpanID:         s.SpanID,
				MetricName:     s.MetricName,
				MetricKind:     s.MetricKind,
				Score:          s.Score,
				Passed:         s.Passed,
				Reason:         s.Reason,
				Details:        details,
				EvaluatorRunID: s.EvaluatorRunID,
			}
			result, err := deps.EvalsService.CreateEvalScore(r.Context(), domainScore)
			if err != nil {
				writeError(w, http.StatusBadRequest, "create_failed", err.Error())
				return
			}
			results = append(results, result)
		}

		writeJSON(w, http.StatusCreated, results)
	}
}

// handleListEvalScoresByRun handles GET /api/projects/{projectID}/eval-scores/by-run/{runID}
func handleListEvalScoresByRun(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		projectID := chi.URLParam(r, "projectID")
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		runID := chi.URLParam(r, "runID")
		if runID == "" {
			writeError(w, http.StatusBadRequest, "missing_run_id", "Run ID is required")
			return
		}

		scores, err := deps.EvalsService.ListEvalScoresByRun(r.Context(), projectID, runID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", "Failed to list eval scores")
			return
		}

		writeJSON(w, http.StatusOK, scores)
	}
}

// ---- Metric definition handlers ----

type createMetricDefinitionRequest struct {
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	Config      string  `json:"config,omitempty"`
	Weight      float64 `json:"weight,omitempty"`
	PassThreshold float64 `json:"pass_threshold,omitempty"`
	Enabled     *bool   `json:"enabled,omitempty"`
}

// handleCreateMetricDefinition handles POST /api/projects/{projectID}/metric-definitions
func handleCreateMetricDefinition(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		projectID := chi.URLParam(r, "projectID")
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		var req createMetricDefinitionRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}

		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "missing_name", "Name is required")
			return
		}
		if req.Kind == "" {
			writeError(w, http.StatusBadRequest, "missing_kind", "Kind is required")
			return
		}
		if req.Weight < 0 {
			writeError(w, http.StatusBadRequest, "invalid_weight", "Weight must be non-negative")
			return
		}
		if req.PassThreshold < 0 || req.PassThreshold > 1 {
			writeError(w, http.StatusBadRequest, "invalid_pass_threshold", "Pass threshold must be between 0 and 1")
			return
		}

		// Merge weight/threshold/enabled into config JSON
		var cfgMap map[string]interface{}
		if req.Config != "" {
			if err := json.Unmarshal([]byte(req.Config), &cfgMap); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_config", "Invalid config JSON")
				return
			}
		} else {
			cfgMap = make(map[string]interface{})
		}
		if req.Weight > 0 {
			cfgMap["weight"] = req.Weight
		}
		if req.PassThreshold > 0 {
			cfgMap["pass_threshold"] = req.PassThreshold
		}
		if req.Enabled != nil {
			cfgMap["enabled"] = *req.Enabled
		}
		mergedConfig, _ := json.Marshal(cfgMap)

		def, err := deps.EvalsService.CreateMetricDefinition(r.Context(), projectID, req.Name, req.Kind, string(mergedConfig))
		if err != nil {
			writeError(w, http.StatusBadRequest, "create_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, def)
	}
}

// handleListMetricDefinitions handles GET /api/projects/{projectID}/metric-definitions
func handleListMetricDefinitions(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		projectID := chi.URLParam(r, "projectID")
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		defs, err := deps.EvalsService.ListMetricDefinitions(r.Context(), projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", "Failed to list metric definitions")
			return
		}

		writeJSON(w, http.StatusOK, defs)
	}
}

// handleUpdateMetricDefinition handles PUT /api/projects/{projectID}/metric-definitions/{definitionID}
func handleUpdateMetricDefinition(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		projectID := chi.URLParam(r, "projectID")
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		definitionID := chi.URLParam(r, "definitionID")

		var req createMetricDefinitionRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}

		// Build config with weight/threshold/enabled
		var cfgMap map[string]interface{}
		if req.Config != "" {
			if err := json.Unmarshal([]byte(req.Config), &cfgMap); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_config", "Invalid config JSON")
				return
			}
		} else {
			cfgMap = make(map[string]interface{})
		}
		if req.Weight > 0 {
			cfgMap["weight"] = req.Weight
		}
		if req.PassThreshold > 0 {
			cfgMap["pass_threshold"] = req.PassThreshold
		}
		if req.Enabled != nil {
			cfgMap["enabled"] = *req.Enabled
		}
		mergedConfig, _ := json.Marshal(cfgMap)

		def, err := deps.EvalsService.UpdateMetricDefinition(r.Context(), projectID, definitionID, req.Name, req.Kind, string(mergedConfig))
		if err != nil {
			writeError(w, http.StatusBadRequest, "update_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, def)
	}
}

// handleDeleteMetricDefinition handles DELETE /api/projects/{projectID}/metric-definitions/{definitionID}
func handleDeleteMetricDefinition(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		projectID := chi.URLParam(r, "projectID")
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		definitionID := chi.URLParam(r, "definitionID")

		if err := deps.EvalsService.DeleteMetricDefinition(r.Context(), projectID, definitionID); err != nil {
			writeError(w, http.StatusBadRequest, "delete_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// ---- Experiment eval scores handler ----

// handleExperimentEvalScores handles GET /api/projects/{projectID}/experiments/{experimentID}/eval-scores
func handleExperimentEvalScores(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		projectID := chi.URLParam(r, "projectID")
		experimentID := chi.URLParam(r, "experimentID")

		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		agg, err := deps.EvalsService.AggregateEvalScoresByExperiment(r.Context(), projectID, experimentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "aggregate_failed", "Failed to aggregate scores")
			return
		}

		writeJSON(w, http.StatusOK, agg)
	}
}

func evalScoreDomainToResponse(s evals.EvalScore) evals.EvalScore {
	return s
}
