package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"AgentScope/internal/benchmarks"
)

func TestRunnerClaimJobHandler(t *testing.T) {
	fakeRunnerSvc := newFakeRunnerService()
	fakeRunnerSvc.addClaimableJob()

	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_testvalidtoken", "proj-1", []string{"runner:write"})

	deps := Dependencies{
		TokenAuth:     tokenAuth,
		RunnerService: fakeRunnerSvc,
	}
	router := NewRouter(deps)

	t.Run("success with bearer token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/runner/jobs/claim?runner_name=test-runner", nil)
		req.Header.Set("Authorization", "Bearer ascope_testvalidtoken")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		// Assert nested job object exists with required fields
		job, ok := body["job"].(map[string]interface{})
		if !ok {
			t.Fatal("expected 'job' object in claim response")
		}
		if _, ok := job["id"]; !ok || job["id"] == "" {
			t.Fatal("expected non-empty job.id in claim response")
		}
		if _, ok := job["experiment_id"]; !ok || job["experiment_id"] == "" {
			t.Fatal("expected non-empty job.experiment_id in claim response")
		}
		if _, ok := job["dataset_case_id"]; !ok || job["dataset_case_id"] == "" {
			t.Fatal("expected non-empty job.dataset_case_id in claim response")
		}
		// Assert enrichment fields at the top level
		if _, ok := body["case_input"]; !ok || body["case_input"] == "" {
			t.Fatal("expected non-empty case_input in claim response")
		}
		if _, ok := body["target_name"]; !ok || body["target_name"] == "" {
			t.Fatal("expected non-empty target_name in claim response")
		}
	})

	t.Run("missing token returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/runner/jobs/claim", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("invalid token returns 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/runner/jobs/claim", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})

	t.Run("service error does not leak evaluator details", func(t *testing.T) {
		const sensitiveDetail = "foreign-evaluator-id foreign-evaluator-secret"
		errorSvc := newFakeRunnerService()
		errorSvc.claimErr = errors.New("get evaluator config: " + sensitiveDetail)
		errorRouter := NewRouter(Dependencies{
			TokenAuth:     tokenAuth,
			RunnerService: errorSvc,
		})

		req := httptest.NewRequest(http.MethodGet, "/runner/jobs/claim", nil)
		req.Header.Set("Authorization", "Bearer ascope_testvalidtoken")
		rec := httptest.NewRecorder()
		errorRouter.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
		for _, sensitive := range strings.Fields(sensitiveDetail) {
			if strings.Contains(rec.Body.String(), sensitive) {
				t.Fatalf("claim error leaked %q: %s", sensitive, rec.Body.String())
			}
		}
		if !strings.Contains(rec.Body.String(), "No claimable jobs available") {
			t.Fatalf("claim error = %s, want generic response", rec.Body.String())
		}
	})
}

func TestRunnerHeartbeatHandler(t *testing.T) {
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_validtoken", "proj-1", []string{"runner:write"})

	fakeRunnerSvc := newFakeRunnerService()
	jobID := fakeRunnerSvc.addClaimableJob()

	deps := Dependencies{
		TokenAuth:     tokenAuth,
		RunnerService: fakeRunnerSvc,
	}
	router := NewRouter(deps)

	t.Run("success", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"runner_name": "test-runner"})
		req := httptest.NewRequest(http.MethodPost, "/runner/jobs/"+jobID+"/heartbeat", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer ascope_validtoken")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("missing token returns 401", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"runner_name": "test-runner"})
		req := httptest.NewRequest(http.MethodPost, "/runner/jobs/"+jobID+"/heartbeat", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})
}

func TestRunnerSubmitResultHandler(t *testing.T) {
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_validtoken", "proj-1", []string{"runner:write"})

	fakeRunnerSvc := newFakeRunnerService()
	jobID := fakeRunnerSvc.addClaimableJob()

	deps := Dependencies{
		TokenAuth:     tokenAuth,
		RunnerService: fakeRunnerSvc,
	}
	router := NewRouter(deps)

	t.Run("success with details", func(t *testing.T) {
		body := `{"runner_name":"r1","run_id":"run-1","score":0.95,"passed":true,"reason":"ok","details":{"key":"val"},"actual_output":"hello","latency_ms":100}`
		req := httptest.NewRequest(http.MethodPost, "/runner/jobs/"+jobID+"/results", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer ascope_validtoken")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Assert details reached the fake service
		if fakeRunnerSvc.lastSubmitReq == nil {
			t.Fatal("expected SubmitResult to be called")
		}
		if string(fakeRunnerSvc.lastSubmitReq.Details) != `{"key":"val"}` {
			t.Fatalf("expected details to contain key:val, got %s", string(fakeRunnerSvc.lastSubmitReq.Details))
		}
	})

	t.Run("missing runner_name returns 400", func(t *testing.T) {
		body := `{"run_id":"run-1","score":0.5,"passed":true}`
		req := httptest.NewRequest(http.MethodPost, "/runner/jobs/"+jobID+"/results", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer ascope_validtoken")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})
}

func TestRunnerFailJobHandler(t *testing.T) {
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_validtoken", "proj-1", []string{"runner:write"})

	fakeRunnerSvc := newFakeRunnerService()
	jobID := fakeRunnerSvc.addClaimableJob()

	deps := Dependencies{
		TokenAuth:     tokenAuth,
		RunnerService: fakeRunnerSvc,
	}
	router := NewRouter(deps)

	t.Run("success", func(t *testing.T) {
		body := `{"runner_name":"r1","error":"agent crashed"}`
		req := httptest.NewRequest(http.MethodPost, "/runner/jobs/"+jobID+"/fail", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer ascope_validtoken")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("missing error returns 400", func(t *testing.T) {
		body := `{"runner_name":"r1","error":""}`
		req := httptest.NewRequest(http.MethodPost, "/runner/jobs/"+jobID+"/fail", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer ascope_validtoken")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})
}

func TestRunnerFailRunHandler(t *testing.T) {
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("valid_token", "proj-1", []string{"runner:write"})
	tokenAuth.addToken("wrong_scope_token", "proj-1", []string{"otlp:write"})

	fakeRuns := newFakeRunsService()
	// Pre-populate with a run so GetRun succeeds
	fakeRuns.runs = append(fakeRuns.runs, runsEntry{id: "run-existing-001", projectID: "proj-1", traceID: "trace-abc"})

	fakeRunnerSvc := newFakeRunnerService()

	deps := Dependencies{
		TokenAuth:     tokenAuth,
		RunsService:   fakeRuns,
		RunnerService: fakeRunnerSvc,
	}
	router := NewRouter(deps)

	t.Run("rejects token with wrong scope", func(t *testing.T) {
		body := `{"error":"test error"}`
		req := httptest.NewRequest(http.MethodPost, "/runner/runs/run-existing-001/fail", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer wrong_scope_token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})

	t.Run("rejects request without auth token", func(t *testing.T) {
		body := `{"error":"test error"}`
		req := httptest.NewRequest(http.MethodPost, "/runner/runs/run-existing-001/fail", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("missing error returns 400", func(t *testing.T) {
		body := `{"error":""}`
		req := httptest.NewRequest(http.MethodPost, "/runner/runs/run-existing-001/fail", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer valid_token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("successful call with valid token", func(t *testing.T) {
		body := `{"error":"target failed"}`
		req := httptest.NewRequest(http.MethodPost, "/runner/runs/run-existing-001/fail", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer valid_token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

type benchmarkCapableRunnerService struct {
	*fakeRunnerService
	claimedKinds []string
	claimResp    ClaimResponse
	failDetails  json.RawMessage
	failResult   *benchmarks.ResultDetails
}

func (f *benchmarkCapableRunnerService) ClaimByExecutionKinds(_ context.Context, _, _ string, kinds []string) (ClaimResponse, error) {
	f.claimedKinds = append([]string(nil), kinds...)
	return f.claimResp, nil
}

func (f *benchmarkCapableRunnerService) FailWithBenchmarkDetails(_ context.Context, _, _, _, _ string, details json.RawMessage, result *benchmarks.ResultDetails) error {
	f.failDetails = details
	f.failResult = result
	return nil
}

func TestRunnerClaimExecutionKindAndStructuredBenchmarkFailure(t *testing.T) {
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("benchmark-token", "proj-1", []string{"runner:write"})
	base := newFakeRunnerService()
	runner := &benchmarkCapableRunnerService{
		fakeRunnerService: base,
		claimResp: ClaimResponse{
			Job:       EvalJob{ID: "job-1", ExecutionKind: "harbor", ExecutionConfig: json.RawMessage(`{"image":"task"}`), TrialIndex: 2},
			Benchmark: &BenchmarkClaimContext{SuiteID: "suite-1", BenchmarkKey: "terminal_bench", HarnessKind: "harbor"},
		},
	}
	router := NewRouter(Dependencies{TokenAuth: tokenAuth, RunnerService: runner})

	claimReq := httptest.NewRequest(http.MethodGet, "/runner/jobs/claim?runner_name=harbor-1&execution_kind=harbor,tau2", nil)
	claimReq.Header.Set("Authorization", "Bearer benchmark-token")
	claimRec := httptest.NewRecorder()
	router.ServeHTTP(claimRec, claimReq)
	if claimRec.Code != http.StatusOK {
		t.Fatalf("claim status = %d: %s", claimRec.Code, claimRec.Body.String())
	}
	if got, want := strings.Join(runner.claimedKinds, ","), "harbor,tau2"; got != want {
		t.Fatalf("execution kinds = %q, want %q", got, want)
	}
	if !strings.Contains(claimRec.Body.String(), `"execution_kind":"harbor"`) || !strings.Contains(claimRec.Body.String(), `"benchmark_key":"terminal_bench"`) {
		t.Fatalf("claim missing benchmark context: %s", claimRec.Body.String())
	}

	failReq := httptest.NewRequest(http.MethodPost, "/runner/jobs/job-1/fail", strings.NewReader(`{"runner_name":"harbor-1","error":"verifier crashed","details":{"phase":"verify"},"benchmark_result":{"valid":false,"error_type":"verifier_error","official_metrics":{"checks":0}}}`))
	failReq.Header.Set("Authorization", "Bearer benchmark-token")
	failReq.Header.Set("Content-Type", "application/json")
	failRec := httptest.NewRecorder()
	router.ServeHTTP(failRec, failReq)
	if failRec.Code != http.StatusOK {
		t.Fatalf("fail status = %d: %s", failRec.Code, failRec.Body.String())
	}
	if runner.failResult == nil || runner.failResult.ErrorType != "verifier_error" {
		t.Fatalf("structured failure was not forwarded: %#v", runner.failResult)
	}
}
