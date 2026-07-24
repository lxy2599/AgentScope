package httpapi

import (
	"errors"
	"net/http"

	"AgentScope/internal/annotation"
	"github.com/go-chi/chi/v5"
)

type createAnnotationQueueRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type updateAnnotationItemRequest struct {
	Status           *string `json:"status,omitempty"`
	CorrectedOutput  *string `json:"corrected_output,omitempty"`
	ExpectedToolPath *string `json:"expected_tool_path,omitempty"`
	Comment          *string `json:"comment,omitempty"`
	FailureCategory  *string `json:"failure_category,omitempty"`
	PrivacyReviewed  *bool   `json:"privacy_reviewed,omitempty"`
	ReviewerNote     *string `json:"reviewer_note,omitempty"`
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
		if !decodeJSONRequest(w, r, &req) {
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

		// Resolve queue's project_id for permission check
		queue, err := deps.AnnotationService.GetQueue(r.Context(), queueID)
		if err != nil {
			writeError(w, http.StatusNotFound, "queue_not_found", "Queue not found")
			return
		}
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, queue.ProjectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
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

// handleCreateAnnotationItemFromRun handles POST /api/projects/{projectID}/runs/{runID}/annotation-items
// Creates an annotation item by copying run data.
func handleCreateAnnotationItemFromRun(deps Dependencies) http.HandlerFunc {
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

		runID := chi.URLParam(r, "runID")
		if runID == "" {
			writeError(w, http.StatusBadRequest, "missing_run", "Run ID is required")
			return
		}

		// Verify project membership
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		// Verify run belongs to this project
		run, err := deps.RunsService.GetRun(r.Context(), runID)
		if err != nil {
			writeError(w, http.StatusNotFound, "run_not_found", "Run not found")
			return
		}
		if run.ProjectID != projectID {
			writeError(w, http.StatusForbidden, "project_mismatch", "Run does not belong to the specified project")
			return
		}

		var req struct {
			QueueID string `json:"queue_id"`
		}
		// queue_id is optional; if not provided, the default queue will be used.
		if !decodeOptionalJSONRequest(w, r, &req) {
			return
		}

		item, err := deps.AnnotationService.CreateItemFromRun(r.Context(), projectID, runID, req.QueueID)
		if err != nil {
			writeInternalError(w, "create_failed", "Failed to create annotation item", "create annotation item from run", err)
			return
		}

		writeJSON(w, http.StatusCreated, item)
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

		// Resolve item's project_id for permission check
		item, err := deps.AnnotationService.GetItem(r.Context(), itemID)
		if err != nil {
			writeError(w, http.StatusNotFound, "item_not_found", "Annotation item not found")
			return
		}
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, item.ProjectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		var req updateAnnotationItemRequest
		if !decodeJSONRequest(w, r, &req) {
			return
		}

		// Build update parameters with three-state semantics
		status := item.Status
		if req.Status != nil {
			status = *req.Status
		}
		correctedOutput := item.CorrectedOutput
		if req.CorrectedOutput != nil {
			correctedOutput = *req.CorrectedOutput
		}
		expectedToolPath := item.ExpectedToolPath
		if req.ExpectedToolPath != nil {
			expectedToolPath = *req.ExpectedToolPath
		}
		comment := item.Comment
		if req.Comment != nil {
			comment = *req.Comment
		}
		failureCategory := item.FailureCategory
		if req.FailureCategory != nil {
			failureCategory = *req.FailureCategory
		}
		privacyReviewed := item.PrivacyReviewed
		if req.PrivacyReviewed != nil {
			privacyReviewed = *req.PrivacyReviewed
		}
		reviewerNote := item.ReviewerNote
		if req.ReviewerNote != nil {
			reviewerNote = *req.ReviewerNote
		}

		// Update with all fields
		item, err = deps.AnnotationService.UpdateItemFull(r.Context(), itemID,
			status, correctedOutput, expectedToolPath, comment,
			failureCategory, privacyReviewed, reviewerNote)
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

		// Resolve item's project_id for permission check
		item, err := deps.AnnotationService.GetItem(r.Context(), itemID)
		if err != nil {
			writeError(w, http.StatusNotFound, "item_not_found", "Annotation item not found")
			return
		}
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, item.ProjectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		var req struct {
			DatasetID string `json:"dataset_id"`
		}
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		if req.DatasetID == "" {
			writeError(w, http.StatusBadRequest, "missing_dataset_id", "Dataset ID is required")
			return
		}

		// Verify dataset belongs to the same project as the annotation item
		ds, err := deps.DatasetsService.GetDataset(r.Context(), req.DatasetID)
		if err != nil {
			writeError(w, http.StatusBadRequest, "dataset_not_found", "Dataset not found")
			return
		}
		if ds.ProjectID != item.ProjectID {
			writeError(w, http.StatusForbidden, "project_mismatch", "Dataset does not belong to the same project")
			return
		}

		caseID, err := deps.AnnotationService.PromoteToDataset(r.Context(), itemID, req.DatasetID)
		if err != nil {
			if errors.Is(err, annotation.ErrItemAlreadyPromoted) {
				writeError(w, http.StatusConflict, "already_promoted", "Annotation item already promoted")
				return
			}
			if errors.Is(err, annotation.ErrCorrectedOutputRequired) {
				writeError(w, http.StatusBadRequest, "promote_failed", annotation.ErrCorrectedOutputRequired.Error())
				return
			}
			if errors.Is(err, annotation.ErrPrivacyReviewRequired) {
				writeError(w, http.StatusBadRequest, "promote_failed", annotation.ErrPrivacyReviewRequired.Error())
				return
			}
			writeInternalError(w, "promote_failed", "Failed to promote annotation item", "promote annotation item", err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"case_id": caseID})
	}
}
