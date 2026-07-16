import { describe, expect, it } from "vitest";
import type { TraceSpan } from "../../api/types";
import { buildSpanTree } from "./SpanTree";

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
});
