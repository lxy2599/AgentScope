import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ConfigProvider } from "antd";
import zhCN from "antd/locale/zh_CN";
import { StatusSummary } from "./StatusSummary";
import type { ExperimentDetail } from "../../api/types";

function Wrapper({ children }: { children: React.ReactNode }) {
  return <ConfigProvider locale={zhCN}>{children}</ConfigProvider>;
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
  status_counts: { queued: 3, running: 2, passed: 4, failed: 1 },
};

describe("StatusSummary", () => {
  it("renders counts and percentages", () => {
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
