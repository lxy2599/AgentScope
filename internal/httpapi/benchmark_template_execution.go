package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"sort"
	"strings"

	"AgentScope/internal/benchmarks"
)

// benchmarkLaunchPlan is produced once per preflight/create request. Template
// launches carry a fully resolved, immutable execution snapshot; the worker
// must consume that snapshot rather than rereading mutable catalog state.
type benchmarkLaunchPlan struct {
	suite           BenchmarkSuite
	target          benchmarks.Target
	caseIDs         []string
	trialsPerCase   int
	executionConfig string
	preflight       BenchmarkPreflight
	modelProvider   string
	modelName       string
	promptVersion   string
}

func loadBenchmarkLaunchPlan(r *http.Request, deps Dependencies, projectID string, req benchmarkExperimentRequest) (benchmarkLaunchPlan, bool) {
	if strings.TrimSpace(req.RunTemplateID) != "" {
		return loadRunTemplateLaunchPlan(r.Context(), deps, projectID, req)
	}
	return loadLegacyBenchmarkLaunchPlan(r.Context(), deps, projectID, req)
}

func loadLegacyBenchmarkLaunchPlan(ctx context.Context, deps Dependencies, projectID string, req benchmarkExperimentRequest) (benchmarkLaunchPlan, bool) {
	suite, err := deps.BenchmarkSuitesService.GetSuite(ctx, projectID, req.SuiteID)
	if err != nil {
		return benchmarkLaunchPlan{}, false
	}
	target, found := benchmarks.FindTarget(req.TargetName, req.TargetVersion)
	cases, err := deps.DatasetsService.ListCases(ctx, suite.DatasetID)
	if err != nil {
		return benchmarkLaunchPlan{}, false
	}
	caseIDs := req.CaseIDs
	if len(caseIDs) == 0 {
		caseIDs = datasetCaseIDs(cases)
	}
	preflight := buildBenchmarkPreflight(ctx, deps, projectID, suite, target, found, caseIDs, req.TrialsPerCase)
	return benchmarkLaunchPlan{
		suite: suite, target: target, caseIDs: caseIDs, trialsPerCase: req.TrialsPerCase,
		executionConfig: string(req.BenchmarkConfig), preflight: preflight,
	}, true
}

func loadRunTemplateLaunchPlan(ctx context.Context, deps Dependencies, projectID string, req benchmarkExperimentRequest) (benchmarkLaunchPlan, bool) {
	if hasLegacyBenchmarkLaunchFields(req) {
		return benchmarkLaunchPlan{preflight: BenchmarkPreflight{Blockers: []string{"run_template_id cannot be combined with legacy benchmark launch fields"}}}, true
	}
	if deps.BenchmarkCatalog == nil {
		return benchmarkLaunchPlan{preflight: BenchmarkPreflight{Blockers: []string{"benchmark catalog is not configured"}}}, true
	}
	catalog, err := deps.BenchmarkCatalog.ReadCatalog(ctx, projectID)
	if err != nil {
		return benchmarkLaunchPlan{}, false
	}
	resolved, err := benchmarks.ResolveRunTemplate(catalog, req.RunTemplateID)
	if err != nil {
		return benchmarkLaunchPlan{preflight: BenchmarkPreflight{RunTemplateID: req.RunTemplateID, Blockers: []string{"run template is not available in the synchronized catalog"}}}, true
	}
	suite, err := benchmarkSuiteByKey(ctx, deps.BenchmarkSuitesService, projectID, resolved.Template.SuiteID)
	if err != nil {
		return benchmarkLaunchPlan{preflight: BenchmarkPreflight{CatalogRevision: resolved.CatalogRevision, RunTemplateID: resolved.Template.ID, AgentArtifactID: resolved.Artifact.ID, Blockers: []string{"the template suite has not been provisioned for this project"}}}, true
	}
	cases, err := deps.DatasetsService.ListCases(ctx, suite.DatasetID)
	if err != nil {
		return benchmarkLaunchPlan{}, false
	}
	selection, err := benchmarks.ParseTaskSelection(resolved.Template.TaskSelection)
	if err != nil {
		return benchmarkLaunchPlan{}, false // Synced catalogs are validated; this signals corrupt state.
	}
	caseIDs, selectionErr := selectRunTemplateCases(ctx, deps, projectID, suite, selection, cases)
	target := benchmarks.Target{
		Name:         resolved.Artifact.ID,
		Version:      resolved.Artifact.ImageDigest,
		Framework:    resolved.Artifact.Framework,
		Status:       "catalog-pinned",
		Capabilities: append([]string(nil), resolved.Artifact.Capabilities...),
	}
	preflight := buildBenchmarkPreflight(ctx, deps, projectID, suite, target, true, caseIDs, resolved.Template.TrialsPerCase)
	preflight.CatalogRevision = resolved.CatalogRevision
	preflight.RunTemplateID = resolved.Template.ID
	preflight.AgentArtifactID = resolved.Artifact.ID
	if selectionErr != nil {
		preflight.Blockers = append(preflight.Blockers, "template task selection could not be resolved for this project")
		preflight.Compatible = false
		return benchmarkLaunchPlan{suite: suite, target: target, preflight: preflight}, true
	}
	if len(caseIDs) == 0 {
		preflight.Blockers = append(preflight.Blockers, "template task selection resolved to no dataset cases")
		preflight.Compatible = false
		return benchmarkLaunchPlan{suite: suite, target: target, preflight: preflight}, true
	}
	executionConfig, err := immutableTemplateExecutionConfig(resolved, suite, caseIDs)
	if err != nil {
		return benchmarkLaunchPlan{}, false
	}
	return benchmarkLaunchPlan{
		suite: suite, target: target, caseIDs: caseIDs, trialsPerCase: resolved.Template.TrialsPerCase,
		executionConfig: executionConfig, preflight: preflight,
		modelProvider: profileString(resolved.ModelProfile, "provider"),
		modelName:     profileString(resolved.ModelProfile, "model"),
		promptVersion: profileString(resolved.AgentProfile, "prompt_version"),
	}, true
}

func hasLegacyBenchmarkLaunchFields(req benchmarkExperimentRequest) bool {
	return strings.TrimSpace(req.SuiteID) != "" || strings.TrimSpace(req.TargetName) != "" || strings.TrimSpace(req.TargetVersion) != "" || req.TrialsPerCase != 0 || len(req.CaseIDs) != 0 || len(bytes.TrimSpace(req.BenchmarkConfig)) != 0
}

func buildBenchmarkPreflight(ctx context.Context, deps Dependencies, projectID string, suite BenchmarkSuite, target benchmarks.Target, targetFound bool, caseIDs []string, trialsPerCase int) BenchmarkPreflight {
	matched, missing := benchmarks.MatchCapabilities(suite.RequiredCapabilities, target.Capabilities)
	preflight := BenchmarkPreflight{
		ExecutionKind: suite.ExecutionKind, EstimatedTasks: len(caseIDs), EstimatedJobs: len(caseIDs) * trialsPerCase,
		MatchedCapabilities: matched, MissingCapabilities: missing,
	}
	if trialsPerCase < 1 {
		preflight.Blockers = append(preflight.Blockers, "trials_per_case must be positive")
	}
	if !targetFound {
		preflight.Blockers = append(preflight.Blockers, "target is not registered")
	}
	if len(missing) > 0 {
		preflight.Blockers = append(preflight.Blockers, "target is missing required capabilities")
	}
	if deps.BenchmarkWorkers == nil {
		preflight.Warnings = append(preflight.Warnings, "Benchmark worker registry is not configured; jobs can still be created and will wait in durable eval_jobs.")
	} else {
		worker, availabilityErr := deps.BenchmarkWorkers.Availability(ctx, projectID, suite.ExecutionKind)
		if availabilityErr != nil {
			preflight.Warnings = append(preflight.Warnings, "Benchmark worker availability could not be determined; jobs can still be created and will wait in durable eval_jobs.")
		} else {
			preflight.Runner = &worker
			preflight.RunnerAvailable = worker.Available
			if worker.Available {
				preflight.Warnings = append(preflight.Warnings, "Benchmark jobs will be queued for an already registered external worker; AgentScope will not start a local runner.")
			} else {
				preflight.Warnings = append(preflight.Warnings, "No matching worker is registered yet; benchmark jobs may still be created and will remain queued until a worker registers.")
			}
		}
	}
	preflight.Compatible = len(preflight.Blockers) == 0
	return preflight
}

func benchmarkSuiteByKey(ctx context.Context, service BenchmarkSuitesService, projectID, benchmarkKey string) (BenchmarkSuite, error) {
	suites, err := service.ListSuites(ctx, projectID)
	if err != nil {
		return BenchmarkSuite{}, err
	}
	var matches []BenchmarkSuite
	for _, suite := range suites {
		if suite.BenchmarkKey == benchmarkKey || suiteCatalogID(suite) == benchmarkKey {
			matches = append(matches, suite)
		}
	}
	if len(matches) != 1 {
		return BenchmarkSuite{}, errors.New("catalog suite key must resolve to exactly one project benchmark suite")
	}
	return matches[0], nil
}

// suiteCatalogID optionally bridges a catalog suite ID that cannot be used as
// the database benchmark_key (for example an upstream identifier containing a
// dot). The explicit metadata key avoids lossy string transformations.
func suiteCatalogID(suite BenchmarkSuite) string {
	metadata, err := rawJSONObject(suite.Metadata)
	if err != nil {
		return ""
	}
	catalogID, _ := metadata["catalog_suite_id"].(string)
	return catalogID
}

func datasetCaseIDs(cases []DatasetCase) []string {
	ordered := append([]DatasetCase(nil), cases...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].SortOrder == ordered[j].SortOrder {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].SortOrder < ordered[j].SortOrder
	})
	ids := make([]string, 0, len(ordered))
	for _, benchmarkCase := range ordered {
		ids = append(ids, benchmarkCase.ID)
	}
	return ids
}

type templateCaseReference struct {
	databaseCaseID string
	taskID         string
}

// templateCaseReferences keeps the catalog's stable upstream task IDs separate
// from AgentScope's generated dataset_case UUIDs. A case may carry its upstream
// identity in metadata.task_id; old datasets without that field remain
// compatible because their database ID is used as the task ID.
func templateCaseReferences(cases []DatasetCase) ([]templateCaseReference, error) {
	ordered := append([]DatasetCase(nil), cases...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].SortOrder == ordered[j].SortOrder {
			return ordered[i].ID < ordered[j].ID
		}
		return ordered[i].SortOrder < ordered[j].SortOrder
	})
	refs := make([]templateCaseReference, 0, len(ordered))
	seenTaskIDs := make(map[string]struct{}, len(ordered))
	for _, benchmarkCase := range ordered {
		taskID := benchmarkCase.ID
		metadata, err := rawJSONObject(benchmarkCase.Metadata)
		if err != nil {
			return nil, fmt.Errorf("decode metadata for dataset case %q: %w", benchmarkCase.ID, err)
		}
		if configured, ok := metadata["task_id"].(string); ok && strings.TrimSpace(configured) != "" {
			taskID = configured
		}
		if _, duplicate := seenTaskIDs[taskID]; duplicate {
			return nil, fmt.Errorf("duplicate benchmark task_id %q in dataset", taskID)
		}
		seenTaskIDs[taskID] = struct{}{}
		refs = append(refs, templateCaseReference{databaseCaseID: benchmarkCase.ID, taskID: taskID})
	}
	return refs, nil
}

func selectRunTemplateCases(ctx context.Context, deps Dependencies, projectID string, suite BenchmarkSuite, selection benchmarks.TaskSelection, cases []DatasetCase) ([]string, error) {
	refs, err := templateCaseReferences(cases)
	if err != nil {
		return nil, err
	}
	byTaskID := make(map[string]templateCaseReference, len(refs))
	for _, ref := range refs {
		byTaskID[ref.taskID] = ref
	}
	excluded := make(map[string]struct{}, len(selection.Exclude))
	for _, taskID := range selection.Exclude {
		if _, found := byTaskID[taskID]; !found {
			return nil, fmt.Errorf("excluded task %q is not in the suite dataset", taskID)
		}
		excluded[taskID] = struct{}{}
	}
	withoutExcluded := func(values []templateCaseReference) []templateCaseReference {
		selected := make([]templateCaseReference, 0, len(values))
		for _, ref := range values {
			if _, skip := excluded[ref.taskID]; !skip {
				selected = append(selected, ref)
			}
		}
		return selected
	}
	caseIDs := func(values []templateCaseReference) []string {
		selected := make([]string, 0, len(values))
		for _, ref := range values {
			selected = append(selected, ref.databaseCaseID)
		}
		return selected
	}
	switch selection.Mode {
	case "all":
		return caseIDs(refs), nil
	case "ids":
		selected := make([]templateCaseReference, 0, len(selection.Include))
		for _, taskID := range selection.Include {
			ref, found := byTaskID[taskID]
			if !found {
				return nil, fmt.Errorf("selected task %q is not in the suite dataset", taskID)
			}
			if _, skip := excluded[taskID]; !skip {
				selected = append(selected, ref)
			}
		}
		return caseIDs(selected), nil
	case "sample":
		candidates := withoutExcluded(refs)
		if selection.Count > len(candidates) {
			return nil, errors.New("sample count exceeds selected suite dataset cases")
		}
		permutation := rand.New(rand.NewSource(selection.Seed)).Perm(len(candidates)) // #nosec G404 -- catalog pins deterministic benchmark sampling.
		selected := make([]templateCaseReference, selection.Count)
		for i, index := range permutation[:selection.Count] {
			selected[i] = candidates[index]
		}
		return caseIDs(selected), nil
	case "previous_failures":
		source, err := deps.ExperimentsService.GetExperiment(ctx, selection.SourceExperiment)
		if err != nil || source.ProjectID != projectID || source.DatasetID != suite.DatasetID || source.BenchmarkSuiteID != suite.ID {
			return nil, errors.New("source experiment is not a compatible project benchmark experiment")
		}
		results, err := deps.ExperimentsService.ListExperimentResults(ctx, selection.SourceExperiment)
		if err != nil {
			return nil, err
		}
		failed := make(map[string]struct{}, len(results))
		for _, result := range results {
			if !result.Passed {
				failed[result.DatasetCaseID] = struct{}{}
			}
		}
		selected := make([]templateCaseReference, 0, len(failed))
		for _, ref := range refs {
			if _, include := failed[ref.databaseCaseID]; include {
				selected = append(selected, ref)
			}
		}
		return caseIDs(selected), nil
	default:
		return nil, errors.New("unsupported task selection mode")
	}
}

// immutableTemplateExecutionConfig copies the entire resolved catalog
// composition into the durable experiment and every eval_job. A later catalog
// sync therefore cannot change a queued or running benchmark.
func immutableTemplateExecutionConfig(resolved benchmarks.ResolvedRunTemplate, suite BenchmarkSuite, selectedCaseIDs []string) (string, error) {
	suiteDefaults, err := rawJSONObject(suite.DefaultExecutionConfig)
	if err != nil {
		return "", fmt.Errorf("decode suite default execution config: %w", err)
	}
	executionProfile, err := rawJSONObject(resolved.ExecutionProfile.Config)
	if err != nil {
		return "", fmt.Errorf("decode execution profile: %w", err)
	}
	overrides, err := rawJSONObject(resolved.Template.ExecutionOverrides)
	if err != nil {
		return "", fmt.Errorf("decode run template execution overrides: %w", err)
	}
	effectiveExecution := mergeJSONObjectMaps(suiteDefaults, executionProfile, overrides)
	modelConfig, err := rawJSONObject(resolved.ModelProfile.Config)
	if err != nil {
		return "", fmt.Errorf("decode model profile: %w", err)
	}
	agentConfig, err := rawJSONObject(resolved.AgentProfile.Config)
	if err != nil {
		return "", fmt.Errorf("decode agent profile: %w", err)
	}
	taskSelection, err := rawJSONObject(resolved.Template.TaskSelection)
	if err != nil {
		return "", fmt.Errorf("decode task selection: %w", err)
	}

	// Retain effective execution values at the top level for the existing Worker
	// contract, then add a namespaced complete identity snapshot for new Harbor
	// adapters.
	snapshot := mergeJSONObjectMaps(effectiveExecution)
	snapshot["schema_version"] = "agentlab.benchmark-execution.v1"
	snapshot["catalog_revision"] = resolved.CatalogRevision
	snapshot["run_template_id"] = resolved.Template.ID
	snapshot["trials_per_case"] = resolved.Template.TrialsPerCase
	snapshot["selected_case_ids"] = append([]string(nil), selectedCaseIDs...)
	snapshot["task_selection"] = taskSelection
	snapshot["effective_execution"] = effectiveExecution
	snapshot["suite"] = map[string]any{
		"catalog_suite_id":  resolved.Template.SuiteID,
		"database_suite_id": suite.ID,
		"benchmark_key":     suite.BenchmarkKey,
		"benchmark_version": suite.Version,
		"harness_kind":      suite.HarnessKind,
		"execution_kind":    suite.ExecutionKind,
		"source_commit":     suite.SourceCommit,
		"default_execution": suiteDefaults,
	}
	snapshot["agent_artifact"] = map[string]any{
		"id":                   resolved.Artifact.ID,
		"framework":            resolved.Artifact.Framework,
		"image":                resolved.Artifact.Image,
		"image_digest":         resolved.Artifact.ImageDigest,
		"source_commit":        resolved.Artifact.SourceCommit,
		"task_runner_protocol": resolved.Artifact.TaskRunnerProtocol,
		"capabilities":         append([]string(nil), resolved.Artifact.Capabilities...),
	}
	snapshot["model_profile"] = map[string]any{"id": resolved.ModelProfile.ID, "config": modelConfig}
	snapshot["agent_profile"] = map[string]any{"id": resolved.AgentProfile.ID, "framework": resolved.AgentProfile.Framework, "config": agentConfig}
	snapshot["execution_profile"] = map[string]any{"id": resolved.ExecutionProfile.ID, "config": executionProfile}

	encoded, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	normalized, err := benchmarks.NormalizeJSONObject(encoded, "template_execution_config")
	if err != nil {
		return "", err
	}
	return string(normalized), nil
}

func rawJSONObject(raw json.RawMessage) (map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}
	normalized, err := benchmarks.NormalizeJSONObject(raw, "catalog object")
	if err != nil {
		return nil, err
	}
	var value map[string]any
	if err := json.Unmarshal(normalized, &value); err != nil || value == nil {
		return nil, errors.New("must be a JSON object")
	}
	return value, nil
}

func mergeJSONObjectMaps(maps ...map[string]any) map[string]any {
	merged := make(map[string]any)
	for _, values := range maps {
		for key, value := range values {
			merged[key] = value
		}
	}
	return merged
}

func profileString(profile benchmarks.Profile, key string) string {
	config, err := rawJSONObject(profile.Config)
	if err != nil {
		return ""
	}
	value, _ := config[key].(string)
	return value
}
