import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { StatusSummary } from "./StatusSummary";
import type { ExperimentDetail } from "../../api/types";

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
    render(<StatusSummary detail={base} />);
    expect(screen.getByText((c) => c.includes("10") && c.includes("cases"))).toBeInTheDocument();
    expect(screen.getByText("Passed")).toBeInTheDocument();
    expect(screen.getByText("Failed")).toBeInTheDocument();
    expect(screen.getByText("Running")).toBeInTheDocument();
    expect(screen.getByText("Queued")).toBeInTheDocument();
  });

  it("handles zero totals gracefully", () => {
    const empty: ExperimentDetail = {
      ...base,
      total_cases: 0,
      status_counts: {},
    };
    render(<StatusSummary detail={empty} />);
    expect(screen.getByText((c) => c.includes("0") && c.includes("cases"))).toBeInTheDocument();
    expect(screen.getByText("Passed")).toBeInTheDocument();
  });
});
