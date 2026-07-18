package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

type fakeRuntimeService struct {
	startCalled    bool
	stateCalled    bool
	finishCalled   bool
	failCalled     bool
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
		if fa.receivedStateInstanceID != "inst-1" {
			t.Fatalf("expected update instance_id=\x27inst-1\x27, got %q", fa.receivedStateInstanceID)
		}
		t.Fatal("expected UpdateRunState to be called")
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
