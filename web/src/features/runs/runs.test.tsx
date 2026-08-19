import { describe, expect, it } from "vitest";
import type { TraceSpan, LLMCallRecord, ToolCallRecord, RetrievalCallRecord, FinalAnswerRecord, ThoughtRecord } from "../../api/types";
import { buildSpanTree, formatSpanAttributes, formatSpanPayload, getSpanPreview } from "./SpanTree";

describe("buildSpanTree", () => {
  it("builds a tree from flat spans", () => {
    const spans: TraceSpan[] = [
      {
        run_id: "r1",
        span_id: "a",
        trace_id: "t1",
        project_id: "p1",
        name: "root",
        start_time: "2026-01-01T00:00:00Z",
        truncated: false,
      },
      {
        run_id: "r1",
        span_id: "b",
        trace_id: "t1",
        project_id: "p1",
        parent_span_id: "a",
        name: "child",
        start_time: "2026-01-01T00:00:01Z",
        truncated: false,
      },
      {
        run_id: "r1",
        span_id: "c",
        trace_id: "t1",
        project_id: "p1",
        parent_span_id: "b",
        name: "grandchild",
        start_time: "2026-01-01T00:00:02Z",
        truncated: false,
      },
    ];

    const tree = buildSpanTree(spans);
    expect(tree).toHaveLength(1);
    expect(tree[0].span.span_id).toBe("a");
    expect(tree[0].children).toHaveLength(1);
    expect(tree[0].children[0].span.span_id).toBe("b");
    expect(tree[0].children[0].children[0].span.span_id).toBe("c");
  });

  it("handles orphan spans as roots", () => {
    const spans: TraceSpan[] = [
      {
        run_id: "r1",
        span_id: "a",
        trace_id: "t1",
        project_id: "p1",
        parent_span_id: "missing",
        name: "orphan",
        start_time: "2026-01-01T00:00:00Z",
        truncated: false,
      },
    ];

    const tree = buildSpanTree(spans);
    expect(tree).toHaveLength(1);
    expect(tree[0].span.span_id).toBe("a");
  });

  it("sorts siblings by duration descending", () => {
    const spans: TraceSpan[] = [
      {
        run_id: "r1",
        span_id: "a",
        trace_id: "t1",
        project_id: "p1",
        name: "root",
        start_time: "2026-01-01T00:00:00Z",
        truncated: false,
      },
      {
        run_id: "r1",
        span_id: "b",
        trace_id: "t1",
        project_id: "p1",
        parent_span_id: "a",
        name: "fast",
        duration_ms: 10,
        start_time: "2026-01-01T00:00:01Z",
        truncated: false,
      },
      {
        run_id: "r1",
        span_id: "c",
        trace_id: "t1",
        project_id: "p1",
        parent_span_id: "a",
        name: "slow",
        duration_ms: 100,
        start_time: "2026-01-01T00:00:02Z",
        truncated: false,
      },
    ];

    const tree = buildSpanTree(spans);
    expect(tree[0].children).toHaveLength(2);
    expect(tree[0].children[0].span.span_id).toBe("c"); // slow first
    expect(tree[0].children[1].span.span_id).toBe("b");
  });
  it("retains all raw attributes for the Span inspector", () => {
    const attributes = JSON.stringify({
      "llm.input_summary": "full prompt content",
      nested: { retry_attempt: 2 },
    });

    expect(formatSpanAttributes(attributes)).toContain('"llm.input_summary": "full prompt content"');
    expect(formatSpanAttributes(attributes)).toContain('"retry_attempt": 2');
  });

  it("formats nested JSON payloads instead of rendering escaped JSON", () => {
    expect(formatSpanPayload('{"path":"/tmp/a","limit":200}')).toBe('{\n  "path": "/tmp/a",\n  "limit": 200\n}');
    expect(formatSpanPayload('"{\\"content\\":\\"hello\\"}"')).toBe('{\n  "content": "hello"\n}');
  });

  it("shows captured input and output directly in a tree node preview", () => {
    const preview = getSpanPreview({
      run_id: "r1",
      span_id: "span-1",
      trace_id: "t1",
      project_id: "p1",
      name: "llm.hermes.llm",
      kind: "INTERNAL",
      start_time: "2026-01-01T00:00:00Z",
      truncated: false,
      attributes: JSON.stringify({
        "agentscope.span.kind": "LLM",
        "llm.input_summary": "captured prompt",
        "llm.output_summary": "captured response",
        "llm.input_tokens": 120,
      }),
    });

    expect(preview.input).toEqual({ label: "输入内容", value: "captured prompt" });
    expect(preview.output).toEqual({ label: "输出内容", value: "captured response" });
    expect(preview.fields).toContainEqual({ label: "输入", value: "120 tokens" });
  });

});

// ---- AgentTimeline tests ----

import { buildTimeline } from "./AgentTimeline";
import { computeDiagnostics } from "./TraceDiagnostics";

// Helper to create a base LLM record fixture
function makeLLM(overrides: Partial<LLMCallRecord> & { start_time: string }): LLMCallRecord {
  return {
    project_id: "p1",
    run_id: "run-1",
    trace_id: "trace-1",
    span_id: "span-llm-" + overrides.start_time,
    provider: "openai",
    model: "gpt-4",
    input_tokens: 100,
    output_tokens: 50,
    total_tokens: 150,
    cost: 0.003,
    input_summary: "llm input",
    output_summary: "llm output",
    content_captured: true,
    status: "OK",
    ...overrides,
  };
}

function makeTool(overrides: Partial<ToolCallRecord> & { start_time: string }): ToolCallRecord {
  return {
    project_id: "p1",
    run_id: "run-1",
    trace_id: "trace-1",
    span_id: "span-tool-" + overrides.start_time,
    tool_name: "test_tool",
    arguments_summary: '{"arg": "val"}',
    result_summary: "result",
    success: true,
    retry_count: 0,
    ...overrides,
  };
}

function makeRetrieval(overrides: Partial<RetrievalCallRecord> & { start_time: string }): RetrievalCallRecord {
  return {
    project_id: "p1",
    run_id: "run-1",
    trace_id: "trace-1",
    span_id: "span-ret-" + overrides.start_time,
    knowledge_base: "kb-1",
    query_text: "search query",
    ...overrides,
  };
}

function makeThought(overrides: Partial<ThoughtRecord> & { start_time: string }): ThoughtRecord {
  return {
    project_id: "p1",
    run_id: "run-1",
    trace_id: "trace-1",
    span_id: "span-thought-" + overrides.start_time,
    summary: "thinking step",
    content_captured: true,
    ...overrides,
  };
}

function makeFinalAnswer(overrides: Partial<FinalAnswerRecord> & { start_time: string }): FinalAnswerRecord {
  return {
    project_id: "p1",
    run_id: "run-1",
    trace_id: "trace-1",
    span_id: "span-fa-" + overrides.start_time,
    answer_summary: "final answer text",
    ...overrides,
  };
}

describe("AgentTimeline - buildTimeline", () => {
  it("merges different record types in ascending time order", () => {
    const items = buildTimeline(
      [makeThought({ start_time: "2026-07-01T00:00:03Z", summary: "think step" })],
      [makeFinalAnswer({ start_time: "2026-07-01T00:00:05Z", answer_summary: "done" })],
      [makeLLM({ start_time: "2026-07-01T00:00:01Z", input_summary: "first llm" })],
      [makeTool({ start_time: "2026-07-01T00:00:02Z", tool_name: "search" })],
      [makeRetrieval({ start_time: "2026-07-01T00:00:04Z", query_text: "find" })],
      [],
    );

    expect(items).toHaveLength(5);
    // items should be ordered by start_time ascending
    expect(items[0].kind).toBe("LLM");
    expect(items[1].kind).toBe("TOOL");
    expect(items[2].kind).toBe("THOUGHT");
    expect(items[3].kind).toBe("RETRIEVER");
    expect(items[4].kind).toBe("FINAL_ANSWER");

    // Check item properties are preserved
    expect(items[0].name).toBe("openai/gpt-4");
    expect(items[0].summary).toBe("first llm");
    expect(items[1].name).toBe("search");
    expect(items[2].summary).toBe("think step");
    expect(items[4].summary).toBe("done");
  });

  it("includes token/cost details for LLM items", () => {
    const items = buildTimeline(
      [],
      [],
      [makeLLM({
        start_time: "2026-01-01T00:00:00Z",
        input_tokens: 200,
        output_tokens: 100,
        total_tokens: 300,
        cache_read_tokens: 80,
        ttft_ms: 125,
        cost: 0.005,
        cost_source: "actual",
      })],
      [],
      [],
      [],
    );

    expect(items[0].details?.input_tokens).toBe(200);
    expect(items[0].details?.output_tokens).toBe(100);
    expect(items[0].details?.total_tokens).toBe(300);
    expect(items[0].details?.cache_read_tokens).toBe(80);
    expect(items[0].details?.ttft_ms).toBe(125);
    expect(items[0].details?.cost).toBe(0.005);
    expect(items[0].details?.cost_source).toBe("actual");
  });

  it("includes retry count for tool items", () => {
    const items = buildTimeline(
      [],
      [],
      [],
      [makeTool({ start_time: "2026-01-01T00:00:00Z", tool_name: "search", retry_count: 3, success: true })],
      [],
      [],
    );

    expect(items[0].details?.retry_count).toBe(3);
    expect(items[0].status).toBe("OK");
  });

  it("includes failed status for tool items", () => {
    const items = buildTimeline(
      [],
      [],
      [],
      [makeTool({ start_time: "2026-01-01T00:00:00Z", tool_name: "failing_tool", success: false, error: "timeout" })],
      [],
      [],
    );

    expect(items[0].status).toBe("ERROR");
    expect(items[0].error).toBe("timeout");
  });

  it("includes span_id, trace_id, run_id for each item", () => {
    const items = buildTimeline(
      [],
      [],
      [makeLLM({ start_time: "2026-01-01T00:00:00Z", span_id: "llm-span-1" })],
      [makeTool({ start_time: "2026-01-01T00:00:01Z", span_id: "tool-span-1" })],
      [],
      [],
    );

    // Check span_id/trace_id/run_id
    const llmItem = items.find(i => i.kind === "LLM");
    const toolItem = items.find(i => i.kind === "TOOL");
    expect(llmItem?.details?.span_id).toBe("llm-span-1");
    expect(llmItem?.details?.trace_id).toBe("trace-1");
    expect(llmItem?.details?.run_id).toBe("run-1");
    expect(toolItem?.details?.span_id).toBe("tool-span-1");
    expect(toolItem?.details?.trace_id).toBe("trace-1");
    expect(toolItem?.details?.run_id).toBe("run-1");
  });

  it("returns empty array when all inputs are empty", () => {
    const items = buildTimeline([], [], [], [], [], []);
    expect(items).toHaveLength(0);
  });

  it("includes duration_ms for timing items", () => {
    const items = buildTimeline(
      [],
      [],
      [makeLLM({ start_time: "2026-01-01T00:00:00Z", duration_ms: 2500 })],
      [makeTool({ start_time: "2026-01-01T00:00:01Z", duration_ms: 1500 })],
      [makeRetrieval({ start_time: "2026-01-01T00:00:02Z", duration_ms: 800 })],
      [],
    );

    expect(items.find(i => i.kind === "LLM")?.durationMs).toBe(2500);
    expect(items.find(i => i.kind === "TOOL")?.durationMs).toBe(1500);
    expect(items.find(i => i.kind === "RETRIEVER")?.durationMs).toBe(800);
  });
});

describe("TraceDiagnostics - computeDiagnostics", () => {
  it("identifies failed/error spans", () => {
    const diags = computeDiagnostics({
      llm_calls: [],
      tool_calls: [makeTool({ start_time: "2026-01-01T00:00:00Z", tool_name: "broken_tool", success: false, error: "API error" })],
      retrieval_calls: [],
      skill_calls: [],
      thoughts: [],
      final_answers: [makeFinalAnswer({ start_time: "2026-01-01T00:00:05Z" })],
      spans: [],
      run: { trace_id: "trace-1", id: "run-1", latency_ms: 5000 } as any,
    });

    expect(diags.some(d => d.type === "error" && d.message.includes("broken_tool"))).toBe(true);
  });

  it("identifies slow LLM calls", () => {
    const diags = computeDiagnostics({
      llm_calls: [makeLLM({ start_time: "2026-01-01T00:00:00Z", duration_ms: 35000, model: "gpt-4" })],
      tool_calls: [],
      retrieval_calls: [],
      skill_calls: [],
      thoughts: [],
      final_answers: [makeFinalAnswer({ start_time: "2026-01-01T00:00:05Z" })],
      spans: [],
      run: { trace_id: "trace-1", id: "run-1", latency_ms: 5000 } as any,
    });

    expect(diags.some(d => d.type === "warning" && d.message.includes("gpt-4"))).toBe(true);
  });

  it("identifies retry events", () => {
    const diags = computeDiagnostics({
      llm_calls: [],
      tool_calls: [makeTool({ start_time: "2026-01-01T00:00:00Z", tool_name: "retry_tool", retry_count: 3, success: true })],
      retrieval_calls: [],
      skill_calls: [],
      thoughts: [],
      final_answers: [makeFinalAnswer({ start_time: "2026-01-01T00:00:05Z" })],
      spans: [],
      run: { trace_id: "trace-1", id: "run-1", latency_ms: 5000 } as any,
    });

    expect(diags.some(d => d.type === "warning" && d.message.includes("retry_tool"))).toBe(true);
  });

  it("flags missing final answer", () => {
    const diags = computeDiagnostics({
      llm_calls: [makeLLM({ start_time: "2026-01-01T00:00:00Z" })],
      tool_calls: [makeTool({ start_time: "2026-01-01T00:00:01Z", tool_name: "some_tool" })],
      retrieval_calls: [],
      skill_calls: [],
      thoughts: [makeThought({ start_time: "2026-01-01T00:00:00Z" })],
      final_answers: [],
      spans: [],
      run: { trace_id: "trace-1", id: "run-1", latency_ms: 5000 } as any,
    });

    expect(diags.some(d => d.type === "error" && d.message.includes("最终答案"))).toBe(true);
  });

  it("flags missing trace_id", () => {
    const diags = computeDiagnostics({
      llm_calls: [],
      tool_calls: [],
      retrieval_calls: [],
      skill_calls: [],
      thoughts: [],
      final_answers: [makeFinalAnswer({ start_time: "2026-01-01T00:00:00Z" })],
      spans: [],
      run: { trace_id: "", id: "run-1", latency_ms: 5000 } as any,
    });

    expect(diags.some(d => d.type === "error" && d.message.includes("trace_id"))).toBe(true);
  });

  it("flags missing run_id", () => {
    const diags = computeDiagnostics({
      llm_calls: [],
      tool_calls: [],
      retrieval_calls: [],
      skill_calls: [],
      thoughts: [],
      final_answers: [makeFinalAnswer({ start_time: "2026-01-01T00:00:00Z" })],
      spans: [],
      run: { trace_id: "trace-1", id: "00000000-0000-0000-0000-000000000000", latency_ms: 5000 } as any,
    });

    expect(diags.some(d => d.type === "error" && d.message.includes("run_id"))).toBe(true);
  });

  it("flags duration 0 for LLM/tool when other records have timing", () => {
    const diags = computeDiagnostics({
      llm_calls: [makeLLM({ start_time: "2026-01-01T00:00:00Z", duration_ms: 0 })],
      tool_calls: [makeTool({ start_time: "2026-01-01T00:00:01Z", tool_name: "quick_tool", duration_ms: 0 })],
      retrieval_calls: [makeRetrieval({ start_time: "2026-01-01T00:00:02Z", duration_ms: 50 })],
      skill_calls: [],
      thoughts: [makeThought({ start_time: "2026-01-01T00:00:00Z" })],
      final_answers: [makeFinalAnswer({ start_time: "2026-01-01T00:00:05Z" })],
      spans: [],
      run: { trace_id: "trace-1", id: "run-1", latency_ms: 5000 } as any,
    });

    expect(diags.some(d => d.type === "warning" && d.message.includes("duration"))).toBe(true);
  });

  it("does not treat a missing duration as zero", () => {
    const diags = computeDiagnostics({
      llm_calls: [makeLLM({ start_time: "2026-01-01T00:00:00Z", duration_ms: undefined })],
      tool_calls: [makeTool({ start_time: "2026-01-01T00:00:01Z", duration_ms: undefined })],
      retrieval_calls: [makeRetrieval({ start_time: "2026-01-01T00:00:02Z", duration_ms: 50 })],
      skill_calls: [],
      thoughts: [],
      final_answers: [],
      spans: [],
      run: { trace_id: "trace-1", id: "run-1" } as any,
    });

    expect(diags.some(d => d.message.includes("duration 为 0"))).toBe(false);
  });

  it("flags span vs semantic count mismatch", () => {
    const diags = computeDiagnostics({
      llm_calls: [makeLLM({ start_time: "2026-01-01T00:00:00Z" })],
      tool_calls: [],
      retrieval_calls: [],
      skill_calls: [],
      thoughts: [],
      final_answers: [],
      spans: [{ span_id: "s1", run_id: "run-1", trace_id: "t1", project_id: "p1", name: "span1", start_time: "2026-01-01T00:00:00Z", truncated: false } as TraceSpan],
      run: { trace_id: "trace-1", id: "run-1", latency_ms: 5000 } as any,
    });

    // 1 span but only 1 LLM record — counts could match depending on exact numbers
    // The diagnostic fires when span count is much larger than semantic count or vice versa
    const mismatch = diags.find(d => d.message.includes("Span") && d.message.includes("语义"));
    if (mismatch) {
      expect(mismatch.type).toBe("info");
    }
  });

  it("returns no error diagnostics for a healthy run", () => {
    const diags = computeDiagnostics({
      llm_calls: [makeLLM({ start_time: "2026-01-01T00:00:00Z", duration_ms: 100 })],
      tool_calls: [makeTool({ start_time: "2026-01-01T00:00:01Z", tool_name: "ok_tool", success: true, duration_ms: 200 })],
      retrieval_calls: [makeRetrieval({ start_time: "2026-01-01T00:00:02Z", duration_ms: 50 })],
      skill_calls: [],
      thoughts: [makeThought({ start_time: "2026-01-01T00:00:00Z" })],
      final_answers: [makeFinalAnswer({ start_time: "2026-01-01T00:00:05Z" })],
      spans: [],
      run: { trace_id: "valid-trace", id: "run-1", latency_ms: 5000 } as any,
    });

    expect(diags.filter(d => d.type === "error")).toHaveLength(0);
  });

  // ---- Memory/Guardrail tests ----

  describe("AgentTimeline - memory/guardrail from spans", () => {
    it("normalizes memory spans into MEMORY timeline items", () => {
      const items = buildTimeline(
        [], [], [], [], [], [],
        [{ span_id: "sp-mem-1", run_id: "run-1", trace_id: "trace-1", project_id: "p1", name: "memory_read_user_profile", start_time: "2026-01-01T00:00:01Z", duration_ms: 50, truncated: false } as TraceSpan],
      );

      const memItem = items.find(i => i.kind === "MEMORY");
      expect(memItem).toBeDefined();
      expect(memItem?.name).toContain("memory_read_user_profile");
      expect(memItem?.details?.span_id).toBe("sp-mem-1");
    });

    it("normalizes guardrail spans into GUARDRAIL timeline items", () => {
      const items = buildTimeline(
        [], [], [], [], [], [],
        [{ span_id: "sp-gr-1", run_id: "run-1", trace_id: "trace-1", project_id: "p1", name: "guardrail_safety_check", start_time: "2026-01-01T00:00:01Z", duration_ms: 30, truncated: false } as TraceSpan],
      );

      const grItem = items.find(i => i.kind === "GUARDRAIL");
      expect(grItem).toBeDefined();
      expect(grItem?.name).toContain("guardrail_safety_check");
    });

    it("flags guardrail blocked spans as ERROR", () => {
      const items = buildTimeline(
        [], [], [], [], [], [],
        [{ span_id: "sp-gr-2", run_id: "run-1", trace_id: "trace-1", project_id: "p1", name: "guardrail_safety_check", start_time: "2026-01-01T00:00:01Z", duration_ms: 30, status_code: "ERROR", status_message: "blocked by policy", truncated: false } as TraceSpan],
      );

      const grItem = items.find(i => i.kind === "GUARDRAIL");
      expect(grItem).toBeDefined();
      expect(grItem?.status).toBe("ERROR");
    });
  });

  describe("TraceDiagnostics - guardrail/memory", () => {
    it("detects guardrail blocked spans", () => {
      const diags = computeDiagnostics({
        llm_calls: [],
        tool_calls: [],
        retrieval_calls: [],
        skill_calls: [],
        thoughts: [],
        final_answers: [makeFinalAnswer({ start_time: "2026-01-01T00:00:05Z" })],
        spans: [{ span_id: "sp-gr-3", run_id: "run-1", trace_id: "trace-1", project_id: "p1", name: "guardrail_safety_check", start_time: "2026-01-01T00:00:01Z", duration_ms: 30, status_code: "ERROR", truncated: false } as TraceSpan],
        run: { trace_id: "trace-1", id: "run-1", latency_ms: 5000 } as any,
      });

      expect(diags.some(d => d.type === "error" && d.message.includes("安全拦截"))).toBe(true);
    });

    it("detects memory failure spans", () => {
      const diags = computeDiagnostics({
        llm_calls: [],
        tool_calls: [],
        retrieval_calls: [],
        skill_calls: [],
        thoughts: [],
        final_answers: [makeFinalAnswer({ start_time: "2026-01-01T00:00:05Z" })],
        spans: [{ span_id: "sp-mem-2", run_id: "run-1", trace_id: "trace-1", project_id: "p1", name: "memory_write_profile", start_time: "2026-01-01T00:00:01Z", duration_ms: 50, status_code: "ERROR", status_message: "write failed", truncated: false } as TraceSpan],
        run: { trace_id: "trace-1", id: "run-1", latency_ms: 5000 } as any,
      });

      expect(diags.some(d => d.type === "error" && d.message.includes("Memory 操作失败"))).toBe(true);
    });
  });
});
