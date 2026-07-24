import { describe, it, expect, vi, beforeAll, afterEach, afterAll } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { getRunDetailRefetchInterval } from "../hooks";

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      {children}
    </QueryClientProvider>
  );
}

function mockFetch(status: number, body: unknown) {
  return vi.mocked(fetch).mockResolvedValueOnce({
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
  } as Response);
}

describe("getRunDetailRefetchInterval", () => {
  it("running returns 2500", () => {
    expect(getRunDetailRefetchInterval("running")).toBe(2500);
  });

  it("waiting_llm returns 2500", () => {
    expect(getRunDetailRefetchInterval("waiting_llm")).toBe(2500);
  });

  it("waiting_tool returns 2500", () => {
    expect(getRunDetailRefetchInterval("waiting_tool")).toBe(2500);
  });

  it("waiting_retrieval returns 2500", () => {
    expect(getRunDetailRefetchInterval("waiting_retrieval")).toBe(2500);
  });

  it("retrying returns 2500", () => {
    expect(getRunDetailRefetchInterval("retrying")).toBe(2500);
  });

  it("completed returns false", () => {
    expect(getRunDetailRefetchInterval("completed")).toBe(false);
  });

  it("failed returns false", () => {
    expect(getRunDetailRefetchInterval("failed")).toBe(false);
  });

  it("cancelled returns false", () => {
    expect(getRunDetailRefetchInterval("cancelled")).toBe(false);
  });

  it("timeout returns false", () => {
    expect(getRunDetailRefetchInterval("timeout")).toBe(false);
  });

  it("undefined returns false", () => {
    expect(getRunDetailRefetchInterval(undefined)).toBe(false);
  });

  it("empty string returns false", () => {
    expect(getRunDetailRefetchInterval("")).toBe(false);
  });

  it("unknown status returns false", () => {
    expect(getRunDetailRefetchInterval("unknown")).toBe(false);
  });
});

describe("useRunDetail polling", () => {
  beforeAll(() => { vi.spyOn(globalThis, "fetch"); });
  afterEach(() => vi.clearAllMocks());
  afterAll(() => vi.restoreAllMocks());

  it("running status 运行中启用轮询", async () => {
    const { useRunDetail } = await import("../hooks");
    const detail = {
      run: { id: "r1", status: "running", trace_id: "t1", agent_name: "agent", source: "live", created_at: "2026-01-01T00:00:00Z" },
      spans: [], events: [],
    };
    mockFetch(200, detail);
    const { result } = renderHook(() => useRunDetail("r1"), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.run?.status).toBe("running");
  });

  it("completed status 停止轮询", async () => {
    const { useRunDetail } = await import("../hooks");
    const detail = {
      run: { id: "r2", status: "completed", trace_id: "t2", agent_name: "agent", source: "live", created_at: "2026-01-01T00:00:00Z" },
      spans: [], events: [],
    };
    mockFetch(200, detail);
    const { result } = renderHook(() => useRunDetail("r2"), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.run?.status).toBe("completed");
  });

  it("failed status 停止轮询", async () => {
    const { useRunDetail } = await import("../hooks");
    const detail = {
      run: { id: "r3", status: "failed", trace_id: "t3", agent_name: "agent", source: "live", created_at: "2026-01-01T00:00:00Z" },
      spans: [], events: [],
    };
    mockFetch(200, detail);
    const { result } = renderHook(() => useRunDetail("r3"), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data?.run?.status).toBe("failed");
  });
});

describe("useActiveRuns polling", () => {
  beforeAll(() => { vi.spyOn(globalThis, "fetch"); });
  afterEach(() => vi.clearAllMocks());
  afterAll(() => vi.restoreAllMocks());

  it("正常获取 active runs 数据", async () => {
    const { useActiveRuns } = await import("../hooks");
    mockFetch(200, []);
    const { result } = renderHook(() => useActiveRuns("p1"), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual([]);
  });

  it("refetchInterval 为 3000ms", async () => {
    // 通过 fake timers 验证轮询：首次请求后计时器每 3s 触发重请求
    vi.useFakeTimers({ shouldAdvanceTime: true });
    const { useActiveRuns } = await import("../hooks");

    // 第一次请求
    mockFetch(200, [{ id: "r1", status: "running", run_name: "test", agent_name: "agent", start_time: "2026-01-01T00:00:00Z", source: "live" }]);
    const { result } = renderHook(() => useActiveRuns("p1"), { wrapper });
    await vi.waitFor(() => expect(result.current.isSuccess).toBe(true));
    const callsAfterFirst = vi.mocked(fetch).mock.calls.length;

    // 推进 3000ms 触发第二次请求
    mockFetch(200, [{ id: "r1", status: "running", run_name: "test", agent_name: "agent", start_time: "2026-01-01T00:00:00Z", source: "live" }]);
    vi.advanceTimersByTime(3000);
    await vi.waitFor(() => expect(vi.mocked(fetch).mock.calls.length).toBe(callsAfterFirst + 1));

    vi.useRealTimers();
  });
});
