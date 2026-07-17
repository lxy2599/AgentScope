package runs

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeQuerier struct {
	runs          map[string]Run
	traceIDIndex  map[string]string // traceID -> runID
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{
		runs:         make(map[string]Run),
		traceIDIndex: make(map[string]string),
	}
}

func (f *fakeQuerier) CreateRun(ctx context.Context, projectID, source, status, traceID, agentName, agentVersion string, metadata []byte) (Run, error) {
	id := "run-" + itoa(len(f.runs)+1)
	r := Run{
		ID:           id,
		ProjectID:    projectID,
		Source:       source,
		Status:       status,
		TraceID:      traceID,
		AgentName:    agentName,
		AgentVersion: agentVersion,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	f.runs[id] = r
	if traceID != "" {
		f.traceIDIndex[traceID] = id
	}
	return r, nil
}

func (f *fakeQuerier) GetRunByID(ctx context.Context, runID string) (Run, error) {
	r, ok := f.runs[runID]
	if !ok {
		return Run{}, errors.New("run not found")
	}
	return r, nil
}

func (f *fakeQuerier) GetRunByTraceID(ctx context.Context, traceID, projectID string) (Run, error) {
	runID, ok := f.traceIDIndex[traceID]
	if !ok {
		return Run{}, errors.New("run not found")
	}
	r := f.runs[runID]
	if r.ProjectID != projectID {
		return Run{}, errors.New("run not found")
	}
	return r, nil
}

func (f *fakeQuerier) ListRunsByProject(ctx context.Context, projectID string) ([]Run, error) {
	var result []Run
	for _, r := range f.runs {
		if r.ProjectID == projectID {
			result = append(result, r)
		}
	}
	return result, nil
}
func (f *fakeQuerier) UpdateRun(ctx context.Context, runID, agentName, agentVersion, status string, endedAt *time.Time, latencyMs *int64) (Run, error) {
	r, ok := f.runs[runID]
	if !ok {
		return Run{}, errors.New("run not found")
	}
	if agentName != "" {
		r.AgentName = agentName
	}
	r.AgentVersion = agentVersion
	if status != "" {
		r.Status = status
	}
	r.EndedAt = endedAt
	r.LatencyMs = latencyMs
	r.UpdatedAt = time.Now()
	f.runs[runID] = r
	return r, nil
}


type fakeTracestore struct {
	spans  []SpanRecord
	events []SpanEventRecord
}

func newFakeTracestore() *fakeTracestore {
	return &fakeTracestore{}
}

func (f *fakeTracestore) InsertSpans(ctx context.Context, spans []SpanRecord) error {
	f.spans = append(f.spans, spans...)
	return nil
}

func (f *fakeTracestore) InsertSpanEvents(ctx context.Context, events []SpanEventRecord) error {
	f.events = append(f.events, events...)
	return nil
}

func (f *fakeTracestore) ListSpansByRun(ctx context.Context, runID string) ([]SpanRecord, error) {
	var result []SpanRecord
	for _, s := range f.spans {
		result = append(result, s)
	}
	return result, nil
}

func (f *fakeTracestore) ListSpanEventsByRun(ctx context.Context, runID string) ([]SpanEventRecord, error) {
	var result []SpanEventRecord
	for _, e := range f.events {
		result = append(result, e)
	}
	return result, nil
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}

// ---- Tests ----

func TestCreateOrGetLiveRunFromTrace(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newFakeQuerier(), newFakeTracestore())

	attrs := map[string]string{
		"agentscope.agent.name":    "test-agent",
		"agentscope.agent.version": "1.0.0",
	}

	run, err := svc.CreateOrGetLiveRunFromTrace(ctx, "proj-1", "abc123", attrs)
	if err != nil {
		t.Fatalf("CreateOrGetLiveRunFromTrace() error = %v", err)
	}
	if run.ID == "" {
		t.Fatal("run.ID is empty")
	}
	if run.ProjectID != "proj-1" {
		t.Errorf("run.ProjectID = %q, want proj-1", run.ProjectID)
	}
	if run.Source != "live" {
		t.Errorf("run.Source = %q, want live", run.Source)
	}
	if run.AgentName != "test-agent" {
		t.Errorf("run.AgentName = %q, want test-agent", run.AgentName)
	}
}

func TestCreateOrGetLiveRunFromTrace_dedupByTraceID(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newFakeQuerier(), newFakeTracestore())

	attrs := map[string]string{"agentscope.agent.name": "agent-1"}
	run1, err := svc.CreateOrGetLiveRunFromTrace(ctx, "proj-1", "same-trace", attrs)
	if err != nil {
		t.Fatalf("First call: %v", err)
	}

	run2, err := svc.CreateOrGetLiveRunFromTrace(ctx, "proj-1", "same-trace", attrs)
	if err != nil {
		t.Fatalf("Second call: %v", err)
	}

	if run1.ID != run2.ID {
		t.Errorf("expected same run ID, got %q vs %q", run1.ID, run2.ID)
	}
}

func TestListProjectRuns(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newFakeQuerier(), newFakeTracestore())

	svc.CreateOrGetLiveRunFromTrace(ctx, "proj-1", "trace-1", nil)
	svc.CreateOrGetLiveRunFromTrace(ctx, "proj-1", "trace-2", nil)
	svc.CreateOrGetLiveRunFromTrace(ctx, "proj-2", "trace-3", nil)

	runs, err := svc.ListProjectRuns(ctx, "proj-1")
	if err != nil {
		t.Fatalf("ListProjectRuns() error = %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("got %d runs, want 2", len(runs))
	}
}

func TestGetRunDetail(t *testing.T) {
	ctx := context.Background()
	ts := newFakeTracestore()
	svc := NewService(newFakeQuerier(), ts)

	run, err := svc.CreateOrGetLiveRunFromTrace(ctx, "proj-1", "trace-detail", nil)
	if err != nil {
		t.Fatalf("CreateOrGetLiveRunFromTrace: %v", err)
	}

	ts.spans = append(ts.spans, SpanRecord{RunID: run.ID, SpanID: "span-1", Name: "test-span"})

	detail, err := svc.GetRunDetail(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunDetail() error = %v", err)
	}
	if detail.Run.ID != run.ID {
		t.Errorf("detail.Run.ID = %q, want %q", detail.Run.ID, run.ID)
	}
	if len(detail.Spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(detail.Spans))
	}
	if detail.Spans[0].SpanID != "span-1" {
		t.Errorf("span.SpanID = %q, want span-1", detail.Spans[0].SpanID)
	}
}

func TestGetRunDetail_notFound(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newFakeQuerier(), newFakeTracestore())

	_, err := svc.GetRunDetail(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent run")
	}
}
