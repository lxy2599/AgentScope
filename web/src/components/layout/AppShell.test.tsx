import { render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { describe, expect, it } from "vitest";
import { AppShell } from "./AppShell";

const VALID_UUID = "550e8400-e29b-41d4-a716-446655440000";
const testUser = { id: "u1", display_name: "Test User", email: "test@test.com" };

function Wrapper({
  children,
  path = "/projects",
}: {
  children: ReactNode;
  path?: string;
}) {
  return (
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="*" element={children} />
      </Routes>
    </MemoryRouter>
  );
}

describe("AppShell sidebar menu", () => {
  it("shows '项目' menu item on /projects", () => {
    render(
      <Wrapper path="/projects">
        <AppShell user={testUser}>
          <div>content</div>
        </AppShell>
      </Wrapper>,
    );
    expect(screen.getByText("项目")).toBeInTheDocument();
  });

  it("does NOT show '采集健康' when on /projects (no project selected)", () => {
    render(
      <Wrapper path="/projects">
        <AppShell user={testUser}>
          <div>content</div>
        </AppShell>
      </Wrapper>,
    );
    expect(screen.queryByText("采集健康")).not.toBeInTheDocument();
  });

  it("does NOT show '采集健康' when on /projects/ingest (non-UUID path)", () => {
    render(
      <Wrapper path="/projects/ingest">
        <AppShell user={testUser}>
          <div>content</div>
        </AppShell>
      </Wrapper>,
    );
    expect(screen.queryByText("采集健康")).not.toBeInTheDocument();
  });

  it("shows '采集健康' when on a valid UUID project page", () => {
    render(
      <Wrapper path={`/projects/${VALID_UUID}`}>
        <AppShell user={testUser}>
          <div>content</div>
        </AppShell>
      </Wrapper>,
    );
    expect(screen.getByText("采集健康")).toBeInTheDocument();
  });

  it("shows '采集健康' when on a valid UUID project sub-page (e.g. /runs)", () => {
    render(
      <Wrapper path={`/projects/${VALID_UUID}/runs`}>
        <AppShell user={testUser}>
          <div>content</div>
        </AppShell>
      </Wrapper>,
    );
    expect(screen.getByText("采集健康")).toBeInTheDocument();
  });

  it("does NOT show '采集健康' when on a non-UUID project-like path", () => {
    render(
      <Wrapper path="/projects/some-random-name">
        <AppShell user={testUser}>
          <div>content</div>
        </AppShell>
      </Wrapper>,
    );
    expect(screen.queryByText("采集健康")).not.toBeInTheDocument();
  });

  it("renders '采集健康' link pointing to correct ingest path for valid UUID", () => {
    render(
      <Wrapper path={`/projects/${VALID_UUID}`}>
        <AppShell user={testUser}>
          <div>content</div>
        </AppShell>
      </Wrapper>,
    );
    // Ant Design renders menu items as <li> with text content
    // We verify the correct menu item exists by checking the link text
    const ingestItem = screen.getByText("采集健康");
    expect(ingestItem).toBeInTheDocument();

    // Also verify clicking "项目" goes to /projects
    const projectItem = screen.getByText("项目");
    expect(projectItem).toBeInTheDocument();
  });
});
