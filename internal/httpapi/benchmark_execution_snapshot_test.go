package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

type reportSnapshotExperiments struct {
	*fakeExperimentsService
	jobs []EvalJob
}

func (s *reportSnapshotExperiments) ListExperimentJobs(context.Context, string) ([]EvalJob, error) {
	return s.jobs, nil
}

func TestBenchmarkExperimentReportExposesOnlySafeImmutableConfigSnapshot(t *testing.T) {
	projectAuth := newFakeProjectAuth()
	projectAuth.allow("member-1", "project-1")
	experiments := &reportSnapshotExperiments{
		fakeExperimentsService: newFakeExperimentsService(),
		jobs: []EvalJob{{
			ID: "job-1", ExperimentID: "exp-1", DatasetCaseID: "case-db-1", Status: "queued", ExecutionKind: "harbor",
		}},
	}
	experiments.experiments["exp-1"] = Experiment{
		ID: "exp-1", ProjectID: "project-1", DatasetID: "dataset-1", TargetName: "openhands", Status: "queued",
		BenchmarkSuiteID: "suite-1", BenchmarkConfig: `{
			"schema_version":"agentlab.benchmark-execution.v1",
			"catalog_revision":"git:catalog-20260810",
			"run_template_id":"openhands-harbor-smoke",
			"trials_per_case":2,
			"agent_artifact":{"id":"openhands-task-runner","framework":"openhands","image":"agentlab/openhands-task-runner:local","image_digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","source_commit":"abcdef0123456789"},
			"model_profile":{"id":"model-ark","config":{"api_key":"sk-aaaaaaaaaaaaaaaa","base_url":"not-for-browser"}},
			"agent_profile":{"id":"openhands-default","framework":"openhands","config":{"authorization":"Bearer do-not-return"}},
			"execution_profile":{"id":"harbor-local","config":{"password":"do-not-return"}},
			"task_selection":{"mode":"ids","include":["task-1","sk-bbbbbbbbbbbbbbbb"],"exclude":["task-9"],"count":2,"seed":42},
			"selected_case_ids":["case-db-1","case-db-2"],
			"effective_execution":{"token":"do-not-return"}
		}`,
	}

	request := projectRequest(http.MethodGet, "/api/projects/project-1/benchmark-experiments/exp-1/report", "project-1", "member-1")
	chi.RouteContext(request.Context()).URLParams.Add("experimentID", "exp-1")
	response := httptest.NewRecorder()
	handleBenchmarkExperimentReport(Dependencies{
		ProjectAuth: projectAuth, ExperimentsService: experiments,
		BenchmarkSuitesService: preflightBenchmarkSuites{suite: BenchmarkSuite{ID: "suite-1", ProjectID: "project-1", DatasetID: "dataset-1", ExecutionKind: "harbor"}},
	}).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("report status = %d; body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "do-not-return") || strings.Contains(response.Body.String(), `"api_key"`) || strings.Contains(response.Body.String(), "sk-aaaaaaaaaaaaaaaa") || strings.Contains(response.Body.String(), "sk-bbbbbbbbbbbbbbbb") {
		t.Fatalf("report leaked a raw or secret-like config value: %s", response.Body.String())
	}

	var report BenchmarkExperimentReport
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode report: %v", err)
	}
	if report.Experiment.BenchmarkConfig != "" {
		t.Fatalf("raw benchmark_config must be omitted from report, got %q", report.Experiment.BenchmarkConfig)
	}
	if report.ConfigSnapshot == nil {
		t.Fatal("expected safe config snapshot")
	}
	snapshot := report.ConfigSnapshot
	if snapshot.CatalogRevision != "git:catalog-20260810" || snapshot.RunTemplateID != "openhands-harbor-smoke" || snapshot.TrialsPerCase != 2 {
		t.Fatalf("snapshot identity = %#v", snapshot)
	}
	if snapshot.AgentArtifact.ImageDigest != "sha256:"+strings.Repeat("a", 64) || snapshot.AgentArtifact.SourceCommit != "abcdef0123456789" {
		t.Fatalf("snapshot artifact = %#v", snapshot.AgentArtifact)
	}
	if snapshot.Profiles.Model.ID != "model-ark" || snapshot.Profiles.Agent.ID != "openhands-default" || snapshot.Profiles.Execution.ID != "harbor-local" {
		t.Fatalf("snapshot profiles = %#v", snapshot.Profiles)
	}
	if snapshot.TaskSelection.Mode != "ids" || len(snapshot.TaskSelection.Include) != 1 || snapshot.TaskSelection.Include[0] != "task-1" || snapshot.TaskSelection.Seed != 42 || len(snapshot.SelectedCaseIDs) != 2 {
		t.Fatalf("snapshot task selection = %#v", snapshot)
	}
}
