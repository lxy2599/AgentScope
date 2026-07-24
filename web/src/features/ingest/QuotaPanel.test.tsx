import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import QuotaPanel from "./QuotaPanel";
import * as hooks from "../../api/hooks";

vi.mock("../../api/hooks", async () => {
  const actual = await vi.importActual<typeof import("../../api/hooks")>("../../api/hooks");
  return {
    ...actual,
    useProjectQuota: vi.fn(),
    useUpdateProjectQuota: vi.fn(),
  };
});

function renderPanel() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <QuotaPanel projectId="project-1" />
    </QueryClientProvider>,
  );
}

describe("QuotaPanel", () => {
  it("展示配额、当前用量和最近拒绝事件", () => {
    vi.mocked(hooks.useProjectQuota).mockReturnValue({
      data: {
        can_edit: false,
        quota: {
          project_id: "project-1",
          ingest_requests_per_minute: 6000,
          ingest_spans_per_minute: 250000,
          ingest_bytes_per_minute: 1073741824,
          sse_connections: 100,
          tokens_per_day: 100000000,
          cost_micros_per_day: 100000000000,
          version: 2,
        },
        usage: { sse_connections: 3, tokens_daily: 1200, cost_micros_daily: 42 },
        events: [{
          id: "event-1",
          project_id: "project-1",
          dimension: "ingest_requests",
          requested: 1,
          limit: 6000,
          remaining: 0,
          outcome: "rejected",
          fail_mode: "fail_open",
          reason: "quota exceeded",
          occurred_at: "2026-07-24T10:00:00Z",
        }],
      },
      isLoading: false,
      error: null,
    } as unknown as ReturnType<typeof hooks.useProjectQuota>);
    vi.mocked(hooks.useUpdateProjectQuota).mockReturnValue({
      mutateAsync: vi.fn(),
      isPending: false,
    } as unknown as ReturnType<typeof hooks.useUpdateProjectQuota>);

    renderPanel();

    expect(screen.getByText("项目配额")).toBeInTheDocument();
    expect(screen.getByText("3 / 100")).toBeInTheDocument();
    expect(screen.getByText("quota exceeded")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "编辑配额" })).not.toBeInTheDocument();
  });

  it("管理员可以编辑并提交全部限额", async () => {
    const mutateAsync = vi.fn().mockResolvedValue({});
    vi.mocked(hooks.useProjectQuota).mockReturnValue({
      data: {
        can_edit: true,
        quota: {
          project_id: "project-1",
          ingest_requests_per_minute: 10,
          ingest_spans_per_minute: 20,
          ingest_bytes_per_minute: 30,
          sse_connections: 4,
          tokens_per_day: 50,
          cost_micros_per_day: 60,
          version: 1,
        },
        usage: {},
        events: [],
      },
      isLoading: false,
      error: null,
    } as unknown as ReturnType<typeof hooks.useProjectQuota>);
    vi.mocked(hooks.useUpdateProjectQuota).mockReturnValue({
      mutateAsync,
      isPending: false,
    } as unknown as ReturnType<typeof hooks.useUpdateProjectQuota>);

    renderPanel();
    fireEvent.click(screen.getByRole("button", { name: "编辑配额" }));
    fireEvent.click(screen.getByRole("button", { name: "保存" }));

    await waitFor(() => expect(mutateAsync).toHaveBeenCalledWith(expect.objectContaining({
      ingest_requests_per_minute: 10,
      sse_connections: 4,
      cost_micros_per_day: 60,
    })));
  });
});
