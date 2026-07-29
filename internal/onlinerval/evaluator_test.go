package onlinerval

import (
	"context"
	"errors"
	"testing"

	"AgentScope/internal/runs"
)

// ---- Fake Run Detail Provider ----

type fakeRunDetailProvider struct {
	detail *runs.RunDetail
	err    error
}

func (f *fakeRunDetailProvider) GetRunDetail(ctx context.Context, runID string) (*runs.RunDetail, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.detail, nil
}

// ---- Helper to build run details ----

func makeCompletedRunDetail(toolCalls []runs.ToolCallRecord, retrievalCalls []runs.RetrievalCallRecord, spans []runs.SpanRecord, output string) *runs.RunDetail {
	return &runs.RunDetail{
		Run: runs.Run{
			ID:        "run-detail-1",
			ProjectID: "proj-1",
			Status:    "completed",
			TraceID:   "trace-detail-1",
			Output:    output,
			Error:     "",
		},
		Spans:          spans,
		ToolCalls:      toolCalls,
		RetrievalCalls: retrievalCalls,
		Events:         []runs.SpanEventRecord{},
		LLMCalls:       []runs.LLMCallRecord{},
		SkillCalls:     []runs.SkillCallRecord{},
	}
}

func makeFailedRunDetail(toolCalls []runs.ToolCallRecord, retrievalCalls []runs.RetrievalCallRecord, errStr string) *runs.RunDetail {
	return &runs.RunDetail{
		Run: runs.Run{
			ID:        "run-detail-fail",
			ProjectID: "proj-1",
			Status:    "failed",
			TraceID:   "trace-detail-fail",
			Output:    "",
			Error:     errStr,
		},
		Spans:          []runs.SpanRecord{},
		ToolCalls:      toolCalls,
		RetrievalCalls: retrievalCalls,
		Events:         []runs.SpanEventRecord{},
		LLMCalls:       []runs.LLMCallRecord{},
		SkillCalls:     []runs.SkillCallRecord{},
	}
}

func TestTraceAwareEvaluator_CompletedRun_AllTraceData(t *testing.T) {
	detail := makeCompletedRunDetail(
		// Tool calls: one success, one failure
		[]runs.ToolCallRecord{
			{ToolName: "search", Success: true, Error: ""},
			{ToolName: "calculator", Success: false, Error: "division by zero"},
		},
		// Retrieval calls: one with documents
		[]runs.RetrievalCallRecord{
			{QueryText: "test query", ReturnedCount: int64Ptr(3), Scores: []float64{0.9, 0.8, 0.7}},
		},
		// Spans: all OK
		[]runs.SpanRecord{
			{Name: "agent.run", StatusCode: "OK"},
			{Name: "tool.search", StatusCode: "OK"},
			{Name: "tool.calculator", StatusCode: "OK"},
		},
		"Here is the final answer",
	)

	provider := &fakeRunDetailProvider{detail: detail}
	evaluator := NewTraceAwareEvaluator(provider)

	scores, err := evaluator.EvaluateRun(context.Background(), "proj-1", "run-detail-1")
	if err != nil {
		t.Fatalf("EvaluateRun() error = %v", err)
	}

	if len(scores) == 0 {
		t.Fatal("expected at least 1 score, got 0")
	}

	// Verify all 6 metric kinds are present
	metricKinds := make(map[string]float64)
	metricReasons := make(map[string]string)
	for _, s := range scores {
		metricKinds[s.MetricKind] = s.Score
		metricReasons[s.MetricName] = s.Reason
	}

	expectedKinds := []string{"efficiency", "final_answer", "tool_correctness", "trajectory", "rag_groundedness", "safety"}
	for _, kind := range expectedKinds {
		if _, ok := metricKinds[kind]; !ok {
			t.Errorf("missing metric kind %q", kind)
		}
	}

	// efficiency: completed run → 1.0
	if metricKinds["efficiency"] != 1.0 {
		t.Errorf("efficiency score = %f, want 1.0", metricKinds["efficiency"])
	}

	// final_answer: has output → 1.0
	if metricKinds["final_answer"] != 1.0 {
		t.Errorf("final_answer score = %f, want 1.0", metricKinds["final_answer"])
	}

	// tool_correctness: 1/2 tool calls succeeded → 0.5
	if metricKinds["tool_correctness"] != 0.5 {
		t.Errorf("tool_correctness score = %f, want 0.5 (1/2 passed)", metricKinds["tool_correctness"])
	}

	// trajectory: spans exist → 0.7
	if metricKinds["trajectory"] != 0.7 {
		t.Errorf("trajectory score = %f, want 0.7 (spans exist)", metricKinds["trajectory"])
	}

	// rag_groundedness: retrieval calls exist → 0.7
	if metricKinds["rag_groundedness"] != 0.7 {
		t.Errorf("rag_groundedness score = %f, want 0.7 (retrieval records exist)", metricKinds["rag_groundedness"])
	}

	// safety: no error → 1.0
	if metricKinds["safety"] != 1.0 {
		t.Errorf("safety score = %f, want 1.0 (no error)", metricKinds["safety"])
	}

	// Verify no baseline_completion_status
	for _, s := range scores {
		if s.MetricName == "baseline_completion_status" {
			t.Error("baseline_completion_status should not appear when trace-aware evaluator is used")
		}
	}
}

func TestTraceAwareEvaluator_CompletedRun_NoTraceData(t *testing.T) {
	detail := makeCompletedRunDetail(
		[]runs.ToolCallRecord{},      // no tool calls
		[]runs.RetrievalCallRecord{}, // no retrieval calls
		[]runs.SpanRecord{},          // no spans
		"final answer",
	)

	provider := &fakeRunDetailProvider{detail: detail}
	evaluator := NewTraceAwareEvaluator(provider)

	scores, err := evaluator.EvaluateRun(context.Background(), "proj-1", "run-detail-1")
	if err != nil {
		t.Fatalf("EvaluateRun() error = %v", err)
	}

	if len(scores) == 0 {
		t.Fatal("expected at least 1 score, got 0")
	}

	metricReasons := make(map[string]string)
	for _, s := range scores {
		metricReasons[s.MetricName] = s.Reason
	}

	// Trajectory: no spans → explicit reason
	if reason, ok := metricReasons["trajectory_quality"]; ok {
		if reason != traceHeuristicPrefix+"span data is unavailable; trajectory quality cannot be assessed" {
			t.Errorf("trajectory reason = %q, want 'trace data unavailable for trajectory evaluation'", reason)
		}
	} else {
		t.Error("missing trajectory_quality metric")
	}

	// RAG groundedness: no retrieval calls → explicit reason
	if reason, ok := metricReasons["rag_groundedness"]; ok {
		if reason != traceHeuristicPrefix+"retrieval records are unavailable; grounding cannot be assessed" {
			t.Errorf("rag_groundedness reason = %q, want 'retrieval records unavailable for rag_groundedness evaluation'", reason)
		}
	} else {
		t.Error("missing rag_groundedness metric")
	}

	// Tool correctness: no tool calls → explicit reason
	if reason, ok := metricReasons["tool_correctness"]; ok {
		if reason != traceHeuristicPrefix+"tool call records are unavailable; tool execution and task correctness cannot be assessed" {
			t.Errorf("tool_correctness reason = %q, want 'tool call records unavailable for tool_correctness evaluation'", reason)
		}
	} else {
		t.Error("missing tool_correctness metric")
	}
}

func TestTraceAwareEvaluator_FailedRun_WithTraceData(t *testing.T) {
	detail := makeFailedRunDetail(
		[]runs.ToolCallRecord{
			{ToolName: "search", Success: false, Error: "API timeout"},
		},
		[]runs.RetrievalCallRecord{},
		"API timeout during search",
	)

	provider := &fakeRunDetailProvider{detail: detail}
	evaluator := NewTraceAwareEvaluator(provider)

	scores, err := evaluator.EvaluateRun(context.Background(), "proj-1", "run-detail-fail")
	if err != nil {
		t.Fatalf("EvaluateRun() error = %v", err)
	}

	metricKinds := make(map[string]float64)
	for _, s := range scores {
		metricKinds[s.MetricKind] = s.Score
	}

	// efficiency: failed → 0.0
	if metricKinds["efficiency"] != 0.0 {
		t.Errorf("efficiency score = %f, want 0.0", metricKinds["efficiency"])
	}

	// tool_correctness: all tool calls failed → 0.0
	if metricKinds["tool_correctness"] != 0.0 {
		t.Errorf("tool_correctness score = %f, want 0.0", metricKinds["tool_correctness"])
	}

	// final_answer: failed → 0.0
	if metricKinds["final_answer"] != 0.0 {
		t.Errorf("final_answer score = %f, want 0.0", metricKinds["final_answer"])
	}
}

func TestTraceAwareEvaluator_ProviderError(t *testing.T) {
	provider := &fakeRunDetailProvider{
		err: errors.New("clickhouse unavailable"),
	}
	evaluator := NewTraceAwareEvaluator(provider)

	_, err := evaluator.EvaluateRun(context.Background(), "proj-1", "run-detail-1")
	if err == nil {
		t.Fatal("expected error when run detail provider fails")
	}
}

func TestTraceAwareEvaluator_AllToolCallsSucceed(t *testing.T) {
	detail := makeCompletedRunDetail(
		[]runs.ToolCallRecord{
			{ToolName: "search", Success: true},
			{ToolName: "calculator", Success: true},
			{ToolName: "search", Success: true},
		},
		[]runs.RetrievalCallRecord{},
		[]runs.SpanRecord{},
		"answer",
	)

	provider := &fakeRunDetailProvider{detail: detail}
	evaluator := NewTraceAwareEvaluator(provider)

	scores, err := evaluator.EvaluateRun(context.Background(), "proj-1", "run-detail-1")
	if err != nil {
		t.Fatalf("EvaluateRun() error = %v", err)
	}

	for _, s := range scores {
		if s.MetricKind == "tool_correctness" {
			if s.Score != 1.0 {
				t.Errorf("tool_correctness score = %f, want 1.0 (all tool calls succeeded)", s.Score)
			}
			break
		}
	}
}

func TestTraceAwareEvaluator_SafetyWithError(t *testing.T) {
	detail := makeFailedRunDetail(
		[]runs.ToolCallRecord{},
		[]runs.RetrievalCallRecord{},
		"connection timeout",
	)

	provider := &fakeRunDetailProvider{detail: detail}
	evaluator := NewTraceAwareEvaluator(provider)

	scores, err := evaluator.EvaluateRun(context.Background(), "proj-1", "run-detail-fail")
	if err != nil {
		t.Fatalf("EvaluateRun() error = %v", err)
	}

	for _, s := range scores {
		if s.MetricKind == "safety" {
			if s.Score != 0.5 {
				t.Errorf("safety score = %f, want 0.5 (run has error)", s.Score)
			}
			if s.Passed {
				t.Error("safety passed = true, want false")
			}
			break
		}
	}
}

func TestTraceAwareEvaluator_EmptySpans_NoTraceData(t *testing.T) {
	detail := makeCompletedRunDetail(
		[]runs.ToolCallRecord{},
		[]runs.RetrievalCallRecord{},
		nil, // nil spans (not even empty)
		"answer",
	)
	detail.Spans = nil

	provider := &fakeRunDetailProvider{detail: detail}
	evaluator := NewTraceAwareEvaluator(provider)

	scores, err := evaluator.EvaluateRun(context.Background(), "proj-1", "run-detail-1")
	if err != nil {
		t.Fatalf("EvaluateRun() error = %v", err)
	}

	for _, s := range scores {
		if s.MetricKind == "trajectory" {
			if s.Reason != traceHeuristicPrefix+"span data is unavailable; trajectory quality cannot be assessed" {
				t.Errorf("trajectory reason = %q, want 'trace data unavailable for trajectory evaluation'", s.Reason)
			}
			break
		}
	}
}

func int64Ptr(n int) *int64 {
	return &[]int64{int64(n)}[0]
}
