package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// handleSDKStartRun handles POST /api/sdk/runs/start
func handleSDKStartRun(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := authenticateSDKProject(r, deps)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Missing or invalid token")
			return
		}

		var req SDKStartRunRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}

		run, err := deps.RuntimeService.StartRun(r.Context(), projectID, req)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "start_run_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, run)
	}
}

// handleSDKUpdateRunState handles POST /api/sdk/runs/{run_id}/state
func handleSDKUpdateRunState(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := authenticateSDKProject(r, deps)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Missing or invalid token")
			return
		}

		runID := chi.URLParam(r, "run_id")
		if runID == "" {
			writeError(w, http.StatusBadRequest, "missing_run_id", "Run ID is required")
			return
		}

		var req SDKUpdateRunStateRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}

		run, err := deps.RuntimeService.UpdateRunState(r.Context(), projectID, runID, req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "update_state_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, run)
	}
}

// handleSDKFinishRun handles POST /api/sdk/runs/{run_id}/finish
func handleSDKFinishRun(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := authenticateSDKProject(r, deps)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Missing or invalid token")
			return
		}

		runID := chi.URLParam(r, "run_id")
		if runID == "" {
			writeError(w, http.StatusBadRequest, "missing_run_id", "Run ID is required")
			return
		}

		var req SDKFinishRunRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}

		run, err := deps.RuntimeService.FinishRun(r.Context(), projectID, runID, req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "finish_run_failed", err.Error())
			return
		}

		// Trigger online evaluation asynchronously (non-blocking)
		if deps.OnlineEvalDisp != nil {
			go deps.OnlineEvalDisp.DispatchRunAsync(projectID, runID)
		}

		writeJSON(w, http.StatusOK, run)
	}
}

// handleSDKFailRun handles POST /api/sdk/runs/{run_id}/fail
func handleSDKFailRun(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := authenticateSDKProject(r, deps)
		if !ok {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Missing or invalid token")
			return
		}

		runID := chi.URLParam(r, "run_id")
		if runID == "" {
			writeError(w, http.StatusBadRequest, "missing_run_id", "Run ID is required")
			return
		}

		var req SDKFailRunRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}

		run, err := deps.RuntimeService.FailRun(r.Context(), projectID, runID, req)
		if err != nil {
			writeError(w, http.StatusBadRequest, "fail_run_failed", err.Error())
			return
		}

		// Trigger online evaluation asynchronously (non-blocking)
		if deps.OnlineEvalDisp != nil {
			go deps.OnlineEvalDisp.DispatchRunAsync(projectID, runID)
		}

		writeJSON(w, http.StatusOK, run)
	}
}

// authenticateSDKProject extracts project ID from bearer token.
func authenticateSDKProject(r *http.Request, deps Dependencies) (string, bool) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || len(authHeader) < 7 || authHeader[:7] != "Bearer " {
		return "", false
	}
	plainToken := authHeader[7:]

	projectID, _, err := deps.TokenAuth.AuthenticateToken(r.Context(), plainToken, "agent:runtime")
	if err != nil {
		return "", false
	}

	return projectID, true
}
