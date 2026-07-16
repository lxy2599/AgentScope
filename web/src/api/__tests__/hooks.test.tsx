import { beforeAll, afterAll, afterEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook, waitFor } from "@testing-library/react";
import { apiRequest } from "../client";

// Wrap for React Query
function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      {children}
    </QueryClientProvider>
  );
}

// Mock fetch globally
function mockFetch(status: number, body: unknown) {
  return vi.mocked(fetch).mockResolvedValueOnce({
    ok: status >= 200 && status < 300,
    status,
    json: () => Promise.resolve(body),
  } as Response);
}

describe("apiRequest", () => {
  beforeAll(() => {
    vi.spyOn(globalThis, "fetch");
  });
  afterEach(() => vi.clearAllMocks());
  afterAll(() => vi.restoreAllMocks());

  it("login receives raw User object", async () => {
    const userBody = { id: "u1", email: "a@b.com", display_name: "Alice" };
    mockFetch(200, userBody);

    const data = await apiRequest<{ id: string; email: string; display_name: string }>(
      "/api/auth/login",
      { method: "POST", body: "{}" },
    );
    expect(data.id).toBe("u1");
    expect(data.email).toBe("a@b.com");
    expect(data.display_name).toBe("Alice");
    // No wrapping in { user: ... }
    expect((data as Record<string, unknown>).user).toBeUndefined();
  });

  it("register receives raw User object", async () => {
    const userBody = { id: "u2", email: "b@c.com", display_name: "Bob" };
    mockFetch(201, userBody);

    const data = await apiRequest("/api/auth/register", { method: "POST", body: "{}" });
    expect(data).toEqual(userBody);
  });

  it("useProjects receives raw Project[] array", async () => {
    // Dynamic import to avoid hoisting issues
    const { useProjects } = await import("../hooks");
    const projects = [
      { id: "p1", name: "Alpha", created_at: "2026-01-01T00:00:00Z" },
      { id: "p2", name: "Beta", created_at: "2026-01-02T00:00:00Z" },
    ];
    mockFetch(200, projects);

    const { result } = renderHook(() => useProjects(), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(2);
    expect(result.current.data![0].name).toBe("Alpha");
    // Not wrapped in { projects: ... }
    expect((result.current.data as unknown as Record<string, unknown>).projects).toBeUndefined();
  });

  it("useProjectRuns receives raw RunSummary[] array", async () => {
    const { useProjectRuns } = await import("../hooks");
    const runs = [
      {
        id: "r1", project_id: "p1", source: "live",
        status: "completed", trace_id: "t1", agent_name: "agent1",
        created_at: "2026-01-01T00:00:00Z",
      },
    ];
    mockFetch(200, runs);

    const { result } = renderHook(() => useProjectRuns("p1"), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
    expect(result.current.data![0].agent_name).toBe("agent1");
  });

  it("useDatasetCases receives raw DatasetCase[] array", async () => {
    const { useDatasetCases } = await import("../hooks");
    const cases = [
      {
        id: "c1", dataset_id: "d1", input: '"hello"',
        sort_order: 1, created_at: "2026-01-01T00:00:00Z",
      },
    ];
    mockFetch(200, cases);

    const { result } = renderHook(() => useDatasetCases("d1"), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
    expect(result.current.data![0].input).toBe('"hello"');
  });

  it("createProject returns raw Project object", async () => {
    const { useCreateProject } = await import("../hooks");
    const project = { id: "p-new", name: "New Project", created_at: "2026-01-01T00:00:00Z" };
    mockFetch(201, project);

    // Render useCreateProject
    const { result } = renderHook(() => useCreateProject(), { wrapper });
    const res = await result.current.mutateAsync({ name: "New Project" });
    expect(res.id).toBe("p-new");
    expect(res.name).toBe("New Project");
    // Not wrapped in { project: ... }
    expect((res as unknown as Record<string, unknown>).project).toBeUndefined();
  });
});


describe("list hooks return raw arrays", () => {
  beforeAll(() => {
    vi.spyOn(globalThis, "fetch");
  });
  afterEach(() => vi.clearAllMocks());
  afterAll(() => vi.restoreAllMocks());

  it("useDatasets receives raw Dataset[] array", async () => {
    const { useDatasets } = await import("../hooks");
    const datasets = [
      { id: "d1", project_id: "p1", name: "qa-cases", created_at: "2026-01-01T00:00:00Z" },
    ];
    mockFetch(200, datasets);

    const { result } = renderHook(() => useDatasets("p1"), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
    expect(result.current.data![0].name).toBe("qa-cases");
  });

  it("useExperiments receives raw Experiment[] array", async () => {
    const { useExperiments } = await import("../hooks");
    const experiments = [
      { id: "e1", project_id: "p1", dataset_id: "d1", target_name: "agent-a", status: "pending", total_cases: 3, passed_cases: 0, failed_cases: 0, created_at: "2026-01-01T00:00:00Z" },
    ];
    mockFetch(200, experiments);

    const { result } = renderHook(() => useExperiments("p1"), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
    expect(result.current.data![0].target_name).toBe("agent-a");
  });

  it("useExperimentResults receives raw EvalCaseResult[] array with dataset_case_id", async () => {
    const { useExperimentResults } = await import("../hooks");
    const results = [
      { id: "r1", eval_job_id: "ej1", run_id: "run1", dataset_case_id: "case1", score: 0.95, passed: true, reason: "correct", actual_output: "Paris", latency_ms: 100, cost: 0.0, created_at: "2026-01-01T00:00:00Z" },
    ];
    mockFetch(200, results);

    const { result } = renderHook(() => useExperimentResults("p1", "e1"), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(result.current.data).toHaveLength(1);
    expect(result.current.data![0].dataset_case_id).toBe("case1");
    expect(result.current.data![0].score).toBe(0.95);
  });
});
