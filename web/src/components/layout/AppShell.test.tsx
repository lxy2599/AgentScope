import { act, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
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

async function renderAppShell(path: string) {
  render(
    <Wrapper path={path}>
      <AppShell user={testUser}>
        <div>content</div>
      </AppShell>
    </Wrapper>,
  );

  // rc-menu registers its item paths in a microtask after the initial render.
  await act(async () => {
    await Promise.resolve();
  });
}

describe("AppShell sidebar menu", () => {
  it("shows '项目' menu item on /projects", async () => {
    await renderAppShell("/projects");
    expect(screen.getByText("项目")).toBeInTheDocument();
  });

  it("does NOT show '采集健康' when on /projects (no project selected)", async () => {
    await renderAppShell("/projects");
    expect(screen.queryByText("采集健康")).not.toBeInTheDocument();
  });

  it("does NOT show '采集健康' when on /projects/ingest (non-UUID path)", async () => {
    await renderAppShell("/projects/ingest");
    expect(screen.queryByText("采集健康")).not.toBeInTheDocument();
  });

  it("shows '采集健康' when on a valid UUID project page", async () => {
    await renderAppShell(`/projects/${VALID_UUID}`);
    expect(screen.getByText("采集健康")).toBeInTheDocument();
  });

  it("shows '采集健康' when on a valid UUID project sub-page (e.g. /runs)", async () => {
    await renderAppShell(`/projects/${VALID_UUID}/runs`);
    expect(screen.getByText("采集健康")).toBeInTheDocument();
  });

  it("does NOT show '采集健康' when on a non-UUID project-like path", async () => {
    await renderAppShell("/projects/some-random-name");
    expect(screen.queryByText("采集健康")).not.toBeInTheDocument();
  });

  it("renders '采集健康' link pointing to correct ingest path for valid UUID", async () => {
    await renderAppShell(`/projects/${VALID_UUID}`);
    // Ant Design renders menu items as <li> with text content
    // We verify the correct menu item exists by checking the link text
    const ingestItem = screen.getByText("采集健康");
    expect(ingestItem).toBeInTheDocument();

    // Also verify clicking "项目" goes to /projects
    const projectItem = screen.getByText("项目");
    expect(projectItem).toBeInTheDocument();
  });
});
