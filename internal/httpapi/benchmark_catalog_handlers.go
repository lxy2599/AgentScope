package httpapi

import (
	"net/http"

	"AgentScope/internal/benchmarks"
)

func loadBenchmarkCatalog(w http.ResponseWriter, r *http.Request, deps Dependencies) (benchmarks.Catalog, bool) {
	projectID, ok := benchmarkProjectAccess(w, r, deps)
	if !ok {
		return benchmarks.Catalog{}, false
	}
	if deps.BenchmarkCatalog == nil {
		writeError(w, http.StatusServiceUnavailable, "benchmark_catalog_unavailable", "Benchmark catalog read model is not configured")
		return benchmarks.Catalog{}, false
	}
	catalog, err := deps.BenchmarkCatalog.ReadCatalog(r.Context(), projectID)
	if err != nil {
		writeInternalError(w, "read_benchmark_catalog_failed", "Failed to read benchmark catalog", "read benchmark catalog", err)
		return benchmarks.Catalog{}, false
	}
	return catalog, true
}

// handleGetBenchmarkCatalog returns the read-only catalog snapshot that a UI
// can use to select a template, artifact, and versioned profiles.
func handleGetBenchmarkCatalog(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		catalog, ok := loadBenchmarkCatalog(w, r, deps)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, catalog)
	}
}

func handleListBenchmarkArtifacts(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		catalog, ok := loadBenchmarkCatalog(w, r, deps)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, catalog.Artifacts)
	}
}

func handleListBenchmarkProfiles(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		catalog, ok := loadBenchmarkCatalog(w, r, deps)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, catalog.Profiles)
	}
}

func handleListBenchmarkRunTemplates(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		catalog, ok := loadBenchmarkCatalog(w, r, deps)
		if !ok {
			return
		}
		writeJSON(w, http.StatusOK, catalog.RunTemplates)
	}
}

// handleSyncBenchmarkCatalog replaces one project's catalog snapshot with a
// validated manifest produced by the external benchmarks CLI. It is protected
// by the project-admin session boundary; runner tokens cannot mutate catalog
// configuration.
func handleSyncBenchmarkCatalog(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, _, ok := benchmarkProjectAdminAccess(w, r, deps)
		if !ok {
			return
		}
		if deps.BenchmarkCatalogSync == nil {
			writeError(w, http.StatusServiceUnavailable, "benchmark_catalog_sync_unavailable", "Benchmark catalog synchronization is not configured")
			return
		}
		var manifest benchmarks.Catalog
		if !decodeStrictJSONRequest(w, r, &manifest) {
			return
		}
		catalog, err := deps.BenchmarkCatalogSync.SyncCatalog(r.Context(), projectID, manifest)
		if err != nil {
			// Catalog errors can include an untrusted key name. Return a generic
			// client error and do not log/echo the manifest or validation detail.
			writeError(w, http.StatusBadRequest, "invalid_benchmark_catalog", "Benchmark catalog manifest is invalid or contains a prohibited secret")
			return
		}
		writeJSON(w, http.StatusOK, catalog)
	}
}
