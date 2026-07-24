package httpapi

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"

	"AgentScope/internal/quotas"
	domainruns "AgentScope/internal/runs"
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
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		if req.RunnerName == "" {
			writeError(w, http.StatusBadRequest, "missing_runner_name", "Runner name is required")
			return
		}

		if err := deps.RunnerService.Heartbeat(r.Context(), jobID, projectID, req.RunnerName); err != nil {
			writeInternalError(w, "heartbeat_failed", "Failed to heartbeat job", "heartbeat runner job", err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// evalScoreItemRequest is a single multi-dimensional score in the submission body.
type evalScoreItemRequest struct {
	MetricName string          `json:"metric_name"`
	MetricKind string          `json:"metric_kind"`
	Score      float64         `json:"score"`
	Passed     bool            `json:"passed"`
	Reason     string          `json:"reason"`
	Details    json.RawMessage `json:"details,omitempty"`
	TraceID    string          `json:"trace_id,omitempty"`
	SpanID     string          `json:"span_id,omitempty"`
}

// submitResultRequest is the request body for submitting a result.
type submitResultRequest struct {
	RunnerName   string                 `json:"runner_name"`
	RunID        string                 `json:"run_id"`
	Score        float64                `json:"score"`
	Passed       bool                   `json:"passed"`
	Reason       string                 `json:"reason"`
	Details      json.RawMessage        `json:"details,omitempty"`
	ActualOutput string                 `json:"actual_output"`
	LatencyMs    int64                  `json:"latency_ms"`
	Cost         float64                `json:"cost"`
	EvalScores   []evalScoreItemRequest `json:"eval_scores,omitempty"`
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
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		if req.RunnerName == "" {
			writeError(w, http.StatusBadRequest, "missing_runner_name", "Runner name is required")
			return
		}
		if deps.QuotaService != nil && req.Cost > 0 {
			decision := deps.QuotaService.ConsumeDailyBudget(
				r.Context(),
				projectID,
				quotas.DimensionCostMicrosDaily,
				int64(math.Round(req.Cost*1_000_000)),
			)
			writeQuotaHeaders(w, decision)
			if !decision.Allowed {
				writeError(w, http.StatusPaymentRequired, "cost_budget_exceeded", "Project daily cost budget exceeded")
				return
			}
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

		// Convert eval scores if any
		if len(req.EvalScores) > 0 {
			svcReq.EvalScores = make([]EvalScoreItem, 0, len(req.EvalScores))
			for _, es := range req.EvalScores {
				var details []byte
				if es.Details != nil {
					details = es.Details
				}
				svcReq.EvalScores = append(svcReq.EvalScores, EvalScoreItem{
					MetricName: es.MetricName,
					MetricKind: es.MetricKind,
					Score:      es.Score,
					Passed:     es.Passed,
					Reason:     es.Reason,
					Details:    details,
					TraceID:    es.TraceID,
					SpanID:     es.SpanID,
				})
			}
		}

		if err := deps.RunnerService.SubmitResult(r.Context(), jobID, projectID, req.RunnerName, svcReq); err != nil {
			writeInternalError(w, "submit_failed", "Failed to submit job result", "submit runner result", err)
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
		if !decodeJSONRequest(w, r, &req) {
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
			writeInternalError(w, "fail_failed", "Failed to mark job as failed", "fail runner job", err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// runnerFailRunRequest is the request body for failing a run via the runner API.
type runnerFailRunRequest struct {
	Error string `json:"error"`
}

// handleRunnerFailRun handles POST /runner/runs/{runID}/fail
//
// This endpoint allows the runner to fail an OTLP trace-created run using
// the same runner:write scope, without needing agent:runtime scope.
// The run is found by its PostgreSQL ID and verified to belong to the project.
func handleRunnerFailRun(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := authenticateRunner(w, r, deps)
		if !ok {
			return
		}

		runID := chi.URLParam(r, "runID")
		if runID == "" {
			writeError(w, http.StatusBadRequest, "missing_run_id", "Run ID is required")
			return
		}

		var req runnerFailRunRequest
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		if req.Error == "" {
			writeError(w, http.StatusBadRequest, "missing_error", "Error message is required")
			return
		}

		if err := deps.RunsService.FailRunByRunner(r.Context(), runID, projectID, req.Error); err != nil {
			if errors.Is(err, domainruns.ErrRunNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "Run not found")
				return
			}
			writeInternalError(w, "fail_run_failed", "Failed to mark run as failed", "fail runner run", err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// handleGetRunByTrace handles GET /runner/runs/by-trace/{traceID}
func handleGetRunByTrace(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := authenticateRunner(w, r, deps)
		if !ok {
			return
		}

		traceID := chi.URLParam(r, "traceID")
		if traceID == "" {
			writeError(w, http.StatusBadRequest, "missing_trace", "Trace ID is required")
			return
		}

		run, err := deps.RunsService.GetRunByTraceID(r.Context(), traceID, projectID)
		if err != nil {
			if errors.Is(err, domainruns.ErrRunNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "No run found for the given trace ID")
				return
			}
			writeInternalError(w, "get_run_failed", "Failed to get run", "get runner run by trace ID", err)
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"run_id": run.ID})
	}
}

// handleGetRunDetailForRunner handles GET /runner/runs/{runID}/detail
func handleGetRunDetailForRunner(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := authenticateRunner(w, r, deps)
		if !ok {
			return
		}

		runID := chi.URLParam(r, "runID")
		if runID == "" {
			writeError(w, http.StatusBadRequest, "missing_run_id", "Run ID is required")
			return
		}

		// First verify the run belongs to the project
		run, err := deps.RunsService.GetRun(r.Context(), runID)
		if err != nil {
			if errors.Is(err, domainruns.ErrRunNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "Run not found")
				return
			}
			writeInternalError(w, "get_run_failed", "Failed to get run", "get runner run for detail authorization", err)
			return
		}
		if run.ProjectID != projectID {
			writeError(w, http.StatusNotFound, "not_found", "Run not found in this project")
			return
		}

		// Get full detail with deep trace
		detail, err := deps.RunsService.GetRunDetail(r.Context(), runID)
		if err != nil {
			if errors.Is(err, domainruns.ErrRunNotFound) {
				writeError(w, http.StatusNotFound, "not_found", "Run not found")
				return
			}
			writeInternalError(w, "detail_failed", "Failed to get run detail", "get runner run detail", err)
			return
		}

		writeJSON(w, http.StatusOK, detail)
	}
}
