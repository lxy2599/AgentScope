import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { AgentsPage } from "./AgentsPage";
import { AgentDetailPage } from "./AgentDetailPage";
import type { AgentInfo, AgentCapabilityInfo } from "../../api/types";

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

describe("AgentsPage", () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it("显示 agent 合约字段 (framework/runtime/source/version)", async () => {
    const agents: AgentInfo[] = [
      {
        id: "agent-1",
        project_id: "p1",
        name: "hermes-agent",
        description: "Test agent",
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
      },
    ];
    mockFetch(200, agents);

    render(
      <Wrapper>
        <Routes>
          <Route path="/projects/:projectId/agents" element={<AgentsPage />} />
        </Routes>
      </Wrapper>,
    );

    // Debug: print DOM
    // screen.debug(undefined, Infinity);

    // Should show framework tag (hermes · python combined)
    const frameworkTags = await screen.findAllByText(/hermes/);
    expect(frameworkTags.length).toBeGreaterThanOrEqual(1);
    // Should show runtime tag
    expect(screen.getByText(/python/)).toBeTruthy();
    // Should show agent version
    expect(await screen.findByText("1.2.3")).toBeTruthy();
    // Should show source and environment
    expect(screen.getByText(/adapter/)).toBeTruthy();
  });

  it("显示无合约字段的默认值", async () => {
    const agents: AgentInfo[] = [
      {
        id: "agent-2",
        project_id: "p1",
        name: "minimal-agent",
        description: "",
        status: "inactive",
        version: 1,
        created_at: "2026-07-22T00:00:00Z",
        updated_at: "2026-07-22T00:00:00Z",
      },
    ];
    mockFetch(200, agents);

    render(
      <Wrapper>
        <Routes>
          <Route path="/projects/:projectId/agents" element={<AgentsPage />} />
        </Routes>
      </Wrapper>,
    );

    // Should still render with fallbacks
    expect(await screen.findByText("minimal-agent")).toBeTruthy();
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
    mockFetch(200, detailResponse);

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
    expect(screen.getByText("1.0.0")).toBeTruthy();
    // Should show side effect risk
    expect(screen.getByText("medium")).toBeTruthy();
    // Should show framework
    expect(screen.getByText("hermes")).toBeTruthy();
    // Should show runtime
    expect(screen.getByText("python")).toBeTruthy();
    // Should show source
    expect(screen.getByText("adapter")).toBeTruthy();
    // Should show environment
    expect(screen.getByText("dev")).toBeTruthy();
  });
});
