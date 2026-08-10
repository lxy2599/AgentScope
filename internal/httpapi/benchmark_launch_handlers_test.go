package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"AgentScope/internal/benchmarks"
	"github.com/go-chi/chi/v5"
)

type fakeBenchmarkWorkers struct {
	availability      benchmarks.WorkerAvailability
	availabilityErr   error
	availabilityCalls int
	registrations     []benchmarks.WorkerRegistration
}

func (f *fakeBenchmarkWorkers) Register(_ context.Context, registration benchmarks.WorkerRegistration) (benchmarks.Worker, error) {
	f.registrations = append(f.registrations, registration)
	return benchmarks.Worker{}, nil
}

func (f *fakeBenchmarkWorkers) List(context.Context, string) ([]benchmarks.Worker, error) {
	return nil, nil
}

func (f *fakeBenchmarkWorkers) Availability(context.Context, string, string) (benchmarks.WorkerAvailability, error) {
	f.availabilityCalls++
	return f.availability, f.availabilityErr
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
	workers := &fakeBenchmarkWorkers{}
	experiments := launchExperiments("project-1", "harbor")
	response := serveBenchmarkLaunch(t, Dependencies{
		ExperimentsService: experiments,
		ProjectAdminAuth:   benchmarkLaunchProjectAdmin{err: errors.New("admin required")},
		BenchmarkWorkers:   workers,
	}, "project-1", "exp-1")
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if workers.availabilityCalls != 0 {
		t.Fatalf("availability calls = %d, want none", workers.availabilityCalls)
	}
}

func TestLaunchBenchmarkExperimentQueuesDurableJobsWithoutForkingRunner(t *testing.T) {
	workers := &fakeBenchmarkWorkers{availability: benchmarks.WorkerAvailability{
		ProjectID: "project-1", ExecutionKind: "tau2", Available: true, State: "ready", RegisteredWorkers: 1, MatchingWorkers: 1, AdvertisedCapacity: 2,
	}}
	experiments := launchExperiments("other-project", "harbor")
	response := serveBenchmarkLaunch(t, Dependencies{
		ExperimentsService: experiments,
		ProjectAdminAuth:   benchmarkLaunchProjectAdmin{},
		BenchmarkWorkers:   workers,
	}, "project-1", "exp-1")
	if response.Code != http.StatusNotFound {
		t.Fatalf("ownership status = %d, want %d", response.Code, http.StatusNotFound)
	}
	if workers.availabilityCalls != 0 {
		t.Fatalf("availability calls after ownership failure = %d, want none", workers.availabilityCalls)
	}

	experiments = launchExperiments("project-1", "tau2")
	response = serveBenchmarkLaunch(t, Dependencies{
		ExperimentsService: experiments,
		ProjectAdminAuth:   benchmarkLaunchProjectAdmin{},
		BenchmarkWorkers:   workers,
	}, "project-1", "exp-1")
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusAccepted, response.Body.String())
	}
	if workers.availabilityCalls != 1 {
		t.Fatalf("availability calls = %d, want 1", workers.availabilityCalls)
	}
	if len(workers.registrations) != 0 {
		t.Fatalf("launch unexpectedly registered/started a worker: %#v", workers.registrations)
	}
	var body BenchmarkLaunchResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.ExperimentID != "exp-1" || body.Launch.Status != "queued" || !body.Launch.Available || body.Launch.ExecutionKind != "tau2" || body.Launch.MatchingWorkers != 1 || body.Launch.AdvertisedCapacity != 2 {
		t.Fatalf("launch response = %#v", body)
	}
	if body.Launch.PID != 0 || body.Launch.RunnerName != "" {
		t.Fatalf("queue-only launch leaked local runner fields: %#v", body.Launch)
	}
}

func TestLaunchBenchmarkExperimentQueuesEvenWhenNoWorkerIsRegistered(t *testing.T) {
	workers := &fakeBenchmarkWorkers{availability: benchmarks.WorkerAvailability{
		ProjectID: "project-1", ExecutionKind: "harbor", Available: false, State: "unavailable", Reason: "No active benchmark worker supports execution kind \"harbor\".", Blockers: []string{"No active benchmark worker supports execution kind \"harbor\"."},
	}}
	response := serveBenchmarkLaunch(t, Dependencies{
		ExperimentsService: launchExperiments("project-1", "harbor"),
		ProjectAdminAuth:   benchmarkLaunchProjectAdmin{},
		BenchmarkWorkers:   workers,
	}, "project-1", "exp-1")
	if response.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusAccepted, response.Body.String())
	}
	var body BenchmarkLaunchResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Launch.Status != "queued" || body.Launch.Available || body.Launch.UnavailableReason == "" {
		t.Fatalf("queue-only unavailable response = %#v", body.Launch)
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
