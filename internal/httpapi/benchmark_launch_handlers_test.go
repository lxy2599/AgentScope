package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"AgentScope/internal/benchlauncher"
	"github.com/go-chi/chi/v5"
)

type fakeBenchmarkLauncher struct {
	status   benchlauncher.Status
	err      error
	launches []benchlauncher.LaunchRequest
}

func (f *fakeBenchmarkLauncher) Availability(context.Context, string, string, string) benchlauncher.Status {
	return f.status
}

func (f *fakeBenchmarkLauncher) Launch(_ context.Context, request benchlauncher.LaunchRequest) (benchlauncher.Status, error) {
	f.launches = append(f.launches, request)
	return f.status, f.err
}

type benchmarkLaunchExperiments struct {
	*fakeExperimentsService
	jobs []EvalJob
}

func (f *benchmarkLaunchExperiments) ListExperimentJobs(context.Context, string) ([]EvalJob, error) {
	return f.jobs, nil
}

type benchmarkLaunchProjectAdmin struct {
	err error
}

func (f benchmarkLaunchProjectAdmin) IsProjectAdmin(context.Context, string, string) error {
	return f.err
}

func TestLaunchBenchmarkExperimentRequiresProjectAdmin(t *testing.T) {
	launcher := &fakeBenchmarkLauncher{}
	experiments := launchExperiments("project-1", "harbor")
	response := serveBenchmarkLaunch(t, Dependencies{
		ExperimentsService: experiments,
		ProjectAdminAuth:   benchmarkLaunchProjectAdmin{err: errors.New("admin required")},
		BenchmarkLauncher:  launcher,
	}, "project-1", "exp-1")
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if len(launcher.launches) != 0 {
		t.Fatalf("launches = %#v, want none", launcher.launches)
	}
}

func TestLaunchBenchmarkExperimentValidatesOwnershipAndUsesJobExecutionKind(t *testing.T) {
	launcher := &fakeBenchmarkLauncher{status: benchlauncher.Status{Available: true, State: "running", ExecutionKind: "harbor", RunnerName: "benchmark-project-1-harbor", PID: 1234}}
	experiments := launchExperiments("other-project", "harbor")
	response := serveBenchmarkLaunch(t, Dependencies{
		ExperimentsService: experiments,
		ProjectAdminAuth:   benchmarkLaunchProjectAdmin{},
		BenchmarkLauncher:  launcher,
	}, "project-1", "exp-1")
	if response.Code != http.StatusNotFound {
		t.Fatalf("ownership status = %d, want %d", response.Code, http.StatusNotFound)
	}
	if len(launcher.launches) != 0 {
		t.Fatalf("launches after ownership failure = %#v", launcher.launches)
	}

	experiments = launchExperiments("project-1", "tau2")
	response = serveBenchmarkLaunch(t, Dependencies{
		ExperimentsService: experiments,
		ProjectAdminAuth:   benchmarkLaunchProjectAdmin{},
		BenchmarkLauncher:  launcher,
	}, "project-1", "exp-1")
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusAccepted, response.Body.String())
	}
	if len(launcher.launches) != 1 {
		t.Fatalf("launches = %#v, want one", launcher.launches)
	}
	launch := launcher.launches[0]
	if launch.ProjectID != "project-1" || launch.UserID != "admin-1" || launch.ExecutionKind != "tau2" {
		t.Fatalf("launch request = %#v", launch)
	}
}

func TestLaunchBenchmarkExperimentReturnsActionableBlockers(t *testing.T) {
	launcher := &fakeBenchmarkLauncher{
		status: benchlauncher.Status{
			Available:     false,
			State:         "blocked",
			ExecutionKind: "harbor",
			Reason:        "Docker must be installed and reachable for Harbor benchmark execution.",
			Blockers:      []string{"Docker must be installed and reachable for Harbor benchmark execution."},
		},
		err: benchlauncher.ErrUnavailable,
	}
	response := serveBenchmarkLaunch(t, Dependencies{
		ExperimentsService: launchExperiments("project-1", "harbor"),
		ProjectAdminAuth:   benchmarkLaunchProjectAdmin{},
		BenchmarkLauncher:  launcher,
	}, "project-1", "exp-1")
	if response.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusConflict, response.Body.String())
	}
}

func launchExperiments(projectID, executionKind string) *benchmarkLaunchExperiments {
	base := newFakeExperimentsService()
	base.experiments["exp-1"] = Experiment{
		ID:               "exp-1",
		ProjectID:        projectID,
		BenchmarkSuiteID: "suite-1",
	}
	return &benchmarkLaunchExperiments{
		fakeExperimentsService: base,
		jobs:                   []EvalJob{{ID: "job-1", ExecutionKind: executionKind}},
	}
}

func serveBenchmarkLaunch(t *testing.T, deps Dependencies, projectID, experimentID string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/benchmark-experiments/"+experimentID+"/launch", nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("projectID", projectID)
	routeContext.URLParams.Add("experimentID", experimentID)
	ctx := context.WithValue(request.Context(), userIDKey, "admin-1")
	request = request.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeContext))
	response := httptest.NewRecorder()
	handleLaunchBenchmarkExperiment(deps).ServeHTTP(response, request)
	return response
}
