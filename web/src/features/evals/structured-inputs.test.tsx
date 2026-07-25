import { App as AntdApp, ConfigProvider } from "antd";
import zhCN from "antd/locale/zh_CN";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { SamplingRulesPage } from "./SamplingRulesPage";
import { AnnotationQueuesPage } from "./AnnotationQueuesPage";

vi.mock("react-router-dom", () => ({
  useParams: () => ({ projectId: "project-1" }),
  useNavigate: () => vi.fn(),
}));

vi.mock("../../api/hooks", () => ({
  useSamplingRules: () => ({ data: [], isLoading: false }),
  useCreateSamplingRule: () => ({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false }),
  useUpdateSamplingRule: () => ({ mutateAsync: vi.fn().mockResolvedValue({}) }),
  useDeleteSamplingRule: () => ({ mutateAsync: vi.fn().mockResolvedValue({}) }),
  useAnnotationQueues: () => ({ data: [{ id: "queue-1", name: "质量复核" }], isLoading: false }),
  useCreateAnnotationQueue: () => ({ mutateAsync: vi.fn().mockResolvedValue({ id: "queue-1" }), isPending: false }),
  useAnnotationQueueItems: (queueId: string) => ({
    data: queueId ? [{
      id: "item-1",
      queue_id: "queue-1",
      project_id: "project-1",
      run_id: "run-1",
      status: "pending",
      priority: 1,
      agent_name: "support-agent",
      input_summary: "用户查询订单状态",
      privacy_reviewed: true,
      is_gold: false,
      expected_tool_path: '["lookup_order"]',
    }] : [],
    isLoading: false,
  }),
  useUpdateAnnotationItem: () => ({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false }),
  usePromoteAnnotationItem: () => ({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false }),
  useSubmitAnnotationGold: () => ({ mutateAsync: vi.fn().mockResolvedValue({}), isPending: false }),
  useDatasets: () => ({ data: [{ id: "dataset-1", name: "回归用例" }] }),
}));

function Wrapper({ children }: { children: React.ReactNode }) {
  return <ConfigProvider locale={zhCN}><AntdApp>{children}</AntdApp></ConfigProvider>;
}

describe("结构化运营输入", () => {
  it("按采样策略展示业务阈值，不要求填写 JSON", async () => {
    const user = userEvent.setup();
    render(<Wrapper><SamplingRulesPage /></Wrapper>);
    await user.click(screen.getByText("创建规则"));
    expect(screen.queryByText("策略阈值（JSON）")).not.toBeInTheDocument();
    await user.click(screen.getByText("随机"));
    await user.click(await screen.findByText("高成本"));
    expect(await screen.findByText("最低成本")).toBeInTheDocument();
  });

  it("用工具标签、失败类别和维度分数替代标注 JSON", async () => {
    const user = userEvent.setup();
    render(<Wrapper><AnnotationQueuesPage /></Wrapper>);
    await user.click(screen.getByText("质量复核"));
    await user.click(await screen.findByText("编辑"));
    expect(screen.getByText("标签顺序就是期望的工具调用顺序。")).toBeInTheDocument();
    expect(screen.getByText("选择最主要的失败原因")).toBeInTheDocument();

    await user.keyboard("{Escape}");
    await user.click(screen.getByText("标为 Gold"));
    expect(await screen.findByText("提交人工 Gold 分数")).toBeInTheDocument();
    expect(screen.getByDisplayValue("quality")).toBeInTheDocument();
    expect(screen.getByText("添加评测维度")).toBeInTheDocument();
  });
});
