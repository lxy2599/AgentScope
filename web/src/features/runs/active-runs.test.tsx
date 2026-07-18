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
});
