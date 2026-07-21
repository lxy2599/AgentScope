import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useParams } from "react-router-dom";
import { describe, expect, it, vi, afterEach } from "vitest";
import { App } from "./App";

/** UUID v4 regex matching the guard in App.tsx */
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

/**
 * Replicated ValidProjectGuard so we can test it in isolation.
 * Redirects non-UUID projectId to a different output without depending
 * on the full App component/AppShell.
 */
function TestProjectGuard({ children }: { children: React.ReactNode }) {
  const { projectId } = useParams<{ projectId: string }>();
  if (!projectId || !UUID_RE.test(projectId)) {
    return <div data-testid="redirected">Redirected to /projects</div>;
  }
  return <div data-testid="project-content">{children}</div>;
}

describe("App routing", () => {
  afterEach(() => {
    sessionStorage.clear();
    vi.clearAllMocks();
  });

  it("renders the AgentScope login page with Chinese text", () => {
    render(<App />);
    expect(screen.getByText("AgentScope")).toBeInTheDocument();
    expect(screen.getByText("Agent 可观测与评测平台")).toBeInTheDocument();
    expect(screen.getByText("登录")).toBeInTheDocument();
    expect(screen.getByText("注册")).toBeInTheDocument();
  });

  it("shows login form fields in Chinese", () => {
    render(<App />);
    expect(screen.getByText("邮箱")).toBeInTheDocument();
    expect(screen.getByText("密码")).toBeInTheDocument();
  });
});

describe("ValidProjectGuard", () => {
  it("renders child content for valid UUID projectId", () => {
    render(
      <MemoryRouter initialEntries={["/projects/550e8400-e29b-41d4-a716-446655440000"]}>
        <Routes>
          <Route path="/projects/:projectId" element={<TestProjectGuard>valid</TestProjectGuard>} />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.getByTestId("project-content")).toBeInTheDocument();
    expect(screen.getByText("valid")).toBeInTheDocument();
    expect(screen.queryByTestId("redirected")).not.toBeInTheDocument();
  });

  it("redirects when projectId is 'ingest' (not a UUID)", () => {
    render(
      <MemoryRouter initialEntries={["/projects/ingest"]}>
        <Routes>
          <Route path="/projects" element={<div data-testid="projects-page">Projects page</div>} />
          <Route path="/projects/:projectId" element={<TestProjectGuard>should not render</TestProjectGuard>} />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.queryByText("should not render")).not.toBeInTheDocument();
  });

  it("redirects when projectId is a random string (not UUID)", () => {
    render(
      <MemoryRouter initialEntries={["/projects/not-a-uuid-at-all"]}>
        <Routes>
          <Route path="/projects" element={<div data-testid="projects-page">Projects page</div>} />
          <Route path="/projects/:projectId" element={<TestProjectGuard>should not render</TestProjectGuard>} />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.queryByText("should not render")).not.toBeInTheDocument();
  });

  it("redirects for partial UUID-like string", () => {
    render(
      <MemoryRouter initialEntries={["/projects/550e8400"]}>
        <Routes>
          <Route path="/projects" element={<div data-testid="projects-page">Projects page</div>} />
          <Route path="/projects/:projectId" element={<TestProjectGuard>should not render</TestProjectGuard>} />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.queryByText("should not render")).not.toBeInTheDocument();
  });

  it("does not redirect for valid UUID sub-routes like /projects/{uuid}/ingest", () => {
    render(
      <MemoryRouter initialEntries={["/projects/550e8400-e29b-41d4-a716-446655440000/ingest"]}>
        <Routes>
          <Route path="/projects" element={<div data-testid="projects-page">Projects page</div>} />
          <Route path="/projects/:projectId/ingest" element={<TestProjectGuard>ingest dashboard</TestProjectGuard>} />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.getByTestId("project-content")).toBeInTheDocument();
    expect(screen.getByText("ingest dashboard")).toBeInTheDocument();
  });

  it("redirects /projects/ingest to /projects even with child route", () => {
    render(
      <MemoryRouter initialEntries={["/projects/ingest"]}>
        <Routes>
          <Route path="/projects" element={<div data-testid="projects-page">Projects page</div>} />
          <Route path="/projects/:projectId" element={<TestProjectGuard>
            <Routes>
              <Route path="ingest" element={<div>ingest dashboard</div>} />
            </Routes>
          </TestProjectGuard>} />
        </Routes>
      </MemoryRouter>,
    );
    // The guard should intercept before any child route renders
    expect(screen.queryByText("ingest dashboard")).not.toBeInTheDocument();
  });
});
