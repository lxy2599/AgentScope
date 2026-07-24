package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ---- Comparison request types ----

type createComparisonRequest struct {
	BaselineExperimentID  string `json:"baseline_experiment_id"`
	CandidateExperimentID string `json:"candidate_experiment_id"`
	ComparisonType        string `json:"comparison_type"`
	RegressionGates       string `json:"regression_gates,omitempty"`
}

// handleCreateComparison handles POST /api/projects/{projectID}/comparisons
func handleCreateComparison(deps Dependencies) http.HandlerFunc {
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

		var req createComparisonRequest
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		if req.BaselineExperimentID == "" {
			writeError(w, http.StatusBadRequest, "missing_baseline", "Baseline experiment ID is required")
			return
		}
		if req.CandidateExperimentID == "" {
			writeError(w, http.StatusBadRequest, "missing_candidate", "Candidate experiment ID is required")
			return
		}

		// Default comparison type
		compType := req.ComparisonType
		if compType == "" {
			compType = "general"
		}
		if compType != "model_swap" && compType != "ablation" && compType != "general" {
			writeError(w, http.StatusBadRequest, "invalid_comparison_type", "Comparison type must be one of: model_swap, ablation, general")
			return
		}

		detail, err := deps.CompareService.CreateComparison(
			r.Context(), projectID,
			req.BaselineExperimentID, req.CandidateExperimentID,
			userID, compType, req.RegressionGates,
		)
		if err != nil {
			writeInternalError(w, "create_comparison_failed", "Failed to create comparison", "create comparison", err)
			return
		}

		writeJSON(w, http.StatusCreated, detail)
	}
}

// handleGetComparison handles GET /api/comparisons/{comparisonID}
func handleGetComparison(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		comparisonID := chi.URLParam(r, "comparisonID")

		detail, err := deps.CompareService.GetComparison(r.Context(), comparisonID)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "Comparison not found")
			return
		}

		// Verify project membership
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, detail.ProjectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}

		writeJSON(w, http.StatusOK, detail)
	}
}

// handleListComparisons handles GET /api/projects/{projectID}/comparisons
func handleListComparisons(deps Dependencies) http.HandlerFunc {
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

		comparisons, err := deps.CompareService.ListComparisons(r.Context(), projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", "Failed to list comparisons")
			return
		}

		writeJSON(w, http.StatusOK, comparisons)
	}
}
