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
		if !decodeJSONRequest(w, r, &req) {
			return
		}

		run, err := deps.RuntimeService.StartRun(r.Context(), projectID, req)
		if err != nil {
			writeInternalError(w, "start_run_failed", "Failed to start run", "start SDK run", err)
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
		if !decodeJSONRequest(w, r, &req) {
			return
		}

		run, err := deps.RuntimeService.UpdateRunState(r.Context(), projectID, runID, req)
		if err != nil {
			writeInternalError(w, "update_state_failed", "Failed to update run state", "update SDK run state", err)
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
		if !decodeJSONRequest(w, r, &req) {
			return
		}

		run, err := deps.RuntimeService.FinishRun(r.Context(), projectID, runID, req)
		if err != nil {
			writeInternalError(w, "finish_run_failed", "Failed to finish run", "finish SDK run", err)
			return
		}

		// Persist durable online-evaluation work before acknowledging the terminal
		// Run. A worker performs the expensive evaluation after the HTTP response.
		if deps.OnlineEvalJobs != nil {
			if _, err := deps.OnlineEvalJobs.Enqueue(r.Context(), projectID, runID, run.TraceID); err != nil {
				writeInternalError(w, "enqueue_online_eval_failed", "Run finished but online evaluation could not be queued", "enqueue online evaluation", err)
				return
			}
		} else if deps.OnlineEvalDisp != nil {
			// Compatibility path for test and pre-migration dependency wiring.
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
		if !decodeJSONRequest(w, r, &req) {
			return
		}

		run, err := deps.RuntimeService.FailRun(r.Context(), projectID, runID, req)
		if err != nil {
			writeInternalError(w, "fail_run_failed", "Failed to fail run", "fail SDK run", err)
			return
		}

		// Persist durable online-evaluation work before acknowledging the terminal
		// Run. A worker performs the expensive evaluation after the HTTP response.
		if deps.OnlineEvalJobs != nil {
			if _, err := deps.OnlineEvalJobs.Enqueue(r.Context(), projectID, runID, run.TraceID); err != nil {
				writeInternalError(w, "enqueue_online_eval_failed", "Run finished but online evaluation could not be queued", "enqueue online evaluation", err)
				return
			}
		} else if deps.OnlineEvalDisp != nil {
			// Compatibility path for test and pre-migration dependency wiring.
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
