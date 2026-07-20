import { describe, it, expect, vi, beforeAll, afterEach, afterAll } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { apiRequest } from "../client";

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

  it("failed status 不轮询", async () => {
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

  it("active runs 有轮询间隔", async () => {
    const { useActiveRuns } = await import("../hooks");
    mockFetch(200, []);
    const { result } = renderHook(() => useActiveRuns("p1"), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toEqual([]);
  });
});
