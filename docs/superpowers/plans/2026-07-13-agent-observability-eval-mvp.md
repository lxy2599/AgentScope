# AgentScope MVP 实现计划

> **给执行 agent 的要求：** 必须按任务顺序执行。一次只做一个任务。每个任务都要先写测试、确认失败、再实现、再验证、再提交。不要跨任务改文件。

**目标：** 做出 AgentScope 第一条端到端竖切：项目 token、OTLP trace ingestion、run 查询、dataset、experiment、远程 Python runner、规则 evaluator、LLM judge 抽象、React UI 和 demo。

**架构：** Go 单体服务负责 API、鉴权、ingestion、job 编排和静态 UI；PostgreSQL 保存所有持久化数据；Python SDK/runner 在用户自己的环境执行 agent 并回传结果；React UI 用来查看 run、trace 和 eval 结果。

**技术栈：** Go 1.26、chi、pgx、sqlc、goose、PostgreSQL、React、TypeScript、Vite、TanStack Query、React Router、Tailwind CSS、Python 3.11+、OpenTelemetry Python SDK、Pydantic、Typer、httpx、pytest、Playwright。

## 全局约束

- 后端第一版保持 Go 单体，不拆微服务。
- PostgreSQL 是唯一数据库。
- project token 同时服务 OTLP ingestion 和 runner 鉴权。
- 用户登录使用 cookie session + bcrypt。
- Go 服务端不执行用户 agent 代码。
- Python runner 必须运行在用户本地、CI 或私有执行环境。
- 真实运行和 eval case 都落到统一 `runs` 模型。
- evaluator 同时支持确定性规则和 LLM judge。
- LLM provider 密钥只通过环境变量引用，不明文入库。
- 每个任务完成后单独 commit。

## 目标目录

```text
cmd/agentscope-server/main.go
internal/app/app.go
internal/config/config.go
internal/httpapi/
internal/auth/
internal/projects/
internal/runs/
internal/ingest/
internal/datasets/
internal/experiments/
internal/runnerapi/
internal/evaluators/
internal/store/
migrations/
queries/
python/agentscope_sdk/
python/tests/
web/
examples/
scripts/
docs/development.md
```

---

## Task 1：仓库基线和工具链

**目标：** 让仓库具备 Go 服务端最小骨架、本地 PostgreSQL、测试入口和 sqlc 配置。

**涉及文件：**

- 修改：`go.mod`
- 新建：`.gitignore`
- 新建：`Makefile`
- 新建：`docker-compose.yml`
- 新建：`sqlc.yaml`
- 新建：`cmd/agentscope-server/main.go`
- 新建：`internal/config/config.go`
- 新建：`internal/config/config_test.go`

**对外接口：**

- `config.Config`
- `config.Load(env map[string]string) (Config, error)`

**步骤：**

1. 写 `internal/config/config_test.go`，覆盖缺少 `AGENTSCOPE_DATABASE_URL` 应报错，以及默认监听地址为 `:8080`。
2. 运行 `go test ./internal/config`，确认因为 `Load` 不存在而失败。
3. 修改 `go.mod`，加入 `chi`、`pgx`、`x/crypto`。
4. 创建 `internal/config/config.go`，读取：
   - `AGENTSCOPE_HTTP_ADDR`，默认 `:8080`
   - `AGENTSCOPE_DATABASE_URL`，必填
   - `AGENTSCOPE_SESSION_SECRET`，必填且至少 8 字符
5. 创建 `cmd/agentscope-server/main.go`，暂时只加载配置并打印启动信息。
6. 创建 `.gitignore`，排除 `.idea/`、`.env`、`bin/`、`web/node_modules/`、`web/dist/`、`python/.venv/`、`__pycache__/`、`coverage.out`。
7. 创建 `docker-compose.yml`，只包含 PostgreSQL 16，数据库名/用户名/密码均为 `agentscope`。
8. 创建 `Makefile`，至少包含 `test`、`server`、`db-up`、`db-down`。
9. 创建 `sqlc.yaml`，schema 指向 `migrations`，queries 指向 `queries`，输出到 `internal/store`。
10. 运行 `go mod tidy && go test ./...`。
11. 提交：

```bash
git add .gitignore Makefile docker-compose.yml sqlc.yaml go.mod go.sum cmd internal/config
git commit -m "chore: add repository baseline"
```

---

## Task 2：数据库 schema 和 store

**目标：** 建立核心表结构和 sqlc 数据访问层。

**涉及文件：**

- 新建：`migrations/00001_init.sql`
- 新建：`queries/auth.sql`
- 新建：`queries/projects.sql`
- 新建：`queries/runs.sql`
- 新建：`queries/evals.sql`
- 新建：`internal/store/db.go`
- 新建：`internal/store/store_test.go`

**对外接口：**

- `store.Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error)`
- `store.New(db)` 由 sqlc 生成

**schema 必须包含：**

- `users`
- `projects`
- `project_members`
- `project_tokens`
- `runs`
- `spans`
- `span_events`
- `datasets`
- `dataset_cases`
- `evaluators`
- `experiments`
- `eval_jobs`
- `eval_case_results`

**字段要求：**

- ID 使用 UUID，默认 `gen_random_uuid()`。
- 时间使用 `timestamptz`。
- attributes、metadata、details 使用 `jsonb`。
- `runs.source` 支持 `live` 和 `eval`。
- `eval_jobs` 有 `status`、`runner_name`、`lease_expires_at`、`attempts`。
- `eval_case_results` 必须能关联 `run_id`。

**步骤：**

1. 写 `internal/store/store_test.go`，测试创建 user、project、run。没有 `AGENTSCOPE_TEST_DATABASE_URL` 时 skip。
2. 运行 `go test ./internal/store`，确认因 store/query 不存在而失败。
3. 写 migration。
4. 写 auth/projects/runs/evals 四组 sqlc query。
5. 写 `internal/store/db.go`，封装 pgxpool 连接。
6. 运行：

```bash
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
sqlc generate
go test ./...
```

7. 提交：

```bash
git add migrations queries internal/store go.mod go.sum
git commit -m "feat: add database schema and store"
```

---

## Task 3：HTTP server 基线

**目标：** 服务端能启动，提供健康检查和统一 JSON 响应工具。

**涉及文件：**

- 新建：`internal/httpapi/respond.go`
- 新建：`internal/httpapi/router.go`
- 新建：`internal/httpapi/router_test.go`
- 新建：`internal/app/app.go`
- 修改：`cmd/agentscope-server/main.go`

**对外接口：**

- `httpapi.NewRouter(deps httpapi.Dependencies) http.Handler`
- `GET /healthz`

**步骤：**

1. 写 router 测试，请求 `/healthz`，期望 `200` 和 `{"ok":true}`。
2. 运行 `go test ./internal/httpapi`，确认失败。
3. 实现 `writeJSON` 和 `writeError`。
4. 用 `chi` 实现 router。
5. 实现 `app.NewServer(ctx, cfg)`。
6. 修改 main，让它启动 HTTP server。
7. 运行 `go test ./...`。
8. 提交：

```bash
git add cmd internal/app internal/httpapi
git commit -m "feat: add http server baseline"
```

---

## Task 4：用户、项目和 project token

**目标：** 支持注册/登录、创建项目、创建 project token，并用 token 鉴权。

**涉及文件：**

- 新建：`internal/auth/service.go`
- 新建：`internal/auth/service_test.go`
- 新建：`internal/projects/service.go`
- 新建：`internal/httpapi/auth_handlers.go`
- 新建：`internal/httpapi/project_handlers.go`
- 新建：`internal/httpapi/middleware.go`
- 修改：`internal/httpapi/router.go`
- 必要时修改：`queries/auth.sql`、`queries/projects.sql`

**对外接口：**

- `auth.HashPassword(password string) ([]byte, error)`
- `auth.CheckPassword(hash []byte, password string) bool`
- `projects.Service.CreateToken(...) (plainToken string, err error)`
- `projects.Service.AuthenticateToken(ctx, plainToken, scope string)`

**API：**

- `POST /api/auth/register`
- `POST /api/auth/login`
- `POST /api/projects`
- `GET /api/projects`
- `POST /api/projects/{projectID}/tokens`

**步骤：**

1. 写 bcrypt round-trip 单元测试。
2. 运行 `go test ./internal/auth`，确认失败。
3. 实现密码 hash/check。
4. 实现 token 生成：32 字节随机数，base64url，明文前缀 `ascope_`，数据库只存 SHA-256 hash。
5. token scope 至少支持 `otlp:write` 和 `runner:write`。
6. 实现 auth/project handlers。
7. session cookie 使用 httpOnly；开发阶段可先不强制 secure。
8. 运行 `go test ./...`。
9. 提交：

```bash
git add internal/auth internal/projects internal/httpapi queries
git commit -m "feat: add auth projects and tokens"
```

---

## Task 5：Run 和 OTLP trace ingestion

**目标：** 接收 OTLP HTTP trace，创建/关联 run，保存 spans/events，并提供 run 查询 API。

**涉及文件：**

- 新建：`internal/runs/service.go`
- 新建：`internal/ingest/otlp.go`
- 新建：`internal/ingest/otlp_test.go`
- 新建：`internal/httpapi/ingest_handlers.go`
- 新建：`internal/httpapi/run_handlers.go`
- 修改：`internal/httpapi/router.go`
- 必要时修改：`queries/runs.sql`

**对外接口：**

- `ingest.DecodeOTLPTraceRequest(body []byte) ([]ingest.SpanRecord, error)`
- `runs.Service.CreateLiveRunFromTrace(...)`

**API：**

- `POST /v1/traces`
- `GET /api/projects/{projectID}/runs`
- `GET /api/runs/{runID}`

**AgentScope OTel attributes：**

- `agentscope.run.id`
- `agentscope.run.name`
- `agentscope.agent.name`
- `agentscope.agent.version`

**步骤：**

1. 写 ingestion 单元测试，验证 `SpanRecord` 能承载 trace id、span id、agent name、agent version、attributes。
2. 运行 `go test ./internal/ingest`，确认失败。
3. 引入 `go.opentelemetry.io/proto/otlp` 和 protobuf 依赖。
4. 实现 OTLP protobuf 解码，trace/span id 统一转小写 hex。
5. 实现 `/v1/traces`：
   - bearer token 鉴权，要求 `otlp:write`
   - 解码 spans
   - 按 run id 或 trace id 找/建 run
   - 保存 spans/events
6. 实现 run 列表和详情 API。
7. 运行：

```bash
go test ./...
```

8. 提交：

```bash
git add go.mod go.sum internal/runs internal/ingest internal/httpapi queries migrations
git commit -m "feat: add otlp trace ingestion"
```

---

## Task 6：Dataset、Experiment 和 runner job API

**目标：** 用户能创建 dataset/experiment，服务端能生成 job，runner 能 claim/heartbeat/submit/fail。

**涉及文件：**

- 新建：`internal/datasets/service.go`
- 新建：`internal/experiments/service.go`
- 新建：`internal/runnerapi/service.go`
- 新建：`internal/runnerapi/service_test.go`
- 新建：`internal/httpapi/dataset_handlers.go`
- 新建：`internal/httpapi/experiment_handlers.go`
- 新建：`internal/httpapi/runner_handlers.go`
- 修改：`internal/httpapi/router.go`
- 必要时修改：`queries/evals.sql`

**API：**

- `POST /api/projects/{projectID}/datasets`
- `POST /api/datasets/{datasetID}/cases`
- `POST /api/projects/{projectID}/experiments`
- `GET /api/experiments/{experimentID}`
- `GET /runner/jobs/claim`
- `POST /runner/jobs/{jobID}/heartbeat`
- `POST /runner/jobs/{jobID}/results`
- `POST /runner/jobs/{jobID}/fail`

**步骤：**

1. 写 job lease 单元测试：同一个 queued job 第一次 claim 成功，lease 未过期时第二次 claim 失败。
2. 运行 `go test ./internal/runnerapi`，确认失败。
3. 实现纯函数 `TryClaim`，lease 默认 2 分钟。
4. SQL claim 使用事务和 `FOR UPDATE SKIP LOCKED`。
5. 创建 experiment 时按 dataset case 生成 eval job。
6. claim 响应包含 case input、expected、metadata、target name、agent version、evaluator config、OTLP endpoint。
7. results 接口写入 `eval_case_results`，并完成 job。
8. fail 接口记录错误并释放或标记 job。
9. 运行 `go test ./...`。
10. 提交：

```bash
git add internal/datasets internal/experiments internal/runnerapi internal/httpapi queries
git commit -m "feat: add datasets experiments and runner jobs"
```

---

## Task 7：Python SDK、runner 和规则 evaluator

**目标：** 用户可以用 Python 注册 target，runner 能领取 job、调用 target、跑规则 evaluator、提交结果。

**涉及文件：**

- 新建：`python/pyproject.toml`
- 新建：`python/agentscope_sdk/__init__.py`
- 新建：`python/agentscope_sdk/target.py`
- 新建：`python/agentscope_sdk/runner.py`
- 新建：`python/agentscope_sdk/evaluators.py`
- 新建：`python/tests/test_target.py`
- 新建：`python/tests/test_evaluators.py`
- 新建：`python/tests/test_runner_protocol.py`

**对外接口：**

- `@agentscope_sdk.target(name: str, version: str)`
- `agentscope_sdk.get_target(name: str)`
- `EvaluationResult(score, passed, reason, details)`
- CLI：`agentscope-runner start --project-token ... --server-url ...`

**规则 evaluator：**

- exact match
- regex
- contains
- not contains
- numeric threshold
- latency threshold

**步骤：**

1. 写 target 注册测试。
2. 写 contains evaluator 测试。
3. 在 `python/` 下创建 venv，运行 pytest，确认失败。
4. 创建 Python package，依赖 `httpx`、`pydantic`、`typer`、`opentelemetry-sdk`、`pytest`。
5. 实现 target registry。
6. 实现 evaluator 基类和规则 evaluator。
7. 实现 runner：claim job、调用 target、执行 evaluator、提交 results；target 抛错时调用 fail。
8. 运行：

```bash
cd python
. .venv/bin/activate
pytest
```

9. 提交：

```bash
git add python
git commit -m "feat: add python sdk runner and rule evaluators"
```

---

## Task 8：LLM judge 抽象

**目标：** 在 Python SDK 中加入 judge provider 抽象，并在 Go 侧校验 provider config 不能保存明文密钥。

**涉及文件：**

- 新建：`python/agentscope_sdk/judges.py`
- 新建：`python/tests/test_judges.py`
- 修改：`python/agentscope_sdk/evaluators.py`
- 新建：`internal/evaluators/service.go`
- 新建：`internal/evaluators/service_test.go`

**对外接口：**

- `JudgeRequest`
- `JudgeResult`
- `JudgeProvider.evaluate(request: JudgeRequest) -> JudgeResult`
- provider 名称：`openai-compatible`、`anthropic-compatible`

**步骤：**

1. 写 judge JSON 解析测试：`{"score":0.8,"pass":true,"reason":"mostly correct"}` 应映射到 `passed=True`。
2. 运行 pytest，确认失败。
3. 实现 `judges.py`：
   - `JudgeRequest`
   - `JudgeResult`
   - `parse_judge_response`
   - `JudgeResponseError`
4. 实现 OpenAI-compatible provider。
5. 实现 Anthropic-compatible provider。
6. provider 默认 temperature 为 0，请求结构化 JSON。
7. Go 侧写测试：`api_key_env` 为空或看起来像明文 key（如 `sk-...`）必须拒绝。
8. 实现 Go 侧 provider config 校验。
9. 运行：

```bash
go test ./...
cd python && . .venv/bin/activate && pytest
```

10. 提交：

```bash
git add internal/evaluators python
git commit -m "feat: add llm judge abstraction"
```

---

## Task 9：React UI MVP

**目标：** 做出能完成核心调试闭环的 UI：项目首页、run 详情、experiment 详情和失败样本跳 trace。

**涉及文件：**

- 新建：`web/package.json`
- 新建：`web/vite.config.ts`
- 新建：`web/tsconfig.json`
- 新建：`web/src/main.tsx`
- 新建：`web/src/api/client.ts`
- 新建：`web/src/routes/App.tsx`
- 新建：`web/src/routes/Login.tsx`
- 新建：`web/src/routes/ProjectHome.tsx`
- 新建：`web/src/routes/RunDetail.tsx`
- 新建：`web/src/routes/ExperimentDetail.tsx`
- 新建：`web/src/styles.css`
- 新建：`web/tests/app.spec.ts`
- 修改：`internal/httpapi/router.go`

**页面：**

- `/login`
- `/projects/:projectId`
- `/runs/:runId`
- `/experiments/:experimentId`

**步骤：**

1. 写 Playwright smoke test：首页能看到 `Recent runs` 和 `Recent evals`。
2. 运行前端测试，确认失败。
3. 创建 Vite React + TS 项目文件。
4. 实现 API client。
5. 实现 Login、ProjectHome、RunDetail、ExperimentDetail。
6. UI 风格按开发工具处理：信息密度高、少装饰、不做 landing page。
7. RunDetail 展示 span tree、timeline、LLM/tool、events、raw attributes。
8. ExperimentDetail 展示 aggregate、case results、failure reason、trace link。
9. Go router 支持生产环境托管 `web/dist`。
10. 运行：

```bash
cd web && npm run build && npm test
cd .. && go test ./...
```

11. 提交：

```bash
git add web internal/httpapi
git commit -m "feat: add react ui mvp"
```

---

## Task 10：端到端 demo 和开发文档

**目标：** 用一个示例 Python agent 证明完整链路跑通。

**涉及文件：**

- 新建：`examples/python_support_agent.py`
- 新建：`examples/sample_dataset.jsonl`
- 新建：`scripts/e2e.sh`
- 新建：`docs/development.md`
- 修改：`Makefile`

**验收链路：**

1. 启动 PostgreSQL。
2. 执行 migration。
3. 启动 Go server。
4. 注册用户。
5. 创建项目和 token。
6. 导入 JSONL dataset。
7. 创建 experiment。
8. 启动 Python runner。
9. 等待 case 完成。
10. 验证 result 中包含 `run_id`，并能查询对应 run。

**步骤：**

1. 创建 `scripts/e2e.sh`，先只检查 `/healthz`。
2. 创建 `examples/sample_dataset.jsonl`，至少包含 refund 和 support 两个 case。
3. 创建 `examples/python_support_agent.py`，用 `@agentscope_sdk.target` 注册 deterministic agent。
4. 扩展 e2e 脚本完成完整链路。
5. 创建 `docs/development.md`，写清楚本地启动、migration、后端、前端、runner 的命令。
6. Makefile 增加 `e2e`。
7. 运行：

```bash
make test
make e2e
cd python && . .venv/bin/activate && pytest
cd web && npm test
```

8. 提交：

```bash
git add examples scripts docs/development.md Makefile
git commit -m "test: add end-to-end demo"
```

---

## 执行提示词模板

把下面这段给另一个 agent，然后每次只追加当前 Task 名称。

```text
你在 /home/luxinyu/GolandProjects/AgentScope 工作。

请严格按照 docs/superpowers/plans/2026-07-13-agent-observability-eval-mvp.md 执行。

要求：
1. 一次只执行我指定的一个 Task。
2. 不要提前做后续 Task。
3. 必须先写失败测试，再实现，再运行验证命令。
4. 当前 Task 完成后必须 git commit。
5. 不要改动无关文件，尤其不要提交 .idea/。
6. 不要使用 git reset --hard、git checkout -- .、git clean -fd。
7. 如果测试失败，先定位并修复，不要绕过。
8. 完成后停止，并汇报修改文件、测试命令、commit hash 和遗留问题。
```

## 自检

- 计划覆盖了设计文档中的 Go 服务端、PostgreSQL、OTLP ingestion、统一 Run、Python runner、规则 evaluator、LLM judge、React UI 和端到端 demo。
- 范围保持为 MVP，不包含 ClickHouse、Kafka、SaaS 多租户、服务端执行用户代码、告警和模型网关。
- 每个任务都有明确文件、接口、验证命令和提交信息。
