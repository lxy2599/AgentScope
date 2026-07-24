# CLAUDE.md

这个文件给 Claude Code / Codex / 其他代码代理提供本仓库的工作约束。开始实现前，先读：

- `docs/superpowers/specs/2026-07-13-agent-observability-eval-design.md`
- `docs/superpowers/plans/2026-07-13-agent-observability-eval-mvp.md`

## 当前阶段

AgentScope 已具备可运行的全栈 MVP，当前重点是持续强化安全性、可靠性、性能和发布门禁。

已落地的主要能力：

- Go 单体服务、PostgreSQL 业务存储和 ClickHouse trace 存储
- 用户、项目、project token、session 和项目级权限边界
- OTLP ingestion、Run/Trace 关联、在线运行状态和采样评测
- Dataset、experiment、runner job、规则 evaluator 与 LLM-as-Judge
- Python SDK、远程 runner、OpenTelemetry tracing 和 Agent 适配层
- React Web UI、OpenClaw 可观测插件和端到端 demo
- Go/Python/Web/OpenClaw 的格式、静态检查、测试、构建、依赖审计与 CI

新增功能必须同时维护跨项目隔离、对外错误脱敏、密钥目标白名单、真实存储测试和前端类型检查。

## 关键命令

```bash
make db-up               # 启动 PostgreSQL 和 ClickHouse
make db-down             # 停止本地依赖
make fmt-check           # 检查 Go 格式
make go-mod-check        # 校验模块并检查 tidy 漂移
make vet                 # Go 静态检查
make test                # Go 测试
make test-race           # Go race detector
make test-python         # Python 测试
make python-lint         # Ruff + mypy
make web-lint            # TypeScript 类型检查
make web-test            # Web 测试
make web-build           # Web 生产构建
make openclaw-test       # OpenClaw 类型检查与测试
make test-all            # 本地全栈质量门禁
make server              # 启动 Go 服务端
make demo                # 运行端到端 demo
```

真实存储测试必须使用名称以 `_test` 结尾的隔离数据库，并设置
`AGENTSCOPE_TEST_DATABASE_URL` 与 `AGENTSCOPE_TEST_CLICKHOUSE_DSN`。

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
migrations/                # 按编号顺序执行的 PostgreSQL SQL migration
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
- LLM judge 的密钥只能通过环境变量引用，不能明文入库；密钥变量名和目标 origin
  必须通过 runner 的默认或运维显式白名单。
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
- MVP 总计划在 `docs/superpowers/plans/2026-07-13-agent-observability-eval-mvp.md`。
- 后端 API 细分计划在 `docs/superpowers/plans/2026-07-15-next-backend-api-split.md`。
- 下一阶段 Python SDK / runner 计划在 `docs/superpowers/plans/2026-07-15-python-sdk-runner-rule-evaluator.md`。
- 执行时可以按阶段一次性完成，但必须按计划任务分 commit。

## 历史实现顺序

MVP 最初按以下阶段交付；它用于理解依赖关系，不表示后续只能串行修改：

1. 仓库基线和工具链
2. 数据库 schema 和 store
3. HTTP server 基线
4. 用户、项目和 project token（已完成）
5. Run 与 OTLP trace ingestion（已完成）
6. Dataset、experiment 和 runner job API
7. Python SDK、runner 和规则 evaluator
8. LLM judge 抽象
9. React UI MVP
10. 端到端 demo 和开发文档

每项改动都要做到可测试、可 review，并更新受影响的文档和质量门禁。
