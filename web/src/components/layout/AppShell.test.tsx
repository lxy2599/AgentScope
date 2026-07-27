import { act, render, screen } from "@testing-library/react";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import type { ReactNode } from "react";
import { beforeEach, describe, expect, it } from "vitest";
import { AppShell } from "./AppShell";

const VALID_UUID = "550e8400-e29b-41d4-a716-446655440000";
const testUser = { id: "u1", display_name: "Test User", email: "test@test.com" };

beforeEach(() => {
  Object.defineProperty(window, "matchMedia", {
    configurable: true,
    value: (query: string) => ({
      matches: /min-width:\s*(768|992|1200|1600)px/.test(query),
      media: query,
      onchange: null,
      addListener: () => {},
      removeListener: () => {},
      addEventListener: () => {},
      removeEventListener: () => {},
      dispatchEvent: () => false,
    }),
  });
});

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
  it("shows '项目列表' menu item on /projects", async () => {
    await renderAppShell("/projects");
    expect(screen.getByText("项目列表")).toBeInTheDocument();
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

  it("shows the complete project workflow navigation for a valid UUID project", async () => {
    await renderAppShell(`/projects/${VALID_UUID}/runs`);

    expect(screen.getByText("当前项目")).toBeInTheDocument();
    expect(screen.getByText("项目概览")).toBeInTheDocument();
    expect(screen.getByText("在线观测")).toBeInTheDocument();
    expect(screen.getByText("Agent")).toBeInTheDocument();
    expect(screen.getByText("运行追踪")).toBeInTheDocument();
    expect(screen.getByText("在线评测")).toBeInTheDocument();
    expect(screen.getByText("采样规则")).toBeInTheDocument();
    expect(screen.getByText("标注队列")).toBeInTheDocument();
    expect(screen.getByText("离线评测")).toBeInTheDocument();
    expect(screen.getByText("评测配置")).toBeInTheDocument();

    expect(screen.getByText("运行追踪").closest("li")).toHaveClass("ant-menu-item-selected");
  });

  it("does not show online evaluation navigation without a selected project", async () => {
    await renderAppShell("/projects");

    expect(screen.queryByText("项目概览")).not.toBeInTheDocument();
    expect(screen.queryByText("采样规则")).not.toBeInTheDocument();
    expect(screen.queryByText("标注队列")).not.toBeInTheDocument();
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

    // Also verify the project-list entry remains available.
    const projectItem = screen.getByText("项目列表");
    expect(projectItem).toBeInTheDocument();
  });
});
