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
}

type batchCreateEvalScoresRequest struct {
	Scores []createEvalScoreRequest `json:"scores"`
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
		if !decodeJSONRequest(w, r, &req) {
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

		domainScore := evalScoreRequestToDomain(projectID, req)
		if err := evals.ValidateEvalScore(domainScore); err != nil {
			writeError(
				w,
				http.StatusBadRequest,
				"invalid_score",
				"Score fields, metric kind, and score must be valid",
			)
			return
		}

		result, err := deps.EvalsService.CreateEvalScore(r.Context(), domainScore)
		if err != nil {
			writeInternalError(w, "create_failed", "Failed to create eval score", "create eval score", err)
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

		var req batchCreateEvalScoresRequest
		if !decodeJSONRequest(w, r, &req) {
			return
		}

		if len(req.Scores) == 0 {
			writeError(w, http.StatusBadRequest, "no_scores", "At least one score is required")
			return
		}
		if len(req.Scores) > evals.MaxEvalScoreBatchSize {
			writeError(w, http.StatusBadRequest, "too_many_scores", "A maximum of 500 scores is allowed")
			return
		}

		scores := make([]evals.EvalScore, 0, len(req.Scores))
		for _, s := range req.Scores {
			domainScore := evalScoreRequestToDomain(projectID, s)
			if err := evals.ValidateEvalScore(domainScore); err != nil {
				writeError(
					w,
					http.StatusBadRequest,
					"invalid_score",
					"Each score must have valid required fields, metric kind, and score",
				)
				return
			}
			scores = append(scores, domainScore)
		}

		results, err := deps.EvalsService.CreateEvalScores(r.Context(), scores)
		if err != nil {
			writeInternalError(w, "create_failed", "Failed to create eval scores", "batch create eval scores", err)
			return
		}

		writeJSON(w, http.StatusCreated, results)
	}
}

func evalScoreRequestToDomain(projectID string, req createEvalScoreRequest) evals.EvalScore {
	details := ""
	if req.Details != nil {
		details = string(req.Details)
	}
	return evals.EvalScore{
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
	Name          string  `json:"name"`
	Kind          string  `json:"kind"`
	Config        string  `json:"config,omitempty"`
	Weight        float64 `json:"weight,omitempty"`
	PassThreshold float64 `json:"pass_threshold,omitempty"`
	Enabled       *bool   `json:"enabled,omitempty"`
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
		if !decodeJSONRequest(w, r, &req) {
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
			writeInternalError(w, "create_failed", "Failed to create metric definition", "create metric definition", err)
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
		if !decodeJSONRequest(w, r, &req) {
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
			writeInternalError(w, "update_failed", "Failed to update metric definition", "update metric definition", err)
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
			writeInternalError(w, "delete_failed", "Failed to delete metric definition", "delete metric definition", err)
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
