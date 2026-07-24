import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import type { ReactNode } from "react";
import { describe, expect, it, beforeEach, vi } from "vitest";
import { AgentsPage } from "./AgentsPage";
import { AgentDetailPage } from "./AgentDetailPage";
import type { AgentCapabilityInfo } from "../../api/types";

function mockFetch(status: number, body: unknown) {
  globalThis.fetch = vi.fn().mockResolvedValue({
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
  beforeEach(() => {
    vi.restoreAllMocks();
  });

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

  it("展示 Agent 在线状态/Runtime Contract 字段/实例数", async () => {
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
    // Runtime Contract fields
    expect(screen.getByText("langchain")).toBeInTheDocument();
    expect(screen.getByText("python")).toBeInTheDocument();
    expect(screen.getByText(/sdk/)).toBeInTheDocument();
    // Status badge should be visible in the table
    expect(screen.getByRole("cell", { name: /运行中/ })).toBeInTheDocument();
  });

  it("展示错误率标签和离线状态", async () => {
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

describe("AgentDetailPage", () => {
  it("显示 capability kind/version/risk", async () => {
    const capabilities: AgentCapabilityInfo[] = [
      {
        id: "cap-1",
        agent_id: "agent-1",
        name: "query_order",
        description: "查询订单",
        metadata: {
          kind: "tool",
          version: "1.0.0",
          side_effect_risk: "medium",
        },
        created_at: "2026-07-22T00:00:00Z",
      },
    ];

    const detailResponse = {
      id: "agent-1",
      project_id: "p1",
      name: "hermes-agent",
      description: "Test",
      status: "active",
      version: 2,
      metadata: {
        contract_version: "agent-runtime-v1",
        agent_version: "1.2.3",
        framework: "hermes",
        runtime: "python",
        source: "adapter",
        environment: "dev",
      },
      created_at: "2026-07-22T00:00:00Z",
      updated_at: "2026-07-22T00:00:00Z",
      instances: [],
      capabilities,
    };
    // Use mockResolvedValueOnce to handle 3 sequential fetches
    // 1. Agent detail, 2. Recent errors, 3. Active runs
    globalThis.fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true, status: 200,
        json: async () => detailResponse,
      } as Response)
      .mockResolvedValueOnce({
        ok: true, status: 200,
        json: async () => [],
      } as Response)
      .mockResolvedValueOnce({
        ok: true, status: 200,
        json: async () => [],
      } as Response);

    render(
      <Wrapper path="/projects/p1/agents/agent-1">
        <Routes>
          <Route path="/projects/:projectId/agents/:agentId" element={<AgentDetailPage />} />
        </Routes>
      </Wrapper>,
    );

    // Should show capability kind
    expect(await screen.findByText("tool")).toBeTruthy();
    // Should show capability version
    expect(await screen.findByText("1.0.0")).toBeTruthy();
    // Should show side effect risk
    expect(await screen.findByText("medium")).toBeTruthy();
    // Verify Runtime Contract fields are present (use findAll due to duplicates)
    expect((await screen.findAllByText(/hermes/)).length).toBeGreaterThanOrEqual(1);
    expect((await screen.findAllByText(/python/)).length).toBeGreaterThanOrEqual(1);
    expect((await screen.findAllByText(/adapter/)).length).toBeGreaterThanOrEqual(1);
    expect((await screen.findAllByText(/dev/)).length).toBeGreaterThanOrEqual(1);
  });

  it("渲染最近错误表", async () => {
    // Mock all 3 API calls that the detail page makes
    const detailResponse = {
      id: "agent-err", project_id: "p1", name: "error-agent",
      description: "Test errors", status: "active", version: 1,
      metadata: {},
      created_at: "2026-07-22T00:00:00Z", updated_at: "2026-07-22T00:00:00Z",
      instances: [], capabilities: [],
    };
    const recentErrors = [
      { id: "run-err-1", project_id: "p1", status: "failed",
        error: "API timeout after 30s", agent_name: "error-agent",
        latency_ms: 30500, input_summary: "test query",
        created_at: "2026-07-22T10:00:00Z", updated_at: "2026-07-22T10:00:30Z" },
      { id: "run-err-2", project_id: "p1", status: "timeout",
        error: "LLM response timeout", agent_name: "error-agent",
        latency_ms: 60000, input_summary: "another query",
        created_at: "2026-07-22T09:00:00Z", updated_at: "2026-07-22T09:01:00Z" },
    ];
    // Use a persistent fetch mock that returns different data based on URL
    globalThis.fetch = vi.fn().mockImplementation((url: string) => {
      if (url.includes("/recent-errors")) {
        return Promise.resolve({ ok: true, status: 200, json: async () => recentErrors });
      }
      // Use mockResolvedValue for agent detail and active runs (they return empty)
      return Promise.resolve({ ok: true, status: 200, json: async () =>
        url.includes("/active-runs") ? [] : detailResponse
      });
    });

    render(
      <Wrapper path="/projects/p1/agents/agent-err">
        <Routes>
          <Route path="/projects/:projectId/agents/:agentId" element={<AgentDetailPage />} />
        </Routes>
      </Wrapper>,
    );

    // Should show "最近错误" title
    expect((await screen.findAllByText("最近错误")).length).toBeGreaterThanOrEqual(1);
    // Wait for error messages to appear
    expect(await screen.findByText(/API timeout/)).toBeInTheDocument();
    expect(await screen.findByText(/LLM response timeout/)).toBeInTheDocument();
  });
});
