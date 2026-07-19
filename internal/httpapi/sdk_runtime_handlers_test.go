package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

type fakeRuntimeService struct {
	startCalled              bool
	stateCalled              bool
	finishCalled             bool
	failCalled               bool
	receivedStartInstanceID  string
	receivedStateInstanceID  string
	receivedFinishInstanceID string
	receivedFailInstanceID   string
}

func (f *fakeRuntimeService) StartRun(ctx context.Context, projectID string, req SDKStartRunRequest) (RuntimeRunResponse, error) {
	f.startCalled = true
	f.receivedStartInstanceID = req.GetInstanceID()
	return RuntimeRunResponse{ID: "run-1", ProjectID: projectID, Status: "running"}, nil
}

func (f *fakeRuntimeService) UpdateRunState(ctx context.Context, projectID, runID string, req SDKUpdateRunStateRequest) (RuntimeRunResponse, error) {
	f.stateCalled = true
	f.receivedStateInstanceID = req.GetInstanceID()
	return RuntimeRunResponse{ID: runID, ProjectID: projectID, Status: req.Status}, nil
}

func (f *fakeRuntimeService) FinishRun(ctx context.Context, projectID, runID string, req SDKFinishRunRequest) (RuntimeRunResponse, error) {
	f.finishCalled = true
	f.receivedFinishInstanceID = req.GetInstanceID()
	return RuntimeRunResponse{ID: runID, ProjectID: projectID, Status: "completed"}, nil
}

func (f *fakeRuntimeService) FailRun(ctx context.Context, projectID, runID string, req SDKFailRunRequest) (RuntimeRunResponse, error) {
	f.failCalled = true
	f.receivedFailInstanceID = req.GetInstanceID()
	return RuntimeRunResponse{ID: runID, ProjectID: projectID, Status: "failed", Error: req.Error}, nil
}

func (f *fakeRuntimeService) ListActiveRuns(ctx context.Context, projectID string, limit, offset int) ([]ActiveRunResponse, error) {
	return []ActiveRunResponse{
		{ID: "run-1", ProjectID: projectID, Status: "running"},
	}, nil
}

func (f *fakeRuntimeService) GetRuntimeRunDetail(ctx context.Context, projectID, runID string) (RuntimeRunDetailResponse, error) {
	return RuntimeRunDetailResponse{ID: runID, ProjectID: projectID, Status: "running"}, nil
}

func TestSDKStartRunWithCorrectScope(t *testing.T) {
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_valid", "proj-1", []string{"agent:runtime"})
	fa := &fakeRuntimeService{}
	deps := Dependencies{TokenAuth: tokenAuth, RuntimeService: fa}

	body := `{"agent_id":"agent-1","instance_id":"inst-1","input":"hello","trace_id":"trace-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sdk/runs/start", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer ascope_valid")
	rr := httptest.NewRecorder()

	handleSDKStartRun(deps).ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if !fa.startCalled {
		t.Fatal("expected StartRun to be called")
	}
	if fa.receivedStartInstanceID != "inst-1" {
		t.Fatalf("expected instance_id='inst-1', got %q", fa.receivedStartInstanceID)
	}
	var resp RuntimeRunResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json: %v", err)
	}
	if resp.Status != "running" {
		t.Fatalf("status = %q", resp.Status)
	}
}

func TestSDKStartRunWithWrongScopeFails(t *testing.T) {
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("runner_token", "proj-1", []string{"runner:write"})
	deps := Dependencies{TokenAuth: tokenAuth, RuntimeService: &fakeRuntimeService{}}

	body := `{"agent_id":"agent-1","input":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sdk/runs/start", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer runner_token")
	rr := httptest.NewRecorder()

	handleSDKStartRun(deps).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for wrong scope, got %d", rr.Code)
	}
}

func TestSDKStartRunWithoutTokenFails(t *testing.T) {
	deps := Dependencies{TokenAuth: newFakeTokenAuth(), RuntimeService: &fakeRuntimeService{}}

	body := `{"agent_id":"agent-1","input":"hello"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sdk/runs/start", bytes.NewBufferString(body))
	rr := httptest.NewRecorder()

	handleSDKStartRun(deps).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for missing token, got %d", rr.Code)
	}
}

func TestSDKUpdateRunState(t *testing.T) {
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_valid", "proj-1", []string{"agent:runtime"})
	fa := &fakeRuntimeService{}
	deps := Dependencies{TokenAuth: tokenAuth, RuntimeService: fa}

	body := `{"status":"waiting_llm","message":"calling model","instance_id":"inst-1","agent_id":"agent-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sdk/runs/run-1/state", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer ascope_valid")
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("run_id", "run-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, ctx))
	rr := httptest.NewRecorder()

	handleSDKUpdateRunState(deps).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if !fa.stateCalled {
		t.Fatal("expected UpdateRunState to be called")
	}
	if fa.receivedStateInstanceID != "inst-1" {
		t.Fatalf("expected update instance_id='inst-1', got %q", fa.receivedStateInstanceID)
	}
}

func TestSDKFinishRun(t *testing.T) {
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_valid", "proj-1", []string{"agent:runtime"})
	fa := &fakeRuntimeService{}
	deps := Dependencies{TokenAuth: tokenAuth, RuntimeService: fa}

	body := `{"output":"result","instance_id":"inst-1","latency_ms":500,"agent_id":"agent-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sdk/runs/run-1/finish", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer ascope_valid")
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("run_id", "run-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, ctx))
	rr := httptest.NewRecorder()

	handleSDKFinishRun(deps).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if !fa.finishCalled {
		t.Fatal("expected FinishRun to be called")
	}
	if fa.receivedFinishInstanceID != "inst-1" {
		t.Fatalf("expected finish instance_id='inst-1', got %q", fa.receivedFinishInstanceID)
	}
}

func TestSDKFailRun(t *testing.T) {
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_valid", "proj-1", []string{"agent:runtime"})
	fa := &fakeRuntimeService{}
	deps := Dependencies{TokenAuth: tokenAuth, RuntimeService: fa}

	body := `{"error":"something broke","instance_id":"inst-1","latency_ms":100}`
	req := httptest.NewRequest(http.MethodPost, "/api/sdk/runs/run-1/fail", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer ascope_valid")
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("run_id", "run-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, ctx))
	rr := httptest.NewRecorder()

	handleSDKFailRun(deps).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if !fa.failCalled {
		t.Fatal("expected FailRun to be called")
	}
}

// ---- Alias-only compat tests (agent_instance_id → InstanceID fallback) ----

func TestSDKStartRunAliasOnly(t *testing.T) {
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_valid", "proj-1", []string{"agent:runtime"})
	fa := &fakeRuntimeService{}
	deps := Dependencies{TokenAuth: tokenAuth, RuntimeService: fa}

	body := `{"agent_id":"agent-1","agent_instance_id":"alias-inst","input":"hello","trace_id":"trace-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sdk/runs/start", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer ascope_valid")
	rr := httptest.NewRecorder()

	handleSDKStartRun(deps).ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if !fa.startCalled {
		t.Fatal("expected StartRun to be called")
	}
	if fa.receivedStartInstanceID != "alias-inst" {
		t.Fatalf("expected instance_id='alias-inst' from agent_instance_id, got %q", fa.receivedStartInstanceID)
	}
}

func TestSDKUpdateRunStateAliasOnly(t *testing.T) {
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_valid", "proj-1", []string{"agent:runtime"})
	fa := &fakeRuntimeService{}
	deps := Dependencies{TokenAuth: tokenAuth, RuntimeService: fa}

	body := `{"status":"running","message":"test","agent_id":"agent-1","agent_instance_id":"alias-inst"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sdk/runs/run-1/state", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer ascope_valid")
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("run_id", "run-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, ctx))
	rr := httptest.NewRecorder()

	handleSDKUpdateRunState(deps).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if !fa.stateCalled {
		t.Fatal("expected UpdateRunState to be called")
	}
	if fa.receivedStateInstanceID != "alias-inst" {
		t.Fatalf("expected instance_id='alias-inst' from agent_instance_id, got %q", fa.receivedStateInstanceID)
	}
}

func TestSDKFinishRunAliasOnly(t *testing.T) {
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_valid", "proj-1", []string{"agent:runtime"})
	fa := &fakeRuntimeService{}
	deps := Dependencies{TokenAuth: tokenAuth, RuntimeService: fa}

	body := `{"output":"result","agent_id":"agent-1","agent_instance_id":"alias-inst","latency_ms":500}`
	req := httptest.NewRequest(http.MethodPost, "/api/sdk/runs/run-1/finish", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer ascope_valid")
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("run_id", "run-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, ctx))
	rr := httptest.NewRecorder()

	handleSDKFinishRun(deps).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if !fa.finishCalled {
		t.Fatal("expected FinishRun to be called")
	}
	if fa.receivedFinishInstanceID != "alias-inst" {
		t.Fatalf("expected instance_id='alias-inst' from agent_instance_id, got %q", fa.receivedFinishInstanceID)
	}
}

func TestSDKFailRunAliasOnly(t *testing.T) {
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_valid", "proj-1", []string{"agent:runtime"})
	fa := &fakeRuntimeService{}
	deps := Dependencies{TokenAuth: tokenAuth, RuntimeService: fa}

	body := `{"error":"oops","agent_id":"agent-1","agent_instance_id":"alias-inst","latency_ms":100}`
	req := httptest.NewRequest(http.MethodPost, "/api/sdk/runs/run-1/fail", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer ascope_valid")
	ctx := chi.NewRouteContext()
	ctx.URLParams.Add("run_id", "run-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, ctx))
	rr := httptest.NewRecorder()

	handleSDKFailRun(deps).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
	if !fa.failCalled {
		t.Fatal("expected FailRun to be called")
	}
	if fa.receivedFailInstanceID != "alias-inst" {
		t.Fatalf("expected instance_id='alias-inst' from agent_instance_id, got %q", fa.receivedFailInstanceID)
	}
}

// ---- Online eval async dispatch tests ----

type recordingAsyncDispatcher struct {
	dispatchRunCalled      bool
	dispatchRunAsyncCalled bool
	capturedProjectID      string
	capturedRunID          string
	mu                     sync.Mutex
	done                   chan struct{}
}

func newRecordingAsyncDispatcher() *recordingAsyncDispatcher {
	return &recordingAsyncDispatcher{
		done: make(chan struct{}, 1),
	}
}

func (d *recordingAsyncDispatcher) DispatchRun(ctx context.Context, projectID, runID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dispatchRunCalled = true
	d.capturedProjectID = projectID
	d.capturedRunID = runID
	select {
	case d.done <- struct{}{}:
	default:
	}
	return nil
}

func (d *recordingAsyncDispatcher) DispatchRunAsync(projectID, runID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dispatchRunAsyncCalled = true
	d.capturedProjectID = projectID
	d.capturedRunID = runID
	select {
	case d.done <- struct{}{}:
	default:
	}
}

func (d *recordingAsyncDispatcher) waitForCall(t *testing.T, timeout time.Duration) {
	t.Helper()
	select {
	case <-d.done:
	case <-time.After(timeout):
	}
}

func (d *recordingAsyncDispatcher) wasDispatchRunCalled() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dispatchRunCalled
}

func (d *recordingAsyncDispatcher) wasDispatchRunAsyncCalled() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dispatchRunAsyncCalled
}

func (d *recordingAsyncDispatcher) getCaptured() (string, string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.capturedProjectID, d.capturedRunID
}

func TestSDKFinishRun_CallsDispatchRunAsync(t *testing.T) {
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_valid", "proj-1", []string{"agent:runtime"})
	disp := newRecordingAsyncDispatcher()
	deps := Dependencies{
		TokenAuth:      tokenAuth,
		RuntimeService: &fakeRuntimeService{},
		OnlineEvalDisp: disp,
	}

	body := `{"output":"result","instance_id":"inst-1","latency_ms":500,"agent_id":"agent-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sdk/runs/run-1/finish", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer ascope_valid")
	chiCtx := chi.NewRouteContext()
	chiCtx.URLParams.Add("run_id", "run-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, chiCtx))
	rr := httptest.NewRecorder()

	handleSDKFinishRun(deps).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}

	disp.waitForCall(t, 5*time.Second)

	if !disp.wasDispatchRunAsyncCalled() {
		t.Fatal("expected DispatchRunAsync to be called")
	}
	if disp.wasDispatchRunCalled() {
		t.Error("DispatchRun should NOT be called from the handler (use DispatchRunAsync)")
	}
	projID, rID := disp.getCaptured()
	if projID != "proj-1" {
		t.Errorf("projectID = %q, want %q", projID, "proj-1")
	}
	if rID != "run-1" {
		t.Errorf("runID = %q, want %q", rID, "run-1")
	}
}

func TestSDKFailRun_CallsDispatchRunAsync(t *testing.T) {
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_valid", "proj-1", []string{"agent:runtime"})
	disp := newRecordingAsyncDispatcher()
	deps := Dependencies{
		TokenAuth:      tokenAuth,
		RuntimeService: &fakeRuntimeService{},
		OnlineEvalDisp: disp,
	}

	body := `{"error":"something broke","instance_id":"inst-1","latency_ms":100}`
	req := httptest.NewRequest(http.MethodPost, "/api/sdk/runs/run-1/fail", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer ascope_valid")
	chiCtx := chi.NewRouteContext()
	chiCtx.URLParams.Add("run_id", "run-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, chiCtx))
	rr := httptest.NewRecorder()

	handleSDKFailRun(deps).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}

	disp.waitForCall(t, 5*time.Second)

	if !disp.wasDispatchRunAsyncCalled() {
		t.Fatal("expected DispatchRunAsync to be called")
	}
	if disp.wasDispatchRunCalled() {
		t.Error("DispatchRun should NOT be called from the handler (use DispatchRunAsync)")
	}
	projID, rID := disp.getCaptured()
	if projID != "proj-1" {
		t.Errorf("projectID = %q, want %q", projID, "proj-1")
	}
	if rID != "run-1" {
		t.Errorf("runID = %q, want %q", rID, "run-1")
	}
}

func TestSDKFinishRun_NonBlockingWithOnlineEval(t *testing.T) {
	// Verify that the handler returns immediately even when online eval
	// dispatch is configured (since DispatchRunAsync is called in a goroutine).
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_valid", "proj-1", []string{"agent:runtime"})
	disp := newRecordingAsyncDispatcher()
	deps := Dependencies{
		TokenAuth:      tokenAuth,
		RuntimeService: &fakeRuntimeService{},
		OnlineEvalDisp: disp,
	}

	body := `{"output":"result","instance_id":"inst-1","latency_ms":500,"agent_id":"agent-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sdk/runs/run-1/finish", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer ascope_valid")
	chiCtx := chi.NewRouteContext()
	chiCtx.URLParams.Add("run_id", "run-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, chiCtx))
	rr := httptest.NewRecorder()

	start := time.Now()
	handleSDKFinishRun(deps).ServeHTTP(rr, req)
	duration := time.Since(start)

	if duration > 500*time.Millisecond {
		t.Errorf("handler took %v, expected fast return (non-blocking)", duration)
	}

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestSDKFinishRun_NoOnlineEvalDispDoesNotCrash(t *testing.T) {
	// When OnlineEvalDisp is nil, the handler should still work.
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_valid", "proj-1", []string{"agent:runtime"})
	deps := Dependencies{
		TokenAuth:      tokenAuth,
		RuntimeService: &fakeRuntimeService{},
		OnlineEvalDisp: nil,
	}

	body := `{"output":"result","instance_id":"inst-1","latency_ms":500,"agent_id":"agent-1"}`
	req := httptest.NewRequest(http.MethodPost, "/api/sdk/runs/run-1/finish", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer ascope_valid")
	chiCtx := chi.NewRouteContext()
	chiCtx.URLParams.Add("run_id", "run-1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, chiCtx))
	rr := httptest.NewRecorder()

	handleSDKFinishRun(deps).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rr.Code, rr.Body.String())
	}
}
