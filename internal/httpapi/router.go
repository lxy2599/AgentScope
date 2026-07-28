package httpapi

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

// NewRouter creates a chi router with all application routes mounted.
// If deps.StaticFS is non-nil, it also serves the SPA at the root after all
// API routes are registered.
func NewRouter(deps Dependencies) http.Handler {
	r := chi.NewRouter()

	// Public routes
	r.Get("/healthz", handleHealthz)
	r.Post("/api/auth/register", handleRegister(deps))
	r.Post("/api/auth/login", handleLogin(deps))

	// OTLP trace ingestion (uses bearer token auth, not session)
	r.Post("/v1/traces", handleIngest(deps))

	// Agent SDK API (uses project token bearer auth)
	r.Post("/api/v1/agents/register", handleAgentRegister(deps))
	r.Post("/api/v1/agents/{agentID}/heartbeat", handleAgentHeartbeat(deps))
	r.Post("/api/v1/agents/{agentID}/capabilities", handleSetCapability(deps))

	// SDK Runtime API (uses project token bearer auth)
	r.Post("/api/sdk/runs/start", handleSDKStartRun(deps))
	r.Post("/api/sdk/runs/{run_id}/state", handleSDKUpdateRunState(deps))
	r.Post("/api/sdk/runs/{run_id}/finish", handleSDKFinishRun(deps))
	r.Post("/api/sdk/runs/{run_id}/fail", handleSDKFailRun(deps))

	// Protected routes (require valid session)
	r.Group(func(r chi.Router) {
		r.Use(requireAuth(deps))
		r.Post("/api/auth/logout", handleLogout(deps))
		r.Post("/api/auth/logout-all", handleLogoutAll(deps))
		r.Get("/api/auth/sessions", handleListSessions(deps))
		r.Delete("/api/auth/sessions/{sessionID}", handleDeleteSession(deps))
		r.Post("/api/projects", handleCreateProject(deps))
		r.Get("/api/projects", handleListProjects(deps))
		r.Get("/api/projects/{projectID}/tokens", handleListProjectTokens(deps))
		r.Post("/api/projects/{projectID}/tokens", handleCreateToken(deps))
		r.Post("/api/projects/{projectID}/tokens/{tokenID}/revoke", handleRevokeProjectToken(deps))
		r.Post("/api/projects/{projectID}/tokens/{tokenID}/rotate", handleRotateProjectToken(deps))
		r.Get("/api/projects/{projectID}/tokens/{tokenID}/audit-events", handleListTokenAudit(deps))
		r.Get("/api/projects/{projectID}/token-policy", handleGetTokenPolicy(deps))
		r.Put("/api/projects/{projectID}/token-policy", handleUpdateTokenPolicy(deps))
		r.Get("/api/projects/{projectID}/tokens/risk-summary", handleGetTokenRiskSummary(deps))
		r.Get("/api/projects/{projectID}/runs", handleListProjectRuns(deps))
		r.Get("/api/projects/{projectID}/online-eval-jobs", handleListOnlineEvalJobs(deps))
		r.Get("/api/projects/{projectID}/online-eval-jobs/{jobID}", handleGetOnlineEvalJob(deps))
		r.Post("/api/projects/{projectID}/online-eval-jobs/{jobID}/retry", handleRetryOnlineEvalJob(deps))
		r.Get("/api/runs/{runID}", handleGetRunDetail(deps))
		r.Get("/api/runs/{runID}/snapshot", handleGetRunSnapshot(deps))
		r.Post("/api/projects/{projectID}/datasets", handleCreateDataset(deps))
		r.Get("/api/projects/{projectID}/datasets", handleListDatasets(deps))
		r.Post("/api/datasets/{datasetID}/cases", handleAddCase(deps))
		r.Get("/api/datasets/{datasetID}/cases", handleListCases(deps))
		r.Post("/api/projects/{projectID}/experiments", handleCreateExperiment(deps))
		r.Get("/api/projects/{projectID}/experiments", handleListExperiments(deps))
		r.Get("/api/experiments/{experimentID}", handleGetExperiment(deps))
		r.Get("/api/projects/{projectID}/experiments/{experimentID}/results", handleListExperimentResults(deps))
		r.Get("/api/projects/{projectID}/experiments/{experimentID}/report", handleExperimentReport(deps))
		r.Post("/api/projects/{projectID}/evaluators", handleCreateEvaluator(deps))
		r.Get("/api/projects/{projectID}/evaluators", handleListEvaluators(deps))
		r.Post("/api/projects/{projectID}/metric-definitions", handleCreateMetricDefinition(deps))
		r.Get("/api/projects/{projectID}/metric-definitions", handleListMetricDefinitions(deps))
		r.Post("/api/projects/{projectID}/rubrics", handleCreateRubricTemplate(deps))
		r.Get("/api/projects/{projectID}/rubrics", handleListRubricTemplates(deps))
		r.Post("/api/projects/{projectID}/rubrics/{templateID}/versions", handleCreateRubricVersion(deps))
		r.Get("/api/projects/{projectID}/rubrics/{templateID}/versions", handleListRubricVersions(deps))
		r.Post("/api/projects/{projectID}/rubric-versions/{versionID}/publish", handlePublishRubricVersion(deps))
		r.Get("/api/projects/{projectID}/runs/{runID}/judge-evaluation", handleListJudgeEvaluation(deps))
		r.Post("/api/projects/{projectID}/eval-calibrations", handleRunCalibration(deps))
		r.Get("/api/projects/{projectID}/eval-calibrations", handleListCalibrations(deps))

		// Update metric definition
		r.Put("/api/projects/{projectID}/metric-definitions/{definitionID}", handleUpdateMetricDefinition(deps))
		r.Delete("/api/projects/{projectID}/metric-definitions/{definitionID}", handleDeleteMetricDefinition(deps))

		r.Post("/api/projects/{projectID}/eval-scores", handleCreateEvalScore(deps))
		r.Post("/api/projects/{projectID}/eval-scores/batch", handleBatchCreateEvalScores(deps))
		r.Get("/api/projects/{projectID}/eval-scores/by-run/{runID}", handleListEvalScoresByRun(deps))
		r.Get("/api/projects/{projectID}/experiments/{experimentID}/eval-scores", handleExperimentEvalScores(deps))
		r.Get("/api/v1/agents", handleAgentList(deps))
		r.Get("/api/v1/agent-summary", handleAgentSummary(deps))
		r.Get("/api/v1/agents/{agentID}", handleAgentDetail(deps))
		r.Get("/api/v1/agents/{agentID}/recent-errors", handleAgentRecentErrors(deps))
		r.Get("/api/projects/{projectID}/active-runs", handleListActiveRuns(deps))
		r.Get("/api/projects/{projectID}/runs/{runID}/runtime", handleGetRuntimeRunDetail(deps))
		r.Post("/api/projects/{projectID}/sampling-rules", handleCreateSamplingRule(deps))
		r.Get("/api/projects/{projectID}/sampling-rules", handleListSamplingRules(deps))
		r.Get("/api/sampling-rules/{ruleID}", handleGetSamplingRule(deps))
		r.Patch("/api/sampling-rules/{ruleID}", handleUpdateSamplingRule(deps))
		r.Delete("/api/sampling-rules/{ruleID}", handleDeleteSamplingRule(deps))
		r.Post("/api/projects/{projectID}/annotation-queues", handleCreateAnnotationQueue(deps))
		r.Get("/api/projects/{projectID}/annotation-queues", handleListAnnotationQueues(deps))
		r.Get("/api/projects/{projectID}/annotation-items", handleListProjectAnnotationItems(deps))
		r.Get("/api/annotation-queues/{queueID}/items", handleListAnnotationItems(deps))
		r.Patch("/api/annotation-items/{itemID}", handleUpdateAnnotationItem(deps))
		r.Post("/api/annotation-items/{itemID}/gold", handleSubmitAnnotationGold(deps))
		r.Post("/api/annotation-items/{itemID}/promote-to-dataset", handlePromoteAnnotationItem(deps))
		r.Post("/api/projects/{projectID}/runs/{runID}/annotation-items", handleCreateAnnotationItemFromRun(deps))
		r.Post("/api/projects/{projectID}/comparisons", handleCreateComparison(deps))
		r.Get("/api/projects/{projectID}/comparisons", handleListComparisons(deps))
		r.Get("/api/comparisons/{comparisonID}", handleGetComparison(deps))
		r.Get("/api/projects/{projectID}/ingest-stats", handleProjectIngestStats(deps))
		r.Get("/api/projects/{projectID}/ingest-stats/agents", handleProjectIngestAgentStats(deps))
		r.Get("/api/projects/{projectID}/ingest-stats/events", handleProjectIngestHealthEvents(deps))
		r.Get("/api/projects/{projectID}/quotas", handleGetProjectQuota(deps))
		r.Put("/api/projects/{projectID}/quotas", handleUpdateProjectQuota(deps))
		r.Get("/api/projects/{projectID}/anomalies", handleListProjectAnomalies(deps))
		r.Post("/api/projects/{projectID}/anomalies/detect", handleDetectProjectAnomalies(deps))
		r.Get("/api/projects/{projectID}/events/stream", handleRealtimeStream(deps))
	})

	// Runner API (uses project token bearer auth)
	r.Group(func(r chi.Router) {
		r.Get("/runner/jobs/claim", handleClaimJob(deps))
		r.Post("/runner/jobs/{jobID}/heartbeat", handleHeartbeat(deps))
		r.Post("/runner/jobs/{jobID}/results", handleSubmitResult(deps))
		r.Post("/runner/jobs/{jobID}/fail", handleFailJob(deps))
		r.Get("/runner/runs/by-trace/{traceID}", handleGetRunByTrace(deps))
		r.Get("/runner/runs/{runID}/detail", handleGetRunDetailForRunner(deps))
		r.Post("/runner/runs/{runID}/fail", handleRunnerFailRun(deps))
		r.Post("/runner/runs/{runID}/judge-votes", handleRunnerSubmitJudgeVotes(deps))
	})

	// SPA static file serving (only if StaticFS is configured)
	if deps.StaticFS != nil {
		fileServer := http.FileServer(http.FS(deps.StaticFS))

		r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
			path := req.URL.Path
			// Don't interfere with API routes
			if strings.HasPrefix(path, "/api/") ||
				strings.HasPrefix(path, "/runner/") ||
				strings.HasPrefix(path, "/v1/") ||
				path == "/healthz" {
				http.NotFound(w, req)
				return
			}
			// Keep Vite's immutable assets addressable. Previously every non-API
			// request was rewritten to index.html, making module scripts load as
			// text/html in the production server.
			if strings.HasPrefix(path, "/assets/") || path == "/favicon.ico" {
				fileServer.ServeHTTP(w, req)
				return
			}

			// SPA fallback: serve index.html for browser routes.
			req.URL.Path = "/"
			fileServer.ServeHTTP(w, req)
		})
	}

	return r
}

// handleHealthz responds with {"ok":true} to signal the server is running.
func handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
