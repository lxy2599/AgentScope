import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi, beforeEach } from "vitest";
import type { ReactNode } from "react";
import { App as AntdApp } from "antd";

import { RunDetailPage } from "./RunDetailPage";
import { CasesTable } from "../datasets/CasesTable";

// ── Wrappers ──
function renderRunDetail(element: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  return render(
    <QueryClientProvider client={qc}>
      <AntdApp>
        <MemoryRouter initialEntries={["/runs/run-1"]}>
          <Routes>
            <Route path="/runs/:runId" element={element} />
          </Routes>
        </MemoryRouter>
      </AntdApp>
    </QueryClientProvider>
  );
}

function renderElement(element: ReactNode) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
  return render(
    <QueryClientProvider client={qc}>
      <AntdApp>
        <MemoryRouter>{element}</MemoryRouter>
      </AntdApp>
    </QueryClientProvider>
  );
}

// ── Sample data ──
const SAMPLE_RUN = {
  run: {
    id: "run-1", project_id: "proj-1", source: "live", status: "completed",
    agent_name: "demo-agent", trace_id: "trace-abc",
    input_summary: "用户提问", output_summary: "回答内容",
    created_at: "2026-07-23T12:00:00Z",
  },
  spans: [], events: [], llm_calls: [], tool_calls: [],
  retrieval_calls: [], skill_calls: [], thoughts: [], final_answers: [],
};

const SAMPLE_RUN_NO_TRACE = {
  ...SAMPLE_RUN,
  run: { ...SAMPLE_RUN.run, trace_id: "" },
};

const SAMPLE_CASES = [
  {
    id: "case-1", dataset_id: "ds-1", input: "{}", expected: "correct",
    metadata: JSON.stringify({ source: "annotation", source_run_id: "run-1", source_trace_id: "trace-abc" }),
    sort_order: 0, created_at: "2026-07-23T12:00:00Z",
  },
  {
    id: "case-2", dataset_id: "ds-1", input: "{}", expected: "",
    metadata: "", sort_order: 1, created_at: "2026-07-23T12:00:00Z",
  },
];

// ── Mock helper ──
function mockAPIResponses(runDetailBody: unknown) {
  const emptyOk = () => Promise.resolve({
    ok: true, status: 200,
    json: async () => [],
  } as Response);

  global.fetch = vi.fn().mockImplementation((url: string) => {
    if (url.includes("/runs/run-1") && !url.includes("by-run")) {
      return Promise.resolve({
        ok: true, status: 200,
        json: async () => runDetailBody,
      } as Response);
    }
    return emptyOk();
  });
}

// ══════════════════════════════════════════════════════════
//  RunDetailPage
// ══════════════════════════════════════════════════════════

describe("RunDetailPage - annotation button", () => {
  beforeEach(() => { vi.clearAllMocks(); });

  it("渲染「加入标注队列」按钮", async () => {
    mockAPIResponses(SAMPLE_RUN);
    renderRunDetail(<RunDetailPage />);
    expect(await screen.findByText("加入标注队列")).toBeTruthy();
  });

  it("点击按钮后显示 modal", async () => {
    mockAPIResponses(SAMPLE_RUN);
    renderRunDetail(<RunDetailPage />);
    await screen.findByText("加入标注队列");
    await userEvent.click(screen.getByText("加入标注队列"));
    expect(await screen.findByText(/选择目标队列/)).toBeTruthy();
  });

  it("run 缺少 trace_id 时显示提示", async () => {
    mockAPIResponses(SAMPLE_RUN_NO_TRACE);
    renderRunDetail(<RunDetailPage />);
    await screen.findByText("加入标注队列");
    await userEvent.click(screen.getByText("加入标注队列"));
    expect(await screen.findByText(/该运行缺少 trace_id/)).toBeTruthy();
  });
});

// ══════════════════════════════════════════════════════════
//  CasesTable
// ══════════════════════════════════════════════════════════

describe("CasesTable - source display", () => {
  beforeEach(() => { vi.clearAllMocks(); });

  it("metadata.source=annotation 时显示「线上 Trace」", async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: true, status: 200,
      json: async () => SAMPLE_CASES,
    } as Response);
    renderElement(<CasesTable datasetId="ds-1" />);
    expect(await screen.findByText("线上 Trace")).toBeTruthy();
  });

  it("无 metadata 的 case 显示「手工」", async () => {
    global.fetch = vi.fn().mockResolvedValue({
      ok: true, status: 200,
      json: async () => SAMPLE_CASES,
    } as Response);
    renderElement(<CasesTable datasetId="ds-1" />);
    expect(await screen.findByText("手工")).toBeTruthy();
  });
});
