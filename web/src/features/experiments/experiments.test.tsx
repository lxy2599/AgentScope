import { render, screen } from "@testing-library/react";
import { beforeAll, afterAll, afterEach, describe, expect, it, vi } from "vitest";
import { ConfigProvider, App as AntdApp } from "antd";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import zhCN from "antd/locale/zh_CN";
import { StatusSummary } from "./StatusSummary";
import { CreateExperimentPanel } from "./CreateExperimentPanel";
import type { ExperimentDetail } from "../../api/types";

function Wrapper({ children }: { children: React.ReactNode }) {
  return (
    <ConfigProvider locale={zhCN}>
      <AntdApp>{children}</AntdApp>
    </ConfigProvider>
  );
}

const base: ExperimentDetail = {
  id: "exp-1",
  project_id: "p1",
  dataset_id: "d1",
  target_name: "test-agent",
  status: "running",
  total_cases: 10,
  passed_cases: 0,
  failed_cases: 0,
  created_at: "2026-01-01T00:00:00Z",
  status_counts: { queued: 0, running: 0, succeeded: 4, failed: 0 },
};

describe("StatusSummary", () => {
  it("renders succeeded counts and percentages", () => {
    render(
      <Wrapper>
        <StatusSummary detail={base} />
      </Wrapper>,
    );
    expect(screen.getByText((c) => c.includes("10") && c.includes("个用例"))).toBeInTheDocument();
    expect(screen.getByText("通过")).toBeInTheDocument();
    expect(screen.getByText("未通过")).toBeInTheDocument();
    expect(screen.getByText("运行中")).toBeInTheDocument();
    expect(screen.getByText("排队中")).toBeInTheDocument();
    // Should show 通过: 4/10 (40.0%) from succeeded key
    expect(screen.getByText("4")).toBeInTheDocument();
    expect(screen.getByText((c) => c.includes("10") && c.includes("40.0"))).toBeInTheDocument();
    // 0 appears in failed (0), running (0), and queued (0) — use getAllByText
    expect(screen.getAllByText("0")).toHaveLength(3);
  });

  it("falls back to passed when succeeded key is missing", () => {
    const oldFormat: ExperimentDetail = {
      ...base,
      status_counts: { queued: 0, running: 0, passed: 3, failed: 1 },
    };
    render(
      <Wrapper>
        <StatusSummary detail={oldFormat} />
      </Wrapper>,
    );
    expect(screen.getByText("3")).toBeInTheDocument();
    expect(screen.getByText((c) => c.includes("10") && c.includes("30.0"))).toBeInTheDocument();
    expect(screen.getAllByText("0").length).toBeGreaterThanOrEqual(2);
  });

  it("handles zero totals gracefully", () => {
    const empty: ExperimentDetail = {
      ...base,
      total_cases: 0,
      status_counts: {},
    };
    render(
      <Wrapper>
        <StatusSummary detail={empty} />
      </Wrapper>,
    );
    expect(screen.getByText((c) => c.includes("0") && c.includes("个用例"))).toBeInTheDocument();
    expect(screen.getByText("通过")).toBeInTheDocument();
  });
});

// Mock fetch for CreateExperimentPanel
function mockFetch(status: number, body: unknown) {
  return vi.mocked(fetch).mockResolvedValueOnce({
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
  } as Response);
}

describe("CreateExperimentPanel", () => {
  beforeAll(() => {
    vi.spyOn(globalThis, "fetch");
  });
  afterEach(() => vi.clearAllMocks());
  afterAll(() => vi.restoreAllMocks());

  it("同步 datasetId prop 到表单字段", async () => {
    mockFetch(201, { id: "exp-1" });
    const qc = new QueryClient();
    const { rerender } = render(
      <QueryClientProvider client={qc}>
        <Wrapper>
          <CreateExperimentPanel projectId="p1" datasetId="d1" />
        </Wrapper>
      </QueryClientProvider>,
    );
    // 初始值应为 d1
    const input = await screen.findByDisplayValue("d1");
    expect(input).toBeInTheDocument();

    // 更新 datasetId 为 d2
    mockFetch(201, { id: "exp-2" });
    rerender(
      <QueryClientProvider client={qc}>
        <Wrapper>
          <CreateExperimentPanel projectId="p1" datasetId="d2" />
        </Wrapper>
      </QueryClientProvider>,
    );
    // 值应更新为 d2
    expect(await screen.findByDisplayValue("d2")).toBeInTheDocument();
  });
});
