package httpapi

import (
	"context"
	"errors"
	"net/http"

	"AgentScope/internal/benchmarkcancel"
	"github.com/go-chi/chi/v5"
)

// BenchmarkJobCancellationService is the narrow administrative boundary for
// cancelling stale or otherwise invalid immutable benchmark snapshots.
type BenchmarkJobCancellationService interface {
	CancelExperiment(ctx context.Context, request benchmarkcancel.Request) (benchmarkcancel.Result, error)
	ListAudit(ctx context.Context, projectID, experimentID string) ([]benchmarkcancel.AuditEvent, error)
}

type benchmarkExperimentCancellationRequest struct {
	Reason string `json:"reason"`
}

// handleCancelBenchmarkExperiment durably cancels every queued or running
// eval_job in a benchmark experiment. It does not expose worker credentials,
// sidecar endpoints, task-run IDs, or the supplied audit reason.
func handleCancelBenchmarkExperiment(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, actorUserID, ok := benchmarkProjectAdminAccess(w, r, deps)
		if !ok {
			return
		}
		if deps.BenchmarkJobCanceller == nil {
			writeError(w, http.StatusServiceUnavailable, "benchmark_cancellation_unavailable", "Benchmark cancellation is not configured")
			return
		}

		var request benchmarkExperimentCancellationRequest
		if !decodeStrictJSONRequest(w, r, &request) {
			return
		}
		result, err := deps.BenchmarkJobCanceller.CancelExperiment(r.Context(), benchmarkcancel.Request{
			ProjectID:    projectID,
			ExperimentID: chi.URLParam(r, "experimentID"),
			ActorUserID:  actorUserID,
			Reason:       request.Reason,
		})
		if err != nil {
			switch {
			case errors.Is(err, benchmarkcancel.ErrExperimentNotFound):
				writeError(w, http.StatusNotFound, "benchmark_experiment_not_found", "Benchmark experiment not found")
			case errors.Is(err, benchmarkcancel.ErrInvalidReason):
				// Do not return the invalid value: this field is retained as audit
				// data, and a client may accidentally paste a credential into it.
				writeError(w, http.StatusBadRequest, "invalid_benchmark_cancellation_reason", "Benchmark cancellation reason is invalid")
			default:
				writeInternalError(w, "cancel_benchmark_experiment_failed", "Failed to cancel benchmark experiment", "cancel benchmark experiment", err)
			}
			return
		}
		writeJSON(w, http.StatusOK, result)
	}
}

// handleListBenchmarkCancellationAudit exposes retained cancellation reasons
// only to project administrators, matching the mutation authorization boundary.
func handleListBenchmarkCancellationAudit(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, _, ok := benchmarkProjectAdminAccess(w, r, deps)
		if !ok {
			return
		}
		if deps.BenchmarkJobCanceller == nil {
			writeError(w, http.StatusServiceUnavailable, "benchmark_cancellation_unavailable", "Benchmark cancellation is not configured")
			return
		}
		events, err := deps.BenchmarkJobCanceller.ListAudit(r.Context(), projectID, chi.URLParam(r, "experimentID"))
		if err != nil {
			if errors.Is(err, benchmarkcancel.ErrExperimentNotFound) {
				writeError(w, http.StatusNotFound, "benchmark_experiment_not_found", "Benchmark experiment not found")
				return
			}
			writeInternalError(w, "list_benchmark_cancellation_audit_failed", "Failed to load benchmark cancellation audit", "list benchmark cancellation audit", err)
			return
		}
		writeJSON(w, http.StatusOK, events)
	}
}
