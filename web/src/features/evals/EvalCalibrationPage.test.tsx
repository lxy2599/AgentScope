import { render, screen } from "@testing-library/react";
import { App as AntdApp } from "antd";
import { describe, expect, it, vi } from "vitest";
import { EvalCalibrationPage } from "./EvalCalibrationPage";

vi.mock("react-router-dom", () => ({
  useParams: () => ({ projectId: "project-1" }),
}));

vi.mock("../../api/hooks", () => ({
  useRubricTemplates: () => ({ data: [{ id: "template-1", name: "客服质量" }] }),
  useRubricVersions: () => ({ data: [] }),
  useProjectAnnotationItems: () => ({ data: [] }),
  useRunEvalCalibration: () => ({ mutateAsync: vi.fn(), isPending: false }),
  useEvalCalibrations: () => ({
    isLoading: false,
    data: [{
      id: "calibration-1",
      status: "completed",
      report: {
        sample_count: 12,
        observation_count: 12,
        mae: 0.08,
        agreement_rate: 0.92,
        cohens_kappa: 0.81,
        warnings: [],
        dimensions: {
          quality: {
            sample_count: 12,
            mae: 0.08,
            mean_bias: -0.02,
            agreement_rate: 0.92,
            cohens_kappa: 0.81,
          },
        },
      },
    }],
  }),
}));

describe("EvalCalibrationPage", () => {
  it("展示校准可信度和分维度偏差", () => {
    render(<AntdApp><EvalCalibrationPage /></AntdApp>);
    expect(screen.getByText("Judge 校准")).toBeInTheDocument();
    expect(screen.getByText("MAE 0.0800")).toBeInTheDocument();
    expect(screen.getByText("Kappa 0.8100")).toBeInTheDocument();
    expect(screen.getByText("quality")).toBeInTheDocument();
  });
});
