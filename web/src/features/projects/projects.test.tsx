import { render, screen } from "@testing-library/react";
import { BrowserRouter } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { beforeAll, afterAll, afterEach, describe, expect, it, vi } from "vitest";
import { ProjectsPage } from "./ProjectsPage";

function Wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={new QueryClient()}>
      <BrowserRouter>{children}</BrowserRouter>
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

describe("ProjectsPage", () => {
  beforeAll(() => {
    vi.spyOn(globalThis, "fetch");
  });
  afterEach(() => vi.clearAllMocks());
  afterAll(() => vi.restoreAllMocks());

  it("shows loading state initially", () => {
    vi.mocked(fetch).mockReturnValue(new Promise(() => {}));
    render(
      <Wrapper>
        <ProjectsPage />
      </Wrapper>,
    );
    expect(screen.getByText("加载中...")).toBeInTheDocument();
  });

  it("renders project list from raw Project[] array", async () => {
    const projects = [
      { id: "p1", name: "Alpha", created_at: "2026-01-01T00:00:00Z" },
      { id: "p2", name: "Beta" },
    ];
    mockFetch(200, projects);

    render(
      <Wrapper>
        <ProjectsPage />
      </Wrapper>,
    );

    const alpha = await screen.findByText("Alpha");
    expect(alpha).toBeInTheDocument();
    expect(screen.getByText("Beta")).toBeInTheDocument();
  });
});
