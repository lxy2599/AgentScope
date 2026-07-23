package adapter

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"AgentScope/internal/runs"
)

func TestRuntimeDetailToResponseContainsSemanticFields(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)

	detail := runs.RuntimeRunDetail{
		RuntimeRun: runs.RuntimeRun{
			ID:        "run-1",
			ProjectID: "p1",
			Status:    "completed",
			TraceID:   "trace-1",
			AgentName: "test-agent",
			CreatedAt: now,
			UpdatedAt: now,
		},
		AgentInstanceID:   "inst-1",
		AgentInstanceName: "instance-1",
		Events: []runs.RunStateEvent{
			{ID: "evt-1", RunID: "run-1", ToStatus: "completed", CreatedAt: now},
		},
		Thoughts: []runs.ThoughtRecord{
			{ProjectID: "p1", RunID: "run-1", TraceID: "trace-1", SpanID: "span-th-1", Summary: "thinking step", ContentCaptured: true, StartTime: now},
		},
		LLMCalls: []runs.LLMCallRecord{
			{ProjectID: "p1", RunID: "run-1", TraceID: "trace-1", SpanID: "span-llm-1", Provider: "openai", Model: "gpt-4", InputSummary: "input", OutputSummary: "output", ContentCaptured: true, Status: "OK", StartTime: now},
		},
		ToolCalls: []runs.ToolCallRecord{
			{ProjectID: "p1", RunID: "run-1", TraceID: "trace-1", SpanID: "span-tool-1", ToolName: "search", ArgumentsSummary: "{}", ResultSummary: "result", Success: true, StartTime: now},
		},
		RetrievalCalls: []runs.RetrievalCallRecord{
			{ProjectID: "p1", RunID: "run-1", TraceID: "trace-1", SpanID: "span-ret-1", KnowledgeBase: "kb-1", QueryText: "query", StartTime: now},
		},
		SkillCalls: []runs.SkillCallRecord{
			{ProjectID: "p1", RunID: "run-1", TraceID: "trace-1", SpanID: "span-skill-1", SkillName: "test-skill", Success: true, StartTime: now},
		},
		FinalAnswers: []runs.FinalAnswerRecord{
			{ProjectID: "p1", RunID: "run-1", TraceID: "trace-1", SpanID: "span-fa-1", AnswerSummary: "done", StartTime: now},
		},
		Spans: []runs.SpanRecord{
			{RunID: "run-1", SpanID: "span-raw-1", TraceID: "trace-1", ProjectID: "p1", Name: "memory_read", StartTime: now, Truncated: false},
		},
	}

	resp := runtimeDetailToResponse(detail)

	// Assert top-level fields
	if resp.ID != "run-1" {
		t.Fatalf("ID: got %q", resp.ID)
	}
	if resp.TraceID != "trace-1" {
		t.Fatalf("TraceID: got %q", resp.TraceID)
	}
	if resp.Status != "completed" {
		t.Fatalf("Status: got %q", resp.Status)
	}

	// Assert events
	if len(resp.Events) != 1 || resp.Events[0].ID != "evt-1" {
		t.Fatalf("Events: got %+v", resp.Events)
	}

	// Assert semantic events
	if len(resp.Thoughts) != 1 || resp.Thoughts[0].Summary != "thinking step" {
		t.Fatalf("Thoughts: got %+v", resp.Thoughts)
	}
	if len(resp.Thoughts) > 0 {
		if resp.Thoughts[0].RunID != "run-1" || resp.Thoughts[0].TraceID != "trace-1" || resp.Thoughts[0].SpanID != "span-th-1" {
			t.Fatalf("Thought key fields: got RunID=%s TraceID=%s SpanID=%s", resp.Thoughts[0].RunID, resp.Thoughts[0].TraceID, resp.Thoughts[0].SpanID)
		}
	}

	if len(resp.LLMCalls) != 1 || resp.LLMCalls[0].Model != "gpt-4" {
		t.Fatalf("LLMCalls: got %+v", resp.LLMCalls)
	}
	if len(resp.LLMCalls) > 0 {
		if resp.LLMCalls[0].Provider != "openai" || resp.LLMCalls[0].RunID != "run-1" {
			t.Fatalf("LLM key fields: Provider=%s RunID=%s", resp.LLMCalls[0].Provider, resp.LLMCalls[0].RunID)
		}
	}

	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].ToolName != "search" {
		t.Fatalf("ToolCalls: got %+v", resp.ToolCalls)
	}
	if len(resp.ToolCalls) > 0 && resp.ToolCalls[0].RunID != "run-1" {
		t.Fatalf("ToolCall RunID mismatch")
	}

	if len(resp.RetrievalCalls) != 1 || resp.RetrievalCalls[0].KnowledgeBase != "kb-1" {
		t.Fatalf("RetrievalCalls: got %+v", resp.RetrievalCalls)
	}
	if len(resp.SkillCalls) != 1 || resp.SkillCalls[0].SkillName != "test-skill" {
		t.Fatalf("SkillCalls: got %+v", resp.SkillCalls)
	}
	if len(resp.FinalAnswers) != 1 || resp.FinalAnswers[0].AnswerSummary != "done" {
		t.Fatalf("FinalAnswers: got %+v", resp.FinalAnswers)
	}
	if len(resp.Spans) != 1 || resp.Spans[0].Name != "memory_read" {
		t.Fatalf("Spans: got %+v", resp.Spans)
	}
	if len(resp.Spans) > 0 {
		if resp.Spans[0].RunID != "run-1" || resp.Spans[0].TraceID != "trace-1" || resp.Spans[0].SpanID != "span-raw-1" {
			t.Fatalf("Span key fields: RunID=%s TraceID=%s SpanID=%s", resp.Spans[0].RunID, resp.Spans[0].TraceID, resp.Spans[0].SpanID)
		}
	}

	// Verify JSON field names via marshalling
	j, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonStr := string(j)

	requiredFields := []string{
		`"thoughts"`,
		`"llm_calls"`,
		`"tool_calls"`,
		`"retrieval_calls"`,
		`"skill_calls"`,
		`"final_answers"`,
		`"spans"`,
		`"run_id"`,
		`"trace_id"`,
		`"span_id"`,
		`"start_time"`,
		`"model"`,
		`"tool_name"`,
		`"answer_summary"`,
	}
	for _, field := range requiredFields {
		if !strings.Contains(jsonStr, field) {
			t.Fatalf("missing JSON field %q in response: %s", field, jsonStr[:200])
		}
	}
}
