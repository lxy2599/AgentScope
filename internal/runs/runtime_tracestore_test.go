package runs

import (
	"context"
	"testing"
	"time"
)

// ---- fakeRuntimeTracestore ----

type fakeRuntimeTracestore struct {
	llmCalls       map[string][]LLMCallRecord
	toolCalls      map[string][]ToolCallRecord
	retrievalCalls map[string][]RetrievalCallRecord
	skillCalls     map[string][]SkillCallRecord
	thoughts       map[string][]ThoughtRecord
	finalAnswers   map[string][]FinalAnswerRecord
	spans          map[string][]SpanRecord
}

func newFakeRuntimeTracestore() *fakeRuntimeTracestore {
	return &fakeRuntimeTracestore{
		llmCalls:       make(map[string][]LLMCallRecord),
		toolCalls:      make(map[string][]ToolCallRecord),
		retrievalCalls: make(map[string][]RetrievalCallRecord),
		skillCalls:     make(map[string][]SkillCallRecord),
		thoughts:       make(map[string][]ThoughtRecord),
		finalAnswers:   make(map[string][]FinalAnswerRecord),
		spans:          make(map[string][]SpanRecord),
	}
}

func (f *fakeRuntimeTracestore) ListLLMCallsByRun(ctx context.Context, runID string) ([]LLMCallRecord, error) {
	return f.llmCalls[runID], nil
}
func (f *fakeRuntimeTracestore) ListToolCallsByRun(ctx context.Context, runID string) ([]ToolCallRecord, error) {
	return f.toolCalls[runID], nil
}
func (f *fakeRuntimeTracestore) ListRetrievalCallsByRun(ctx context.Context, runID string) ([]RetrievalCallRecord, error) {
	return f.retrievalCalls[runID], nil
}
func (f *fakeRuntimeTracestore) ListSkillCallsByRun(ctx context.Context, runID string) ([]SkillCallRecord, error) {
	return f.skillCalls[runID], nil
}
func (f *fakeRuntimeTracestore) ListThoughtsByRun(ctx context.Context, runID string) ([]ThoughtRecord, error) {
	return f.thoughts[runID], nil
}
func (f *fakeRuntimeTracestore) ListFinalAnswersByRun(ctx context.Context, runID string) ([]FinalAnswerRecord, error) {
	return f.finalAnswers[runID], nil
}
func (f *fakeRuntimeTracestore) ListSpansByRun(ctx context.Context, runID string) ([]SpanRecord, error) {
	return f.spans[runID], nil
}

func TestRuntimeServiceGetRuntimeRunDetailWithTracestore(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	fq := newFakeRuntimeQuerier()
	ft := newFakeRuntimeTracestore()

	_, err := fq.StartRuntimeRun(context.Background(), "p1", "running", "trace-1", "test-agent", "hello", nil)
	if err != nil {
		t.Fatalf("StartRuntimeRun: %v", err)
	}
	runID := ""
	for id := range fq.runs {
		runID = id
		break
	}

	ft.thoughts[runID] = []ThoughtRecord{
		{ProjectID: "p1", RunID: runID, TraceID: "trace-1", SpanID: "span-th-1", Summary: "thinking step", StartTime: now, ContentCaptured: true},
	}
	ft.llmCalls[runID] = []LLMCallRecord{
		{ProjectID: "p1", RunID: runID, TraceID: "trace-1", SpanID: "span-llm-1", Provider: "openai", Model: "gpt-4", InputSummary: "input", OutputSummary: "output", ContentCaptured: true, Status: "OK", StartTime: now},
	}
	ft.toolCalls[runID] = []ToolCallRecord{
		{ProjectID: "p1", RunID: runID, TraceID: "trace-1", SpanID: "span-tool-1", ToolName: "search", ArgumentsSummary: "{}", ResultSummary: "result", Success: true, StartTime: now},
	}
	ft.retrievalCalls[runID] = []RetrievalCallRecord{
		{ProjectID: "p1", RunID: runID, TraceID: "trace-1", SpanID: "span-ret-1", KnowledgeBase: "kb-1", QueryText: "query", StartTime: now},
	}
	ft.skillCalls[runID] = []SkillCallRecord{
		{ProjectID: "p1", RunID: runID, TraceID: "trace-1", SpanID: "span-skill-1", SkillName: "test-skill", Success: true, StartTime: now},
	}
	ft.finalAnswers[runID] = []FinalAnswerRecord{
		{ProjectID: "p1", RunID: runID, TraceID: "trace-1", SpanID: "span-fa-1", AnswerSummary: "done", StartTime: now},
	}
	ft.spans[runID] = []SpanRecord{
		{RunID: runID, SpanID: "span-raw-1", TraceID: "trace-1", ProjectID: "p1", Name: "memory_read", StartTime: now, Truncated: false},
	}

	svc := NewRuntimeService(fq, ft)
	detail, err := svc.GetRuntimeRunDetail(context.Background(), runID, "p1")
	if err != nil {
		t.Fatalf("GetRuntimeRunDetail: %v", err)
	}

	if len(detail.Thoughts) != 1 || detail.Thoughts[0].Summary != "thinking step" {
		t.Fatalf("Thoughts: got %+v", detail.Thoughts)
	}
	if len(detail.LLMCalls) != 1 || detail.LLMCalls[0].Model != "gpt-4" {
		t.Fatalf("LLMCalls: got %+v", detail.LLMCalls)
	}
	if len(detail.ToolCalls) != 1 || detail.ToolCalls[0].ToolName != "search" {
		t.Fatalf("ToolCalls: got %+v", detail.ToolCalls)
	}
	if len(detail.RetrievalCalls) != 1 || detail.RetrievalCalls[0].KnowledgeBase != "kb-1" {
		t.Fatalf("RetrievalCalls: got %+v", detail.RetrievalCalls)
	}
	if len(detail.SkillCalls) != 1 || detail.SkillCalls[0].SkillName != "test-skill" {
		t.Fatalf("SkillCalls: got %+v", detail.SkillCalls)
	}
	if len(detail.FinalAnswers) != 1 || detail.FinalAnswers[0].AnswerSummary != "done" {
		t.Fatalf("FinalAnswers: got %+v", detail.FinalAnswers)
	}
	if len(detail.Spans) != 1 || detail.Spans[0].Name != "memory_read" {
		t.Fatalf("Spans: got %+v", detail.Spans)
	}
}

func TestRuntimeServiceGetRuntimeRunDetailReturnsEmptyWhenNoTracestore(t *testing.T) {
	fq := newFakeRuntimeQuerier()
	_, err := fq.StartRuntimeRun(context.Background(), "p1", "completed", "trace-2", "test-agent", "input", nil)
	if err != nil {
		t.Fatalf("StartRuntimeRun: %v", err)
	}
	svc := NewRuntimeService(fq, nil)
	runID := ""
	for id := range fq.runs {
		runID = id
		break
	}
	detail, err := svc.GetRuntimeRunDetail(context.Background(), runID, "p1")
	if err != nil {
		t.Fatalf("GetRuntimeRunDetail: %v", err)
	}
	if len(detail.Thoughts) != 0 {
		t.Fatalf("expected empty Thoughts, got %d", len(detail.Thoughts))
	}
	if len(detail.Spans) != 0 {
		t.Fatalf("expected empty Spans, got %d", len(detail.Spans))
	}
}
