# CLAUDE.md

这个文件给 Claude Code / Codex / 其他代码代理提供本仓库的工作约束。开始实现前，先读：

- `docs/superpowers/specs/2026-07-13-agent-observability-eval-design.md`
- `docs/superpowers/plans/2026-07-13-agent-observability-eval-mvp.md`

## 当前阶段

AgentScope 目前处于 **MVP 分任务实现中** 的阶段。仓库基线、PostgreSQL 业务存储和 ClickHouse trace 存储已经落地，下一步进入 HTTP API 基线。

已完成：

- Agent 可观测与评测系统设计文档
- MVP 级实现计划
- 初始 git 仓库
- Go 服务端基线
- 第一版 PostgreSQL schema / store
- 第一版 ClickHouse trace schema / store

尚未完成：

- HTTP API 基线
- OTLP trace ingestion
- Python SDK / runner
- React Web UI
- 端到端 demo

## 关键命令

第一阶段实现完成后，应逐步具备这些命令：

```bash
make db-up      # 启动 PostgreSQL 和 ClickHouse
make db-down    # 停止本地依赖
make test       # 运行 Go 测试
make server     # 启动 Go 服务端
make e2e        # 运行端到端 demo

cd python && pytest       # Python SDK / runner 测试
cd web && npm test        # 前端测试
cd web && npm run build   # 前端生产构建
```

如果命令暂时不存在，按实现计划补齐，不要临时绕过。

## 目标架构

```text
cmd/agentscope-server/     # Go 服务端入口
internal/
  app/                     # 依赖装配和生命周期
  config/                  # 环境变量读取
  store/                   # PostgreSQL pgx/sqlc 数据访问
  tracestore/              # ClickHouse trace 明细访问
  httpapi/                 # chi 路由和 handler
  auth/                    # 用户、session、密码
  projects/                # 项目、成员、project token
  runs/                    # live/eval 统一 Run 模型
  ingest/                  # OTLP trace ingestion
  datasets/                # 数据集和样本
  experiments/             # 实验、聚合结果
  runnerapi/               # 远程 runner job 协议
  evaluators/              # evaluator 配置校验
migrations/                # PostgreSQL goose SQL migration
clickhouse/migrations/     # ClickHouse trace schema migration
queries/                   # PostgreSQL sqlc SQL
python/agentscope_sdk/     # Python SDK 和 runner
web/                       # React + TypeScript + Vite UI
examples/                  # demo agent 和 dataset
```

核心原则：**Go 单体服务 + PostgreSQL + ClickHouse + Python 远程 runner + React UI**。第一版不拆微服务，不引入 Kafka，不在服务端执行用户代码。

存储边界：

- PostgreSQL 保存业务实体、权限、状态机和评测结果，包括 users、projects、project tokens、runs、datasets、experiments、eval jobs、eval case results。
- ClickHouse 保存可观测明细，包括 spans、span events、LLM/tool/retrieval events、trace attributes、latency/token/cost 明细。
- `runs` 必须留在 PostgreSQL，作为 live run、eval run、eval result 和 trace 明细之间的业务锚点。
- 不要把业务状态、鉴权、job lease、eval result 放进 ClickHouse。
- 不要把 spans、span_events 这类 trace 明细放进 PostgreSQL。

## 领域模型约束

- `Run` 是统一运行模型：真实 agent 运行和 eval case 都必须落到同一套 `runs` 表。
- OpenTelemetry 是采集协议；AgentScope 只通过约定 attributes 增加 agent 语义。
- eval 失败样本必须能跳转到对应 run trace。
- project token 用于 OTLP ingestion 和 runner 鉴权。
- LLM judge 的密钥只能通过环境变量引用，不能明文入库。
- trace/event 大字段写入 ClickHouse 前必须限制大小，超限截断并标记。

## API 约束

- 业务 API 使用 `/api/...`
- 健康检查使用 `/healthz`
- OTLP traces 使用 `/v1/traces`
- runner API 使用 `/runner/...`
- handler 只做鉴权、请求解析、响应编码；业务规则放到 service 包。
- 对外错误不能暴露 SQL、内部路径、token、password hash、API key。

## 分层禁止事项

- handler 不直接写 SQL。
- store 不包含 HTTP 语义。
- Python runner 不依赖 Go 内部包。
- Go 服务端不执行用户 agent 代码。
- 不要把 evaluator、runner、trace ingestion 混进一个大文件。
- 不要提交 `.env`、密钥、`node_modules`、`web/dist`、Python venv、数据库 volume。

## Git 工作流

- 所有实现任务必须先创建或切换到隔离 git worktree，禁止直接在 `master` / `main` 上开发。
- 开始实现前先执行并查看：`git branch --show-current && git status --short && git worktree list`。
- 每个计划任务完成后单独提交。
- 提交前运行该任务要求的测试。
- 提交信息必须使用中文，可以保留 `feat:`、`fix:`、`chore:`、`test:` 这类前缀。
- 不要把无关文件混入提交，尤其是 `.idea/`。
- 禁止使用 `git reset --hard`、`git checkout -- .`、`git clean -fd` 这类破坏性命令。
- 当前实现计划在 `docs/superpowers/plans/2026-07-13-agent-observability-eval-mvp.md`，执行时一次只做一个任务。

## 实现顺序

按计划文档顺序推进：

1. 仓库基线和工具链
2. 数据库 schema 和 store
3. HTTP server 基线
4. 用户、项目和 project token
5. Run 与 OTLP trace ingestion
6. Dataset、experiment 和 runner job API
7. Python SDK、runner 和规则 evaluator
8. LLM judge 抽象
9. React UI MVP
10. 端到端 demo 和开发文档

不要跳任务。每个任务都要做到可测试、可提交、可 review。
