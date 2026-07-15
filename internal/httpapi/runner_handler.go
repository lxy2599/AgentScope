package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// authenticateRunner extracts the bearer token and authenticates with scope "runner:write".
func authenticateRunner(w http.ResponseWriter, r *http.Request, deps Dependencies) (string, bool) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || len(authHeader) < 7 || authHeader[:7] != "Bearer " {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Missing or invalid authorization header")
		return "", false
	}
	plainToken := authHeader[7:]

	projectID, _, err := deps.TokenAuth.AuthenticateToken(r.Context(), plainToken, "runner:write")
	if err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "Token lacks required scope or is invalid")
		return "", false
	}

	return projectID, true
}

// handleClaimJob handles GET /runner/jobs/claim
func handleClaimJob(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := authenticateRunner(w, r, deps)
		if !ok {
			return
		}

		runnerName := r.URL.Query().Get("runner_name")
		if runnerName == "" {
			runnerName = "unknown"
		}

		claimResp, err := deps.RunnerService.Claim(r.Context(), projectID, runnerName)
		if err != nil {
			writeError(w, http.StatusNotFound, "no_jobs", "No claimable jobs available")
			return
		}

		writeJSON(w, http.StatusOK, claimResp)
	}
}

// heartbeatRequest is the request body for heartbeat.
type heartbeatRequest struct {
	RunnerName string `json:"runner_name"`
}

// handleHeartbeat handles POST /runner/jobs/{jobID}/heartbeat
func handleHeartbeat(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := authenticateRunner(w, r, deps)
		if !ok {
			return
		}

		jobID := chi.URLParam(r, "jobID")

		var req heartbeatRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}
		if req.RunnerName == "" {
			writeError(w, http.StatusBadRequest, "missing_runner_name", "Runner name is required")
			return
		}

		if err := deps.RunnerService.Heartbeat(r.Context(), jobID, projectID, req.RunnerName); err != nil {
			writeError(w, http.StatusBadRequest, "heartbeat_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// submitResultRequest is the request body for submitting a result.
type submitResultRequest struct {
	RunnerName   string          `json:"runner_name"`
	RunID        string          `json:"run_id"`
	Score        float64         `json:"score"`
	Passed       bool            `json:"passed"`
	Reason       string          `json:"reason"`
	Details      json.RawMessage `json:"details,omitempty"`
	ActualOutput string          `json:"actual_output"`
	LatencyMs    int64           `json:"latency_ms"`
	Cost         float64         `json:"cost"`
}

// handleSubmitResult handles POST /runner/jobs/{jobID}/results
func handleSubmitResult(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := authenticateRunner(w, r, deps)
		if !ok {
			return
		}

		jobID := chi.URLParam(r, "jobID")

		var req submitResultRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}
		if req.RunnerName == "" {
			writeError(w, http.StatusBadRequest, "missing_runner_name", "Runner name is required")
			return
		}

		svcReq := SubmitResultRequest{
			RunID:        req.RunID,
			Score:        req.Score,
			Passed:       req.Passed,
			Reason:       req.Reason,
			Details:      req.Details,
			ActualOutput: req.ActualOutput,
			LatencyMs:    req.LatencyMs,
			Cost:         req.Cost,
		}

		if err := deps.RunnerService.SubmitResult(r.Context(), jobID, projectID, req.RunnerName, svcReq); err != nil {
			writeError(w, http.StatusBadRequest, "submit_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// failJobRequest is the request body for failing a job.
type failJobRequest struct {
	RunnerName string `json:"runner_name"`
	Error      string `json:"error"`
}

// handleFailJob handles POST /runner/jobs/{jobID}/fail
func handleFailJob(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := authenticateRunner(w, r, deps)
		if !ok {
			return
		}

		jobID := chi.URLParam(r, "jobID")

		var req failJobRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}
		if req.RunnerName == "" {
			writeError(w, http.StatusBadRequest, "missing_runner_name", "Runner name is required")
			return
		}
		if req.Error == "" {
			writeError(w, http.StatusBadRequest, "missing_error", "Error message is required")
			return
		}

		if err := deps.RunnerService.FailJob(r.Context(), jobID, projectID, req.RunnerName, req.Error); err != nil {
			writeError(w, http.StatusBadRequest, "fail_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
