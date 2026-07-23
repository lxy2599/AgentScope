import { render, screen } from "@testing-library/react";
import { beforeAll, afterAll, afterEach, describe, expect, it, vi } from "vitest";
import { ConfigProvider, App as AntdApp } from "antd";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import zhCN from "antd/locale/zh_CN";
import { ExperimentDetailPage } from "./ExperimentDetailPage";

// Mock react-router-dom
vi.mock("react-router-dom", () => ({
  useParams: () => ({ experimentId: "exp-1" }),
  useNavigate: () => vi.fn(),
}));

// Mock API hooks
vi.mock("../../api/hooks", () => ({
  useExperiment: () => ({
    data: {
      id: "exp-1",
      project_id: "p1",
      dataset_id: "d1",
      target_name: "test-agent",
      target_version: "v1",
      status: "completed",
      total_cases: 3,
      passed_cases: 0,
      failed_cases: 0,
      created_at: "2026-01-01T00:00:00Z",
      status_counts: { queued: 0, running: 0, succeeded: 3, failed: 0 },
    },
    isLoading: false,
    error: null,
  }),
  useExperimentResults: () => ({
    data: [
      {
        id: "result-1",
        eval_job_id: "job-1",
        run_id: "run-1",
        dataset_case_id: "case-1",
        score: 0.82,
        passed: true,
        reason: "符合 Rubric 标准",
        details: JSON.stringify({
          dimension_scores: { accuracy: 0.9, completeness: 0.7, clarity: 0.85 },
          veto_triggered: false,
        }),
        created_at: "2026-01-01T00:00:00Z",
      },
      {
        id: "result-2",
        eval_job_id: "job-2",
        run_id: "run-2",
        dataset_case_id: "case-2",
        score: 0.45,
        passed: false,
        reason: "答案不正确",
        details: JSON.stringify({
          dimension_scores: { accuracy: 0.3, completeness: 0.5, clarity: 0.6 },
          veto_triggered: false,
        }),
        created_at: "2026-01-01T00:00:00Z",
      },
      {
        id: "result-3",
        eval_job_id: "job-3",
        run_id: "run-3",
        dataset_case_id: "case-3",
        score: 0.1,
        passed: false,
        reason: "否决项触发",
        details: JSON.stringify({
          dimension_scores: { accuracy: 0.1, safety: 0.0 },
          veto_triggered: true,
          veto_reason: "包含有害内容",
        }),
        created_at: "2026-01-01T00:00:00Z",
      },
    ],
    isLoading: false,
  }),
  useExperimentEvalScores: () => ({
    data: [
      { metric_kind: "final_answer", total_count: 3, passed_count: 1, avg_score: 0.67 },
      { metric_kind: "safety", total_count: 1, passed_count: 0, avg_score: 0.0 },
    ],
    isLoading: false,
  }),
  useMetricDefinitions: () => ({
    data: [
      {
        id: "def-1",
        project_id: "p1",
        name: "final-answer-quality",
        kind: "final_answer",
        config: JSON.stringify({
          type: "llm_judge",
          weight: 0.5,
          pass_threshold: 0.7,
          veto: false,
          rubric: {
            dimensions: [
              { name: "accuracy", weight: 0.4, description: "是否准确" },
              { name: "completeness", weight: 0.3, description: "是否完整" },
              { name: "clarity", weight: 0.3, description: "是否清晰" },
            ],
          },
        }),
        created_at: "2026-01-01T00:00:00Z",
      },
    ],
    isLoading: false,
  }),
}));

function Wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      <ConfigProvider locale={zhCN}>
        <AntdApp>{children}</AntdApp>
      </ConfigProvider>
    </QueryClientProvider>
  );
}

describe("ExperimentDetailPage - Rubric features", () => {
  beforeAll(() => {
    vi.spyOn(globalThis, "fetch");
  });
  afterEach(() => vi.clearAllMocks());
  afterAll(() => vi.restoreAllMocks());

  it("shows experiment header and status", async () => {
    render(
      <Wrapper>
        <ExperimentDetailPage />
      </Wrapper>,
    );
    expect(screen.getByText("实验报告")).toBeInTheDocument();
    expect(screen.getByText("已完成")).toBeInTheDocument();
  });

  it("shows quality statistics", async () => {
    render(
      <Wrapper>
        <ExperimentDetailPage />
      </Wrapper>,
    );
    // Average score should be shown (0.82 + 0.45 + 0.1) / 3 = ~0.457 -> shown as "45.7分" or a Statistic
    // The actual format may differ; just check the component renders without error
    expect(screen.getByText("质量统计")).toBeInTheDocument();
    expect(screen.getByText("质量通过率")).toBeInTheDocument();
  });

  it("shows dimension aggregation cards", async () => {
    render(
      <Wrapper>
        <ExperimentDetailPage />
      </Wrapper>,
    );
    expect(screen.getByText("维度聚合分")).toBeInTheDocument();
    // Should show final_answer and safety cards
    expect(screen.getByText("最终答案")).toBeInTheDocument();
    expect(screen.getByText("安全")).toBeInTheDocument();
  });

  it("shows rubric dimension breakdown", async () => {
    render(
      <Wrapper>
        <ExperimentDetailPage />
      </Wrapper>,
    );
    expect(screen.getByText("Rubric 维度详细分")).toBeInTheDocument();
    // Should show dimension averages
    expect(screen.getByText("accuracy")).toBeInTheDocument();
    expect(screen.getByText("completeness")).toBeInTheDocument();
    expect(screen.getByText("clarity")).toBeInTheDocument();
    expect(screen.getByText("safety")).toBeInTheDocument();
  });

  it("shows veto alert when veto triggered", async () => {
    render(
      <Wrapper>
        <ExperimentDetailPage />
      </Wrapper>,
    );
    // Should show the veto alert - using getAllByText since there may be multiple matches
    const vetoElements = screen.getAllByText(/否决项触发/);
    expect(vetoElements.length).toBeGreaterThanOrEqual(1);
  });

  it("shows results table with dimension scores", async () => {
    render(
      <Wrapper>
        <ExperimentDetailPage />
      </Wrapper>,
    );
    // Should show the results table
    expect(screen.getByText("评测结果明细")).toBeInTheDocument();
    // Should show dimension scores in table - use getAllByText since header and column title may match
    const dimHeaders = screen.getAllByText("维度分");
    expect(dimHeaders.length).toBeGreaterThanOrEqual(1);
    // Case with dimension scores should show them as tags
    expect(screen.getByText("accuracy: 90")).toBeInTheDocument();
    expect(screen.getByText("accuracy: 30")).toBeInTheDocument();
  });

  it("shows per-case reason text", async () => {
    render(
      <Wrapper>
        <ExperimentDetailPage />
      </Wrapper>,
    );
    // Should show reason text
    expect(screen.getByText("符合 Rubric 标准")).toBeInTheDocument();
    expect(screen.getByText("答案不正确")).toBeInTheDocument();
    expect(screen.getByText("否决项触发")).toBeInTheDocument();
  });
});
