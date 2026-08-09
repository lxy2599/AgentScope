package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"AgentScope/internal/benchlauncher"
	"AgentScope/internal/benchmarks"
	"AgentScope/internal/experiments"
	"github.com/go-chi/chi/v5"
)

type benchmarkExperimentRequest struct {
	SuiteID         string          `json:"suite_id"`
	TargetName      string          `json:"target_name"`
	TargetVersion   string          `json:"target_version,omitempty"`
	TrialsPerCase   int             `json:"trials_per_case"`
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
	suite, err := deps.BenchmarkSuitesService.GetSuite(r.Context(), projectID, req.SuiteID)
	if err != nil {
		return BenchmarkSuite{}, benchmarks.Target{}, nil, BenchmarkPreflight{}, false
	}
	target, found := benchmarks.FindTarget(req.TargetName, req.TargetVersion)
	cases, err := deps.DatasetsService.ListCases(r.Context(), suite.DatasetID)
	if err != nil {
		return BenchmarkSuite{}, benchmarks.Target{}, nil, BenchmarkPreflight{}, false
	}
	caseIDs := req.CaseIDs
	if len(caseIDs) == 0 {
		caseIDs = make([]string, 0, len(cases))
		for _, benchmarkCase := range cases {
			caseIDs = append(caseIDs, benchmarkCase.ID)
		}
	}
	matched, missing := benchmarks.MatchCapabilities(suite.RequiredCapabilities, target.Capabilities)
	preflight := BenchmarkPreflight{ExecutionKind: suite.ExecutionKind, EstimatedTasks: len(caseIDs), EstimatedJobs: len(caseIDs) * req.TrialsPerCase, MatchedCapabilities: matched, MissingCapabilities: missing}
	if req.TrialsPerCase < 1 {
		preflight.Blockers = append(preflight.Blockers, "trials_per_case must be positive")
	}
	if !found {
		preflight.Blockers = append(preflight.Blockers, "target is not registered")
	}
	if len(missing) > 0 {
		preflight.Blockers = append(preflight.Blockers, "target is missing required capabilities")
	}
	if deps.BenchmarkLauncher == nil {
		preflight.Blockers = append(preflight.Blockers, "Local benchmark runner launcher is not configured.")
	} else {
		runner := deps.BenchmarkLauncher.Availability(r.Context(), projectID, suite.ExecutionKind, "")
		preflight.Runner = &runner
		preflight.RunnerAvailable = runner.Available
		preflight.Blockers = append(preflight.Blockers, runner.Blockers...)
		if runner.Available && runner.State == "idle" {
			preflight.Warnings = append(preflight.Warnings, "A local runner will start only after an administrator launches this benchmark experiment.")
		}
	}
	preflight.Compatible = len(preflight.Blockers) == 0
	return suite, target, caseIDs, preflight, true
}

func handleLaunchBenchmarkExperiment(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, userID, ok := benchmarkProjectAdminAccess(w, r, deps)
		if !ok {
			return
		}
		if deps.BenchmarkLauncher == nil {
			writeError(w, http.StatusServiceUnavailable, "benchmark_launcher_unavailable", "Local benchmark runner launcher is not configured")
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
		executionKind, ok := executionKindFromBenchmarkJobs(jobs)
		if !ok {
			writeError(w, http.StatusBadRequest, "benchmark_execution_kind_unavailable", "Benchmark experiment has no consistent execution kind")
			return
		}
		launch, err := deps.BenchmarkLauncher.Launch(r.Context(), benchlauncher.LaunchRequest{
			ProjectID:     projectID,
			UserID:        userID,
			ExperimentID:  experimentID,
			ExecutionKind: executionKind,
		})
		if err != nil {
			if errors.Is(err, benchlauncher.ErrUnavailable) {
				writeJSON(w, http.StatusConflict, BenchmarkLaunchResponse{ExperimentID: experimentID, Launch: launch})
				return
			}
			writeInternalError(w, "launch_benchmark_runner_failed", "Failed to launch local benchmark runner", "launch benchmark runner", err)
			return
		}
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
		suite, target, caseIDs, preflight, loaded := loadBenchmarkPreflight(r, deps, projectID, req)
		if !loaded || !preflight.Compatible {
			writeJSON(w, http.StatusConflict, preflight)
			return
		}
		experiment, err := deps.ExperimentsService.CreateExperimentWithHarness(r.Context(), projectID, suite.DatasetID, "", experiments.CreateExperimentHarnessParams{ProjectID: projectID, DatasetID: suite.DatasetID, TargetName: target.Name, TargetVersion: target.Version, CaseIDs: caseIDs, BenchmarkSuiteID: suite.ID, ExecutionKind: suite.ExecutionKind, ExecutionConfig: string(req.BenchmarkConfig), TrialsPerCase: req.TrialsPerCase})
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
		validTrials, failedTrials := 0, 0
		casePasses := map[string]bool{}
		caseFirst := map[string]bool{}
		scoreTotal, scoreCount := 0.0, 0
		for _, job := range jobs {
			execution[job.Status]++
			trial := BenchmarkTrialResult{ID: job.ID, EvalJobID: job.ID, DatasetCaseID: job.DatasetCaseID, TaskID: job.DatasetCaseID, TrialIndex: job.TrialIndex, Seed: job.Seed, Status: job.Status, ErrorMessage: ""}
			if result, exists := resultByJob[job.ID]; exists {
				trial.RunID = result.RunID
				if result.LatencyMs > 0 {
					latency := result.LatencyMs
					trial.LatencyMs = &latency
				}
				passed := result.Passed
				trial.Passed = &passed
				trial.ErrorMessage = result.Reason
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
					valid := job.Status == "succeeded" || job.Status == "failed"
					trial.Valid = &valid
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
		quality := map[string]any{"pass_at_1": passAtOne, "pass_at_k": passAtK, "valid_trials": validTrials, "total_trials": len(jobs), "failed_trials": failedTrials}
		if scoreCount > 0 {
			quality["official_score"] = scoreTotal / float64(scoreCount)
		}
		var runner *benchlauncher.Status
		if deps.BenchmarkLauncher != nil {
			executionKind, consistent := executionKindFromBenchmarkJobs(jobs)
			if !consistent {
				writeError(w, http.StatusBadRequest, "benchmark_execution_kind_unavailable", "Benchmark experiment has no consistent execution kind")
				return
			}
			status := deps.BenchmarkLauncher.Availability(r.Context(), projectID, executionKind, experimentID)
			runner = &status
		}
		writeJSON(w, http.StatusOK, BenchmarkExperimentReport{Experiment: experiment, Suite: suite, Execution: execution, Quality: quality, OfficialMetrics: officialMetrics, Trials: trials, Runner: runner})
	}
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
