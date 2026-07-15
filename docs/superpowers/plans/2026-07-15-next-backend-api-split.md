# 下一阶段后端 API 实现计划

> **给执行 agent 的要求：** 本计划用于接在 HTTP server 基线之后继续推进。执行时必须使用 `using-git-worktrees`、`executing-plans`、`test-driven-development`、`systematic-debugging`、`verification-before-completion`。一次只做一个任务，每个任务完成后提交并等待 review。

**目标：** 完成后端从登录、项目、project token 到 OTLP trace ingestion 和 run 查询的可用闭环。

**架构：** handler 只负责鉴权、解析请求和编码响应；业务规则放在 service 包；PostgreSQL 保存用户、项目、token、runs；ClickHouse 保存 span 和 span event 明细。后端仍保持 Go 单体，不引入 Kafka，不执行用户 agent 代码。

**技术栈：** Go 1.26、chi、pgx、sqlc、PostgreSQL、ClickHouse、bcrypt、cookie session、OpenTelemetry OTLP protobuf。

## 全局约束

- 所有实现必须在隔离 git worktree 中完成，禁止直接在 `master` / `main` 上开发。
- 每个任务先写失败测试，再实现，再运行验证命令，再提交。
- 提交信息必须使用中文，可以保留 `feat:`、`fix:`、`test:` 前缀。
- 不要跨任务改文件，不要提前做后续任务。
- handler 不直接写 SQL。
- store 不包含 HTTP 语义。
- token 明文只在创建响应中返回一次，数据库只保存 SHA-256 hash。
- password hash 使用 bcrypt，不能保存明文密码。
- 对外错误不能暴露 SQL、内部路径、token、password hash、API key。
- ClickHouse 继续只保存 trace 明细，不保存用户、鉴权、job 状态和 eval result。

---

## 任务总览

下一阶段拆成 4 个可独立 review 的任务：

1. **Task 4A：认证服务和 session 基础**
2. **Task 4B：项目服务和 project token**
3. **Task 4C：HTTP API 接入认证、项目和 token**
4. **Task 5A：Run 服务和 OTLP ingestion 最小闭环**

这样拆的原因是：密码/session、project token、安全边界、OTLP 解析和双库写入是不同风险面。拆开后每一步都有明确测试和审核重点，避免一次性把鉴权、业务 API、采集链路混在一个大 diff 里。

---

## Task 4A：认证服务和 session 基础

**目标：** 建立 `internal/auth`，支持密码 hash/check、注册、登录校验和签名 session cookie 值。

**Worktree：**

```bash
git worktree add .claude/worktrees/task4a-auth-service -b task4a-auth-service master
cd .claude/worktrees/task4a-auth-service
```

**涉及文件：**

- 新建：`internal/auth/service.go`
- 新建：`internal/auth/service_test.go`
- 必要时修改：`go.mod`
- 必要时修改：`go.sum`

**接口：**

- `auth.HashPassword(password string) ([]byte, error)`
- `auth.CheckPassword(hash []byte, password string) bool`
- `auth.NewSessionSigner(secret []byte) SessionSigner`
- `SessionSigner.SignUserID(userID string) (string, error)`
- `SessionSigner.VerifyUserID(cookieValue string) (string, error)`

**测试要求：**

- bcrypt round-trip：正确密码返回 true，错误密码返回 false。
- 空密码或过短密码返回明确错误。
- session cookie 值被篡改后必须验证失败。
- session signer secret 少于 8 字节时必须拒绝。

**验证命令：**

```bash
go test ./internal/auth -count=1
go test ./... -count=1
go vet ./...
```

**提交：**

```bash
git add internal/auth go.mod go.sum
git commit -m "feat: 添加认证服务基础"
```

**审核重点：**

- bcrypt 成本不要写成不可维护的魔法散落值，默认成本可集中定义。
- session cookie 不要只是 base64 user id，必须有 HMAC 或等价签名。
- 不要引入复杂 session 表；MVP 先用签名 cookie。

---

## Task 4B：项目服务和 project token

**目标：** 建立 `internal/projects`，支持创建项目、列出用户项目、创建 project token、按 scope 认证 token。

**Worktree：**

```bash
git worktree add .claude/worktrees/task4b-project-token -b task4b-project-token master
cd .claude/worktrees/task4b-project-token
```

如果 Task 4A 已合并到 `master`，从最新 `master` 创建；不要从旧提交分叉。

**涉及文件：**

- 新建：`internal/projects/service.go`
- 新建：`internal/projects/service_test.go`
- 修改：`queries/projects.sql`
- 重新生成：`internal/store/projects.sql.go`
- 必要时修改：`internal/store/models.go`

**接口：**

- `projects.Service.CreateProject(ctx, userID, name, description) (Project, error)`
- `projects.Service.ListProjects(ctx, userID) ([]Project, error)`
- `projects.Service.CreateToken(ctx, projectID, description string, scopes []string) (plainToken string, token ProjectToken, error)`
- `projects.Service.AuthenticateToken(ctx, plainToken, requiredScope string) (AuthenticatedToken, error)`

**project token 规则：**

- 明文格式：`ascope_` + 32 字节随机数的 base64url 无 padding 字符串。
- 数据库只保存 `sha256(plainToken)`。
- `token_prefix` 只保存便于识别的短前缀，例如 `ascope_abc123...`，不能足以还原 token。
- scope 至少支持 `otlp:write` 和 `runner:write`。
- `AuthenticateToken` 必须检查 required scope。

**测试要求：**

- 创建 token 返回明文，但数据库记录中不能保存明文。
- 相同 required scope 认证成功，缺少 required scope 认证失败。
- 非 `ascope_` 前缀 token 直接拒绝。
- 创建项目时必须把创建者加入 `project_members`，角色为 `owner`。

**验证命令：**

```bash
sqlc generate
go test ./internal/projects -count=1
go test ./internal/store -count=1
go test ./... -count=1
go vet ./...
```

没有 `AGENTSCOPE_TEST_DATABASE_URL` 时，数据库集成测试可以 skip，但纯单元测试必须运行。

**提交：**

```bash
git add internal/projects queries internal/store
git commit -m "feat: 添加项目和令牌服务"
```

**审核重点：**

- token 明文不能入库、不能进日志、不能出现在错误消息里。
- scope 判断不能用脆弱 substring，例如 `strings.Contains("not-otlp:write", "otlp:write")`。
- 项目成员关系必须和项目创建在同一个业务操作里处理；如果没有事务，要在 review 中说明风险并补上。

---

## Task 4C：HTTP API 接入认证、项目和 token

**目标：** 在 HTTP 层暴露注册、登录、创建项目、列出项目、创建 project token API，并接入 session cookie。

**Worktree：**

```bash
git worktree add .claude/worktrees/task4c-auth-project-api -b task4c-auth-project-api master
cd .claude/worktrees/task4c-auth-project-api
```

如果 Task 4A 和 Task 4B 已合并到 `master`，从最新 `master` 创建；不要从旧提交分叉。

**涉及文件：**

- 新建：`internal/httpapi/auth_handlers.go`
- 新建：`internal/httpapi/project_handlers.go`
- 新建：`internal/httpapi/middleware.go`
- 新建：`internal/httpapi/auth_handlers_test.go`
- 新建：`internal/httpapi/project_handlers_test.go`
- 修改：`internal/httpapi/router.go`
- 修改：`internal/httpapi/respond.go`
- 修改：`internal/app/app.go`
- 必要时修改：`internal/config/config.go`
- 必要时修改：`internal/config/config_test.go`

**API：**

- `POST /api/auth/register`
- `POST /api/auth/login`
- `POST /api/projects`
- `GET /api/projects`
- `POST /api/projects/{projectID}/tokens`

**响应约束：**

- 注册和登录成功后设置 `HttpOnly` session cookie。
- 创建 project token 的响应只在创建时返回一次 `plain_token`。
- 列出 token 时不返回 token hash，也不返回完整明文 token。
- JSON 错误格式沿用 `writeError`，不要每个 handler 自造格式。

**测试要求：**

- `/api/auth/register` 成功后返回用户摘要并设置 `HttpOnly` cookie。
- `/api/auth/login` 密码错误返回 401。
- 未登录访问 `/api/projects` 返回 401。
- 已登录 `POST /api/projects` 创建项目成功。
- 已登录 `POST /api/projects/{projectID}/tokens` 返回 `ascope_` 开头的 `plain_token`。
- handler 测试使用 fake service，避免 HTTP 测试依赖真实 PostgreSQL。

**验证命令：**

```bash
go test ./internal/httpapi -count=1
go test ./internal/app -count=1
go test ./... -count=1
go vet ./...
```

**提交：**

```bash
git add internal/httpapi internal/app internal/config
git commit -m "feat: 添加认证和项目 API"
```

**审核重点：**

- `httpapi.Dependencies` 必须通过接口注入服务，不能在 handler 内部创建 store 连接。
- cookie 属性至少包含 `HttpOnly`、`SameSite=Lax`、`Path=/`。
- handler 不得直接调用 sqlc query。
- API 输出不能包含 `password_hash`、`token_hash`。

---

## Task 5A：Run 服务和 OTLP ingestion 最小闭环

**目标：** 接收 OTLP HTTP trace，通过 project token 鉴权，创建或关联 run，并把 spans/events 写入 ClickHouse。

**Worktree：**

```bash
git worktree add .claude/worktrees/task5a-otlp-ingest -b task5a-otlp-ingest master
cd .claude/worktrees/task5a-otlp-ingest
```

如果 Task 4C 已合并到 `master`，从最新 `master` 创建；不要从旧提交分叉。

**涉及文件：**

- 新建：`internal/runs/service.go`
- 新建：`internal/runs/service_test.go`
- 新建：`internal/ingest/otlp.go`
- 新建：`internal/ingest/otlp_test.go`
- 新建：`internal/httpapi/ingest_handlers.go`
- 新建：`internal/httpapi/run_handlers.go`
- 修改：`internal/httpapi/router.go`
- 修改：`queries/runs.sql`
- 重新生成：`internal/store/runs.sql.go`
- 必要时修改：`internal/tracestore/clickhouse.go`
- 必要时修改：`go.mod`
- 必要时修改：`go.sum`

**API：**

- `POST /v1/traces`
- `GET /api/projects/{projectID}/runs`
- `GET /api/runs/{runID}`

**接口：**

- `ingest.DecodeOTLPTraceRequest(body []byte) ([]ingest.SpanRecord, []ingest.SpanEventRecord, error)`
- `runs.Service.CreateOrGetLiveRunFromTrace(ctx, projectID, traceID, attrs) (Run, error)`
- `runs.Service.ListProjectRuns(ctx, projectID, filters) ([]RunSummary, error)`
- `runs.Service.GetRunDetail(ctx, runID) (RunDetail, error)`

**OTel attribute 约定：**

- `agentscope.run.id`
- `agentscope.run.name`
- `agentscope.agent.name`
- `agentscope.agent.version`

**行为要求：**

- `/v1/traces` 使用 bearer project token，required scope 为 `otlp:write`。
- trace id 和 span id 统一转小写 hex。
- 缺少 `agentscope.run.id` 时，按 trace id 自动创建 `live` run。
- ClickHouse 写入失败时返回 500；第一版不做 Kafka 或后台补偿。
- attributes/event payload 写入 ClickHouse 前必须限制大小，超限截断并设置 `truncated=true`。

**测试要求：**

- OTLP protobuf 解码能得到 trace id、span id、parent span id、name、kind、status、attributes。
- 缺少 token 调 `/v1/traces` 返回 401。
- 无 `otlp:write` scope 的 token 调 `/v1/traces` 返回 403。
- fake runs service + fake tracestore 下，合法 OTLP 请求能触发 run 创建和 ClickHouse 写入。
- run detail 从 PostgreSQL run summary + ClickHouse spans/events 组合响应。

**验证命令：**

```bash
sqlc generate
go test ./internal/ingest -count=1
go test ./internal/runs -count=1
go test ./internal/httpapi -count=1
go test ./... -count=1
go vet ./...
```

如果本地 ClickHouse 可用，再运行：

```bash
AGENTSCOPE_TEST_CLICKHOUSE_DSN='clickhouse://localhost:19000/agentscope_traces_test' go test ./internal/tracestore -count=1 -v
```

**提交：**

```bash
git add internal/runs internal/ingest internal/httpapi internal/store internal/tracestore queries go.mod go.sum
git commit -m "feat: 添加 OTLP Trace 采集"
```

**审核重点：**

- ingestion 不应把 span/event 写回 PostgreSQL。
- run 仍然是 PostgreSQL 业务锚点，trace 明细从 ClickHouse 读取。
- 大字段截断必须有测试。
- OTLP 解码不能依赖手写 JSON；应使用 OpenTelemetry proto 类型。

---

## 执行提示词模板

把下面提示词给另一个 agent，每次只替换 `{任务名}`、`{worktree路径}` 和 `{分支名}`。

```text
你在 /home/luxinyu/GolandProjects/AgentScope 工作。

请执行 docs/superpowers/plans/2026-07-15-next-backend-api-split.md 中的 {任务名}，只做这一个任务。

本次必须使用这些 skill：
- using-git-worktrees：开始写代码前创建或切换到隔离 worktree。
- executing-plans：按计划逐项执行，不跳步骤。
- test-driven-development：先写失败测试，再实现，再验证。
- systematic-debugging：遇到失败先定位根因，不要绕过。
- verification-before-completion：提交前运行验证命令并确认真实通过。

开始前先运行并检查：
git branch --show-current
git status --short
git worktree list

如果 {worktree路径} 不存在，请从当前 master 创建：
git worktree add {worktree路径} -b {分支名} master

然后进入 worktree：
cd {worktree路径}

执行要求：
1. 只做 {任务名}，不要提前做后续任务。
2. handler 不直接写 SQL，业务逻辑放 service。
3. 不要把 token、password hash、API key 或内部 SQL 错误暴露给外部响应。
4. 必须先写失败测试并运行确认失败，再实现。
5. 必须运行计划中列出的验证命令。
6. 完成后必须提交，commit message 使用计划中的中文提交信息。
7. 不要提交 .idea/、.env、数据库 volume、临时日志。
8. 禁止使用 git reset --hard、git checkout -- .、git clean -fd。

完成后汇报：
- worktree 路径
- commit hash
- 修改文件列表
- 测试命令和结果
- 是否存在未提交改动
- 如果有跳过的集成测试，说明跳过原因
```

## Review 策略

每个任务完成后，用 `brooks-lint:brooks-review` 做一次 PR 级审核。审核顺序：

1. 确认 worktree 干净、commit 存在、diff 只覆盖当前任务。
2. 运行该任务计划里的验证命令。
3. 检查分层边界：handler、service、store、tracestore 是否各司其职。
4. 检查安全边界：password/token/API key/SQL 错误是否泄露。
5. 检查测试是否覆盖本任务最核心失败模式。
6. 如果有问题，直接给修复提示词，不进入下一任务。

## 下一步建议

现在先执行 **Task 4A：认证服务和 session 基础**。它是后续项目 API、project token、OTLP bearer token 鉴权的基础，风险小、边界清晰，适合作为下一次实现任务。
