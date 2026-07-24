package runs

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

type fakeQuerier struct {
	runs               map[string]Run
	traceIDIndex       map[string]string // traceID -> runID
	lastMetadata       []byte            // metadata from the most recent CreateRun call
	createCalls        int
	getRunByIDErr      error
	getRunByTraceIDErr error
	updateTraceIDFn    func(ctx context.Context, runID, traceID string) (Run, error)
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{
		runs:         make(map[string]Run),
		traceIDIndex: make(map[string]string),
	}
}

func (f *fakeQuerier) CreateRun(ctx context.Context, projectID, source, status, traceID, agentName, agentVersion string, metadata []byte) (Run, error) {
	f.createCalls++
	f.lastMetadata = metadata
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
	if f.getRunByIDErr != nil {
		return Run{}, f.getRunByIDErr
	}
	r, ok := f.runs[runID]
	if !ok {
		return Run{}, ErrRunNotFound
	}
	return r, nil
}

func (f *fakeQuerier) GetRunByTraceID(ctx context.Context, traceID, projectID string) (Run, error) {
	if f.getRunByTraceIDErr != nil {
		return Run{}, f.getRunByTraceIDErr
	}
	runID, ok := f.traceIDIndex[traceID]
	if !ok {
		return Run{}, ErrRunNotFound
	}
	r := f.runs[runID]
	if r.ProjectID != projectID {
		return Run{}, ErrRunNotFound
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
		return Run{}, ErrRunNotFound
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

func (f *fakeQuerier) UpdateRunAgentInfo(ctx context.Context, runID, agentName, agentVersion string) (Run, error) {
	r, ok := f.runs[runID]
	if !ok {
		return Run{}, ErrRunNotFound
	}
	r.AgentName = agentName
	r.AgentVersion = agentVersion
	r.UpdatedAt = time.Now()
	f.runs[runID] = r
	return r, nil
}

func (f *fakeQuerier) UpdateRunTraceID(ctx context.Context, runID, traceID string) (Run, error) {
	if f.updateTraceIDFn != nil {
		return f.updateTraceIDFn(ctx, runID, traceID)
	}
	r, ok := f.runs[runID]
	if !ok {
		return Run{}, ErrRunNotFound
	}
	r.TraceID = traceID
	r.UpdatedAt = time.Now()
	f.runs[runID] = r
	return r, nil
}

func (f *fakeQuerier) FailRunnerRun(ctx context.Context, runID, projectID, errorStr string) (Run, error) {
	r, ok := f.runs[runID]
	if !ok {
		return Run{}, ErrRunNotFound
	}
	if r.ProjectID != projectID {
		return Run{}, fmt.Errorf("%w in project", ErrRunNotFound)
	}
	r.Status = "failed"
	r.Error = errorStr
	now := time.Now()
	r.EndedAt = &now
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
	result := append([]SpanRecord(nil), f.spans...)
	return result, nil
}

func (f *fakeTracestore) ListSpanEventsByRun(ctx context.Context, runID string) ([]SpanEventRecord, error) {
	result := append([]SpanEventRecord(nil), f.events...)
	return result, nil
}

func (f *fakeTracestore) ListLLMCallsByRun(ctx context.Context, runID string) ([]LLMCallRecord, error) {
	return nil, nil
}

func (f *fakeTracestore) ListToolCallsByRun(ctx context.Context, runID string) ([]ToolCallRecord, error) {
	return nil, nil
}

func (f *fakeTracestore) ListRetrievalCallsByRun(ctx context.Context, runID string) ([]RetrievalCallRecord, error) {
	return nil, nil
}

func (f *fakeTracestore) ListSkillCallsByRun(ctx context.Context, runID string) ([]SkillCallRecord, error) {
	return nil, nil
}

func (f *fakeTracestore) ListThoughtsByRun(ctx context.Context, runID string) ([]ThoughtRecord, error) {
	return nil, nil
}

func (f *fakeTracestore) ListFinalAnswersByRun(ctx context.Context, runID string) ([]FinalAnswerRecord, error) {
	return nil, nil
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

func TestCreateOrGetLiveRunFromTrace_metadataNotEmpty(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQuerier()
	svc := NewService(fq, newFakeTracestore())

	attrs := map[string]string{
		"agentscope.agent.name":    "test-agent",
		"agentscope.agent.version": "1.0.0",
	}

	_, err := svc.CreateOrGetLiveRunFromTrace(ctx, "proj-1", "meta-test-trace", attrs)
	if err != nil {
		t.Fatalf("CreateOrGetLiveRunFromTrace() error = %v", err)
	}

	if fq.lastMetadata == nil {
		t.Fatal("CreateRun metadata is nil, want non-nil JSON object")
	}
	if string(fq.lastMetadata) != "{}" {
		t.Fatalf("CreateRun metadata = %q, want {}", string(fq.lastMetadata))
	}
}

func TestCreateOrGetLiveRunFromTrace_propagatesRunIDLookupFailure(t *testing.T) {
	ctx := context.Background()
	databaseErr := errors.New("database connection lost")
	fq := newFakeQuerier()
	fq.getRunByIDErr = databaseErr
	svc := NewService(fq, newFakeTracestore())

	_, err := svc.CreateOrGetLiveRunFromTrace(ctx, "proj-1", "trace-1", map[string]string{
		"agentscope.run.id": "run-existing",
	})
	if !errors.Is(err, databaseErr) {
		t.Fatalf("CreateOrGetLiveRunFromTrace() error = %v, want wrapped database error", err)
	}
	if fq.createCalls != 0 {
		t.Fatalf("CreateRun calls = %d, want 0 after run ID lookup failure", fq.createCalls)
	}
}

func TestCreateOrGetLiveRunFromTrace_createsAfterExplicitNotFound(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQuerier()
	svc := NewService(fq, newFakeTracestore())

	run, err := svc.CreateOrGetLiveRunFromTrace(ctx, "proj-1", "trace-1", map[string]string{
		"agentscope.run.id": "run-missing",
	})
	if err != nil {
		t.Fatalf("CreateOrGetLiveRunFromTrace() error = %v", err)
	}
	if run.ID == "" {
		t.Fatal("CreateOrGetLiveRunFromTrace() returned an empty run ID")
	}
	if fq.createCalls != 1 {
		t.Fatalf("CreateRun calls = %d, want 1 after explicit not-found lookups", fq.createCalls)
	}
}

func TestCreateOrGetLiveRunFromTrace_propagatesTraceLookupFailure(t *testing.T) {
	ctx := context.Background()
	databaseErr := errors.New("database connection lost")
	fq := newFakeQuerier()
	fq.getRunByTraceIDErr = databaseErr
	svc := NewService(fq, newFakeTracestore())

	_, err := svc.CreateOrGetLiveRunFromTrace(ctx, "proj-1", "trace-1", nil)
	if !errors.Is(err, databaseErr) {
		t.Fatalf("CreateOrGetLiveRunFromTrace() error = %v, want wrapped database error", err)
	}
	if fq.createCalls != 0 {
		t.Fatalf("CreateRun calls = %d, want 0 after trace lookup failure", fq.createCalls)
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

func TestCreateOrGetLiveRunFromTrace_usesExistingRunID(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQuerier()
	svc := NewService(fq, newFakeTracestore())

	// Pre-create a run (as the SDK runtime would)
	preRun, err := fq.CreateRun(ctx, "proj-1", "live", "running", "existing-trace", "sdk-agent", "", []byte("{}"))
	if err != nil {
		t.Fatalf("pre-create run: %v", err)
	}

	// Now simulate OTLP ingestion that sees agentscope.run.id in span attrs
	attrs := map[string]string{
		"agentscope.run.id":        preRun.ID,
		"agentscope.agent.name":    "sdk-agent",
		"agentscope.agent.version": "1.0.0",
	}

	// Call with a different trace_id — should still find the pre-created run via agentscope.run.id
	run, err := svc.CreateOrGetLiveRunFromTrace(ctx, "proj-1", "otlp-trace-id-xyz", attrs)
	if err != nil {
		t.Fatalf("CreateOrGetLiveRunFromTrace with agentscope.run.id: %v", err)
	}
	if run.ID != preRun.ID {
		t.Errorf("got run ID %q, want pre-created run ID %q", run.ID, preRun.ID)
	}
	if run.TraceID != "otlp-trace-id-xyz" {
		t.Errorf("got TraceID %q, want %q (should be updated to OTLP trace_id)", run.TraceID, "otlp-trace-id-xyz")
	}
}

func TestCreateOrGetLiveRunFromTrace_updatesTraceID(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQuerier()
	svc := NewService(fq, newFakeTracestore())

	// Pre-create a run with a previous/empty trace_id (as the SDK runtime would)
	preRun, err := fq.CreateRun(ctx, "proj-1", "live", "running", "old-trace-id", "sdk-agent", "", []byte("{}"))
	if err != nil {
		t.Fatalf("pre-create run: %v", err)
	}
	if preRun.TraceID != "old-trace-id" {
		t.Fatalf("expected trace_id=old-trace-id, got %q", preRun.TraceID)
	}

	// Simulate OTLP ingestion that finds the run via agentscope.run.id
	// with a *different* OTLP trace_id
	newOTLPTraceID := "otlp-trace-xyz-789"
	attrs := map[string]string{
		"agentscope.run.id":        preRun.ID,
		"agentscope.agent.name":    "sdk-agent",
		"agentscope.agent.version": "1.0.0",
	}
	run, err := svc.CreateOrGetLiveRunFromTrace(ctx, "proj-1", newOTLPTraceID, attrs)
	if err != nil {
		t.Fatalf("CreateOrGetLiveRunFromTrace with agentscope.run.id: %v", err)
	}

	// The run's trace_id should now be the real OTLP trace_id
	if run.TraceID != newOTLPTraceID {
		t.Errorf("got TraceID %q, want %q (should be updated to real OTLP trace_id)", run.TraceID, newOTLPTraceID)
	}

	// Verify via GetRunByID from querier
	persisted, err := fq.GetRunByID(ctx, preRun.ID)
	if err != nil {
		t.Fatalf("GetRunByID error = %v", err)
	}
	if persisted.TraceID != newOTLPTraceID {
		t.Errorf("persisted TraceID = %q, want %q", persisted.TraceID, newOTLPTraceID)
	}
}

func TestCreateOrGetLiveRunFromTrace_updatesEmptyTraceID(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQuerier()
	svc := NewService(fq, newFakeTracestore())

	// Pre-create a run with empty trace_id (SDK runtime start_run with no trace_id)
	preRun, err := fq.CreateRun(ctx, "proj-1", "live", "running", "", "sdk-agent", "", []byte("{}"))
	if err != nil {
		t.Fatalf("pre-create run: %v", err)
	}
	if preRun.TraceID != "" {
		t.Fatalf("expected trace_id=empty, got %q", preRun.TraceID)
	}

	// OTLP ingestion finds the run via agentscope.run.id with a real trace_id
	realTraceID := "real-otlp-trace-00112233"
	attrs := map[string]string{
		"agentscope.run.id": preRun.ID,
	}
	run, err := svc.CreateOrGetLiveRunFromTrace(ctx, "proj-1", realTraceID, attrs)
	if err != nil {
		t.Fatalf("CreateOrGetLiveRunFromTrace: %v", err)
	}

	if run.TraceID != realTraceID {
		t.Errorf("got TraceID %q, want %q (empty should be filled)", run.TraceID, realTraceID)
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
	if !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("GetRunDetail() error = %v, want ErrRunNotFound", err)
	}
}

func TestUpdateRunFromTrace_updatesUnknownAgent(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQuerier()
	svc := NewService(fq, newFakeTracestore())
	// Create a run with unknown agent_name
	attrs := map[string]string{}
	run, err := svc.CreateOrGetLiveRunFromTrace(ctx, "proj-1", "trace-upd", attrs)
	if err != nil {
		t.Fatalf("CreateOrGetLiveRunFromTrace: %v", err)
	}
	if run.AgentName != "unknown" {
		t.Fatalf("expected agent_name=unknown, got %q", run.AgentName)
	}
	// Update with real agent name
	updated, err := svc.UpdateRunFromTrace(ctx, run.ID, "real-agent", "1.0.0")
	if err != nil {
		t.Fatalf("UpdateRunFromTrace error = %v", err)
	}
	if updated.AgentName != "real-agent" {
		t.Errorf("agent_name = %q, want real-agent", updated.AgentName)
	}
	if updated.AgentVersion != "1.0.0" {
		t.Errorf("agent_version = %q, want 1.0.0", updated.AgentVersion)
	}
	// Verify via GetRunByID from querier
	got, err := fq.GetRunByID(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunByID error = %v", err)
	}
	if got.AgentName != "real-agent" {
		t.Errorf("persisted agent_name = %q, want real-agent", got.AgentName)
	}
}

func TestUpdateRunFromTrace_doesNotOverwriteKnownAgent(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQuerier()
	svc := NewService(fq, newFakeTracestore())
	attrs := map[string]string{"agentscope.agent.name": "original-agent"}
	run, err := svc.CreateOrGetLiveRunFromTrace(ctx, "proj-1", "trace-keep", attrs)
	if err != nil {
		t.Fatalf("CreateOrGetLiveRunFromTrace: %v", err)
	}
	if run.AgentName != "original-agent" {
		t.Fatalf("expected agent_name=original-agent, got %q", run.AgentName)
	}
	// Try to update with different agent name
	updated, err := svc.UpdateRunFromTrace(ctx, run.ID, "other-agent", "2.0.0")
	if err != nil {
		t.Fatalf("UpdateRunFromTrace error = %v", err)
	}
	// Should NOT have changed
	if updated.AgentName != "original-agent" {
		t.Errorf("agent_name was overwritten to %q, want original-agent", updated.AgentName)
	}
}

func TestUpdateRunFromTrace_doesNotClearLifecycleFields(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQuerier()
	svc := NewService(fq, newFakeTracestore())
	// Create a run with unknown agent
	attrs := map[string]string{}
	run, err := svc.CreateOrGetLiveRunFromTrace(ctx, "proj-1", "trace-lifecycle", attrs)
	if err != nil {
		t.Fatalf("CreateOrGetLiveRunFromTrace: %v", err)
	}
	// Mark it as completed with ended_at and latency_ms via UpdateRun
	now := time.Now()
	latency := int64(1500)
	completed, err := fq.UpdateRun(ctx, run.ID, run.AgentName, run.AgentVersion, "completed", &now, &latency)
	if err != nil {
		t.Fatalf("UpdateRun(error) = %v", err)
	}
	if completed.EndedAt == nil || *completed.EndedAt != now {
		t.Fatal("UpdateRun should have set ended_at")
	}
	if completed.LatencyMs == nil || *completed.LatencyMs != latency {
		t.Fatal("UpdateRun should have set latency_ms")
	}
	// Now call UpdateRunFromTrace — should NOT clear ended_at or latency_ms
	updated, err := svc.UpdateRunFromTrace(ctx, run.ID, "real-agent", "1.0.0")
	if err != nil {
		t.Fatalf("UpdateRunFromTrace error = %v", err)
	}
	if updated.EndedAt == nil {
		t.Fatal("UpdateRunFromTrace cleared ended_at")
	}
	if updated.LatencyMs == nil {
		t.Fatal("UpdateRunFromTrace cleared latency_ms")
	}
	if *updated.LatencyMs != latency {
		t.Errorf("latency_ms = %d, want %d", *updated.LatencyMs, latency)
	}
	if updated.AgentName != "real-agent" {
		t.Errorf("agent_name = %q, want real-agent", updated.AgentName)
	}
}

func TestFailRunByRunner_preservesAgentAndWritesError(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQuerier()
	svc := NewService(fq, newFakeTracestore())

	// Create a running run with agent info set
	run, err := fq.CreateRun(ctx, "proj-1", "live", "running", "trace-1", "my-agent", "2.0.0", []byte("{}"))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	err = svc.FailRunByRunner(ctx, run.ID, "proj-1", "target error: HTTP 500")
	if err != nil {
		t.Fatalf("FailRunByRunner error = %v", err)
	}

	// Verify the run was marked failed with correct error
	updated, err := fq.GetRunByID(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunByID error = %v", err)
	}
	if updated.Status != "failed" {
		t.Errorf("status = %q, want failed", updated.Status)
	}
	if updated.Error != "target error: HTTP 500" {
		t.Errorf("error = %q, want %q", updated.Error, "target error: HTTP 500")
	}
	// agent_name and agent_version must NOT be cleared
	if updated.AgentName != "my-agent" {
		t.Errorf("agent_name was cleared: got %q, want my-agent", updated.AgentName)
	}
	if updated.AgentVersion != "2.0.0" {
		t.Errorf("agent_version was cleared: got %q, want 2.0.0", updated.AgentVersion)
	}
}

func TestFailRunByRunner_crossProjectIsRejected(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQuerier()
	svc := NewService(fq, newFakeTracestore())

	run, err := fq.CreateRun(ctx, "proj-1", "live", "running", "trace-cross", "agent-x", "1.0.0", []byte("{}"))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	// Try to fail the run with a different project ID
	err = svc.FailRunByRunner(ctx, run.ID, "proj-2", "cross project attack")
	if !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("FailRunByRunner() error = %v, want ErrRunNotFound", err)
	}
}

func TestFailRunByRunner_alreadyTerminalIsIdempotent(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQuerier()
	svc := NewService(fq, newFakeTracestore())

	run, err := fq.CreateRun(ctx, "proj-1", "live", "completed", "trace-done", "agent-done", "1.0.0", []byte("{}"))
	if err != nil {
		t.Fatalf("create run: %v", err)
	}

	// Failing an already completed run should succeed (idempotent)
	err = svc.FailRunByRunner(ctx, run.ID, "proj-1", "already done")
	if err != nil {
		t.Fatalf("idempotent FailRunByRunner error = %v", err)
	}
}

func TestCreateOrGetLiveRunFromTrace_returnsErrorWhenTraceIDUpdateFails(t *testing.T) {
	ctx := context.Background()
	fq := newFakeQuerier()
	svc := NewService(fq, newFakeTracestore())

	// Pre-create a run (as the SDK runtime would)
	preRun, err := fq.CreateRun(ctx, "proj-1", "live", "running", "old-trace", "sdk-agent", "", []byte("{}"))
	if err != nil {
		t.Fatalf("pre-create run: %v", err)
	}

	// Make UpdateRunTraceID fail so we can verify the error propagates
	fq.updateTraceIDFn = func(_ context.Context, runID, traceID string) (Run, error) {
		return Run{}, errors.New("update trace_id mocked failure")
	}

	attrs := map[string]string{
		"agentscope.run.id":     preRun.ID,
		"agentscope.agent.name": "sdk-agent",
	}
	// This should NOT silently succeed with old trace_id
	_, err = svc.CreateOrGetLiveRunFromTrace(ctx, "proj-1", "new-otlp-trace", attrs)
	if err == nil {
		t.Fatal("expected error when UpdateRunTraceID fails, but got nil")
	}
}
