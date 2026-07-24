package httpapi

import (
	"encoding/json"
	"net/http"

	"AgentScope/internal/evals"
	"github.com/go-chi/chi/v5"
)

type createRubricTemplateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type createRubricVersionRequest struct {
	Rubric json.RawMessage `json:"rubric"`
}

type submitJudgeVotesRequest struct {
	MetricName      string                      `json:"metric_name"`
	MetricKind      string                      `json:"metric_kind"`
	EvaluatorID     string                      `json:"evaluator_id,omitempty"`
	RubricVersionID string                      `json:"rubric_version_id,omitempty"`
	Aggregation     evals.AggregationPolicy     `json:"aggregation"`
	Votes           []evals.JudgeVoteSubmission `json:"votes"`
}

type runCalibrationRequest struct {
	RubricVersionID string   `json:"rubric_version_id"`
	SampleRunIDs    []string `json:"sample_run_ids"`
	PassThreshold   float64  `json:"pass_threshold"`
}

func handleCreateRubricTemplate(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, projectID, ok := authorizeProjectRequest(w, r, deps)
		_ = userID
		if !ok {
			return
		}
		var req createRubricTemplateRequest
		if decodeJSON(r, &req) != nil || req.Name == "" {
			writeError(w, http.StatusBadRequest, "invalid_request", "Name is required")
			return
		}
		template, err := deps.EvalGovernance.CreateRubricTemplate(r.Context(), projectID, req.Name, req.Description)
		if err != nil {
			writeError(w, http.StatusBadRequest, "create_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, template)
	}
}

func handleListRubricTemplates(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, projectID, ok := authorizeProjectRequest(w, r, deps)
		if !ok {
			return
		}
		templates, err := deps.EvalGovernance.ListRubricTemplates(r.Context(), projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", "Failed to list rubric templates")
			return
		}
		writeJSON(w, http.StatusOK, templates)
	}
}

func handleCreateRubricVersion(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, projectID, ok := authorizeProjectRequest(w, r, deps)
		if !ok {
			return
		}
		var req createRubricVersionRequest
		if decodeJSON(r, &req) != nil || len(req.Rubric) == 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "Rubric is required")
			return
		}
		version, err := deps.EvalGovernance.CreateRubricVersion(
			r.Context(), projectID, chi.URLParam(r, "templateID"), string(req.Rubric), userID,
		)
		if err != nil {
			writeError(w, http.StatusBadRequest, "create_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, version)
	}
}

func handleListRubricVersions(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, projectID, ok := authorizeProjectRequest(w, r, deps)
		if !ok {
			return
		}
		versions, err := deps.EvalGovernance.ListRubricVersions(
			r.Context(), projectID, chi.URLParam(r, "templateID"),
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", "Failed to list rubric versions")
			return
		}
		writeJSON(w, http.StatusOK, versions)
	}
}

func handlePublishRubricVersion(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, projectID, ok := authorizeProjectRequest(w, r, deps)
		if !ok {
			return
		}
		version, err := deps.EvalGovernance.PublishRubricVersion(
			r.Context(), projectID, chi.URLParam(r, "versionID"),
		)
		if err != nil {
			writeError(w, http.StatusBadRequest, "publish_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, version)
	}
}

func handleListJudgeEvaluation(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, projectID, ok := authorizeProjectRequest(w, r, deps)
		if !ok {
			return
		}
		result, err := deps.EvalGovernance.ListJudgeEvaluation(
			r.Context(), projectID, chi.URLParam(r, "runID"),
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", "Failed to list Judge evaluation")
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

func handleRunnerSubmitJudgeVotes(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := authenticateRunner(w, r, deps)
		if !ok {
			return
		}
		var req submitJudgeVotesRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}
		result, err := deps.EvalGovernance.SubmitJudgeVotes(r.Context(), evals.JudgeVoteBatch{
			ProjectID: projectID, RunID: chi.URLParam(r, "runID"),
			MetricName: req.MetricName, MetricKind: req.MetricKind,
			EvaluatorID: req.EvaluatorID, RubricVersionID: req.RubricVersionID,
			Policy: req.Aggregation, Votes: req.Votes,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "submit_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
}

func handleRunCalibration(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID, projectID, ok := authorizeProjectRequest(w, r, deps)
		if !ok {
			return
		}
		var req runCalibrationRequest
		if decodeJSON(r, &req) != nil || req.RubricVersionID == "" || len(req.SampleRunIDs) == 0 {
			writeError(w, http.StatusBadRequest, "invalid_request", "Rubric version and sample runs are required")
			return
		}
		if req.PassThreshold == 0 {
			req.PassThreshold = 0.5
		}
		result, err := deps.EvalGovernance.RunCalibration(
			r.Context(), projectID, req.RubricVersionID, req.SampleRunIDs, userID, req.PassThreshold,
		)
		if err != nil {
			writeError(w, http.StatusBadRequest, "calibration_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, result)
	}
}

func handleListCalibrations(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, projectID, ok := authorizeProjectRequest(w, r, deps)
		if !ok {
			return
		}
		results, err := deps.EvalGovernance.ListCalibrations(r.Context(), projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", "Failed to list calibrations")
			return
		}
		writeJSON(w, http.StatusOK, results)
	}
}

func authorizeProjectRequest(w http.ResponseWriter, r *http.Request, deps Dependencies) (string, string, bool) {
	userID := userIDFromCtx(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
		return "", "", false
	}
	projectID := chi.URLParam(r, "projectID")
	if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
		return "", "", false
	}
	return userID, projectID, true
}
