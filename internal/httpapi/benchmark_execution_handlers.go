package httpapi

import (
	"encoding/json"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"

	"AgentScope/internal/benchmarks"
	"AgentScope/internal/experiments"
	"github.com/go-chi/chi/v5"
)

type benchmarkExperimentRequest struct {
	// RunTemplateID is the preferred, reproducible launch input. It resolves a
	// catalog-pinned artifact/profile composition and does not accept mutable
	// request-time overrides.
	RunTemplateID   string          `json:"run_template_id,omitempty"`
	SuiteID         string          `json:"suite_id,omitempty"`
	TargetName      string          `json:"target_name,omitempty"`
	TargetVersion   string          `json:"target_version,omitempty"`
	TrialsPerCase   int             `json:"trials_per_case,omitempty"`
	BenchmarkConfig json.RawMessage `json:"benchmark_config,omitempty"`
	CaseIDs         []string        `json:"case_ids,omitempty"`
}

func benchmarkProjectAccess(w http.ResponseWriter, r *http.Request, deps Dependencies) (string, bool) {
	userID, projectID := userIDFromCtx(r.Context()), chi.URLParam(r, "projectID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
		return "", false
	}
	if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "Not a project member")
		return "", false
	}
	return projectID, true
}

func benchmarkProjectAdminAccess(w http.ResponseWriter, r *http.Request, deps Dependencies) (string, string, bool) {
	userID, projectID := userIDFromCtx(r.Context()), chi.URLParam(r, "projectID")
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
		return "", "", false
	}
	if deps.ProjectAdminAuth == nil || deps.ProjectAdminAuth.IsProjectAdmin(r.Context(), userID, projectID) != nil {
		writeError(w, http.StatusForbidden, "forbidden", "Project administrator access is required")
		return "", "", false
	}
	return projectID, userID, true
}

func handleListBenchmarkTargets(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if _, ok := benchmarkProjectAccess(w, r, deps); !ok {
			return
		}
		targets := benchmarks.DefaultTargets()
		response := make([]BenchmarkTarget, 0, len(targets))
		for _, target := range targets {
			response = append(response, BenchmarkTarget{Name: target.Name, Version: target.Version, Description: target.Description, Framework: target.Framework, Status: target.Status, Capabilities: target.Capabilities})
		}
		writeJSON(w, http.StatusOK, response)
	}
}

func loadBenchmarkPreflight(r *http.Request, deps Dependencies, projectID string, req benchmarkExperimentRequest) (BenchmarkSuite, benchmarks.Target, []string, BenchmarkPreflight, bool) {
	plan, loaded := loadBenchmarkLaunchPlan(r, deps, projectID, req)
	if !loaded {
		return BenchmarkSuite{}, benchmarks.Target{}, nil, BenchmarkPreflight{}, false
	}
	return plan.suite, plan.target, plan.caseIDs, plan.preflight, true
}

func handleLaunchBenchmarkExperiment(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, _, ok := benchmarkProjectAdminAccess(w, r, deps)
		if !ok {
			return
		}
		experimentID := chi.URLParam(r, "experimentID")
		experiment, err := deps.ExperimentsService.GetExperiment(r.Context(), experimentID)
		if err != nil || experiment.ProjectID != projectID {
			writeError(w, http.StatusNotFound, "benchmark_experiment_not_found", "Benchmark experiment not found")
			return
		}
		if experiment.BenchmarkSuiteID == "" {
			writeError(w, http.StatusBadRequest, "not_benchmark_experiment", "Experiment is not backed by a runnable benchmark suite")
			return
		}
		jobs, err := deps.ExperimentsService.ListExperimentJobs(r.Context(), experimentID)
		if err != nil {
			writeInternalError(w, "list_benchmark_jobs_failed", "Failed to load benchmark jobs", "list benchmark jobs", err)
			return
		}
		executionKind, consistent := executionKindFromBenchmarkJobs(jobs)
		if !consistent {
			writeError(w, http.StatusBadRequest, "benchmark_execution_kind_unavailable", "Benchmark experiment has no consistent execution kind")
			return
		}

		// eval_jobs were durably created with the experiment and are already
		// claimable. This endpoint is intentionally queue-only: it performs no
		// subprocess launch, token minting, or server-local prerequisite check.
		availability := unavailableWorkerAvailability(projectID, executionKind, "Benchmark worker registry is not configured.")
		if deps.BenchmarkWorkers != nil {
			var availabilityErr error
			availability, availabilityErr = deps.BenchmarkWorkers.Availability(r.Context(), projectID, executionKind)
			if availabilityErr != nil {
				writeInternalError(w, "benchmark_worker_availability_failed", "Failed to determine benchmark worker availability", "get benchmark worker availability", availabilityErr)
				return
			}
		}
		launch := benchmarkExperimentLaunchFromAvailability(availability, "Benchmark jobs are queued in durable eval_jobs for external workers to claim.")
		writeJSON(w, http.StatusAccepted, BenchmarkLaunchResponse{ExperimentID: experimentID, Launch: launch})
	}
}

func handleBenchmarkPreflight(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := benchmarkProjectAccess(w, r, deps)
		if !ok {
			return
		}
		var req benchmarkExperimentRequest
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		_, _, _, preflight, loaded := loadBenchmarkPreflight(r, deps, projectID, req)
		if !loaded {
			writeError(w, http.StatusBadRequest, "benchmark_preflight_failed", "Benchmark suite or dataset could not be loaded")
			return
		}
		writeJSON(w, http.StatusOK, preflight)
	}
}

func handleCreateBenchmarkExperiment(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := benchmarkProjectAccess(w, r, deps)
		if !ok {
			return
		}
		var req benchmarkExperimentRequest
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		plan, loaded := loadBenchmarkLaunchPlan(r, deps, projectID, req)
		if !loaded || !plan.preflight.Compatible {
			writeJSON(w, http.StatusConflict, plan.preflight)
			return
		}
		params := experiments.CreateExperimentHarnessParams{
			ProjectID:        projectID,
			DatasetID:        plan.suite.DatasetID,
			TargetName:       plan.target.Name,
			TargetVersion:    plan.target.Version,
			CaseIDs:          plan.caseIDs,
			BenchmarkSuiteID: plan.suite.ID,
			ExecutionKind:    plan.suite.ExecutionKind,
			ExecutionConfig:  plan.executionConfig,
			TrialsPerCase:    plan.trialsPerCase,
			ModelProvider:    plan.modelProvider,
			ModelName:        plan.modelName,
			PromptVersion:    plan.promptVersion,
			HarnessVersion:   plan.suite.Version,
			DatasetVersion:   plan.suite.Version,
		}
		experiment, err := deps.ExperimentsService.CreateExperimentWithHarness(r.Context(), projectID, plan.suite.DatasetID, "", params)
		if err != nil {
			writeInternalError(w, "create_benchmark_experiment_failed", "Failed to create benchmark experiment", "create benchmark experiment", err)
			return
		}
		writeJSON(w, http.StatusCreated, experiment)
	}
}

func handleBenchmarkExperimentReport(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := benchmarkProjectAccess(w, r, deps)
		if !ok {
			return
		}
		experimentID := chi.URLParam(r, "experimentID")
		experiment, err := deps.ExperimentsService.GetExperiment(r.Context(), experimentID)
		if err != nil || experiment.ProjectID != projectID {
			writeError(w, http.StatusNotFound, "benchmark_experiment_not_found", "Benchmark experiment not found")
			return
		}
		if experiment.BenchmarkSuiteID == "" {
			writeError(w, http.StatusBadRequest, "not_benchmark_experiment", "Experiment is not backed by a benchmark suite")
			return
		}
		suite, err := deps.BenchmarkSuitesService.GetSuite(r.Context(), projectID, experiment.BenchmarkSuiteID)
		if err != nil {
			writeError(w, http.StatusNotFound, "benchmark_suite_not_found", "Benchmark suite not found")
			return
		}
		caseByID := make(map[string]DatasetCase)
		if deps.DatasetsService != nil {
			cases, err := deps.DatasetsService.ListCases(r.Context(), suite.DatasetID)
			if err != nil {
				writeInternalError(w, "list_benchmark_cases_failed", "Failed to load benchmark task details", "list benchmark cases", err)
				return
			}
			for _, benchmarkCase := range cases {
				caseByID[benchmarkCase.ID] = benchmarkCase
			}
		}
		jobs, err := deps.ExperimentsService.ListExperimentJobs(r.Context(), experimentID)
		if err != nil {
			writeInternalError(w, "list_benchmark_jobs_failed", "Failed to load benchmark jobs", "list benchmark jobs", err)
			return
		}
		results, err := deps.ExperimentsService.ListExperimentResults(r.Context(), experimentID)
		if err != nil {
			writeInternalError(w, "list_benchmark_results_failed", "Failed to load benchmark results", "list benchmark results", err)
			return
		}
		resultByJob := make(map[string]EvalCaseResult, len(results))
		for _, result := range results {
			resultByJob[result.EvalJobID] = result
		}
		execution := map[string]int{"queued": 0, "running": 0, "succeeded": 0, "failed": 0, "total_jobs": len(jobs)}
		trials := make([]BenchmarkTrialResult, 0, len(jobs))
		officialMetrics := make(map[string]any)
		validTrials, invalidTrials, failedTrials, completedTrials := 0, 0, 0, 0
		latencies := make([]int64, 0, len(jobs))
		totalCost, costSamples := 0.0, 0
		failureAttribution := make(map[string]int)
		casePasses := map[string]bool{}
		caseFirst := map[string]bool{}
		scoreTotal, scoreCount := 0.0, 0
		for _, job := range jobs {
			execution[job.Status]++
			benchmarkCase, hasCase := caseByID[job.DatasetCaseID]
			taskID := job.DatasetCaseID
			question := ""
			if hasCase {
				taskID = benchmarkCaseTaskID(benchmarkCase)
				question = benchmarkCaseQuestion(benchmarkCase)
			}
			trial := BenchmarkTrialResult{ID: job.ID, EvalJobID: job.ID, DatasetCaseID: job.DatasetCaseID, TaskID: taskID, TrialIndex: job.TrialIndex, Seed: job.Seed, Status: job.Status, ErrorMessage: "", Question: question}
			if result, exists := resultByJob[job.ID]; exists {
				completedTrials++
				trial.RunID = result.RunID
				if result.LatencyMs > 0 {
					latency := result.LatencyMs
					trial.LatencyMs = &latency
					latencies = append(latencies, latency)
				}
				cost := result.Cost
				trial.Cost = &cost
				totalCost += cost
				costSamples++
				passed := result.Passed
				trial.Passed = &passed
				trial.ErrorMessage = result.Reason
				trial.Answer = benchmarkDetailText(result.ActualOutput)
				if result.Benchmark != nil {
					trial.Valid = result.Benchmark.Valid
					trial.ErrorType = result.Benchmark.ErrorType
					trial.OfficialMetrics = result.Benchmark.OfficialMetrics
					trial.Artifacts = result.Benchmark.ArtifactManifest
					var metrics map[string]any
					if json.Unmarshal(result.Benchmark.OfficialMetrics, &metrics) == nil {
						for key, value := range metrics {
							officialMetrics[key] = value
						}
					}
				}
				if trial.Valid == nil {
					valid := job.Status == "succeeded"
					trial.Valid = &valid
				}
				if !*trial.Valid {
					invalidTrials++
				}
				if trial.ErrorType != "" {
					failureAttribution[trial.ErrorType]++
				} else if !result.Passed {
					failureAttribution["verifier_failed"]++
				}
				if *trial.Valid {
					validTrials++
					score := result.Score
					trial.OfficialScore = &score
					scoreTotal += score
					scoreCount++
					if result.Passed {
						casePasses[job.DatasetCaseID] = true
						if job.TrialIndex == 0 {
							caseFirst[job.DatasetCaseID] = true
						}
					}
				}
			}
			if job.Status == "failed" {
				failedTrials++
				if _, hasResult := resultByJob[job.ID]; !hasResult {
					failureAttribution["job_failed"]++
				}
				if trial.ErrorMessage == "" {
					trial.ErrorMessage = job.Status
				}
			}
			trials = append(trials, trial)
		}
		passAtOne, passAtK := 0.0, 0.0
		taskCount := len(casePasses)
		if len(jobs) > 0 {
			unique := map[string]struct{}{}
			for _, job := range jobs {
				unique[job.DatasetCaseID] = struct{}{}
			}
			taskCount = len(unique)
			if taskCount > 0 {
				passAtOne = float64(len(caseFirst)) / float64(taskCount)
				passAtK = float64(len(casePasses)) / float64(taskCount)
			}
		}
		quality := map[string]any{"pass_at_1": passAtOne, "pass_at_k": passAtK, "valid_trials": validTrials, "invalid_trials": invalidTrials, "completed_trials": completedTrials, "total_trials": len(jobs), "failed_trials": failedTrials}
		if scoreCount > 0 {
			quality["official_score"] = scoreTotal / float64(scoreCount)
		}
		efficiency := map[string]any{"latency_samples": len(latencies), "cost_samples": costSamples}
		if len(latencies) > 0 {
			var latencyTotal int64
			for _, latency := range latencies {
				latencyTotal += latency
			}
			efficiency["avg_latency_ms"] = float64(latencyTotal) / float64(len(latencies))
			efficiency["p95_latency_ms"] = benchmarkP95Latency(latencies)
		}
		if costSamples > 0 {
			efficiency["total_cost"] = totalCost
			efficiency["avg_cost"] = totalCost / float64(costSamples)
		}
		var runner *BenchmarkExperimentLaunch
		executionKind, consistent := executionKindFromBenchmarkJobs(jobs)
		if !consistent {
			writeError(w, http.StatusBadRequest, "benchmark_execution_kind_unavailable", "Benchmark experiment has no consistent execution kind")
			return
		}
		availability := unavailableWorkerAvailability(projectID, executionKind, "Benchmark worker registry is not configured.")
		if deps.BenchmarkWorkers != nil {
			var availabilityErr error
			availability, availabilityErr = deps.BenchmarkWorkers.Availability(r.Context(), projectID, executionKind)
			if availabilityErr != nil {
				writeInternalError(w, "benchmark_worker_availability_failed", "Failed to determine benchmark worker availability", "get benchmark worker availability", availabilityErr)
				return
			}
		}
		launch := benchmarkExperimentLaunchFromAvailability(availability, "Benchmark jobs are durable and may be claimed by external workers.")
		runner = &launch

		// benchmark_config is an internal execution document and can contain
		// opaque profile/execution values. Never return it to the browser. The
		// public snapshot is a strict, identity-only projection used by the
		// experiment detail page.
		configSnapshot := benchmarkExperimentConfigSnapshot(experiment.BenchmarkConfig)
		experiment.BenchmarkConfig = ""
		writeJSON(w, http.StatusOK, BenchmarkExperimentReport{Experiment: experiment, Suite: suite, Execution: execution, Quality: quality, OfficialMetrics: officialMetrics, Efficiency: efficiency, FailureAttribution: failureAttribution, Trials: trials, Runner: runner, ConfigSnapshot: configSnapshot})
	}
}

func benchmarkP95Latency(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	index := int(math.Ceil(0.95*float64(len(sorted)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sorted) {
		index = len(sorted) - 1
	}
	return sorted[index]
}

func unavailableWorkerAvailability(projectID, executionKind, reason string) benchmarks.WorkerAvailability {
	return benchmarks.WorkerAvailability{
		ProjectID:     projectID,
		ExecutionKind: executionKind,
		State:         "unavailable",
		Reason:        reason,
		Blockers:      []string{reason},
	}
}

func benchmarkExperimentLaunchFromAvailability(availability benchmarks.WorkerAvailability, message string) BenchmarkExperimentLaunch {
	launch := BenchmarkExperimentLaunch{
		Status:             "queued",
		Available:          availability.Available,
		Message:            message,
		ExecutionKind:      availability.ExecutionKind,
		RegisteredWorkers:  availability.RegisteredWorkers,
		MatchingWorkers:    availability.MatchingWorkers,
		AdvertisedCapacity: availability.AdvertisedCapacity,
	}
	if !availability.Available {
		launch.UnavailableReason = availability.Reason
	}
	return launch
}

func executionKindFromBenchmarkJobs(jobs []EvalJob) (string, bool) {
	if len(jobs) == 0 || jobs[0].ExecutionKind == "" {
		return "", false
	}
	executionKind := jobs[0].ExecutionKind
	for _, job := range jobs[1:] {
		if job.ExecutionKind != executionKind {
			return "", false
		}
	}
	return executionKind, true
}

var benchmarkSnapshotSecretPattern = regexp.MustCompile(`(?i)(?:api[_-]?key|secret|password|authorization|bearer|token)\s*[:=]|^sk-[a-z0-9_-]{16,}$|^akia[0-9a-z]{16}$|^eyj[a-z0-9_-]{16,}\.[a-z0-9_-]{16,}\.[a-z0-9_-]{16,}$`)

// benchmarkExperimentConfigSnapshot converts the persisted configuration into
// the deliberately small public view defined in handlers.go. json.Unmarshal
// into this local wire type ignores every config and override field by design.
// A malformed or non-template legacy config simply has no public snapshot; it
// must not make a report unavailable.
func benchmarkExperimentConfigSnapshot(raw string) *BenchmarkExperimentConfigSnapshot {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var stored struct {
		SchemaVersion   string `json:"schema_version"`
		CatalogRevision string `json:"catalog_revision"`
		RunTemplateID   string `json:"run_template_id"`
		TrialsPerCase   int    `json:"trials_per_case"`
		AgentArtifact   struct {
			ID           string `json:"id"`
			Framework    string `json:"framework"`
			Image        string `json:"image"`
			ImageDigest  string `json:"image_digest"`
			SourceCommit string `json:"source_commit"`
		} `json:"agent_artifact"`
		ModelProfile struct {
			ID string `json:"id"`
		} `json:"model_profile"`
		AgentProfile struct {
			ID        string `json:"id"`
			Framework string `json:"framework"`
		} `json:"agent_profile"`
		ExecutionProfile struct {
			ID string `json:"id"`
		} `json:"execution_profile"`
		TaskSelection struct {
			Mode             string   `json:"mode"`
			Include          []string `json:"include"`
			Exclude          []string `json:"exclude"`
			Count            int      `json:"count"`
			Seed             int64    `json:"seed"`
			SourceExperiment string   `json:"source_experiment"`
		} `json:"task_selection"`
		SelectedCaseIDs []string `json:"selected_case_ids"`
	}
	if err := json.Unmarshal([]byte(raw), &stored); err != nil || stored.SchemaVersion != "agentlab.benchmark-execution.v1" {
		return nil
	}

	snapshot := &BenchmarkExperimentConfigSnapshot{
		SchemaVersion:   publicBenchmarkSnapshotString(stored.SchemaVersion, 128),
		CatalogRevision: publicBenchmarkSnapshotString(stored.CatalogRevision, 256),
		RunTemplateID:   publicBenchmarkSnapshotString(stored.RunTemplateID, 256),
		TrialsPerCase:   publicBenchmarkSnapshotInt(stored.TrialsPerCase),
		AgentArtifact: BenchmarkExperimentArtifactSnapshot{
			ID:           publicBenchmarkSnapshotString(stored.AgentArtifact.ID, 256),
			Framework:    publicBenchmarkSnapshotString(stored.AgentArtifact.Framework, 128),
			Image:        publicBenchmarkSnapshotString(stored.AgentArtifact.Image, 512),
			ImageDigest:  publicBenchmarkSnapshotString(stored.AgentArtifact.ImageDigest, 128),
			SourceCommit: publicBenchmarkSnapshotString(stored.AgentArtifact.SourceCommit, 256),
		},
		Profiles: BenchmarkExperimentProfileSnapshots{
			Model:     BenchmarkExperimentProfileSnapshot{ID: publicBenchmarkSnapshotString(stored.ModelProfile.ID, 256)},
			Agent:     BenchmarkExperimentProfileSnapshot{ID: publicBenchmarkSnapshotString(stored.AgentProfile.ID, 256), Framework: publicBenchmarkSnapshotString(stored.AgentProfile.Framework, 128)},
			Execution: BenchmarkExperimentProfileSnapshot{ID: publicBenchmarkSnapshotString(stored.ExecutionProfile.ID, 256)},
		},
		TaskSelection: BenchmarkExperimentTaskSelectionSnapshot{
			Mode:             publicBenchmarkSnapshotString(stored.TaskSelection.Mode, 64),
			Include:          publicBenchmarkSnapshotStrings(stored.TaskSelection.Include, 256),
			Exclude:          publicBenchmarkSnapshotStrings(stored.TaskSelection.Exclude, 256),
			Count:            publicBenchmarkSnapshotInt(stored.TaskSelection.Count),
			Seed:             stored.TaskSelection.Seed,
			SourceExperiment: publicBenchmarkSnapshotString(stored.TaskSelection.SourceExperiment, 256),
		},
		SelectedCaseIDs: publicBenchmarkSnapshotStrings(stored.SelectedCaseIDs, 256),
	}
	if snapshot.CatalogRevision == "" && snapshot.RunTemplateID == "" {
		return nil
	}
	return snapshot
}

func publicBenchmarkSnapshotInt(value int) int {
	if value < 0 || value > 1_000_000 {
		return 0
	}
	return value
}

func publicBenchmarkSnapshotString(value string, maxLength int) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > maxLength || benchmarkSnapshotSecretPattern.MatchString(value) {
		return ""
	}
	return value
}

func publicBenchmarkSnapshotStrings(values []string, maxLength int) []string {
	if len(values) == 0 || len(values) > 10_000 {
		return nil
	}
	public := make([]string, 0, len(values))
	for _, value := range values {
		if value = publicBenchmarkSnapshotString(value, maxLength); value != "" {
			public = append(public, value)
		}
	}
	if len(public) == 0 {
		return nil
	}
	return public
}

var benchmarkDetailSecretAssignmentPattern = regexp.MustCompile(`(?i)(["']?[a-z0-9_.-]*(?:api[_-]?key|token|secret|password|passwd|credential|authorization)[a-z0-9_.-]*["']?\s*[:=]\s*)(?:["']?)[^\s,;}&"']+(?:["']?)`)
var benchmarkDetailSensitiveQueryPattern = regexp.MustCompile(`(?i)([?&](?:api[_-]?key|token|secret|password|passwd|credential)=)[^&\s]+`)

func benchmarkCaseTaskID(benchmarkCase DatasetCase) string {
	metadata, err := rawJSONObject(benchmarkCase.Metadata)
	if err != nil {
		return benchmarkCase.ID
	}
	taskID, ok := metadata["task_id"].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return benchmarkCase.ID
	}
	return publicBenchmarkDetailText(taskID, 256)
}

func benchmarkCaseQuestion(benchmarkCase DatasetCase) string {
	var input struct {
		Instruction string `json:"instruction"`
	}
	if err := json.Unmarshal(benchmarkCase.Input, &input); err == nil && strings.TrimSpace(input.Instruction) != "" {
		return benchmarkDetailText(input.Instruction)
	}
	return benchmarkDetailText(string(benchmarkCase.Input))
}

func benchmarkDetailText(value string) string {
	return publicBenchmarkDetailText(value, 24_000)
}

func publicBenchmarkDetailText(value string, limit int) string {
	value = strings.TrimSpace(value)
	value = benchmarkDetailSecretAssignmentPattern.ReplaceAllString(value, "$1[REDACTED]")
	value = benchmarkDetailSensitiveQueryPattern.ReplaceAllString(value, "$1[REDACTED]")
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "\n… [truncated]"
}
