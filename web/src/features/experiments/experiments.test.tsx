import { render, screen, waitFor } from "@testing-library/react";
import { beforeAll, afterAll, afterEach, describe, expect, it, vi } from "vitest";
import { ConfigProvider, App as AntdApp } from "antd";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import zhCN from "antd/locale/zh_CN";
import { StatusSummary } from "./StatusSummary";
import { buildExperimentMetricConfig, CreateExperimentPanel } from "./CreateExperimentPanel";
import type { ExperimentDetail, MetricDefinition } from "../../api/types";

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

describe("CreateExperimentPanel", () => {
  beforeAll(() => {
    vi.spyOn(globalThis, "fetch");
  });
  afterEach(() => vi.clearAllMocks());
  afterAll(() => vi.restoreAllMocks());

  it("同步 datasetId prop 到表单字段", async () => {
    vi.mocked(fetch).mockImplementation(async (request) => {
      const url = String(request);
      let body: unknown = [];
      if (url.endsWith("/datasets")) {
        body = [
          { id: "d1", project_id: "p1", name: "数据集一", created_at: "2026-01-01T00:00:00Z" },
          { id: "d2", project_id: "p1", name: "数据集二", created_at: "2026-01-01T00:00:00Z" },
        ];
      } else if (url.includes("/api/datasets/d1/cases")) {
        body = [{ id: "c1", dataset_id: "d1", input: '"问题一"', expected: '"答案一"', sort_order: 1, created_at: "2026-01-01T00:00:00Z" }];
      } else if (url.includes("/api/datasets/d2/cases")) {
        body = [{ id: "c2", dataset_id: "d2", input: '"问题二"', expected: '"答案二"', sort_order: 1, created_at: "2026-01-01T00:00:00Z" }];
      }
      return { ok: true, status: 200, json: async () => body } as Response;
    });
    const qc = new QueryClient();
    const { rerender } = render(
      <QueryClientProvider client={qc}>
        <Wrapper>
          <CreateExperimentPanel projectId="p1" datasetId="d1" />
        </Wrapper>
      </QueryClientProvider>,
    );
    expect(await screen.findByText("数据集一")).toBeInTheDocument();
    expect(await screen.findByText("问题一")).toBeInTheDocument();
    expect(await screen.findByText("已选择 1/1 条用例；新数据集默认全选")).toBeInTheDocument();

    // 更新 datasetId 为 d2
    rerender(
      <QueryClientProvider client={qc}>
        <Wrapper>
          <CreateExperimentPanel projectId="p1" datasetId="d2" />
        </Wrapper>
      </QueryClientProvider>,
    );
    await waitFor(() => expect(screen.getByText("数据集二")).toBeInTheDocument());
    expect(await screen.findByText("问题二")).toBeInTheDocument();
    expect(screen.queryByText("请填写数据集的 UUID")).not.toBeInTheDocument();
  });

  it("保留 LLM Judge 指标的完整配置", () => {
    const metric: MetricDefinition = {
      id: "metric-judge",
      project_id: "p1",
      name: "semantic-correctness",
      kind: "final_answer",
      config: JSON.stringify({
        type: "llm_judge",
        weight: 1,
        pass_threshold: 0.7,
        judges: [{
          provider: "openai-compatible",
          model: "deepseek-v4-flash",
          api_key_env: "ARK_API_KEY",
          base_url: "https://example.com/v1",
        }],
        rubric: {
          dimensions: [{ name: "concept_accuracy", weight: 1, description: "Checks core concepts." }],
        },
      }),
      created_at: "2026-01-01T00:00:00Z",
    };

    expect(buildExperimentMetricConfig(metric)).toMatchObject({
      type: "llm_judge",
      metric_name: "semantic-correctness",
      metric_kind: "final_answer",
      judges: [{
        model: "deepseek-v4-flash",
        api_key_env: "ARK_API_KEY",
        base_url: "https://example.com/v1",
      }],
      rubric: {
        dimensions: [{ name: "concept_accuracy" }],
      },
    });
  });
});
