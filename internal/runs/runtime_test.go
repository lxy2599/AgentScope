package runs

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeRuntimeQuerier struct {
	runs    map[string]Run
	events  map[string][]RunStateEvent
	activeRunIDMap map[string]string // instanceID -> runID
	nextID  int
}

func newFakeRuntimeQuerier() *fakeRuntimeQuerier {
	return &fakeRuntimeQuerier{
		runs:    make(map[string]Run),
		events:  make(map[string][]RunStateEvent),
		activeRunIDMap: make(map[string]string),
	}
}

func (f *fakeRuntimeQuerier) CreateRun(ctx context.Context, projectID, source, status, traceID, agentName, agentVersion string, metadata []byte) (Run, error) {
	f.nextID++
	id := "run-" + itoa(f.nextID)
	r := Run{
		ID:        id,
		ProjectID: projectID,
		Source:    source,
		Status:    status,
		TraceID:   traceID,
		AgentName: agentName,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	f.runs[id] = r
	return r, nil
}

func (f *fakeRuntimeQuerier) StartRuntimeRun(ctx context.Context, projectID, status, traceID, agentName, inputSummary string, metadata []byte) (Run, error) {
	f.nextID++
	id := "run-" + itoa(f.nextID)
	r := Run{
		ID:           id,
		ProjectID:    projectID,
		Status:       status,
		TraceID:      traceID,
		AgentName:    agentName,
		InputSummary: inputSummary,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	f.runs[id] = r
	return r, nil
}

func (f *fakeRuntimeQuerier) CompleteRuntimeRun(ctx context.Context, runID, projectID, outputSummary string, latencyMs int64) (Run, error) {
	r, ok := f.runs[runID]
	if !ok || r.ProjectID != projectID {
		return Run{}, errors.New("run not found")
	}
	r.Status = string(RunStatusCompleted)
	r.OutputSummary = outputSummary
	r.LatencyMs = &latencyMs
	r.UpdatedAt = time.Now()
	f.runs[runID] = r
	return r, nil
}

func (f *fakeRuntimeQuerier) FailRuntimeRun(ctx context.Context, runID, projectID, errorStr string, latencyMs int64) (Run, error) {
	r, ok := f.runs[runID]
	if !ok || r.ProjectID != projectID {
		return Run{}, errors.New("run not found")
	}
	r.Status = string(RunStatusFailed)
	r.Error = errorStr
	var ms int64 = latencyMs
	r.LatencyMs = &ms
	r.UpdatedAt = time.Now()
	f.runs[runID] = r
	return r, nil
}

func (f *fakeRuntimeQuerier) GetRunByID(ctx context.Context, runID string) (Run, error) {
	r, ok := f.runs[runID]
	if !ok {
		return Run{}, errors.New("run not found")
	}
	return r, nil
}

func (f *fakeRuntimeQuerier) UpdateRunStatus(ctx context.Context, runID, projectID, status, errorStr string) (Run, error) {
	r, ok := f.runs[runID]
	if !ok || r.ProjectID != projectID {
		return Run{}, errors.New("run not found")
	}
	r.Status = status
	r.Error = errorStr
	r.UpdatedAt = time.Now()
	f.runs[runID] = r
	return r, nil
}

func (f *fakeRuntimeQuerier) CreateRunStateEvent(ctx context.Context, projectID, runID string, agentID, agentInstanceID *string, fromStatus, toStatus, message string, attributes []byte) (RunStateEvent, error) {
	f.nextID++
	evt := RunStateEvent{
		ID:         "evt-" + itoa(f.nextID),
		RunID:      runID,
		FromStatus: fromStatus,
		ToStatus:   toStatus,
		Message:    message,
		CreatedAt:  time.Now(),
	}
	f.events[runID] = append(f.events[runID], evt)
	return evt, nil
}

func (f *fakeRuntimeQuerier) ListRunStateEvents(ctx context.Context, runID string) ([]RunStateEvent, error) {
	return f.events[runID], nil
}

func (f *fakeRuntimeQuerier) ListActiveRunsByProject(ctx context.Context, projectID string, limit, offset int) ([]ActiveRun, error) {
	var result []ActiveRun
	for _, r := range f.runs {
		if r.ProjectID == projectID && !IsTerminalRunStatus(r.Status) {
			result = append(result, ActiveRun{
				ID:        r.ID,
				ProjectID: r.ProjectID,
				Status:    r.Status,
				CreatedAt: r.CreatedAt,
				UpdatedAt: r.UpdatedAt,
			})
		}
	}
	return result, nil
}

func (f *fakeRuntimeQuerier) GetRunRuntimeDetail(ctx context.Context, runID, projectID string) (*RuntimeRunDetail, error) {
	r, ok := f.runs[runID]
	if !ok || r.ProjectID != projectID {
		return nil, errors.New("run not found")
	}
	return &RuntimeRunDetail{
		RuntimeRun: RuntimeRun{
			ID:        r.ID,
			ProjectID: r.ProjectID,
			Status:    r.Status,
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
		},
	}, nil
}

func (f *fakeRuntimeQuerier) SetAgentInstanceActiveRun(ctx context.Context, projectID, agentID, instanceID, runID string) (string, error) {
	dbID := "db-inst-uuid-" + instanceID
	f.activeRunIDMap[instanceID] = runID
	return dbID, nil
}

func (f *fakeRuntimeQuerier) ClearAgentInstanceActiveRun(ctx context.Context, projectID, agentID, instanceID string) (string, error) {
	dbID := "db-inst-uuid-" + instanceID
	delete(f.activeRunIDMap, instanceID)
	return dbID, nil
}

func (f *fakeRuntimeQuerier) GetAgentInstanceDBID(ctx context.Context, projectID, agentID, instanceID string) (string, error) {
	if _, ok := f.activeRunIDMap[instanceID]; !ok {
		return "", errors.New("instance not found")
	}
	return "db-inst-uuid-" + instanceID, nil
}

func TestRuntimeServiceRejectsInvalidStatus(t *testing.T) {
	svc := NewRuntimeService(newFakeRuntimeQuerier())
	_, err := svc.UpdateRunState(context.Background(), UpdateRunStateInput{
		ProjectID: "p1", RunID: "run-1", Status: "invalid_status",
	})
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestRuntimeServiceRejectsTerminalStatusInUpdate(t *testing.T) {
	svc := NewRuntimeService(newFakeRuntimeQuerier())
	_, err := svc.UpdateRunState(context.Background(), UpdateRunStateInput{
		ProjectID: "p1", RunID: "run-1", Status: "completed",
	})
	if err == nil {
		t.Fatal("expected error for terminal status via UpdateRunState")
	}
}

func TestRuntimeServiceStartRunWritesStateEvent(t *testing.T) {
	fq := newFakeRuntimeQuerier()
	svc := NewRuntimeService(fq)
	run, err := svc.StartRun(context.Background(), StartRunInput{
		ProjectID: "p1", AgentID: "agent-1", Input: "hello", TraceID: "trace-1",
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if run.Status != string(RunStatusRunning) {
		t.Fatalf("status = %q, want running", run.Status)
	}
	events := fq.events[run.ID]
	if len(events) != 1 {
		t.Fatalf("expected 1 state event, got %d", len(events))
	}
	if events[0].ToStatus != string(RunStatusRunning) {
		t.Fatalf("event to_status = %q", events[0].ToStatus)
	}
}

func TestRuntimeServiceFinishReturnsOutputAndLatency(t *testing.T) {
	fq := newFakeRuntimeQuerier()
	svc := NewRuntimeService(fq)
	run, err := svc.StartRun(context.Background(), StartRunInput{
		ProjectID: "p1", Input: "hello world",
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if run.Input != "hello world" {
		t.Fatalf("start run input = %q, want 'hello world'", run.Input)
	}

	finished, err := svc.FinishRun(context.Background(), FinishRunInput{
		ProjectID: "p1", RunID: run.ID, Output: "goodbye", LatencyMS: 1500,
	})
	if err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
	if finished.Output != "goodbye" {
		t.Fatalf("output = %q, want 'goodbye'", finished.Output)
	}
	if finished.LatencyMs == nil || *finished.LatencyMs != 1500 {
		t.Fatalf("latency_ms = %v, want 1500", finished.LatencyMs)
	}
}

func TestRuntimeServiceFailReturnsErrorAndLatency(t *testing.T) {
	fq := newFakeRuntimeQuerier()
	svc := NewRuntimeService(fq)
	run, err := svc.StartRun(context.Background(), StartRunInput{
		ProjectID: "p1", Input: "hello",
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}

	failed, err := svc.FailRun(context.Background(), FailRunInput{
		ProjectID: "p1", RunID: run.ID, Error: "something broke", LatencyMS: 500,
	})
	if err != nil {
		t.Fatalf("FailRun: %v", err)
	}
	if failed.Error != "something broke" {
		t.Fatalf("error = %q, want 'something broke'", failed.Error)
	}
	if failed.LatencyMs == nil || *failed.LatencyMs != 500 {
		t.Fatalf("latency_ms = %v, want 500", failed.LatencyMs)
	}
}

func TestRuntimeServiceFinishClearsActiveRun(t *testing.T) {
	fq := newFakeRuntimeQuerier()
	svc := NewRuntimeService(fq)
	run, err := svc.StartRun(context.Background(), StartRunInput{
		ProjectID: "p1", AgentID: "agent-1", InstanceID: "inst-1", Input: "hello",
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	// Verify Active Run was set
	if fq.activeRunIDMap["inst-1"] != run.ID {
		t.Fatalf("active run not set for instance")
	}
	// Finish the run
	_, err = svc.FinishRun(context.Background(), FinishRunInput{
		ProjectID: "p1", RunID: run.ID, AgentID: "agent-1", InstanceID: "inst-1",
	})
	if err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
	// Verify Active Run was cleared
	if fq.activeRunIDMap["inst-1"] != "" {
		t.Fatalf("active run not cleared for instance, got %q", fq.activeRunIDMap["inst-1"])
	}
}

func TestRuntimeServiceFailClearsActiveRun(t *testing.T) {
	fq := newFakeRuntimeQuerier()
	svc := NewRuntimeService(fq)
	run, err := svc.StartRun(context.Background(), StartRunInput{
		ProjectID: "p1", AgentID: "agent-1", InstanceID: "inst-1", Input: "hello",
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	if fq.activeRunIDMap["inst-1"] != run.ID {
		t.Fatalf("active run not set for instance")
	}
		_, err = svc.FailRun(context.Background(), FailRunInput{
		ProjectID: "p1", RunID: run.ID, AgentID: "agent-1", InstanceID: "inst-1", Error: "something broke",
	})
	if err != nil {
		t.Fatalf("FailRun: %v", err)
	}
	if _, ok := fq.activeRunIDMap["inst-1"]; ok {
		t.Fatal("active run should be cleared for instance")
	}
}

func TestRuntimeServiceRejectsCrossProjectRun(t *testing.T) {
	fq := newFakeRuntimeQuerier()
	svc := NewRuntimeService(fq)
	run, err := svc.StartRun(context.Background(), StartRunInput{
		ProjectID: "p1", AgentID: "agent-1", Input: "hello",
	})
	if err != nil {
		t.Fatalf("StartRun: %v", err)
	}
	// Try to finish with wrong project
	_, err = svc.FinishRun(context.Background(), FinishRunInput{
		ProjectID: "p2", RunID: run.ID,
	})
	if err == nil {
		t.Fatal("expected error for cross-project finish")
	}
	// Try to fail with wrong project
	_, err = svc.FailRun(context.Background(), FailRunInput{
		ProjectID: "p2", RunID: run.ID, Error: "nope",
	})
	if err == nil {
		t.Fatal("expected error for cross-project fail")
	}
}

func TestRuntimeServiceListActiveRuns(t *testing.T) {
	fq := newFakeRuntimeQuerier()
	svc := NewRuntimeService(fq)
	r1, _ := svc.StartRun(context.Background(), StartRunInput{ProjectID: "p1", Input: "run-1"})
	svc.StartRun(context.Background(), StartRunInput{ProjectID: "p1", Input: "run-2"})
	svc.FinishRun(context.Background(), FinishRunInput{ProjectID: "p1", RunID: r1.ID})

	active, err := svc.ListActiveRuns(context.Background(), "p1", 50, 0)
	if err != nil {
		t.Fatalf("ListActiveRuns: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("expected 1 active run, got %d", len(active))
	}
}

func TestRuntimeServiceStartRunWithoutProjectID(t *testing.T) {
	svc := NewRuntimeService(newFakeRuntimeQuerier())
	_, err := svc.StartRun(context.Background(), StartRunInput{ProjectID: ""})
	if err == nil {
		t.Fatal("expected error for empty projectID")
	}
}
