import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import * as hooks from "../../api/hooks";
import AnomalyPanel from "./AnomalyPanel";

vi.mock("../../api/hooks", async () => {
  const actual = await vi.importActual<typeof import("../../api/hooks")>("../../api/hooks");
  return { ...actual, useProjectAnomalies: vi.fn() };
});

function renderPanel() {
  return render(
    <QueryClientProvider client={new QueryClient()}>
      <AnomalyPanel projectId="project-1" />
    </QueryClientProvider>,
  );
}

describe("AnomalyPanel", () => {
  it("分别展示异常、正常和基线不足状态及统计证据", () => {
    vi.mocked(hooks.useProjectAnomalies).mockReturnValue({
      data: [
        {
          id: "a1",
          group: {
            project_id: "project-1",
            agent_name: "agent-a",
            agent_version: "v2",
            environment: "prod",
            metric: "run_latency_ms",
          },
          window_start: "2026-07-24T10:00:00Z",
          window_end: "2026-07-24T10:15:00Z",
          observed_value: 250,
          baseline_median: 100,
          baseline_mad: 5,
          deviation_score: 20,
          sample_count: 8,
          baseline_sample_count: 48,
          status: "anomaly",
          first_detected_at: "2026-07-24T10:15:00Z",
          last_detected_at: "2026-07-24T10:16:00Z",
        },
        {
          id: "a2",
          group: {
            project_id: "project-1",
            agent_name: "agent-a",
            agent_version: "v2",
            environment: "prod",
            metric: "llm_cost",
          },
          window_start: "2026-07-24T10:00:00Z",
          window_end: "2026-07-24T10:15:00Z",
          observed_value: 0.2,
          baseline_median: 0,
          baseline_mad: 0,
          deviation_score: 0,
          sample_count: 2,
          baseline_sample_count: 3,
          status: "insufficient_baseline",
          first_detected_at: "2026-07-24T10:15:00Z",
          last_detected_at: "2026-07-24T10:15:00Z",
        },
      ],
      isLoading: false,
      error: null,
    } as unknown as ReturnType<typeof hooks.useProjectAnomalies>);

    renderPanel();

    expect(screen.getByText("运行异常")).toBeInTheDocument();
    expect(screen.getByText("异常")).toBeInTheDocument();
    expect(screen.getByText("基线不足")).toBeInTheDocument();
    expect(screen.getByText("250.00")).toBeInTheDocument();
    expect(screen.getByText("100.00")).toBeInTheDocument();
    expect(screen.getByText("20.00 MAD")).toBeInTheDocument();
    expect(screen.getByText("8 / 48")).toBeInTheDocument();
  });
});
