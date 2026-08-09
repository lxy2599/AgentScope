package httpapi

import (
	"encoding/json"
	"net/http"

	"AgentScope/internal/benchmarks"
	"github.com/go-chi/chi/v5"
)

type createBenchmarkSuiteRequest struct {
	DatasetID              string          `json:"dataset_id"`
	BenchmarkKey           string          `json:"benchmark_key"`
	Name                   string          `json:"name"`
	Description            string          `json:"description,omitempty"`
	Version                string          `json:"version"`
	HarnessKind            string          `json:"harness_kind"`
	ExecutionKind          string          `json:"execution_kind"`
	SourceURI              string          `json:"source_uri,omitempty"`
	SourceCommit           string          `json:"source_commit,omitempty"`
	DefaultExecutionConfig json.RawMessage `json:"default_execution_config,omitempty"`
	RequiredCapabilities   []string        `json:"required_capabilities,omitempty"`
	Metadata               json.RawMessage `json:"metadata,omitempty"`
}

func handleCreateBenchmarkSuite(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		projectID := chi.URLParam(r, "projectID")
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}
		var req createBenchmarkSuiteRequest
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		if deps.BenchmarkSuitesService == nil {
			writeError(w, http.StatusServiceUnavailable, "benchmark_unavailable", "Benchmark suites are unavailable")
			return
		}
		suite, err := deps.BenchmarkSuitesService.CreateSuite(r.Context(), benchmarks.CreateSuiteParams{
			ProjectID: projectID, DatasetID: req.DatasetID, BenchmarkKey: req.BenchmarkKey, Name: req.Name,
			Description: req.Description, Version: req.Version, HarnessKind: req.HarnessKind, ExecutionKind: req.ExecutionKind,
			SourceURI: req.SourceURI, SourceCommit: req.SourceCommit, DefaultExecutionConfig: req.DefaultExecutionConfig,
			RequiredCapabilities: req.RequiredCapabilities, Metadata: req.Metadata,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, "create_benchmark_suite_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, suite)
	}
}

func handleListBenchmarkSuites(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		projectID := chi.URLParam(r, "projectID")
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}
		suites, err := deps.BenchmarkSuitesService.ListSuites(r.Context(), projectID)
		if err != nil {
			writeInternalError(w, "list_benchmark_suites_failed", "Failed to list benchmark suites", "list benchmark suites", err)
			return
		}
		writeJSON(w, http.StatusOK, suites)
	}
}

func handleGetBenchmarkSuite(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		projectID := chi.URLParam(r, "projectID")
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
			return
		}
		suite, err := deps.BenchmarkSuitesService.GetSuite(r.Context(), projectID, chi.URLParam(r, "suiteID"))
		if err != nil {
			writeError(w, http.StatusNotFound, "benchmark_suite_not_found", "Benchmark suite not found")
			return
		}
		writeJSON(w, http.StatusOK, suite)
	}
}
