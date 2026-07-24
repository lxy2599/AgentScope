package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ---- Dataset handlers ----

type createDatasetRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// handleCreateDataset handles POST /api/projects/{projectID}/datasets
func handleCreateDataset(deps Dependencies) http.HandlerFunc {
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

		var req createDatasetRequest
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "missing_name", "Dataset name is required")
			return
		}

		dataset, err := deps.DatasetsService.CreateDataset(r.Context(), projectID, req.Name, req.Description)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "create_failed", "Failed to create dataset")
			return
		}

		writeJSON(w, http.StatusCreated, dataset)
	}
}

// handleAddCase handles POST /api/datasets/{datasetID}/cases
func handleAddCase(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		datasetID := chi.URLParam(r, "datasetID")

		dataset, err := deps.DatasetsService.GetDataset(r.Context(), datasetID)
		if err != nil {
			writeError(w, http.StatusNotFound, "dataset_not_found", "Dataset not found")
			return
		}
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, dataset.ProjectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		var req struct {
			Input    json.RawMessage `json:"input"`
			Expected json.RawMessage `json:"expected,omitempty"`
			Metadata json.RawMessage `json:"metadata,omitempty"`
		}
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		if len(req.Input) == 0 {
			writeError(w, http.StatusBadRequest, "missing_input", "Input is required")
			return
		}

		datasetCase, err := deps.DatasetsService.AddCase(r.Context(), datasetID, req.Input, req.Expected, req.Metadata)
		if err != nil {
			writeInternalError(w, "add_case_failed", "Failed to add dataset case", "add dataset case", err)
			return
		}

		writeJSON(w, http.StatusCreated, datasetCase)
	}
}

// handleListCases handles GET /api/datasets/{datasetID}/cases
func handleListCases(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		datasetID := chi.URLParam(r, "datasetID")

		dataset, err := deps.DatasetsService.GetDataset(r.Context(), datasetID)
		if err != nil {
			writeError(w, http.StatusNotFound, "dataset_not_found", "Dataset not found")
			return
		}
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, dataset.ProjectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		cases, err := deps.DatasetsService.ListCases(r.Context(), datasetID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", "Failed to list cases")
			return
		}

		writeJSON(w, http.StatusOK, cases)
	}
}

// handleListDatasets handles GET /api/projects/{projectID}/datasets
func handleListDatasets(deps Dependencies) http.HandlerFunc {
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

		datasets, err := deps.DatasetsService.ListDatasets(r.Context(), projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", "Failed to list datasets")
			return
		}

		writeJSON(w, http.StatusOK, datasets)
	}
}
