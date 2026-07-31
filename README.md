# AgentScope

AgentScope 是面向生产 Agent 的可观测、评测与治理平台。平台通过 Python SDK 和
OpenTelemetry 接入 Agent，在同一个项目中关联在线运行、语义 Trace、离线评测、
人工校准、实验对比和成本治理。

## 已实现能力

- **实时运行**：Agent 注册、实例心跳、Run 状态机、Redis Streams 和 SSE 实时事件；
- **深度可观测**：思考、LLM、工具、检索、Skill、终端、文件和最终答案语义记录；
- **运行快照**：对模型、Prompt、工具、代码和环境保存脱敏、不可变、可校验快照；
- **可信评测**：数据集、版本化 Rubric、多 Judge 独立投票、置信聚合和安全否决；
- **可靠在线评测**：Run 终态、状态事件与在线评测 Job 原子提交；PostgreSQL 持久队列通过 lease token、重试、死信和周期恢复保障异步评测闭环；
- **人工校准**：Gold 标注、Judge 校准报告、线上抽样、标注队列和回归数据集；
- **实验治理**：Baseline/Candidate 对比、统计显著性和回归门禁；
- **生产治理**：跨实例项目配额、SDK 背压、Token、TTFT、成本、Rollup 和异常检测；
- **真实接入**：通用 Python SDK、受控 Hermes fork（源码位于 `integrations/hermes-agent/`）的原生 SDK 埋点，以及受控 OpenHands fork（源码位于 `integrations/openhands-agent/`）的原生 SDK 埋点；OpenHands 不再采用 `OpenHandsAdapter`。

## 快速开始

### 环境要求

- Go 1.26.5（与 `go.mod` 一致）
- Python 3.11+，推荐 uv 0.11.4 和已提交的 `python/uv.lock`
- Node.js `^20.19.0` 或 `>=22.12.0`；OpenClaw 插件测试要求 `>=22.22.3`
- Docker 和 Docker Compose

基础 SDK 支持 Python 3.11+；运行固定版本 OpenHands 集成需要 Python 3.12+。

### 启动平台

```bash
make dev
```

该命令启动 PostgreSQL、ClickHouse、Redis、Go 后端和 React 前端，并自动选择可用的
宿主机数据库端口。默认访问地址：

- 前端：<http://127.0.0.1:5173>
- 后端：<http://127.0.0.1:8080>
- 健康检查：<http://127.0.0.1:8080/healthz>

按 `Ctrl+C` 停止前后端进程；使用 `make db-down` 停止基础设施。

生产部署必须配置至少 32 字节的 `AGENTSCOPE_SESSION_SECRET`。通过 HTTPS 提供服务时
还必须设置 `AGENTSCOPE_SECURE_COOKIES=true`；服务端不会回退到公开默认密钥。

## 统一企业演示

`make demo-phase4-agentops` 是当前唯一的主验收入口。它使用一个账号、一个项目和一套
资源，串联真实 OpenHands 运行、实时 SSE、运行快照、多 Judge、人工 Gold、实验对比、
配额、成本和异常检测，并对 PostgreSQL、ClickHouse、Redis 与 OpenHands 工作区做
精确 ID 校验。

先配置 OpenAI 兼容模型和每百万 Token 单价：

```bash
export OPENHANDS_LLM_PROVIDER=openai-compatible
export OPENHANDS_LLM_BASE_URL=https://example.com/v1
export OPENHANDS_LLM_API_KEY=你的密钥
export OPENHANDS_LLM_MODEL=你的模型
export OPENHANDS_LLM_INPUT_COST_PER_MILLION=输入单价
export OPENHANDS_LLM_OUTPUT_COST_PER_MILLION=输出单价

make demo-phase4-agentops
```

API Key 只从环境变量读取，不写入数据库、结果文件或日志。演示成功后，使用输出的同一
账号登录前端即可查看全部页面，不需要切换账号。

完整步骤见：

- [Phase 4 企业 AgentOps 统一演示](docs/phase4-enterprise-demo.md)
- [企业演示与验收指南](docs/enterprise-demo-guide.md)
- [Phase 4 验证报告](docs/phase4-verification-report.md)
- [面试项目深度报告](docs/interview-project-report.md)

旧的 `demo-real-agent`、`demo-support-agent`、`demo-multi-eval` 等命令保留为兼容性
回归入口，不再作为完整能力验收依据。

## SDK 接入模型

业务状态和 Trace 使用同一个 `run_id` 关联：

```python
from agentscope_sdk import init, run

runtime = init(
    server_url="http://127.0.0.1:8080",
    project_token="ascope_...",
    agent_name="my-agent",
)

with run("处理用户请求") as current:
    # 在 Agent 的真实执行路径中记录 thought、LLM、tool、retrieval、
    # skill 和 final_answer；SDK 会批量导出并处理背压。
    ...
```

普通框架 Adapter 只能使用上游公开生命周期事件或 callback，不允许修改上游源码或依赖
私有内部结构。Hermes 是已实现的受控 fork 例外；OpenHands 仅在受控 fork 原生路径落地后才可使用
相同模式。详细语义约定见 `python/agentscope_sdk/` 和接入设计文档。

## 架构

```text
Agent / OpenHands / Hermes
        │ Python SDK + OTLP + Runtime API
        ▼
Go 单体服务
  ├─ PostgreSQL：鉴权、项目、Run、Snapshot、评测、校准、配额、Outbox
  │     └─ Run 终态事务 → online_eval_jobs → Worker（lease/retry/dead-letter）
  ├─ ClickHouse：Span、语义事件、Token/成本/延迟、Rollup
  └─ Redis（可选）：低延迟实时 Stream 和短期配额窗口
        │
        ▼
React 控制台：实时运行、Trace、评测、对比、采集和治理
```

存储边界：

- PostgreSQL 是业务事实和状态机的权威存储；
- ClickHouse 保存高吞吐可观测明细和分析聚合；
- Redis 只保存可重建的实时投影和短期计数，不能成为业务事实源；
- `realtime_outbox.id` 是 SSE 的稳定游标；Redis Stream ID 只是内部传输游标；
- `Run` 是线上执行、Trace、评测结果和人工反馈的统一锚点。

### 实时链路的降级、重放与保留

每个实时事件先写入 PostgreSQL `realtime_outbox`，其单调递增 ID 会作为 SSE
`Last-Event-ID` 返回给浏览器。Redis 可用时，新连接优先走 Redis Stream 以降低延迟；
Redis 订阅失败、运行中断开或浏览器携带游标重连时，服务切换为按该 outbox ID 轮询
PostgreSQL。因此 Redis 故障不会让 SSE 彻底不可用，但降级模式受
`AGENTSCOPE_REALTIME_POSTGRES_POLL_INTERVAL`（默认 `1s`）影响。

该通道是 **at-least-once**：前端按事件 ID 去重。默认保留已发布 outbox 记录 7 天，
清理器用有界批次与 `FOR UPDATE SKIP LOCKED` 删除；未发布记录不会被清理。游标早于
保留窗口会收到 `reset(cursor_expired)`，旧 Redis 格式游标会收到
`reset(cursor_format_unsupported)`；前端随后失效 REST 查询、销毁带旧游标的 EventSource 并重新拉取事实数据。若希望
在未部署 Redis 的环境启用 PostgreSQL-only SSE，可设置
`AGENTSCOPE_REALTIME_ENABLED=true`。

### 认证入口保护

登录与注册在 bcrypt 前经过本机 admission control：分别按 IP 前缀和规范化邮箱哈希执行
token bucket，并使用非阻塞并发闸门限制 bcrypt 工作量。命中限流或并发满载时返回
`429`、`Retry-After` 与稳定错误码 `auth_rate_limited` / `auth_busy`。该保护无需依赖
Redis；可通过 `AGENTSCOPE_AUTH_RATE_LIMIT_ENABLED`、
`AGENTSCOPE_AUTH_BCRYPT_MAX_CONCURRENT` 和
`AGENTSCOPE_AUTH_RATE_LIMIT_MAX_TRACKED_PRINCIPALS` 调整。

主要目录：

```text
cmd/agentscope-server/     Go 服务入口
internal/                  领域服务、HTTP API、存储 Adapter 和采集链路
migrations/                PostgreSQL 迁移
clickhouse/migrations/     ClickHouse 迁移
python/agentscope_sdk/     Python SDK、Runner 和框架 Adapter
web/                       React + TypeScript 控制台
scripts/                   开发、演示、容量和恢复测试
docs/                      设计、计划、接入和验收文档
```

## 开发与验证

| 命令 | 用途 |
|---|---|
| `make fmt-check` / `make go-mod-check` | Go 格式和依赖漂移检查 |
| `make vet` / `make test-race` | Go 静态检查和竞态测试 |
| `make test` | Go 测试 |
| `make test-python` | Python SDK 测试 |
| `make python-lint` | Ruff、格式和 mypy 检查 |
| `make web-lint` | TypeScript 类型检查 |
| `make web-test` | 前端测试 |
| `make web-build` | 前端生产构建 |
| `make openclaw-test` | OpenClaw 插件类型检查和测试 |
| `make test-all` | 本地全栈质量门禁 |
| `make demo-phase4-agentops` | 统一真实企业闭环 |
| `make phase4-load-smoke` | 并发采集和实时链路容量烟测 |
| `make phase4-recovery-test` | Redis 故障恢复和最终一致性 |
| `make db-up` / `make db-down` | 启停本地基础设施 |

完整发布前验证：

```bash
cd python && uv sync --frozen --extra dev && cd ..
make web-install
make openclaw-install
make test-all
```

真实 PostgreSQL 和 ClickHouse 测试必须使用名称以 `_test` 结尾的隔离数据库，并设置
`AGENTSCOPE_TEST_DATABASE_URL` 与 `AGENTSCOPE_TEST_CLICKHOUSE_DSN`。CI 会应用全部
migration，再执行竞态检测、依赖审计、前后端测试和生产构建。

LLM-as-Judge 默认只读取官方供应商密钥并访问对应 HTTPS Origin。兼容供应商必须由运维
同时配置 `AGENTSCOPE_JUDGE_ALLOWED_API_KEY_ENVS` 和
`AGENTSCOPE_JUDGE_ALLOWED_BASE_URLS`，避免 evaluator 配置读取任意进程密钥或触发
SSRF。

真实基础设施验收还必须运行统一 Demo、容量、恢复和 Playwright 桌面/移动端测试。

## 设计与实施基线

- [Phase 4 实时可观测、可信评测与生产治理设计](docs/superpowers/specs/2026-07-24-phase4-realtime-evaluation-governance-design.md)
- [Phase 4 实施计划](docs/superpowers/plans/2026-07-24-phase4-realtime-evaluation-governance.md)
- [容量与恢复测试](docs/phase4-capacity-testing.md)

后续修改必须保持项目隔离、脱敏、不可变快照、Outbox 最终一致性和存储边界，不得为了
演示绕过真实 API 或直接向数据库写测试结果。
