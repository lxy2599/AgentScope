package httpapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"AgentScope/internal/benchmarks"
	"AgentScope/internal/experiments"
	"github.com/go-chi/chi/v5"
)

func TestBenchmarkWorkerRegistrationAndProjectReadAPI(t *testing.T) {
	registry := benchmarks.NewInMemoryWorkerRegistry(time.Minute)
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("runner-token", "project-1", []string{"runner:write"})

	registration := httptest.NewRequest(http.MethodPost, "/runner/benchmark-workers/register", strings.NewReader(`{"worker_name":"harbor_worker","execution_kinds":["harbor"],"capacity":2,"version":"v1"}`))
	registration.Header.Set("Content-Type", "application/json")
	registration.Header.Set("Authorization", "Bearer runner-token")
	response := httptest.NewRecorder()
	handleRegisterBenchmarkWorker(Dependencies{TokenAuth: tokenAuth, BenchmarkWorkers: registry}).ServeHTTP(response, registration)
	if response.Code != http.StatusOK {
		t.Fatalf("register status = %d; body=%s", response.Code, response.Body.String())
	}

	projectAuth := newFakeProjectAuth()
	projectAuth.allow("member-1", "project-1")
	listRequest := projectRequest(http.MethodGet, "/api/projects/project-1/benchmark-workers", "project-1", "member-1")
	listResponse := httptest.NewRecorder()
	handleListBenchmarkWorkers(Dependencies{ProjectAuth: projectAuth, BenchmarkWorkers: registry}).ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d; body=%s", listResponse.Code, listResponse.Body.String())
	}
	var workers []benchmarks.Worker
	if err := json.Unmarshal(listResponse.Body.Bytes(), &workers); err != nil {
		t.Fatalf("decode workers: %v", err)
	}
	if len(workers) != 1 || workers[0].Name != "harbor_worker" || workers[0].Capacity != 2 {
		t.Fatalf("workers = %#v", workers)
	}

	availabilityRequest := projectRequest(http.MethodGet, "/api/projects/project-1/benchmark-workers/harbor/availability", "project-1", "member-1")
	routeContext := chi.RouteContext(availabilityRequest.Context())
	routeContext.URLParams.Add("executionKind", "harbor")
	availabilityResponse := httptest.NewRecorder()
	handleGetBenchmarkWorkerAvailability(Dependencies{ProjectAuth: projectAuth, BenchmarkWorkers: registry}).ServeHTTP(availabilityResponse, availabilityRequest)
	if availabilityResponse.Code != http.StatusOK {
		t.Fatalf("availability status = %d; body=%s", availabilityResponse.Code, availabilityResponse.Body.String())
	}
	var availability benchmarks.WorkerAvailability
	if err := json.Unmarshal(availabilityResponse.Body.Bytes(), &availability); err != nil {
		t.Fatalf("decode availability: %v", err)
	}
	if !availability.Available || availability.MatchingWorkers != 1 || availability.AdvertisedCapacity != 2 {
		t.Fatalf("availability = %#v", availability)
	}
}

func TestBenchmarkCatalogReadAPIsAreReadOnlyAndProjectScoped(t *testing.T) {
	catalog := benchmarks.NewStaticCatalogReader()
	catalog.Set("project-1", benchmarks.Catalog{
		Revision:     "git:abc123",
		Artifacts:    []benchmarks.AgentArtifact{{ID: "openhands-dev", Framework: "openhands", Image: "local/openhands:dev", ImageDigest: "sha256:test", TaskRunnerProtocol: "v1"}},
		Profiles:     []benchmarks.Profile{{ID: "model-fixed", Kind: "model", Config: json.RawMessage(`{"temperature":0}`)}},
		RunTemplates: []benchmarks.RunTemplate{{ID: "openhands-smoke", SuiteID: "terminal-bench-2.0", AgentArtifactID: "openhands-dev", ModelProfileID: "model-fixed", AgentProfileID: "openhands-default", ExecutionProfileID: "smoke", TaskSelection: json.RawMessage(`{"mode":"sample"}`), TrialsPerCase: 1}},
	})
	projectAuth := newFakeProjectAuth()
	projectAuth.allow("member-1", "project-1")
	deps := Dependencies{ProjectAuth: projectAuth, BenchmarkCatalog: catalog}

	response := httptest.NewRecorder()
	handleGetBenchmarkCatalog(deps).ServeHTTP(response, projectRequest(http.MethodGet, "/api/projects/project-1/benchmark-catalog", "project-1", "member-1"))
	if response.Code != http.StatusOK {
		t.Fatalf("catalog status = %d; body=%s", response.Code, response.Body.String())
	}
	var got benchmarks.Catalog
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if got.Revision != "git:abc123" || len(got.Artifacts) != 1 || len(got.Profiles) != 1 || len(got.RunTemplates) != 1 {
		t.Fatalf("catalog = %#v", got)
	}

	artifactsResponse := httptest.NewRecorder()
	handleListBenchmarkArtifacts(deps).ServeHTTP(artifactsResponse, projectRequest(http.MethodGet, "/api/projects/project-1/benchmark-artifacts", "project-1", "member-1"))
	if artifactsResponse.Code != http.StatusOK || !json.Valid(artifactsResponse.Body.Bytes()) {
		t.Fatalf("artifacts response = %d %s", artifactsResponse.Code, artifactsResponse.Body.String())
	}
}

func projectRequest(method, target, projectID, userID string) *http.Request {
	request := httptest.NewRequest(method, target, nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("projectID", projectID)
	ctx := context.WithValue(request.Context(), userIDKey, userID)
	return request.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeContext))
}

type preflightBenchmarkSuites struct{ suite BenchmarkSuite }

func (s preflightBenchmarkSuites) CreateSuite(context.Context, benchmarks.CreateSuiteParams) (BenchmarkSuite, error) {
	return s.suite, nil
}
func (s preflightBenchmarkSuites) GetSuite(context.Context, string, string) (BenchmarkSuite, error) {
	return s.suite, nil
}
func (s preflightBenchmarkSuites) ListSuites(context.Context, string) ([]BenchmarkSuite, error) {
	return []BenchmarkSuite{s.suite}, nil
}

func TestBenchmarkPreflightReportsRegisteredWorkerAvailability(t *testing.T) {
	registry := benchmarks.NewInMemoryWorkerRegistry(time.Minute)
	if _, err := registry.Register(context.Background(), benchmarks.WorkerRegistration{
		ProjectID: "project-1", Name: "harbor_worker", ExecutionKinds: []string{"harbor"}, Capacity: 3,
	}); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	datasets := newFakeDatasetsService()
	datasets.cases["dataset-1"] = []DatasetCase{{ID: "case-1"}, {ID: "case-2"}}
	request := httptest.NewRequest(http.MethodPost, "/api/projects/project-1/benchmark-preflight", nil)
	_, _, _, preflight, loaded := loadBenchmarkPreflight(request, Dependencies{
		BenchmarkSuitesService: preflightBenchmarkSuites{suite: BenchmarkSuite{ID: "suite-1", DatasetID: "dataset-1", ExecutionKind: "harbor"}},
		DatasetsService:        datasets,
		BenchmarkWorkers:       registry,
	}, "project-1", benchmarkExperimentRequest{SuiteID: "suite-1", TargetName: "openhands", TrialsPerCase: 2})
	if !loaded {
		t.Fatal("preflight did not load")
	}
	if !preflight.Compatible || !preflight.RunnerAvailable || preflight.Runner == nil {
		t.Fatalf("preflight = %#v", preflight)
	}
	if preflight.Runner.State != "ready" || preflight.Runner.MatchingWorkers != 1 || preflight.Runner.AdvertisedCapacity != 3 || preflight.EstimatedJobs != 4 {
		t.Fatalf("worker preflight = %#v", preflight)
	}
}

func TestBenchmarkPreflightAllowsDurableQueueWithoutWorker(t *testing.T) {
	datasets := newFakeDatasetsService()
	datasets.cases["dataset-1"] = []DatasetCase{{ID: "case-1"}}
	request := httptest.NewRequest(http.MethodPost, "/api/projects/project-1/benchmark-preflight", nil)
	_, _, _, preflight, loaded := loadBenchmarkPreflight(request, Dependencies{
		BenchmarkSuitesService: preflightBenchmarkSuites{suite: BenchmarkSuite{ID: "suite-1", DatasetID: "dataset-1", ExecutionKind: "harbor"}},
		DatasetsService:        datasets,
		BenchmarkWorkers:       benchmarks.NewInMemoryWorkerRegistry(time.Minute),
	}, "project-1", benchmarkExperimentRequest{SuiteID: "suite-1", TargetName: "openhands", TrialsPerCase: 1})
	if !loaded || !preflight.Compatible {
		t.Fatalf("preflight = %#v, want a queueable experiment", preflight)
	}
	if preflight.RunnerAvailable || preflight.Runner == nil || len(preflight.Warnings) == 0 {
		t.Fatalf("worker absence should be reported as a warning: %#v", preflight)
	}
}

func TestSyncBenchmarkCatalogRequiresAdminPersistsManifestAndRejectsSecrets(t *testing.T) {
	catalog := benchmarks.NewStaticCatalogReader()
	adminDeps := Dependencies{
		ProjectAdminAuth:     benchmarkLaunchProjectAdmin{},
		BenchmarkCatalog:     catalog,
		BenchmarkCatalogSync: catalog,
	}
	valid := `{
		"revision":"git:abc123",
		"artifacts":[{"id":"openhands-dev","framework":"openhands","image":"local/openhands:dev","image_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","task_runner_protocol":"v1"}],
		"profiles":[
			{"id":"model-fixed","kind":"model","config":{"model":"openai/example","max_tokens":8192}},
			{"id":"openhands-default","kind":"agent","framework":"openhands","config":{"max_iterations":50}},
			{"id":"smoke","kind":"execution","config":{"concurrency":1}}
		],
		"run_templates":[{"id":"openhands-smoke","suite_id":"terminal-bench-2.0","agent_artifact_id":"openhands-dev","model_profile_id":"model-fixed","agent_profile_id":"openhands-default","execution_profile_id":"smoke","task_selection":{"mode":"sample","count":1,"seed":42},"trials_per_case":1}]
	}`
	response := serveCatalogSync(t, adminDeps, "project-1", valid)
	if response.Code != http.StatusOK {
		t.Fatalf("sync status = %d; body=%s", response.Code, response.Body.String())
	}
	var synced benchmarks.Catalog
	if err := json.Unmarshal(response.Body.Bytes(), &synced); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if synced.Revision != "git:abc123" || len(synced.Artifacts) != 1 || len(synced.Profiles) != 3 || len(synced.RunTemplates) != 1 {
		t.Fatalf("sync response = %#v", synced)
	}

	secretManifest := strings.Replace(valid, `"max_tokens":8192`, `"api_key":"must-not-persist"`, 1)
	response = serveCatalogSync(t, adminDeps, "project-1", secretManifest)
	if response.Code != http.StatusBadRequest || strings.Contains(response.Body.String(), "must-not-persist") {
		t.Fatalf("secret manifest response = %d %s", response.Code, response.Body.String())
	}
	stored, err := catalog.ReadCatalog(context.Background(), "project-1")
	if err != nil || stored.Revision != "git:abc123" {
		t.Fatalf("secret sync altered catalog: %#v, %v", stored, err)
	}

	response = serveCatalogSync(t, adminDeps, "project-1", strings.Replace(valid, `"revision"`, `"secret"`, 1))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("unknown/secret field status = %d; body=%s", response.Code, response.Body.String())
	}

	nonAdmin := Dependencies{ProjectAdminAuth: benchmarkLaunchProjectAdmin{err: context.Canceled}, BenchmarkCatalog: catalog, BenchmarkCatalogSync: catalog}
	response = serveCatalogSync(t, nonAdmin, "project-1", valid)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-admin sync status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func serveCatalogSync(t *testing.T, deps Dependencies, projectID, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := projectRequest(http.MethodPost, "/api/projects/"+projectID+"/benchmark-catalog/sync", projectID, "admin-1")
	request.Body = io.NopCloser(strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handleSyncBenchmarkCatalog(deps).ServeHTTP(response, request)
	return response
}

type templateCapturingExperiments struct {
	*fakeExperimentsService
	params *experiments.CreateExperimentHarnessParams
}

func (f *templateCapturingExperiments) CreateExperimentWithHarness(ctx context.Context, projectID, datasetID, evaluatorID string, params experiments.CreateExperimentHarnessParams) (Experiment, error) {
	copied := params
	copied.CaseIDs = append([]string(nil), params.CaseIDs...)
	f.params = &copied
	return f.fakeExperimentsService.CreateExperimentWithHarness(ctx, projectID, datasetID, evaluatorID, params)
}

func TestRunTemplateLaunchSnapshotsImmutableArtifactAndProfiles(t *testing.T) {
	catalog := benchmarks.NewStaticCatalogReader()
	_, err := catalog.SyncCatalog(context.Background(), "project-1", benchmarks.Catalog{
		Revision: "git:template-snapshot",
		Artifacts: []benchmarks.AgentArtifact{{
			ID: "openhands-dev", Framework: "openhands", Image: "local/openhands:dev",
			ImageDigest: "sha256:" + strings.Repeat("a", 64), SourceCommit: "abcdef0123456789abcdef0123456789abcdef01",
			TaskRunnerProtocol: "v1", Capabilities: []string{"shell", "filesystem", "git"},
		}},
		Profiles: []benchmarks.Profile{
			{ID: "model-fixed", Kind: "model", Config: json.RawMessage(`{"provider":"openai-compatible","model":"openai/example","parameters":{"temperature":0}}`)},
			{ID: "openhands-default", Kind: "agent", Framework: "openhands", Config: json.RawMessage(`{"prompt_version":"openhands-se-v3","max_iterations":50}`)},
			{ID: "harbor-smoke", Kind: "execution", Config: json.RawMessage(`{"concurrency":1,"task_timeout_s":900,"fail_fast":true}`)},
		},
		RunTemplates: []benchmarks.RunTemplate{{
			ID: "openhands-terminal-smoke", SuiteID: "terminal-bench-2.0", AgentArtifactID: "openhands-dev",
			ModelProfileID: "model-fixed", AgentProfileID: "openhands-default", ExecutionProfileID: "harbor-smoke",
			TaskSelection: json.RawMessage(`{"mode":"ids","include":["case-2"]}`), ExecutionOverrides: json.RawMessage(`{"task_timeout_s":120}`), TrialsPerCase: 2,
		}},
	})
	if err != nil {
		t.Fatalf("SyncCatalog() error = %v", err)
	}
	datasets := newFakeDatasetsService()
	datasets.cases["dataset-1"] = []DatasetCase{{ID: "case-1", DatasetID: "dataset-1", SortOrder: 1}, {ID: "case-2", DatasetID: "dataset-1", SortOrder: 2}}
	projectAuth := newFakeProjectAuth()
	projectAuth.allow("member-1", "project-1")
	service := &templateCapturingExperiments{fakeExperimentsService: newFakeExperimentsService()}
	deps := Dependencies{
		ProjectAuth:      projectAuth,
		BenchmarkCatalog: catalog,
		BenchmarkSuitesService: preflightBenchmarkSuites{suite: BenchmarkSuite{
			ID: "suite-db-1", DatasetID: "dataset-1", BenchmarkKey: "terminal-bench-2.0", Version: "2.0",
			HarnessKind: "harbor", ExecutionKind: "harbor", SourceCommit: "0123456789abcdef0123456789abcdef01234567",
			DefaultExecutionConfig: json.RawMessage(`{"task_timeout_s":1800,"max_retries":0}`), RequiredCapabilities: []string{"shell", "filesystem", "git"},
		}},
		DatasetsService:    datasets,
		ExperimentsService: service,
		BenchmarkWorkers:   benchmarks.NewInMemoryWorkerRegistry(time.Minute),
	}

	request := httptest.NewRequest(http.MethodPost, "/api/projects/project-1/benchmark-preflight", nil)
	plan, loaded := loadBenchmarkLaunchPlan(request, deps, "project-1", benchmarkExperimentRequest{RunTemplateID: "openhands-terminal-smoke"})
	if !loaded || !plan.preflight.Compatible {
		t.Fatalf("template preflight = %#v", plan.preflight)
	}
	if plan.preflight.CatalogRevision != "git:template-snapshot" || plan.preflight.AgentArtifactID != "openhands-dev" || plan.preflight.EstimatedJobs != 2 {
		t.Fatalf("template preflight identity = %#v", plan.preflight)
	}
	if len(plan.caseIDs) != 1 || plan.caseIDs[0] != "case-2" || plan.target.Version != "sha256:"+strings.Repeat("a", 64) {
		t.Fatalf("template case/artifact selection = %#v %#v", plan.caseIDs, plan.target)
	}
	var snapshot map[string]any
	if err := json.Unmarshal([]byte(plan.executionConfig), &snapshot); err != nil {
		t.Fatalf("decode immutable execution config: %v", err)
	}
	if snapshot["catalog_revision"] != "git:template-snapshot" || snapshot["run_template_id"] != "openhands-terminal-smoke" || snapshot["task_timeout_s"] != float64(120) {
		t.Fatalf("execution snapshot identity/merge = %#v", snapshot)
	}
	artifact, _ := snapshot["agent_artifact"].(map[string]any)
	if artifact["image_digest"] != "sha256:"+strings.Repeat("a", 64) || artifact["task_runner_protocol"] != "v1" {
		t.Fatalf("artifact snapshot = %#v", artifact)
	}

	createRequest := projectRequest(http.MethodPost, "/api/projects/project-1/benchmark-experiments", "project-1", "member-1")
	createRequest.Body = io.NopCloser(strings.NewReader(`{"run_template_id":"openhands-terminal-smoke"}`))
	createRequest.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handleCreateBenchmarkExperiment(deps).ServeHTTP(response, createRequest)
	if response.Code != http.StatusCreated {
		t.Fatalf("create template experiment status = %d; body=%s", response.Code, response.Body.String())
	}
	if service.params == nil || service.params.ExecutionConfig != plan.executionConfig || service.params.TargetName != "openhands-dev" || service.params.TrialsPerCase != 2 || service.params.ModelName != "openai/example" {
		t.Fatalf("created template params = %#v", service.params)
	}
}

func TestRunTemplateLaunchRejectsMutableLegacyOverrides(t *testing.T) {
	plan, loaded := loadRunTemplateLaunchPlan(context.Background(), Dependencies{}, "project-1", benchmarkExperimentRequest{RunTemplateID: "template-1", TargetName: "openhands"})
	if !loaded || plan.preflight.Compatible || len(plan.preflight.Blockers) != 1 {
		t.Fatalf("mixed template/legacy request = %#v", plan.preflight)
	}
}
