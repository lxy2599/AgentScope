# React UI MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `using-git-worktrees` and `executing-plans` to implement this plan task-by-task. Also use `test-driven-development`, `systematic-debugging`, `frontend-design`, `webapp-testing`, and `verification-before-completion`. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 AgentScope 做出第一版可用 Web UI，让用户可以登录、管理项目和 token、查看 runs/trace、创建 dataset/experiment，并从评测失败样本跳到对应 run。

**Architecture:** 前端使用 React + TypeScript + Vite，放在 `web/` 目录，通过 cookie session 调用现有 `/api/...` 接口。Go 服务端继续负责 API 和鉴权，并在生产构建后托管 `web/dist` 静态文件。UI 是开发工具界面，不做营销页。

**Tech Stack:** React、TypeScript、Vite、React Router、TanStack Query、Tailwind CSS、lucide-react、Vitest、Testing Library、Playwright、Go `embed`/`http.FileServer`。

## Global Constraints

- 开发必须从已合并 Task8 的 `master` 开始；如果 `master` 不包含 `5aa80ba fix: 收紧 Judge Provider 密钥配置`，先停止并提示需要合并 Task8。
- 所有实现必须在隔离 worktree 中完成：`.claude/worktrees/task9-react-ui-mvp`。
- 分支名：`worktree-task9-react-ui-mvp`。
- 不改写现有后端业务语义；前端只消费已有 API。只有静态资源托管和必要的 CORS/dev 支持可以改 Go。
- 不新增复杂后端查询接口；如果 UI 需要额外数据，先用现有响应降级展示。
- 不做 landing page；登录后第一屏是工具界面。
- UI 风格要信息密集、克制、适合开发者反复排查问题。
- 不把 LLM provider API key 明文展示或提交；UI 只显示环境变量名或配置摘要。
- 每个任务完成后单独 commit。
- 提交信息必须使用中文，可以保留 `feat:`、`fix:`、`chore:`、`test:` 前缀。

---

## File Structure

- Create: `web/package.json` - 前端脚本、依赖和测试命令。
- Create: `web/index.html` - Vite 入口。
- Create: `web/vite.config.ts` - Vite dev proxy 到 Go API。
- Create: `web/tsconfig.json`, `web/tsconfig.node.json` - TypeScript 配置。
- Create: `web/tailwind.config.ts`, `web/postcss.config.js`, `web/src/styles.css` - 样式系统。
- Create: `web/src/main.tsx`, `web/src/app/App.tsx` - React 根入口和路由。
- Create: `web/src/api/client.ts` - fetch 封装、错误模型、cookie session 支持。
- Create: `web/src/api/types.ts` - 后端 API 类型。
- Create: `web/src/api/hooks.ts` - TanStack Query hooks。
- Create: `web/src/components/layout/*` - 顶部栏、侧边栏、页面骨架。
- Create: `web/src/components/ui/*` - Button、Input、Badge、Table、EmptyState、Spinner、Dialog。
- Create: `web/src/features/auth/*` - 登录/注册页和认证状态。
- Create: `web/src/features/projects/*` - 项目列表、创建项目、token 创建。
- Create: `web/src/features/datasets/*` - dataset/case 创建与查看。
- Create: `web/src/features/runs/*` - runs 列表、run detail、span tree、events。
- Create: `web/src/features/experiments/*` - experiment 创建、详情、状态统计、失败样本入口。
- Create: `web/src/test/*` - 测试工具和 mock。
- Create: `web/tests/smoke.spec.ts` - Playwright smoke 测试。
- Modify: `internal/httpapi/router.go` - 生产环境托管前端静态资源，并保持 `/api`、`/runner`、`/v1/traces` 优先。
- Modify: `cmd/agentscope-server/main.go` or `internal/app/app.go` - 如需将静态文件 handler 注入 router，在 app 层接线。
- Modify: `Makefile` - 增加 `web-install`、`web-test`、`web-build`、`test-all`。
- Modify: `.gitignore` - 确认 `web/node_modules/`、`web/dist/` 保持忽略。

---

## Task 0: Worktree And Baseline Check

**Files:**
- Read: `CLAUDE.md`
- Read: `docs/superpowers/plans/2026-07-16-react-ui-mvp.md`

**Interfaces:**
- Consumes: clean `master` including Task8 commit `5aa80ba`
- Produces: isolated worktree and branch for Task9

- [ ] **Step 1: Confirm master contains Task8**

Run:

```bash
git checkout master
git status --short --branch
git merge-base --is-ancestor 5aa80ba HEAD
```

Expected:

```text
## master...origin/master
```

`git merge-base` exits `0`. If it exits non-zero, stop and report: `master 尚未包含 Task8，不能开始 Task9 UI。`

- [ ] **Step 2: Create worktree**

Run:

```bash
git worktree add .claude/worktrees/task9-react-ui-mvp -b worktree-task9-react-ui-mvp master
cd .claude/worktrees/task9-react-ui-mvp
```

Expected: new worktree checked out on `worktree-task9-react-ui-mvp`.

- [ ] **Step 3: Commit nothing**

Run:

```bash
git status --short
```

Expected: no output.

---

## Task 1: Frontend Tooling Baseline

**Files:**
- Create: `web/package.json`
- Create: `web/index.html`
- Create: `web/vite.config.ts`
- Create: `web/tsconfig.json`
- Create: `web/tsconfig.node.json`
- Create: `web/tailwind.config.ts`
- Create: `web/postcss.config.js`
- Create: `web/src/main.tsx`
- Create: `web/src/app/App.tsx`
- Create: `web/src/styles.css`
- Create: `web/src/test/setup.ts`
- Create: `web/src/app/App.test.tsx`
- Modify: `Makefile`

**Interfaces:**
- Produces: `npm run dev`, `npm run build`, `npm test`, `npm run lint`
- Produces: SPA root route `/`

- [ ] **Step 1: Create frontend package**

Create `web/package.json` with scripts:

```json
{
  "name": "agentscope-web",
  "private": true,
  "version": "0.1.0",
  "type": "module",
  "scripts": {
    "dev": "vite --host 127.0.0.1",
    "build": "tsc -b && vite build",
    "test": "vitest run",
    "test:watch": "vitest",
    "lint": "tsc -b --pretty false"
  },
  "dependencies": {
    "@tanstack/react-query": "^5.51.0",
    "clsx": "^2.1.1",
    "lucide-react": "^0.468.0",
    "react": "^18.3.1",
    "react-dom": "^18.3.1",
    "react-router-dom": "^6.26.0"
  },
  "devDependencies": {
    "@testing-library/jest-dom": "^6.4.8",
    "@testing-library/react": "^16.0.0",
    "@testing-library/user-event": "^14.5.2",
    "@types/react": "^18.3.3",
    "@types/react-dom": "^18.3.0",
    "@vitejs/plugin-react": "^4.3.1",
    "autoprefixer": "^10.4.20",
    "jsdom": "^24.1.1",
    "postcss": "^8.4.41",
    "tailwindcss": "^3.4.10",
    "typescript": "^5.5.4",
    "vite": "^5.4.0",
    "vitest": "^2.0.5"
  }
}
```

- [ ] **Step 2: Add Vite config**

Create `web/vite.config.ts`:

```ts
import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": "http://127.0.0.1:8080",
      "/runner": "http://127.0.0.1:8080",
      "/v1": "http://127.0.0.1:8080"
    }
  },
  test: {
    environment: "jsdom",
    setupFiles: "./src/test/setup.ts",
    globals: true
  }
});
```

- [ ] **Step 3: Add root app smoke test first**

Create `web/src/app/App.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { App } from "./App";

describe("App", () => {
  it("renders the AgentScope shell", () => {
    render(<App />);
    expect(screen.getByText("AgentScope")).toBeInTheDocument();
  });
});
```

- [ ] **Step 4: Implement minimal shell**

Create `web/src/app/App.tsx`:

```tsx
export function App() {
  return (
    <main className="min-h-screen bg-slate-50 text-slate-950">
      <header className="border-b border-slate-200 bg-white px-6 py-4">
        <div className="text-lg font-semibold">AgentScope</div>
      </header>
    </main>
  );
}
```

Create `web/src/main.tsx`:

```tsx
import React from "react";
import ReactDOM from "react-dom/client";
import { App } from "./app/App";
import "./styles.css";

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
```

- [ ] **Step 5: Add build files and styles**

Create `web/index.html`, TypeScript configs, Tailwind config, PostCSS config, and `web/src/styles.css`. Use Tailwind base/components/utilities and CSS variables for neutral surfaces, success, warning, danger, and accent colors.

- [ ] **Step 6: Add Makefile commands**

Add:

```make
web-install:
	cd web && npm install

web-test:
	cd web && npm test

web-build:
	cd web && npm run build

test-all: test web-test web-build
```

- [ ] **Step 7: Verify and commit**

Run:

```bash
cd web && npm install && npm test && npm run build
cd .. && go test ./...
git status --short
```

Commit:

```bash
git add Makefile web
git commit -m "feat: 添加前端工程基线"
```

---

## Task 2: API Client, Auth, And App Shell

**Files:**
- Create: `web/src/api/client.ts`
- Create: `web/src/api/types.ts`
- Create: `web/src/api/hooks.ts`
- Create: `web/src/components/layout/AppShell.tsx`
- Create: `web/src/components/ui/Button.tsx`
- Create: `web/src/components/ui/Input.tsx`
- Create: `web/src/components/ui/Badge.tsx`
- Create: `web/src/features/auth/LoginPage.tsx`
- Create: `web/src/features/auth/auth.test.tsx`
- Modify: `web/src/app/App.tsx`

**Interfaces:**
- Consumes: `POST /api/auth/register`, `POST /api/auth/login`
- Produces: `apiRequest<T>(path: string, options?: RequestInit): Promise<T>`
- Produces: routes `/login`, `/register`, `/projects`

- [ ] **Step 1: Write API client tests**

Test that `apiRequest` parses JSON, includes credentials, and raises an `ApiError` with `code` and `message` when backend returns `{ "error": { ... } }`.

- [ ] **Step 2: Implement API client**

Implement `web/src/api/client.ts` with:

```ts
export class ApiError extends Error {
  constructor(public status: number, public code: string, message: string) {
    super(message);
  }
}

export async function apiRequest<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(path, {
    ...options,
    credentials: "include",
    headers: {
      "Content-Type": "application/json",
      ...(options.headers ?? {})
    }
  });
  const data = await response.json().catch(() => null);
  if (!response.ok) {
    const err = data?.error;
    throw new ApiError(response.status, err?.code ?? "request_failed", err?.message ?? "Request failed");
  }
  return data as T;
}
```

- [ ] **Step 3: Define API types**

Include `User`, `Project`, `ProjectToken`, `RunSummary`, `RunDetail`, `TraceSpan`, `TraceEvent`, `Dataset`, `DatasetCase`, `ExperimentDetail`, `StatusCounts`.

- [ ] **Step 4: Build auth pages**

Implement login/register forms with email, password, display name for register, server error display, loading state, and redirect to `/projects` after success.

- [ ] **Step 5: Build app shell**

Create a dense developer-tool layout: left navigation for Projects, Runs, Datasets, Experiments, Settings; top area shows current project context when available. Use icons from `lucide-react`.

- [ ] **Step 6: Verify and commit**

Run:

```bash
cd web && npm test && npm run build
cd .. && go test ./...
```

Commit:

```bash
git add web
git commit -m "feat: 添加前端 API 客户端和认证界面"
```

---

## Task 3: Projects, Tokens, Datasets, And Cases

**Files:**
- Create: `web/src/features/projects/ProjectsPage.tsx`
- Create: `web/src/features/projects/ProjectHomePage.tsx`
- Create: `web/src/features/projects/TokenPanel.tsx`
- Create: `web/src/features/datasets/DatasetsPanel.tsx`
- Create: `web/src/features/datasets/CasesTable.tsx`
- Create: `web/src/features/projects/projects.test.tsx`
- Modify: `web/src/api/hooks.ts`
- Modify: `web/src/app/App.tsx`

**Interfaces:**
- Consumes: `GET /api/projects`
- Consumes: `POST /api/projects`
- Consumes: `POST /api/projects/{projectID}/tokens`
- Consumes: `POST /api/projects/{projectID}/datasets`
- Consumes: `POST /api/datasets/{datasetID}/cases`
- Consumes: `GET /api/datasets/{datasetID}/cases`
- Produces: routes `/projects`, `/projects/:projectId`

- [ ] **Step 1: Write UI tests**

Cover: empty project list, create project form, token creation reveals token once, dataset case table renders JSON input/expected output summaries.

- [ ] **Step 2: Implement query hooks**

Add hooks:

```ts
useProjects()
useCreateProject()
useCreateProjectToken(projectId: string)
useCreateDataset(projectId: string)
useDatasetCases(datasetId: string)
useAddDatasetCase(datasetId: string)
```

- [ ] **Step 3: Implement Projects page**

Show project list as a compact table. Empty state should include a create form, not explanatory marketing copy.

- [ ] **Step 4: Implement Project Home**

Show panels for token creation, recent runs entry point, dataset/case management, and experiment creation entry point. If the API cannot list datasets yet, keep dataset creation local to the page after create and explain this limitation only in code comments, not visible product copy.

- [ ] **Step 5: Verify and commit**

Run:

```bash
cd web && npm test && npm run build
```

Commit:

```bash
git add web
git commit -m "feat: 添加项目和数据集工作流界面"
```

---

## Task 4: Runs List And Trace Detail

**Files:**
- Create: `web/src/features/runs/RunsPage.tsx`
- Create: `web/src/features/runs/RunDetailPage.tsx`
- Create: `web/src/features/runs/SpanTree.tsx`
- Create: `web/src/features/runs/TraceTimeline.tsx`
- Create: `web/src/features/runs/EventTable.tsx`
- Create: `web/src/features/runs/runs.test.tsx`
- Modify: `web/src/api/hooks.ts`
- Modify: `web/src/app/App.tsx`

**Interfaces:**
- Consumes: `GET /api/projects/{projectID}/runs`
- Consumes: `GET /api/runs/{runID}`
- Produces: routes `/projects/:projectId/runs`, `/runs/:runId`

- [ ] **Step 1: Write trace transformation tests**

Test span tree construction with root spans, child spans, missing parent fallback, and duration sorting.

- [ ] **Step 2: Implement run hooks**

Add:

```ts
useProjectRuns(projectId: string)
useRunDetail(runId: string)
```

- [ ] **Step 3: Implement Runs page**

Show filters for source/status/agent text. Keep filters client-side for MVP. Table columns: status, source, agent, trace id, created time, link to detail.

- [ ] **Step 4: Implement Run detail**

Layout: left span tree, right detail panels. Include run summary, timeline bars, event table, raw attributes viewer, and visible `truncated` badges.

- [ ] **Step 5: Verify and commit**

Run:

```bash
cd web && npm test && npm run build
```

Commit:

```bash
git add web
git commit -m "feat: 添加 Run 和 Trace 查看界面"
```

---

## Task 5: Experiments UI And Failure-To-Trace Flow

**Files:**
- Create: `web/src/features/experiments/CreateExperimentPanel.tsx`
- Create: `web/src/features/experiments/ExperimentDetailPage.tsx`
- Create: `web/src/features/experiments/StatusSummary.tsx`
- Create: `web/src/features/experiments/experiments.test.tsx`
- Modify: `web/src/api/hooks.ts`
- Modify: `web/src/app/App.tsx`

**Interfaces:**
- Consumes: `POST /api/projects/{projectID}/experiments`
- Consumes: `GET /api/experiments/{experimentID}`
- Produces: route `/experiments/:experimentId`

- [ ] **Step 1: Write status summary tests**

Cover queued/running/succeeded/failed count rendering and percentage calculation from `total_cases`.

- [ ] **Step 2: Implement experiment hooks**

Add:

```ts
useCreateExperiment(projectId: string)
useExperiment(experimentId: string)
```

- [ ] **Step 3: Implement creation form**

Fields: dataset id, evaluator id optional, target name, target version, case ids textarea. Convert case ids textarea into string array and reject empty values before submit.

- [ ] **Step 4: Implement detail page**

Show status counts, total/pass/fail cards, target/version, dataset id, evaluator id. If result rows are not exposed by API yet, do not invent fake result data; show only available aggregate state and keep the failure-to-trace route ready for result rows after backend exposes them.

- [ ] **Step 5: Verify and commit**

Run:

```bash
cd web && npm test && npm run build
```

Commit:

```bash
git add web
git commit -m "feat: 添加 Experiment 创建和详情界面"
```

---

## Task 6: Static Asset Serving And End-To-End Smoke

**Files:**
- Modify: `internal/httpapi/router.go`
- Modify: `internal/httpapi/router_test.go`
- Modify: `internal/app/app.go`
- Modify: `cmd/agentscope-server/main.go`
- Create: `web/tests/smoke.spec.ts`
- Modify: `web/package.json`
- Modify: `Makefile`

**Interfaces:**
- Consumes: `web/dist`
- Produces: `GET /` serves React app in production
- Preserves: `/api/*`, `/runner/*`, `/v1/traces`, `/healthz`

- [ ] **Step 1: Write Go router tests first**

Add tests proving:

```text
GET /healthz returns JSON health response
GET /api/unknown does not return index.html
GET / returns index.html when static handler is configured
GET /runs/some-id returns index.html when static handler is configured
```

- [ ] **Step 2: Implement static handler**

Add an optional static file handler to `httpapi.Dependencies` or router options. Keep API routes registered before SPA fallback. The fallback should only handle `GET` and `HEAD`.

- [ ] **Step 3: Add Playwright smoke**

Install Playwright dev dependency if needed. Add `web/tests/smoke.spec.ts` to verify the built app renders login/shell and navigation does not blank.

- [ ] **Step 4: Add full verification command**

Update `Makefile`:

```make
verify:
	go test ./...
	go vet ./...
	cd web && npm test && npm run build
```

- [ ] **Step 5: Run full verification**

Run:

```bash
go test ./... -count=1
go vet ./...
cd web && npm test && npm run build
cd .. && git diff --check master...HEAD
```

- [ ] **Step 6: Commit**

Commit:

```bash
git add Makefile cmd internal web
git commit -m "feat: 托管前端静态资源并补充端到端验证"
```

---

## Final Verification

Run from `.claude/worktrees/task9-react-ui-mvp`:

```bash
git status --short
git log --oneline --decorate -6
go test ./... -count=1
go vet ./...
cd web && npm test && npm run build
cd .. && git diff --check master...HEAD
```

If Docker dependencies are available, also run:

```bash
docker compose up -d postgres clickhouse
AGENTSCOPE_TEST_DATABASE_URL='postgres://agentscope:agentscope@localhost:15432/agentscope?sslmode=disable' \
AGENTSCOPE_TEST_CLICKHOUSE_DSN='clickhouse://localhost:19000/agentscope_traces_test' \
go test ./... -count=1
```

Final report must include:

- Worktree path
- Branch name
- Commit list
- Verification commands and results
- Any UI limitations caused by missing backend list/detail APIs
- Confirmation that no API key or secret was committed

## Self-Review

- Spec coverage: covers Web UI for projects, tokens, datasets, runs, trace detail, experiments, and static serving. It intentionally does not add comparison view because backend does not yet expose experiment result lists or comparison APIs.
- Placeholder scan: no `TBD`, `TODO`, or fake data is allowed in implementation. Where backend API is missing, UI must show only real available data.
- Type consistency: API hooks and route names are defined before feature pages consume them.
