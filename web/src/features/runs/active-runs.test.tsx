import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import ActiveRunsPage from "./ActiveRunsPage";
import RunRuntimeDetailPage from "./RunRuntimeDetailPage";

function mockFetch(status: number, body: unknown) {
  global.fetch = vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  } as Response);
}

function Wrapper({
  children,
  path = "/projects/p1/active-runs",
}: {
  children: ReactNode;
  path?: string;
}) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>{children}</MemoryRouter>
    </QueryClientProvider>
  );
}

describe("ActiveRunsPage", () => {
  it("渲染 active run 列表和中文状态", async () => {
    mockFetch(200, [
      {
        id: "run-1",
        project_id: "p1",
        status: "waiting_llm",
        agent_name: "test-agent",
        agent_instance_name: "inst-1",
        input_summary: "hello",
        updated_at: "2026-07-18T10:00:00Z",
      },
      {
        id: "run-2",
        project_id: "p1",
        status: "running",
        agent_name: "agent-b",
        agent_instance_name: "inst-2",
        input_summary: "world",
        updated_at: "2026-07-18T10:01:00Z",
      },
    ]);

    render(
      <Wrapper>
        <Routes>
          <Route
            path="/projects/:projectId/active-runs"
            element={<ActiveRunsPage />}
          />
        </Routes>
      </Wrapper>,
    );

    expect(await screen.findByText("test-agent")).toBeInTheDocument();
    expect(screen.getByText("等待 LLM")).toBeInTheDocument();
    expect(screen.getByText("agent-b")).toBeInTheDocument();
    expect(screen.getByText("运行中")).toBeInTheDocument();
    expect(screen.getByText("在线运行")).toBeInTheDocument();
  });

  it("空状态显示中文提示", async () => {
    mockFetch(200, []);

    render(
      <Wrapper>
        <Routes>
          <Route
            path="/projects/:projectId/active-runs"
            element={<ActiveRunsPage />}
          />
        </Routes>
      </Wrapper>,
    );

    expect(await screen.findByText("暂无在线运行")).toBeInTheDocument();
  });

  it("渲染详情链接", async () => {
    mockFetch(200, [
      {
        id: "run-1",
        project_id: "p1",
        status: "running",
        agent_name: "test-agent",
        updated_at: "2026-07-18T10:00:00Z",
      },
    ]);

    render(
      <Wrapper>
        <Routes>
          <Route
            path="/projects/:projectId/active-runs"
            element={<ActiveRunsPage />}
          />
        </Routes>
      </Wrapper>,
    );

    expect(await screen.findByText("查看详情")).toBeInTheDocument();
    expect(screen.getByText("查看详情").closest("a")).toHaveAttribute(
      "href",
      "/projects/p1/runs/run-1/runtime",
    );
  });
});

describe("RunRuntimeDetailPage", () => {
  it("渲染运行中 run 的语义事件（thought/LLM/tool）", async () => {
    mockFetch(200, {
      id: "run-1",
      project_id: "p1",
      status: "running",
      agent_name: "test-agent",
      agent_instance_name: "inst-1",
      input: "用户查询",
      trace_id: "trace-abc",
      created_at: "2026-07-18T10:00:00Z",
      updated_at: "2026-07-18T10:01:00Z",
      events: [
        { id: "evt-1", run_id: "run-1", from_status: "", to_status: "running", message: "started", created_at: "2026-07-18T10:00:00Z" },
        { id: "evt-2", run_id: "run-1", from_status: "running", to_status: "waiting_llm", message: "calling model", created_at: "2026-07-18T10:00:05Z" },
      ],
      // Simulating already-arrived semantic events from ClickHouse
      thoughts: [
        { project_id: "p1", run_id: "run-1", trace_id: "trace-abc", span_id: "span-th-1", summary: "用户需要查询订单状态", content_captured: true, start_time: "2026-07-18T10:00:03Z" },
      ],
      llm_calls: [
        { project_id: "p1", run_id: "run-1", trace_id: "trace-abc", span_id: "span-llm-1", provider: "openai", model: "gpt-4", input_tokens: 150, output_tokens: 30, total_tokens: 180, cost: 0.002, input_summary: "query order", output_summary: "use query_order tool", content_captured: true, status: "OK", start_time: "2026-07-18T10:00:01Z", duration_ms: 3000 },
      ],
      tool_calls: [
        { project_id: "p1", run_id: "run-1", trace_id: "trace-abc", span_id: "span-tool-1", tool_name: "query_order", arguments_summary: '{"order_id":"123"}', result_summary: '{"status":"delivered"}', success: true, start_time: "2026-07-18T10:00:04Z", duration_ms: 500, retry_count: 0 },
      ],
      final_answers: [],
    });

    render(
      <Wrapper path="/projects/p1/runs/run-1/runtime">
        <Routes>
          <Route
            path="/projects/:projectId/runs/:runId/runtime"
            element={<RunRuntimeDetailPage />}
          />
        </Routes>
      </Wrapper>,
    );

    // Should show basic run info
    expect(await screen.findByText("运行详情")).toBeInTheDocument();

    // Should show the semantic events section
    expect(screen.getByText("已采集语义事件")).toBeInTheDocument();

    // Should show thought, LLM, and tool events
    expect(screen.getByText("用户需要查询订单状态")).toBeInTheDocument();
    expect(screen.getByText("query_order")).toBeInTheDocument();

    // Running state tag - may appear multiple times (semantic card + event list)
    expect(screen.getAllByText("运行中").length).toBeGreaterThanOrEqual(1);
  });

  it("运行中 run 无语义事件时显示等待状态", async () => {
    mockFetch(200, {
      id: "run-1",
      project_id: "p1",
      status: "running",
      agent_name: "test-agent",
      input: "test input",
      trace_id: "",
      created_at: "2026-07-18T10:00:00Z",
      updated_at: "2026-07-18T10:00:00Z",
      events: [
        { id: "evt-1", run_id: "run-1", from_status: "", to_status: "running", message: "started", created_at: "2026-07-18T10:00:00Z" },
      ],
      thoughts: [],
      llm_calls: [],
      tool_calls: [],
      final_answers: [],
    });

    render(
      <Wrapper path="/projects/p1/runs/run-1/runtime">
        <Routes>
          <Route
            path="/projects/:projectId/runs/:runId/runtime"
            element={<RunRuntimeDetailPage />}
          />
        </Routes>
      </Wrapper>,
    );

    expect(await screen.findByText("运行详情")).toBeInTheDocument();
    expect(screen.getByText("等待语义事件到达")).toBeInTheDocument();
  });

  it("渲染状态事件时间线", async () => {
    mockFetch(200, {
      id: "run-1",
      project_id: "p1",
      status: "completed",
      agent_name: "test-agent",
      agent_instance_name: "inst-1",
      input: "hello world",
      error: "",
      created_at: "2026-07-18T10:00:00Z",
      updated_at: "2026-07-18T10:02:00Z",
      events: [
        {
          id: "evt-1",
          run_id: "run-1",
          from_status: "",
          to_status: "running",
          message: "started",
          created_at: "2026-07-18T10:00:00Z",
        },
        {
          id: "evt-2",
          run_id: "run-1",
          from_status: "running",
          to_status: "waiting_llm",
          message: "calling model",
          created_at: "2026-07-18T10:00:05Z",
        },
        {
          id: "evt-3",
          run_id: "run-1",
          from_status: "waiting_llm",
          to_status: "completed",
          message: "completed",
          created_at: "2026-07-18T10:02:00Z",
        },
      ],
    });

    render(
      <Wrapper path="/projects/p1/runs/run-1/runtime">
        <Routes>
          <Route
            path="/projects/:projectId/runs/:runId/runtime"
            element={<RunRuntimeDetailPage />}
          />
        </Routes>
      </Wrapper>,
    );

    expect(await screen.findByText("运行详情")).toBeInTheDocument();
    expect(screen.getByText("等待 LLM")).toBeInTheDocument();
    expect(screen.getByText("状态事件时间线")).toBeInTheDocument();
    expect(screen.getByText("started")).toBeInTheDocument();
  });

  it("Trace 链接存在", async () => {
    mockFetch(200, {
      id: "run-1",
      project_id: "p1",
      status: "completed",
      agent_name: "test-agent",
      trace_id: "trace-abc",
      created_at: "2026-07-18T10:00:00Z",
      updated_at: "2026-07-18T10:02:00Z",
      events: [],
    });

    render(
      <Wrapper path="/projects/p1/runs/run-1/runtime">
        <Routes>
          <Route
            path="/projects/:projectId/runs/:runId/runtime"
            element={<RunRuntimeDetailPage />}
          />
        </Routes>
      </Wrapper>,
    );

    expect(await screen.findByText("查看 Trace")).toBeInTheDocument();
  });

  it("处理空事件列表", async () => {
    mockFetch(200, {
      id: "run-1",
      project_id: "p1",
      status: "running",
      agent_name: "test-agent",
      created_at: "2026-07-18T10:00:00Z",
      updated_at: "2026-07-18T10:00:00Z",
      events: [],
    });

    render(
      <Wrapper path="/projects/p1/runs/run-1/runtime">
        <Routes>
          <Route
            path="/projects/:projectId/runs/:runId/runtime"
            element={<RunRuntimeDetailPage />}
          />
        </Routes>
      </Wrapper>,
    );

    expect(await screen.findByText("运行详情")).toBeInTheDocument();
  });

  it("completed run 仍有语义事件时展示 Agent 时间线", async () => {
    mockFetch(200, {
      id: "run-1",
      project_id: "p1",
      status: "completed",
      agent_name: "test-agent",
      input: "test input",
      trace_id: "trace-abc",
      created_at: "2026-07-18T10:00:00Z",
      updated_at: "2026-07-18T10:02:00Z",
      events: [
        { id: "evt-1", run_id: "run-1", from_status: "", to_status: "running", message: "started", created_at: "2026-07-18T10:00:00Z" },
        { id: "evt-2", run_id: "run-1", from_status: "running", to_status: "completed", message: "completed", created_at: "2026-07-18T10:02:00Z" },
      ],
      llm_calls: [
        { project_id: "p1", run_id: "run-1", trace_id: "trace-abc", span_id: "span-llm-1", provider: "openai", model: "gpt-4", input_tokens: 100, output_tokens: 50, total_tokens: 150, cost: 0.002, input_summary: "query", output_summary: "response", content_captured: true, status: "OK", start_time: "2026-07-18T10:00:01Z", duration_ms: 2000 },
      ],
      final_answers: [
        { project_id: "p1", run_id: "run-1", trace_id: "trace-abc", span_id: "span-fa-1", answer_summary: "已完成操作", start_time: "2026-07-18T10:02:00Z" },
      ],
    });

    render(
      <Wrapper path="/projects/p1/runs/run-1/runtime">
        <Routes>
          <Route
            path="/projects/:projectId/runs/:runId/runtime"
            element={<RunRuntimeDetailPage />}
          />
        </Routes>
      </Wrapper>,
    );

    expect(await screen.findByText("运行详情")).toBeInTheDocument();
    expect(screen.getByText("已采集语义事件")).toBeInTheDocument();
    expect(screen.getByText("已完成操作")).toBeInTheDocument();
    expect(screen.getAllByText("已完成").length).toBeGreaterThanOrEqual(1);
  });
});
