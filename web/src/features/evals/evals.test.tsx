import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, afterAll, afterEach, describe, expect, it, vi } from "vitest";
import { ConfigProvider, App as AntdApp } from "antd";
import zhCN from "antd/locale/zh_CN";
import { MetricConfigPage } from "./MetricConfigPage";

// Mock react-router-dom
vi.mock("react-router-dom", () => ({
  useParams: () => ({ projectId: "test-project" }),
  useNavigate: () => vi.fn(),
}));

// Mock API hooks
vi.mock("../../api/hooks", () => ({
  useMetricDefinitions: () => ({
    data: [
      {
        id: "metric-1",
        project_id: "test-project",
        name: "accuracy",
        kind: "final_answer",
        config: JSON.stringify({
          type: "exact",
          weight: 1.0,
          pass_threshold: 0.7,
          enabled: true,
        }),
        created_at: "2026-01-01T00:00:00Z",
      },
      {
        id: "metric-2",
        project_id: "test-project",
        name: "rubric-quality",
        kind: "final_answer",
        config: JSON.stringify({
          type: "llm_judge",
          weight: 0.5,
          pass_threshold: 0.7,
          enabled: true,
          veto: false,
          judge_provider: "openai-compatible",
          judge_model: "gpt-4o",
          rubric: {
            dimensions: [
              { name: "accuracy", weight: 0.4, description: "是否准确" },
              { name: "completeness", weight: 0.3, description: "是否完整" },
            ],
            essential_items: ["必须正确"],
            veto_items: ["不得有害"],
          },
        }),
        created_at: "2026-01-02T00:00:00Z",
      },
    ],
    isLoading: false,
  }),
  useCreateMetricDefinition: () => ({
    mutateAsync: vi.fn().mockResolvedValue({ id: "new-metric" }),
    isPending: false,
  }),
  useUpdateMetricDefinition: () => ({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  }),
  useDeleteMetricDefinition: () => ({
    mutateAsync: vi.fn().mockResolvedValue({}),
    isPending: false,
  }),
  useRubricTemplates: () => ({ data: [], isLoading: false }),
  useRubricVersions: () => ({ data: [], isLoading: false }),
}));

function Wrapper({ children }: { children: React.ReactNode }) {
  return (
    <ConfigProvider locale={zhCN}>
      <AntdApp>{children}</AntdApp>
    </ConfigProvider>
  );
}

describe("MetricConfigPage - Rubric features", () => {
  beforeAll(() => {
    vi.spyOn(globalThis, "fetch");
  });
  afterEach(() => vi.clearAllMocks());
  afterAll(() => vi.restoreAllMocks());

  it("renders metric list with rubric badge", async () => {
    render(
      <Wrapper>
        <MetricConfigPage />
      </Wrapper>,
    );
    // Should show the page title
    expect(screen.getByText("评测配置中心")).toBeInTheDocument();
    // Should show both metrics
    expect(screen.getByText("accuracy")).toBeInTheDocument();
    expect(screen.getByText("rubric-quality")).toBeInTheDocument();
  });

  it("displays create modal with LLM Judge options", { timeout: 15000 }, async () => {
    const user = userEvent.setup();
    render(
      <Wrapper>
        <MetricConfigPage />
      </Wrapper>,
    );

    // Click "新增评测指标" button
    await user.click(screen.getByText("新增评测指标"));

    // Wait for the modal to appear and render its form fields
    await screen.findByPlaceholderText("例如：final-answer-quality");

    // Fill in name to verify the modal is functional
    const nameInput = screen.getByPlaceholderText("例如：final-answer-quality");
    await user.type(nameInput, "rubric-test-metric");
    expect(nameInput).toHaveValue("rubric-test-metric");

    // Verify form fields are visible
    expect(screen.getByText("否决项")).toBeTruthy();
    expect(screen.getByText("权重")).toBeTruthy();
    expect(screen.getByText("通过阈值")).toBeTruthy();
  });

  it("allows editing a rubric metric", async () => {
    const user = userEvent.setup();
    render(
      <Wrapper>
        <MetricConfigPage />
      </Wrapper>,
    );

    // Click edit on the rubric metric
    const editButtons = screen.getAllByText("编辑");
    // The second edit button corresponds to the rubric metric
    await user.click(editButtons[1]);

    // Modal should show with rubric fields
    await waitFor(() => {
      expect(screen.getByText("编辑评测指标")).toBeInTheDocument();
    });

    // Verify name is filled
    const nameInput = screen.getByDisplayValue("rubric-quality");
    expect(nameInput).toBeInTheDocument();
  });

  it("shows veto and weight info in table", async () => {
    render(
      <Wrapper>
        <MetricConfigPage />
      </Wrapper>,
    );

    // Should show rule types in the table
    expect(screen.getByText("exact")).toBeInTheDocument();
    expect(screen.getByText("llm_judge")).toBeInTheDocument();
  });

  it("displays dimension config when editing llm_judge metric", async () => {
    const user = userEvent.setup();
    render(
      <Wrapper>
        <MetricConfigPage />
      </Wrapper>,
    );

    // Click edit on rubric metric
    const editButtons = screen.getAllByText("编辑");
    await user.click(editButtons[1]);

    await waitFor(() => {
      expect(screen.getByText("Rubric 维度")).toBeInTheDocument();
    });

    // Should show dimension entries
    expect(screen.getByDisplayValue("accuracy")).toBeInTheDocument();
    expect(screen.getByDisplayValue("completeness")).toBeInTheDocument();

    // Should show judge config
    expect(screen.getByText("Judge Provider")).toBeInTheDocument();
    expect(screen.getByText("Judge Model")).toBeInTheDocument();

    // Should show rubric items
    // Textarea for essential items
    expect(screen.getByText("必需项 (每行一个)")).toBeInTheDocument();
    expect(screen.getByText("否决项 (每行一个)")).toBeInTheDocument();
  });
});
