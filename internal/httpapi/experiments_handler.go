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
