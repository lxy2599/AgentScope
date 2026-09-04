import {
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { App as AntdApp, ConfigProvider } from "antd";
import zhCN from "antd/locale/zh_CN";
import { MemoryRouter, Route, Routes } from "react-router-dom";
import {
  afterAll,
  afterEach,
  beforeAll,
  describe,
  expect,
  it,
  vi,
} from "vitest";
import { BenchmarkCatalogPage } from "./BenchmarkCatalogPage";
import { BenchmarkExperimentDetailPage } from "./BenchmarkExperimentDetailPage";
import { getBenchmarkReportRefetchInterval } from "../../api/hooks";

const PROJECT_ID = "550e8400-e29b-41d4-a716-446655440000";

function renderWithApp(ui: React.ReactNode, path: string) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <ConfigProvider locale={zhCN}>
        <AntdApp>
          <MemoryRouter initialEntries={[path]}>
            <Routes>
              <Route path="/projects/:projectId/benchmarks" element={ui} />
              <Route
                path="/projects/:projectId/benchmarks/experiments/:experimentId"
                element={ui}
              />
              <Route path="/runs/:runId" element={<div>Trace route</div>} />
              <Route
                path="/experiments/:experimentId"
                element={<div>Experiment route</div>}
              />
            </Routes>
          </MemoryRouter>
        </AntdApp>
      </ConfigProvider>
    </QueryClientProvider>,
  );
}

beforeAll(() => vi.spyOn(globalThis, "fetch"));
afterEach(() => vi.clearAllMocks());
afterAll(() => vi.restoreAllMocks());

describe("Benchmark catalog", () => {
  it("shows official suite provenance and opens the preflight creation flow", async () => {
    vi.mocked(fetch).mockImplementation(async (request) => {
      const url = String(request);
      const body = url.includes("benchmark-targets")
        ? [
            {
              name: "openhands",
              version: "1.2.0",
              framework: "OpenHands",
              capabilities: ["shell", "docker"],
            },
          ]
        : [
            {
              id: "suite-1",
              project_id: PROJECT_ID,
              dataset_id: "dataset-1",
              benchmark_key: "terminal-bench",
              name: "Terminal-Bench 2.0",
              description: "在隔离容器中验证终端任务的端到端完成情况。",
              version: "2.0",
              harness_kind: "Harbor",
              execution_kind: "harbor",
              source_commit: "0348989adffbb43bf0b410fd36197333239633f1",
              required_capabilities: ["shell", "docker"],
              created_at: "2026-08-08T00:00:00Z",
            },
          ];
      return { ok: true, status: 200, json: async () => body } as Response;
    });

    renderWithApp(
      <BenchmarkCatalogPage />,
      `/projects/${PROJECT_ID}/benchmarks?tab=catalog`,
    );

    expect(await screen.findByText("Terminal-Bench 2.0")).toBeInTheDocument();
    expect(screen.getByText("Harbor 容器环境")).toBeInTheDocument();
    expect(screen.getByText("0348989adf")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /配置评测/ }));
    expect(await screen.findByText("兼容性预检")).toBeInTheDocument();
    expect(screen.getByText(/独立试验次数/)).toBeInTheDocument();
  });

  it("uses a synchronized immutable template as the primary launch entry", async () => {
    vi.mocked(fetch).mockImplementation(async (request) => {
      const url = String(request);
      if (url.endsWith("/benchmark-catalog")) {
        return {
          ok: true,
          status: 200,
          json: async () => ({
            revision: "git:catalog-revision",
            artifacts: [
              {
                id: "openhands-dev-smoke",
                framework: "openhands",
                image: "agentlab/openhands-task-runner:local",
                image_digest:
                  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
                source_commit: "abcdef0",
                task_runner_protocol: "v1",
                capabilities: ["shell"],
              },
            ],
            profiles: [
              { id: "model-fixed", kind: "model", config: {} },
              { id: "exec-fixed", kind: "execution", config: {} },
            ],
            run_templates: [
              {
                id: "openhands-harbor-smoke",
                suite_id: "agentlab-harbor-smoke",
                agent_artifact_id: "openhands-dev-smoke",
                model_profile_id: "model-fixed",
                agent_profile_id: "openhands-default",
                execution_profile_id: "exec-fixed",
                task_selection: {
                  mode: "ids",
                  include: ["harbor-agent-smoke"],
                },
                trials_per_case: 1,
              },
            ],
          }),
        } as Response;
      }
      return { ok: true, status: 200, json: async () => [] } as Response;
    });

    renderWithApp(
      <BenchmarkCatalogPage />,
      `/projects/${PROJECT_ID}/benchmarks?tab=catalog`,
    );
    expect(
      await screen.findByText("已同步的 Run Template"),
    ).toBeInTheDocument();
    expect(
      await screen.findByText("openhands-harbor-smoke"),
    ).toBeInTheDocument();
    expect(screen.getByText("git:catalog-revision")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /使用模板创建/ }));
    const templateDialog = await screen.findByRole("dialog", {
      name: "创建基准实验",
    });
    expect(
      within(templateDialog).getByRole("combobox", {
        name: /选择已同步的 Run Template/,
      }),
    ).toBeInTheDocument();
    expect(
      within(templateDialog).getByText("openhands-dev-smoke"),
    ).toBeInTheDocument();
    expect(
      within(templateDialog).getByText(/sha256:aaaaaaaaaaaa/),
    ).toBeInTheDocument();
  });

  it("registers the first Harbor suite from the catalog with an existing dataset", async () => {
    let createPayload: Record<string, unknown> | undefined;
    vi.mocked(fetch).mockImplementation(async (request, init) => {
      const url = String(request);
      if (url.endsWith("/datasets")) {
        return {
          ok: true,
          status: 200,
          json: async () => [
            {
              id: "dataset-1",
              name: "端到端回归集",
              description: "Harbor 任务映射",
            },
          ],
        } as Response;
      }
      if (url.endsWith("/benchmark-suites") && init?.method === "POST") {
        createPayload = JSON.parse(String(init.body)) as Record<
          string,
          unknown
        >;
        return {
          ok: true,
          status: 201,
          json: async () => ({
            id: "suite-1",
            project_id: PROJECT_ID,
            dataset_id: "dataset-1",
            benchmark_key: "terminal_bench",
            name: "Terminal-Bench 2.0",
            description: "",
            version: "2.0",
            harness_kind: "harbor",
            execution_kind: "harbor",
            required_capabilities: ["shell", "docker"],
            created_at: "2026-08-09T00:00:00Z",
          }),
        } as Response;
      }
      return { ok: true, status: 200, json: async () => [] } as Response;
    });

    renderWithApp(
      <BenchmarkCatalogPage />,
      `/projects/${PROJECT_ID}/benchmarks?tab=catalog`,
    );

    expect(
      await screen.findByText("当前项目还没有可运行的基准套件"),
    ).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /注册第一个基准套件/ }));

    const drawer = await screen.findByRole("dialog", { name: "注册基准套件" });
    expect(within(drawer).getByText("Harbor 内置模板")).toBeInTheDocument();
    expect(
      within(drawer).getByText("凭据只由服务端运行环境提供"),
    ).toBeInTheDocument();

    const datasetSelect = within(drawer).getByRole("combobox", {
      name: "关联数据集",
    });
    await waitFor(() => expect(datasetSelect).not.toBeDisabled());
    fireEvent.mouseDown(datasetSelect);
    fireEvent.click(await screen.findByText(/端到端回归集/));
    fireEvent.click(
      within(drawer).getByRole("button", { name: "注册基准套件" }),
    );

    await waitFor(() =>
      expect(createPayload).toMatchObject({
        dataset_id: "dataset-1",
        benchmark_key: "terminal_bench",
        name: "Terminal-Bench 2.0",
        version: "2.0",
        harness_kind: "harbor",
        execution_kind: "harbor",
        required_capabilities: ["shell"],
        default_execution_config: {
          dataset: "terminal-bench@2.0",
        },
      }),
    );
    expect(JSON.stringify(createPayload)).not.toMatch(
      /(?:api_?key|secret|token|password|credential)/i,
    );
  });

  it("rejects credential fields in a suite execution configuration before submission", async () => {
    vi.mocked(fetch).mockImplementation(async (request) => {
      const url = String(request);
      if (url.endsWith("/datasets")) {
        return {
          ok: true,
          status: 200,
          json: async () => [{ id: "dataset-1", name: "端到端回归集" }],
        } as Response;
      }
      return { ok: true, status: 200, json: async () => [] } as Response;
    });

    renderWithApp(
      <BenchmarkCatalogPage />,
      `/projects/${PROJECT_ID}/benchmarks?tab=catalog`,
    );

    fireEvent.click(
      await screen.findByRole("button", { name: /注册第一个基准套件/ }),
    );
    const drawer = await screen.findByRole("dialog", { name: "注册基准套件" });
    const datasetSelect = within(drawer).getByRole("combobox", {
      name: "关联数据集",
    });
    await waitFor(() => expect(datasetSelect).not.toBeDisabled());
    fireEvent.mouseDown(datasetSelect);
    fireEvent.click(await screen.findByText("端到端回归集"));
    fireEvent.change(within(drawer).getByLabelText("默认执行配置（JSON）"), {
      target: { value: '{"api_key":"not-allowed"}' },
    });
    fireEvent.click(
      within(drawer).getByRole("button", { name: "注册基准套件" }),
    );

    expect(
      await screen.findByText(/默认执行配置不能包含凭据字段/),
    ).toBeInTheDocument();
    expect(vi.mocked(fetch)).not.toHaveBeenCalledWith(
      `/api/projects/${PROJECT_ID}/benchmark-suites`,
      expect.objectContaining({ method: "POST" }),
    );
  });
});

describe("Benchmark experiment report", () => {
  it("renders official score, pass@k, trace, artifacts, and classified execution errors", async () => {
    vi.mocked(fetch).mockImplementation(
      async () =>
        ({
          ok: true,
          status: 200,
          json: async () => ({
            experiment: {
              id: "benchmark-exp-1",
              project_id: PROJECT_ID,
              dataset_id: "dataset-1",
              target_name: "openclaw",
              target_version: "0.4.0",
              status: "completed",
              total_cases: 2,
              passed_cases: 1,
              failed_cases: 1,
              aggregate_score: 0.5,
              trials_per_case: 3,
              created_at: "2026-08-08T00:00:00Z",
              status_counts: { succeeded: 5, failed: 1 },
            },
            suite: {
              id: "suite-1",
              project_id: PROJECT_ID,
              dataset_id: "dataset-1",
              benchmark_key: "tau2-airline",
              name: "τ²-bench Airline",
              description: "",
              version: "2026.08",
              harness_kind: "τ²-bench",
              execution_kind: "tau2",
              created_at: "2026-08-08T00:00:00Z",
            },
            execution: {
              queued: 0,
              running: 0,
              succeeded: 5,
              failed: 1,
              total_jobs: 6,
            },
            quality: {
              official_score: 0.667,
              pass_at_1: 0.5,
              pass_at_k: 0.75,
              valid_trials: 5,
              total_trials: 6,
              failed_trials: 1,
            },
            official_metrics: { success_rate: 0.667, policy_compliance: true },
            // The report only receives the safe projection; the raw benchmark_config
            // must never be rendered by the Experiment detail page.
            benchmark_config_snapshot: {
              schema_version: "agentlab.benchmark-execution.v1",
              catalog_revision: "git:catalog-20260810",
              run_template_id: "openclaw-tau2-airline",
              trials_per_case: 3,
              agent_artifact: {
                id: "openclaw-task-runner",
                framework: "openclaw",
                image: "agentlab/openclaw-task-runner:local",
                image_digest:
                  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
                source_commit: "abcdef0123456789",
              },
              profiles: {
                model: { id: "model-ark" },
                agent: { id: "openclaw-default", framework: "openclaw" },
                execution: { id: "harbor-local" },
              },
              task_selection: {
                mode: "ids",
                include: ["airline_001"],
                exclude: ["airline_999"],
                count: 1,
                seed: 42,
              },
              selected_case_ids: ["case-1"],
            },
            trials: [
              {
                id: "trial-1",
                dataset_case_id: "case-1",
                task_id: "airline_001",
                trial_index: 0,
                seed: 42,
                status: "failed",
                passed: false,
                valid: false,
                official_score: 0,
                run_id: "run-1",
                latency_ms: 1234,
                error_type: "runner_timeout",
                error_message: "Harness exceeded the configured deadline.",
                artifacts: [
                  {
                    name: "official-log.json",
                    kind: "official result",
                    url: "/artifacts/official-log.json",
                  },
                ],
              },
            ],
          }),
        }) as Response,
    );

    renderWithApp(
      <BenchmarkExperimentDetailPage />,
      `/projects/${PROJECT_ID}/benchmarks/experiments/benchmark-exp-1`,
    );

    expect((await screen.findAllByText("0.667")).length).toBeGreaterThan(0);
    expect(
      screen.getByText((_, element) => element?.textContent === "Pass@3"),
    ).toBeInTheDocument();
    expect(screen.getByText("75.0%")).toBeInTheDocument();
    expect(screen.getByText("runner_timeout")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Trace/ })).toBeInTheDocument();
    expect(
      screen.getByRole("link", { name: /official-log\.json/ }),
    ).toHaveAttribute("href", "/artifacts/official-log.json");
    expect(screen.getByText("不可变 Catalog 快照")).toBeInTheDocument();
    expect(screen.getByText("git:catalog-20260810")).toBeInTheDocument();
    expect(screen.getByText("openclaw-tau2-airline")).toBeInTheDocument();
    expect(screen.getByText("model-ark")).toBeInTheDocument();
    expect(screen.getByText("openclaw-default · openclaw")).toBeInTheDocument();
    expect(screen.getByText("harbor-local")).toBeInTheDocument();
    expect(screen.getAllByText("airline_001").length).toBeGreaterThan(0);
    expect(screen.getAllByText("case-1").length).toBeGreaterThan(0);
    expect(screen.queryByText("do-not-return")).not.toBeInTheDocument();
    expect(screen.queryByText("sk-aaaaaaaaaaaaaaaa")).not.toBeInTheDocument();
    expect(screen.queryByText("api_key")).not.toBeInTheDocument();
  });

  it("confirms a local launch without sending credentials and refreshes live status", async () => {
    const report = {
      experiment: {
        id: "benchmark-exp-1",
        project_id: PROJECT_ID,
        dataset_id: "dataset-1",
        target_name: "openhands",
        status: "pending",
        total_cases: 1,
        passed_cases: 0,
        failed_cases: 0,
        trials_per_case: 1,
        created_at: "2026-08-08T00:00:00Z",
        status_counts: {},
      },
      suite: {
        id: "suite-1",
        project_id: PROJECT_ID,
        dataset_id: "dataset-1",
        benchmark_key: "terminal-bench",
        name: "Terminal-Bench 2.0",
        description: "",
        version: "2.0",
        harness_kind: "Harbor",
        execution_kind: "harbor",
        created_at: "2026-08-08T00:00:00Z",
      },
      launch: {
        available: true,
        status: "ready",
        message: "本机 Harbor 执行器已就绪。",
      },
      execution: {
        queued: 0,
        running: 0,
        succeeded: 0,
        failed: 0,
        total_jobs: 1,
      },
      quality: { valid_trials: 0, total_trials: 1 },
      trials: [],
    };

    vi.mocked(fetch).mockImplementation(async (request, init) => {
      const url = String(request);
      if (url.endsWith("/launch")) {
        expect(init).toMatchObject({ method: "POST" });
        expect(init?.body).toBeUndefined();
        return {
          ok: true,
          status: 200,
          json: async () => ({
            experiment_id: "benchmark-exp-1",
            launch: {
              available: true,
              status: "launching",
              message: "正在启动本机执行器。",
            },
          }),
        } as Response;
      }
      return { ok: true, status: 200, json: async () => report } as Response;
    });

    renderWithApp(
      <BenchmarkExperimentDetailPage />,
      `/projects/${PROJECT_ID}/benchmarks/experiments/benchmark-exp-1`,
    );

    expect(await screen.findByText("Worker 已就绪")).toBeInTheDocument();
    fireEvent.click(screen.getAllByRole("button", { name: /加入队列/ })[0]);
    const dialog = await screen.findByRole("dialog", {
      name: "确认将基准实验加入队列",
    });
    expect(within(dialog).getByText("凭据不会经过浏览器")).toBeInTheDocument();
    fireEvent.click(within(dialog).getByRole("button", { name: "加入队列" }));

    await waitFor(() =>
      expect(vi.mocked(fetch)).toHaveBeenCalledWith(
        `/api/projects/${PROJECT_ID}/benchmark-experiments/benchmark-exp-1/launch`,
        expect.objectContaining({ method: "POST", credentials: "include" }),
      ),
    );
  });

  it("explains why local launch is unavailable without exposing a credential flow", async () => {
    vi.mocked(fetch).mockImplementation(
      async () =>
        ({
          ok: true,
          status: 200,
          json: async () => ({
            experiment: {
              id: "benchmark-exp-1",
              project_id: PROJECT_ID,
              dataset_id: "dataset-1",
              target_name: "openclaw",
              status: "pending",
              total_cases: 1,
              passed_cases: 0,
              failed_cases: 0,
              created_at: "2026-08-08T00:00:00Z",
              status_counts: {},
            },
            suite: {
              id: "suite-1",
              project_id: PROJECT_ID,
              dataset_id: "dataset-1",
              benchmark_key: "terminal-bench",
              name: "Terminal-Bench 2.0",
              description: "",
              version: "2.0",
              harness_kind: "Harbor",
              execution_kind: "harbor",
              created_at: "2026-08-08T00:00:00Z",
            },
            launch: {
              available: false,
              status: "unavailable",
              unavailable_reason: "当前主机未发现兼容的本机 Runner。",
            },
            execution: {
              queued: 0,
              running: 0,
              succeeded: 0,
              failed: 0,
              total_jobs: 1,
            },
            quality: {},
            trials: [],
          }),
        }) as Response,
    );

    renderWithApp(
      <BenchmarkExperimentDetailPage />,
      `/projects/${PROJECT_ID}/benchmarks/experiments/benchmark-exp-1`,
    );

    expect(
      (await screen.findAllByText(/当前主机未发现兼容的本机 Runner/)).length,
    ).toBeGreaterThan(0);
    expect(
      screen.getAllByRole("button", { name: /加入队列/ })[0],
    ).not.toBeDisabled();
    expect(
      screen.queryByText(/API Key|访问密钥|模型凭据输入/),
    ).not.toBeInTheDocument();
  });

  it("keeps polling while local launch is preparing or running", () => {
    expect(getBenchmarkReportRefetchInterval("completed", "launching")).toBe(
      2500,
    );
    expect(getBenchmarkReportRefetchInterval("completed", "running")).toBe(
      2500,
    );
    expect(getBenchmarkReportRefetchInterval("completed", "completed")).toBe(
      false,
    );
  });
});
