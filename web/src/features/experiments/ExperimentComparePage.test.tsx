import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ExperimentComparePage } from "./ExperimentComparePage";

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual("react-router-dom");
  return {
    ...actual,
    useParams: () => ({ comparisonId: "cmp-1" }),
  };
});

const mockCreateAnnotationItem = vi.fn().mockResolvedValue({});
vi.mock("../../api/hooks", () => ({
  useComparison: () => ({
    data: {
      id: "cmp-1",
      project_id: "proj-1",
      comparison_type: "general",
      baseline_experiment_id: "exp-bl",
      candidate_experiment_id: "exp-cand",
      baseline_experiment: {
        id: "exp-bl",
        target_name: "baseline-agent",
        target_version: "v1",
        status: "completed",
        model_provider: "openai",
        model_name: "gpt-4",
        harness_version: "1.0",
        prompt_version: "p1",
        dataset_version: "ds-v1",
      },
      candidate_experiment: {
        id: "exp-cand",
        target_name: "candidate-agent",
        target_version: "v2",
        status: "completed",
        model_provider: "openai",
        model_name: "gpt-4o",
        harness_version: "1.0",
        prompt_version: "p2",
        dataset_version: "ds-v1",
      },
      comparison_summary: {
        total_cases: 3,
        paired_cases: 3,
        baseline_pass_rate: 0.667,
        candidate_pass_rate: 0.333,
        baseline_avg_score: 0.7,
        candidate_avg_score: 0.4,
        avg_score_delta: -0.3,
        improved_cases: 0,
        regressed_cases: 1,
        unchanged_cases: 2,
        incomparable_cases: 0,
        baseline_p95_latency_ms: 5000,
        candidate_p95_latency_ms: 6000,
        latency_delta_pct: 20,
        baseline_avg_cost: 0.01,
        candidate_avg_cost: 0.02,
        cost_delta_pct: 100,
        baseline_error_rate: 0,
        candidate_error_rate: 0.333,
        safety_regressed: false,
        regression_triggered: true,
        regression_reasons: ["通过率下降超过阈值：baseline=66.7%, candidate=33.3%, 阈值=10.0%"],
        multi_dim_metrics: {
          "final_answer:accuracy": { metric_name: "accuracy", metric_kind: "final_answer", baseline_avg: 0.9, candidate_avg: 0.8, delta: -0.1, baseline_passed_count: 2, candidate_passed_count: 1, baseline_total_count: 3, candidate_total_count: 3 },
          "safety:safety_check": { metric_name: "safety_check", metric_kind: "safety", baseline_avg: 1.0, candidate_avg: 1.0, delta: 0.0, baseline_passed_count: 0, candidate_passed_count: 1, baseline_total_count: 0, candidate_total_count: 1 },
        },
      },
      case_results: [
        {
          id: "cr1",
          dataset_case_id: "case-1",
          baseline_run_id: "run-bl-1",
          candidate_run_id: "run-cand-1",
          baseline_score: 1.0,
          candidate_score: 0.0,
          baseline_passed: true,
          candidate_passed: false,
          score_diff: -1.0,
          status: "regressed",
          baseline_latency_ms: 1000,
          candidate_latency_ms: 2000,
        },
        {
          id: "cr2",
          dataset_case_id: "case-2",
          baseline_run_id: "run-bl-2",
          candidate_run_id: "run-cand-2",
          baseline_score: 1.0,
          candidate_score: 1.0,
          baseline_passed: true,
          candidate_passed: true,
          score_diff: 0.0,
          status: "unchanged",
          baseline_latency_ms: 2000,
          candidate_latency_ms: 2000,
        },
      ],
    },
    isLoading: false,
    error: null,
  }),
  useAnnotationQueues: () => ({
    data: [{ id: "q1", project_id: "proj-1", name: "默认队列" }],
    isLoading: false,
  }),
  useCreateAnnotationItemFromRun: () => ({
    mutateAsync: mockCreateAnnotationItem,
    isPending: false,
  }),
  useRunSnapshot: (runId: string) => ({
    data: {
      id: `snapshot-${runId}`,
      run_id: runId,
      project_id: "proj-1",
      schema_version: "runtime-contract-v2",
      canonical_payload: {
        model: {
          name: runId.startsWith("run-bl") ? "gpt-4" : "gpt-4o",
        },
      },
      payload_hash: runId.startsWith("run-bl")
        ? "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
        : "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      created_at: "2026-07-24T00:00:00Z",
    },
    isLoading: false,
    error: null,
  }),
}));

function renderWithProviders(ui: React.ReactElement) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("ExperimentComparePage", () => {
  it("renders comparison title", () => {
    renderWithProviders(<ExperimentComparePage />);
    expect(screen.getByText("实验对比报告")).toBeDefined();
  });

  it("shows baseline and candidate experiment names", () => {
    renderWithProviders(<ExperimentComparePage />);
    // Use exact:false since Ant Design Descriptions wraps text in nested elements
    expect(screen.getByText(/baseline-agent/)).toBeDefined();
    expect(screen.getByText(/candidate-agent/)).toBeDefined();
  });

  it("shows regression gate alert when triggered", () => {
    renderWithProviders(<ExperimentComparePage />);
    expect(screen.getByText("回归检测触发")).toBeDefined();
  });

  it("shows quality comparison section", () => {
    renderWithProviders(<ExperimentComparePage />);
    expect(screen.getByText("质量对比")).toBeDefined();
  });

  it("shows case results with regression tag", () => {
    renderWithProviders(<ExperimentComparePage />);
    // The case result table should show status tags
    expect(screen.getByText("不变")).toBeDefined();
  });

  it("shows multi-dim metric comparison section", () => {
    renderWithProviders(<ExperimentComparePage />);
    expect(screen.getByText("多维度指标对比")).toBeDefined();
    expect(screen.getByText("accuracy")).toBeDefined();
    expect(screen.getByText("safety_check")).toBeDefined();
    expect(screen.getByText("final_answer")).toBeDefined();
    expect(screen.getByText("safety")).toBeDefined();
  });

  it("shows '加入标注队列' button for regressed cases", () => {
    renderWithProviders(<ExperimentComparePage />);
    expect(screen.getByText("加入标注队列")).toBeDefined();
  });

  it("shows the immutable run snapshot difference", () => {
    renderWithProviders(<ExperimentComparePage />);
    expect(screen.getByText("运行配置差异")).toBeDefined();
    expect(screen.getByText("model.name")).toBeDefined();
    expect(screen.getByText("gpt-4")).toBeDefined();
    expect(screen.getByText("gpt-4o")).toBeDefined();
  });
});
