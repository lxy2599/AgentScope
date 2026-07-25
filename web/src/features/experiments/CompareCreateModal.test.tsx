import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { CompareCreateModal } from "./CompareCreateModal";

vi.mock("react-router-dom", async () => {
  const actual = await vi.importActual("react-router-dom");
  return { ...actual, useParams: () => ({}) };
});

vi.mock("../../api/hooks", () => ({
  useExperiments: () => ({
    data: [
      { id: "exp-1", project_id: "p1", dataset_id: "d1", target_name: "baseline-agent", target_version: "v1", status: "completed", total_cases: 3, created_at: "2026-07-25T01:00:00Z" },
      { id: "exp-2", project_id: "p1", dataset_id: "d1", target_name: "candidate-agent", target_version: "v2", status: "completed", total_cases: 3, created_at: "2026-07-25T02:00:00Z" },
    ],
    isLoading: false,
  }),
  useCreateComparison: () => ({
    mutateAsync: async () => ({ id: "cmp-1" }),
    isPending: false,
  }),
}));

function renderWithProviders(ui: React.ReactElement) {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter>{ui}</MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("CompareCreateModal", () => {
  it("renders modal title", () => {
    renderWithProviders(
      <CompareCreateModal experimentId="exp-1" projectId="p1" visible={true} onClose={() => {}} />,
    );
    expect(screen.getByText("创建实验对比")).toBeDefined();
  });

  it("shows a readable baseline with a secondary technical ID", () => {
    renderWithProviders(
      <CompareCreateModal experimentId="exp-1" projectId="p1" visible={true} onClose={() => {}} />,
    );
    expect(screen.getByText("exp-1")).toBeDefined();
    expect(screen.getByText(/baseline-agent/)).toBeDefined();
  });

  it("shows comparison type selector", () => {
    renderWithProviders(
      <CompareCreateModal experimentId="exp-1" projectId="p1" visible={true} onClose={() => {}} />,
    );
    expect(screen.getByText("对比类型")).toBeDefined();
    expect(screen.getByText("通用对比")).toBeDefined();
  });

  it("shows regression gates configuration after expanding", async () => {
    renderWithProviders(
      <CompareCreateModal experimentId="exp-1" projectId="p1" visible={true} onClose={() => {}} />,
    );
    // The collapse header should be visible
    expect(screen.getByText("回归检测阈值配置")).toBeDefined();
    // Expand the collapse panel
    await userEvent.click(screen.getByText("回归检测阈值配置"));
    // Now the internal labels should be visible
    expect(screen.getByText("通过率下降阈值")).toBeDefined();
    expect(screen.getByText("分数下降阈值")).toBeDefined();
    expect(screen.getByText("成本增长比率阈值")).toBeDefined();
    expect(screen.getByText("时延增长百分比阈值")).toBeDefined();
    expect(screen.getByText("安全性否决")).toBeDefined();
  });
});
