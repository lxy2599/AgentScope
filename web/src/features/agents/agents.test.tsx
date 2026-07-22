import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { AgentsPage } from "./AgentsPage";

function mockFetch(status: number, body: unknown) {
  global.fetch = vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  } as Response);
}

function Wrapper({
  children,
  path = "/projects/p1/agents",
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

describe("AgentsPage (control center)", () => {
  it("渲染 Agent 控制中心标题", async () => {
    mockFetch(200, []);

    render(
      <Wrapper>
        <Routes>
          <Route path="/projects/:projectId/agents" element={<AgentsPage />} />
        </Routes>
      </Wrapper>,
    );

    expect(await screen.findByText("Agent 控制中心")).toBeInTheDocument();
  });

  it("空状态显示中文提示", async () => {
    mockFetch(200, []);

    render(
      <Wrapper>
        <Routes>
          <Route path="/projects/:projectId/agents" element={<AgentsPage />} />
        </Routes>
      </Wrapper>,
    );

    expect(await screen.findByText("暂无已注册的 Agent")).toBeInTheDocument();
  });

  it("展示 Agent 在线状态和实例数", async () => {
    mockFetch(200, [
      {
        id: "agent-1",
        project_id: "p1",
        name: "test-agent",
        description: "A test agent",
        status: "active",
        version: 1,
        framework: "langchain",
        runtime: "python",
        source: "sdk",
        environment: "dev",
        agent_version: "1.0.0",
        model_provider: "anthropic",
        model_name: "claude-3-opus",
        instance_count: 3,
        online_instance_count: 2,
        active_run_count: 1,
        recent_run_count: 5,
        recent_error_count: 1,
        error_rate: 0.05,
        p95_latency_ms: 1234,
        total_cost: 0.05,
        current_status_summary: "running",
        last_heartbeat_at: new Date().toISOString(),
      },
    ]);

    render(
      <Wrapper>
        <Routes>
          <Route path="/projects/:projectId/agents" element={<AgentsPage />} />
        </Routes>
      </Wrapper>,
    );

    expect(await screen.findByText("test-agent")).toBeInTheDocument();
    expect(screen.getByText("langchain")).toBeInTheDocument();
    // Status badge should be visible in the table
    expect(screen.getByRole("cell", { name: /运行中/ })).toBeInTheDocument();
  });

  it("展示错误率标签", async () => {
    mockFetch(200, [
      {
        id: "agent-err",
        project_id: "p1",
        name: "error-agent",
        description: "",
        status: "active",
        version: 1,
        instance_count: 1,
        online_instance_count: 0,
        active_run_count: 0,
        recent_run_count: 0,
        recent_error_count: 0,
        error_rate: 0.15,
        current_status_summary: "offline",
      },
    ]);

    render(
      <Wrapper>
        <Routes>
          <Route path="/projects/:projectId/agents" element={<AgentsPage />} />
        </Routes>
      </Wrapper>,
    );

    expect(await screen.findByText("error-agent")).toBeInTheDocument();
    // Error rate 15% displayed
    expect(screen.getByText("15.0%")).toBeInTheDocument();
    // Offline status
    expect(screen.getByText("离线")).toBeInTheDocument();
  });
});
