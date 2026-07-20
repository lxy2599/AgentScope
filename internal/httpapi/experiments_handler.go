package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ---- Experiment handlers ----

type createExperimentRequest struct {
	DatasetID       string          `json:"dataset_id"`
	EvaluatorID     string          `json:"evaluator_id,omitempty"`
	EvaluatorConfig json.RawMessage `json:"evaluator_config,omitempty"`
	TargetName      string          `json:"target_name"`
	TargetVersion   string          `json:"target_version,omitempty"`
	CaseIDs         []string        `json:"case_ids"`
}

// handleCreateExperiment handles POST /api/projects/{projectID}/experiments
func handleCreateExperiment(deps Dependencies) http.HandlerFunc {
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

		var req createExperimentRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}
		if req.DatasetID == "" {
			writeError(w, http.StatusBadRequest, "missing_dataset_id", "Dataset ID is required")
			return
		}
		if req.TargetName == "" {
			writeError(w, http.StatusBadRequest, "missing_target_name", "Target name is required")
			return
		}
		if len(req.CaseIDs) == 0 {
			writeError(w, http.StatusBadRequest, "no_cases", "At least one case ID is required")
			return
		}

		// Verify dataset belongs to the project
		dataset, err := deps.DatasetsService.GetDataset(r.Context(), req.DatasetID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "dataset_not_found", "Dataset not found")
			return
		}
		if dataset.ProjectID != projectID {
			writeError(w, http.StatusForbidden, "forbidden", "Dataset does not belong to this project")
			return
		}

		// If evaluator_config is provided without evaluator_id, create an ad-hoc evaluator
		evaluatorID := req.EvaluatorID
		if evaluatorID == "" && len(req.EvaluatorConfig) > 0 {
			var cfgMap map[string]interface{}
			if err := json.Unmarshal(req.EvaluatorConfig, &cfgMap); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_evaluator_config", "Invalid evaluator config JSON")
				return
			}
			evalName := "experiment-evaluator"
			if name, ok := cfgMap["name"].(string); ok && name != "" {
				evalName = name
			}
			evalType := "rule"
			if t, ok := cfgMap["type"].(string); ok && t != "" {
				evalType = t
			}

			evalConfigBytes, _ := json.Marshal(cfgMap)
			evaluator, err := deps.EvaluatorCreator.Create(r.Context(), projectID, evalName, evalType, string(evalConfigBytes))
			if err != nil {
				writeError(w, http.StatusBadRequest, "create_evaluator_failed", err.Error())
				return
			}
			if id, ok := evaluator["id"].(string); ok {
				evaluatorID = id
			}
		}

		experiment, err := deps.ExperimentsService.CreateExperiment(
			r.Context(), projectID, req.DatasetID, evaluatorID,
			req.TargetName, req.TargetVersion, req.CaseIDs,
		)
		if err != nil {
			writeError(w, http.StatusBadRequest, "create_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, experiment)
	}
}

// handleGetExperiment handles GET /api/experiments/{experimentID}
func handleGetExperiment(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		experimentID := chi.URLParam(r, "experimentID")

		detail, err := deps.ExperimentsService.GetExperiment(r.Context(), experimentID)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "Experiment not found")
			return
		}

		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, detail.ProjectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		writeJSON(w, http.StatusOK, detail)
	}
}

// handleListExperiments handles GET /api/projects/{projectID}/experiments
func handleListExperiments(deps Dependencies) http.HandlerFunc {
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

		experiments, err := deps.ExperimentsService.ListExperiments(r.Context(), projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", "Failed to list experiments")
			return
		}

		writeJSON(w, http.StatusOK, experiments)
	}
}

// handleListExperimentResults handles GET /api/projects/{projectID}/experiments/{experimentID}/results
func handleListExperimentResults(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		projectID := chi.URLParam(r, "projectID")
		experimentID := chi.URLParam(r, "experimentID")

		if projectID == "" || experimentID == "" {
			writeError(w, http.StatusBadRequest, "missing_ids", "Project and experiment IDs are required")
			return
		}

		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		// Verify experiment belongs to the project
		exp, err := deps.ExperimentsService.GetExperiment(r.Context(), experimentID)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "Experiment not found")
			return
		}
		if exp.ProjectID != projectID {
			writeError(w, http.StatusNotFound, "not_found", "Experiment not found in this project")
			return
		}

		results, err := deps.ExperimentsService.ListExperimentResults(r.Context(), experimentID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", "Failed to list experiment results")
			return
		}

		writeJSON(w, http.StatusOK, results)
	}
}

// handleExperimentReport handles GET /api/projects/{projectID}/experiments/{experimentID}/report
// Returns a detailed experiment report including quality stats, dimension aggregation, and per-case details.
func handleExperimentReport(deps Dependencies) http.HandlerFunc {
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

		// Get experiment detail
		exp, err := deps.ExperimentsService.GetExperiment(r.Context(), experimentID)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "Experiment not found")
			return
		}
		if exp.ProjectID != projectID {
			writeError(w, http.StatusNotFound, "not_found", "Experiment not found in this project")
			return
		}

		// Get dimension aggregation
		dimAgg, err := deps.EvalsService.AggregateEvalScoresByExperiment(r.Context(), projectID, experimentID)
		if err != nil {
			dimAgg = nil
		}

		// Get results
		results, err := deps.ExperimentsService.ListExperimentResults(r.Context(), experimentID)
		if err != nil {
			results = nil
		}

		// Build quality stats
		var qualityPassCount, qualityFailCount int
		var totalScore float64
		var scoreCount int
		for _, r := range results {
			if r.Passed {
				qualityPassCount++
			} else {
				qualityFailCount++
			}
			totalScore += r.Score
			scoreCount++
		}
		avgScore := 0.0
		if scoreCount > 0 {
			avgScore = totalScore / float64(scoreCount)
		}

		// Read pass_threshold from evaluator config (if experiment has an evaluator)
		passThreshold := 0.7 // default fallback
		if exp.EvaluatorID != "" {
			evaluator, err := deps.EvaluatorCreator.Get(r.Context(), exp.EvaluatorID)
			if err == nil {
				if cfgStr, ok := evaluator["config"].(string); ok {
					var cfgMap map[string]interface{}
					if err := json.Unmarshal([]byte(cfgStr), &cfgMap); err == nil {
						if pt, ok := cfgMap["pass_threshold"].(float64); ok && pt >= 0 && pt <= 1 {
							passThreshold = pt
						}
					}
				}
			}
		}

		type reportResult struct {
			ID            string  `json:"id"`
			DatasetCaseID string  `json:"dataset_case_id"`
			RunID         string  `json:"run_id,omitempty"`
			Score         float64 `json:"score"`
			Passed        bool    `json:"passed"`
			Reason        string  `json:"reason"`
			ActualOutput  string  `json:"actual_output,omitempty"`
			LatencyMs     int64   `json:"latency_ms,omitempty"`
		}

		reportResults := make([]reportResult, 0, len(results))
		for _, r := range results {
			reportResults = append(reportResults, reportResult{
				ID:            r.ID,
				DatasetCaseID: r.DatasetCaseID,
				RunID:         r.RunID,
				Score:         r.Score,
				Passed:        r.Passed,
				Reason:        r.Reason,
				ActualOutput:  r.ActualOutput,
				LatencyMs:     r.LatencyMs,
			})
		}

		report := map[string]interface{}{
			"experiment": exp,
			"status":     exp.Status,
			"total_cases": exp.TotalCases,
			"execution": map[string]int{
				"succeeded": exp.StatusCounts["succeeded"],
				"failed":    exp.StatusCounts["failed"],
				"running":   exp.StatusCounts["running"],
				"queued":    exp.StatusCounts["queued"],
			},
			"quality": map[string]interface{}{
				"avg_score":       avgScore,
				"pass_threshold":  passThreshold,
				"pass_count":      qualityPassCount,
				"fail_count":      qualityFailCount,
				"total_evaluated": scoreCount,
			},
			"dimension_aggregation": dimAgg,
			"results":               reportResults,
		}

		writeJSON(w, http.StatusOK, report)
	}
}
