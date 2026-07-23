import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
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
      { id: "exp-2", project_id: "p1", target_name: "candidate-agent", target_version: "v2", status: "completed", total_cases: 3 },
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

  it("shows baseline experiment ID", () => {
    renderWithProviders(
      <CompareCreateModal experimentId="exp-1" projectId="p1" visible={true} onClose={() => {}} />,
    );
    expect(screen.getByText("exp-1")).toBeDefined();
  });

  it("shows comparison type selector", () => {
    renderWithProviders(
      <CompareCreateModal experimentId="exp-1" projectId="p1" visible={true} onClose={() => {}} />,
    );
    expect(screen.getByText("对比类型")).toBeDefined();
    expect(screen.getByText("通用对比")).toBeDefined();
  });
});
