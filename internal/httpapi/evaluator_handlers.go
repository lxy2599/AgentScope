package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ---- Evaluator handlers ----

type createEvaluatorRequest struct {
	Name   string          `json:"name"`
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

// handleCreateEvaluator handles POST /api/projects/{projectID}/evaluators
func handleCreateEvaluator(deps Dependencies) http.HandlerFunc {
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

		var req createEvaluatorRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "missing_name", "Evaluator name is required")
			return
		}
		if req.Type == "" {
			writeError(w, http.StatusBadRequest, "missing_type", "Evaluator type is required")
			return
		}

		// Evaluate will be created via store directly since there's no service layer for it
		evaluator, err := deps.EvaluatorCreator.Create(r.Context(), projectID, req.Name, req.Type, string(req.Config))
		if err != nil {
			writeError(w, http.StatusBadRequest, "create_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, evaluator)
	}
}

// handleListEvaluators handles GET /api/projects/{projectID}/evaluators
func handleListEvaluators(deps Dependencies) http.HandlerFunc {
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

		evaluators, err := deps.EvaluatorCreator.List(r.Context(), projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", "Failed to list evaluators")
			return
		}

		writeJSON(w, http.StatusOK, evaluators)
	}
}
