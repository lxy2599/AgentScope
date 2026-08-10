package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"AgentScope/internal/benchmarkcancel"
	"github.com/go-chi/chi/v5"
)

type fakeBenchmarkJobCanceller struct {
	request benchmarkcancel.Request
	result  benchmarkcancel.Result
	audit   []benchmarkcancel.AuditEvent
	err     error
	calls   int
}

func (f *fakeBenchmarkJobCanceller) CancelExperiment(_ context.Context, request benchmarkcancel.Request) (benchmarkcancel.Result, error) {
	f.calls++
	f.request = request
	return f.result, f.err
}

func (f *fakeBenchmarkJobCanceller) ListAudit(context.Context, string, string) ([]benchmarkcancel.AuditEvent, error) {
	return f.audit, f.err
}

func TestCancelBenchmarkExperimentRequiresProjectAdmin(t *testing.T) {
	canceller := &fakeBenchmarkJobCanceller{}
	response := serveBenchmarkCancellation(t, Dependencies{
		ProjectAdminAuth:      benchmarkLaunchProjectAdmin{err: errors.New("not administrator")},
		BenchmarkJobCanceller: canceller,
	}, "project-1", "exp-1", `{"reason":"obsolete immutable snapshot"}`)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if canceller.calls != 0 {
		t.Fatalf("canceller calls = %d, want 0", canceller.calls)
	}
}

func TestCancelBenchmarkExperimentPersistsAdminReasonWithoutEchoingIt(t *testing.T) {
	canceller := &fakeBenchmarkJobCanceller{result: benchmarkcancel.Result{
		ExperimentID: "exp-1", Status: "cancelled", CancelledJobs: 7, AuditEvents: 7,
	}}
	response := serveBenchmarkCancellation(t, Dependencies{
		ProjectAdminAuth:      benchmarkLaunchProjectAdmin{},
		BenchmarkJobCanceller: canceller,
	}, "project-1", "exp-1", `{"reason":"obsolete immutable image snapshot was queued before catalog repair"}`)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if canceller.request != (benchmarkcancel.Request{
		ProjectID: "project-1", ExperimentID: "exp-1", ActorUserID: "admin-1", Reason: "obsolete immutable image snapshot was queued before catalog repair",
	}) {
		t.Fatalf("cancellation request = %#v", canceller.request)
	}
	if strings.Contains(response.Body.String(), "obsolete immutable") {
		t.Fatalf("response echoed audit reason: %s", response.Body.String())
	}
	var body benchmarkcancel.Result
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body != canceller.result {
		t.Fatalf("response = %#v, want %#v", body, canceller.result)
	}
}

func TestCancelBenchmarkExperimentDoesNotEchoSecretLikeReason(t *testing.T) {
	canceller := &fakeBenchmarkJobCanceller{err: benchmarkcancel.ErrInvalidReason}
	response := serveBenchmarkCancellation(t, Dependencies{
		ProjectAdminAuth:      benchmarkLaunchProjectAdmin{},
		BenchmarkJobCanceller: canceller,
	}, "project-1", "exp-1", `{"reason":"api_key=should-not-appear"}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if strings.Contains(response.Body.String(), "should-not-appear") || strings.Contains(response.Body.String(), "api_key") {
		t.Fatalf("response leaked submitted reason: %s", response.Body.String())
	}
}

func TestCancelBenchmarkExperimentUsesStrictJSONAndProjectScopedNotFound(t *testing.T) {
	canceller := &fakeBenchmarkJobCanceller{}
	response := serveBenchmarkCancellation(t, Dependencies{
		ProjectAdminAuth:      benchmarkLaunchProjectAdmin{},
		BenchmarkJobCanceller: canceller,
	}, "project-1", "exp-1", `{"reason":"obsolete snapshot","unexpected":true}`)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("strict JSON status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if canceller.calls != 0 {
		t.Fatalf("canceller calls after invalid JSON = %d, want 0", canceller.calls)
	}

	canceller.err = benchmarkcancel.ErrExperimentNotFound
	response = serveBenchmarkCancellation(t, Dependencies{
		ProjectAdminAuth:      benchmarkLaunchProjectAdmin{},
		BenchmarkJobCanceller: canceller,
	}, "project-1", "other-project-experiment", `{"reason":"obsolete snapshot"}`)
	if response.Code != http.StatusNotFound {
		t.Fatalf("not found status = %d, want %d", response.Code, http.StatusNotFound)
	}
}

func serveBenchmarkCancellation(t *testing.T, deps Dependencies, projectID, experimentID, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/api/projects/"+projectID+"/benchmark-experiments/"+experimentID+"/cancel", strings.NewReader(body))
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("projectID", projectID)
	routeContext.URLParams.Add("experimentID", experimentID)
	ctx := context.WithValue(request.Context(), userIDKey, "admin-1")
	request = request.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeContext))
	response := httptest.NewRecorder()
	handleCancelBenchmarkExperiment(deps).ServeHTTP(response, request)
	return response
}

func TestListBenchmarkCancellationAuditRequiresAdminAndReturnsReasons(t *testing.T) {
	canceller := &fakeBenchmarkJobCanceller{audit: []benchmarkcancel.AuditEvent{{
		ID: "audit-1", ProjectID: "project-1", ExperimentID: "exp-1", EvalJobID: "job-1", ActorUserID: "admin-1", Reason: "obsolete immutable snapshot", PreviousStatus: "queued", CreatedAt: "2026-08-10T00:00:00Z",
	}}}
	response := serveBenchmarkCancellationAudit(t, Dependencies{
		ProjectAdminAuth: benchmarkLaunchProjectAdmin{}, BenchmarkJobCanceller: canceller,
	}, "project-1", "exp-1")
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "obsolete immutable snapshot") {
		t.Fatalf("audit reason missing from admin audit response: %s", response.Body.String())
	}

	response = serveBenchmarkCancellationAudit(t, Dependencies{
		ProjectAdminAuth: benchmarkLaunchProjectAdmin{err: errors.New("not administrator")}, BenchmarkJobCanceller: canceller,
	}, "project-1", "exp-1")
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func serveBenchmarkCancellationAudit(t *testing.T, deps Dependencies, projectID, experimentID string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/api/projects/"+projectID+"/benchmark-experiments/"+experimentID+"/cancellation-audit", nil)
	routeContext := chi.NewRouteContext()
	routeContext.URLParams.Add("projectID", projectID)
	routeContext.URLParams.Add("experimentID", experimentID)
	ctx := context.WithValue(request.Context(), userIDKey, "admin-1")
	request = request.WithContext(context.WithValue(ctx, chi.RouteCtxKey, routeContext))
	response := httptest.NewRecorder()
	handleListBenchmarkCancellationAudit(deps).ServeHTTP(response, request)
	return response
}
