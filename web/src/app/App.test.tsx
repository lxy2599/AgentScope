import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { describe, expect, it, vi, afterEach } from "vitest";
import { App, ValidProjectGuard } from "./App";

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
          <Route path="/projects/:projectId" element={<ValidProjectGuard>valid</ValidProjectGuard>} />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.getByText("valid")).toBeInTheDocument();
  });

  it("redirects when projectId is 'ingest' (not a UUID)", () => {
    render(
      <MemoryRouter initialEntries={["/projects/ingest"]}>
        <Routes>
          <Route path="/projects" element={<div data-testid="projects-page">Projects page</div>} />
          <Route path="/projects/:projectId" element={<ValidProjectGuard>should not render</ValidProjectGuard>} />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.queryByText("should not render")).not.toBeInTheDocument();
  });

  it("redirects when projectId is a random non-UUID string", () => {
    render(
      <MemoryRouter initialEntries={["/projects/not-a-uuid-at-all"]}>
        <Routes>
          <Route path="/projects" element={<div>Projects page</div>} />
          <Route path="/projects/:projectId" element={<ValidProjectGuard>should not render</ValidProjectGuard>} />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.queryByText("should not render")).not.toBeInTheDocument();
  });

  it("redirects for partial UUID-like string", () => {
    render(
      <MemoryRouter initialEntries={["/projects/550e8400"]}>
        <Routes>
          <Route path="/projects" element={<div>Projects page</div>} />
          <Route path="/projects/:projectId" element={<ValidProjectGuard>should not render</ValidProjectGuard>} />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.queryByText("should not render")).not.toBeInTheDocument();
  });

  it("does not redirect for valid UUID sub-routes like /projects/{uuid}/ingest", () => {
    render(
      <MemoryRouter initialEntries={["/projects/550e8400-e29b-41d4-a716-446655440000/ingest"]}>
        <Routes>
          <Route path="/projects" element={<div>Projects page</div>} />
          <Route path="/projects/:projectId/ingest" element={<ValidProjectGuard>ingest dashboard</ValidProjectGuard>} />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.getByText("ingest dashboard")).toBeInTheDocument();
  });

  it("redirects /projects/ingest even with nested child route", () => {
    render(
      <MemoryRouter initialEntries={["/projects/ingest"]}>
        <Routes>
          <Route path="/projects" element={<div data-testid="projects-page">Projects page</div>} />
          <Route path="/projects/:projectId" element={<ValidProjectGuard>
            <Routes>
              <Route path="ingest" element={<div>ingest dashboard</div>} />
            </Routes>
          </ValidProjectGuard>} />
        </Routes>
      </MemoryRouter>,
    );
    expect(screen.queryByText("ingest dashboard")).not.toBeInTheDocument();
  });
});
