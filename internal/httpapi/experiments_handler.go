package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ---- Experiment handlers ----

type createExperimentRequest struct {
	DatasetID     string   `json:"dataset_id"`
	EvaluatorID   string   `json:"evaluator_id,omitempty"`
	TargetName    string   `json:"target_name"`
	TargetVersion string   `json:"target_version,omitempty"`
	CaseIDs       []string `json:"case_ids"`
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

		experiment, err := deps.ExperimentsService.CreateExperiment(
			r.Context(), projectID, req.DatasetID, req.EvaluatorID,
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

		writeJSON(w, http.StatusOK, detail)
	}
}
