package httpapi

import (
	"net/http"
	"strings"

	"AgentScope/internal/benchmarks"
	"github.com/go-chi/chi/v5"
)

type benchmarkWorkerRegistrationRequest struct {
	WorkerName     string   `json:"worker_name"`
	ExecutionKinds []string `json:"execution_kinds"`
	Capacity       int      `json:"capacity,omitempty"`
	Version        string   `json:"version,omitempty"`
}

// handleRegisterBenchmarkWorker registers or refreshes a resident external
// worker. It is authenticated with runner:write because the same process will
// claim eval_jobs through the runner API.
func handleRegisterBenchmarkWorker(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := authenticateRunner(w, r, deps)
		if !ok {
			return
		}
		if deps.BenchmarkWorkers == nil {
			writeError(w, http.StatusServiceUnavailable, "benchmark_workers_unavailable", "Benchmark worker registration is not configured")
			return
		}
		var req benchmarkWorkerRegistrationRequest
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		worker, err := deps.BenchmarkWorkers.Register(r.Context(), benchmarks.WorkerRegistration{
			ProjectID:      projectID,
			Name:           req.WorkerName,
			ExecutionKinds: req.ExecutionKinds,
			Capacity:       req.Capacity,
			Version:        req.Version,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_benchmark_worker_registration", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, worker)
	}
}

func handleListBenchmarkWorkers(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := benchmarkProjectAccess(w, r, deps)
		if !ok {
			return
		}
		if deps.BenchmarkWorkers == nil {
			writeError(w, http.StatusServiceUnavailable, "benchmark_workers_unavailable", "Benchmark worker registration is not configured")
			return
		}
		workers, err := deps.BenchmarkWorkers.List(r.Context(), projectID)
		if err != nil {
			writeInternalError(w, "list_benchmark_workers_failed", "Failed to list benchmark workers", "list benchmark workers", err)
			return
		}
		writeJSON(w, http.StatusOK, workers)
	}
}

func handleGetBenchmarkWorkerAvailability(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := benchmarkProjectAccess(w, r, deps)
		if !ok {
			return
		}
		if deps.BenchmarkWorkers == nil {
			writeError(w, http.StatusServiceUnavailable, "benchmark_workers_unavailable", "Benchmark worker registration is not configured")
			return
		}
		executionKind := strings.TrimSpace(chi.URLParam(r, "executionKind"))
		availability, err := deps.BenchmarkWorkers.Availability(r.Context(), projectID, executionKind)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_execution_kind", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, availability)
	}
}
