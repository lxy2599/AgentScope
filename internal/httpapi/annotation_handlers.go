package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

type createAnnotationQueueRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type createAnnotationItemRequest struct {
	RunID    string `json:"run_id"`
	TraceID  string `json:"trace_id,omitempty"`
	Priority int    `json:"priority"`
	Scores   string `json:"scores,omitempty"`
}

type updateAnnotationItemRequest struct {
	Status           *string `json:"status,omitempty"`
	CorrectedOutput  *string `json:"corrected_output,omitempty"`
	ExpectedToolPath *string `json:"expected_tool_path,omitempty"`
	Comment          *string `json:"comment,omitempty"`
}

// handleCreateAnnotationQueue handles POST /api/projects/{projectID}/annotation-queues
func handleCreateAnnotationQueue(deps Dependencies) http.HandlerFunc {
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

		var req createAnnotationQueueRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "missing_name", "Queue name is required")
			return
		}

		queue, err := deps.AnnotationService.CreateQueue(r.Context(), projectID, req.Name, req.Description)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "create_failed", "Failed to create queue")
			return
		}

		writeJSON(w, http.StatusCreated, queue)
	}
}

// handleListAnnotationQueues handles GET /api/projects/{projectID}/annotation-queues
func handleListAnnotationQueues(deps Dependencies) http.HandlerFunc {
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

		queues, err := deps.AnnotationService.ListQueues(r.Context(), projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", "Failed to list queues")
			return
		}

		writeJSON(w, http.StatusOK, queues)
	}
}

// handleListAnnotationItems handles GET /api/annotation-queues/{queueID}/items
func handleListAnnotationItems(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		queueID := chi.URLParam(r, "queueID")
		if queueID == "" {
			writeError(w, http.StatusBadRequest, "missing_queue_id", "Queue ID is required")
			return
		}

		items, err := deps.AnnotationService.ListItemsByQueue(r.Context(), queueID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", "Failed to list items")
			return
		}

		writeJSON(w, http.StatusOK, items)
	}
}

// handleUpdateAnnotationItem handles PATCH /api/annotation-items/{itemID}
func handleUpdateAnnotationItem(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		itemID := chi.URLParam(r, "itemID")
		if itemID == "" {
			writeError(w, http.StatusBadRequest, "missing_item_id", "Item ID is required")
			return
		}

		var req updateAnnotationItemRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}

		// Build update parameters
		status := ""
		if req.Status != nil {
			status = *req.Status
		}
		correctedOutput := ""
		if req.CorrectedOutput != nil {
			correctedOutput = *req.CorrectedOutput
		}
		expectedToolPath := ""
		if req.ExpectedToolPath != nil {
			expectedToolPath = *req.ExpectedToolPath
		}
		comment := ""
		if req.Comment != nil {
			comment = *req.Comment
		}

		item, err := deps.AnnotationService.UpdateItem(r.Context(), itemID, status, correctedOutput, expectedToolPath, comment)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "update_failed", "Failed to update item")
			return
		}

		writeJSON(w, http.StatusOK, item)
	}
}

// handlePromoteAnnotationItem handles POST /api/annotation-items/{itemID}/promote-to-dataset
func handlePromoteAnnotationItem(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		itemID := chi.URLParam(r, "itemID")
		if itemID == "" {
			writeError(w, http.StatusBadRequest, "missing_item_id", "Item ID is required")
			return
		}

		var req struct {
			DatasetID string `json:"dataset_id"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}
		if req.DatasetID == "" {
			writeError(w, http.StatusBadRequest, "missing_dataset_id", "Dataset ID is required")
			return
		}

		caseID, err := deps.AnnotationService.PromoteToDataset(r.Context(), itemID, req.DatasetID)
		if err != nil {
			if err.Error() == "annotation item already promoted" {
				writeError(w, http.StatusConflict, "already_promoted", err.Error())
				return
			}
			writeError(w, http.StatusBadRequest, "promote_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"case_id": caseID})
	}
}
