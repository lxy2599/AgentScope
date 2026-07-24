import { render, screen } from "@testing-library/react";
import { App as AntdApp } from "antd";
import { describe, expect, it, vi } from "vitest";
import { RubricTemplatesPage } from "./RubricTemplatesPage";

vi.mock("react-router-dom", () => ({
  useParams: () => ({ projectId: "project-1" }),
}));

vi.mock("../../api/hooks", () => ({
  useRubricTemplates: () => ({
    data: [{
      id: "rubric-1",
      name: "客服质量",
      description: "企业客服 Rubric",
    }],
  }),
  useRubricVersions: () => ({
    data: [{
      id: "version-1",
      template_id: "rubric-1",
      version: 1,
      status: "published",
      rubric: JSON.stringify({
        dimensions: [{ name: "accuracy", weight: 1 }],
      }),
      created_at: "2026-07-24T00:00:00Z",
    }],
    isLoading: false,
  }),
  useCreateRubricTemplate: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
  }),
  useCreateRubricVersion: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
  }),
  usePublishRubricVersion: () => ({
    mutateAsync: vi.fn(),
    isPending: false,
  }),
}));

describe("RubricTemplatesPage", () => {
  it("展示发布版本及只读状态", () => {
    render(<AntdApp><RubricTemplatesPage /></AntdApp>);
    expect(screen.getByText("Rubric 模板")).toBeInTheDocument();
    expect(screen.getByText("已发布")).toBeInTheDocument();
    expect(screen.getByText("accuracy 1")).toBeInTheDocument();
    expect(screen.getByText("只读")).toBeInTheDocument();
  });
});

