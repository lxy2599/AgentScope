# AgentScope

AgentScope 是面向生产 Agent 的可观测、评测与治理平台。平台通过 Python SDK 和
OpenTelemetry 接入 Agent，在同一个项目中关联在线运行、语义 Trace、离线评测、
人工校准、实验对比和成本治理。

## 已实现能力

- **实时运行**：Agent 注册、实例心跳、Run 状态机、Redis Streams 和 SSE 实时事件；
- **深度可观测**：思考、LLM、工具、检索、Skill、终端、文件和最终答案语义记录；
- **运行快照**：对模型、Prompt、工具、代码和环境保存脱敏、不可变、可校验快照；
- **可信评测**：数据集、版本化 Rubric、多 Judge 独立投票、置信聚合和安全否决；
- **人工校准**：Gold 标注、Judge 校准报告、线上抽样、标注队列和回归数据集；
- **实验治理**：Baseline/Candidate 对比、统计显著性和回归门禁；
- **生产治理**：跨实例项目配额、SDK 背压、Token、TTFT、成本、Rollup 和异常检测；
- **真实接入**：通用 Python SDK、Hermes Adapter 和未修改上游源码的 OpenHands Adapter。

## 快速开始

### 环境要求

- Go 1.23+
- Python 3.10+
- Node.js 20.19+
- Docker 和 Docker Compose

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

框架 Adapter 只能使用上游公开生命周期事件或 callback，不允许修改上游源码或依赖
私有内部结构。详细语义约定见 `python/agentscope_sdk/` 和 Phase 4 设计文档。

## 架构

```text
Agent / OpenHands / Hermes
        │ Python SDK + OTLP + Runtime API
        ▼
Go 单体服务
  ├─ PostgreSQL：鉴权、项目、Run、Snapshot、评测、校准、配额、Outbox
  ├─ ClickHouse：Span、语义事件、Token/成本/延迟、Rollup
  └─ Redis：实时 Stream、SSE Cursor 和短期配额窗口
        │
        ▼
React 控制台：实时运行、Trace、评测、对比、采集和治理
```

存储边界：

- PostgreSQL 是业务事实和状态机的权威存储；
- ClickHouse 保存高吞吐可观测明细和分析聚合；
- Redis 只保存可重建的实时投影和短期计数，不能成为业务事实源；
- `Run` 是线上执行、Trace、评测结果和人工反馈的统一锚点。

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
| `make test` | Go 测试 |
| `make test-python` | Python SDK 测试 |
| `make web-test` | 前端测试 |
| `make web-build` | 前端生产构建 |
| `make demo-phase4-agentops` | 统一真实企业闭环 |
| `make phase4-load-smoke` | 并发采集和实时链路容量烟测 |
| `make phase4-recovery-test` | Redis 故障恢复和最终一致性 |
| `make db-up` / `make db-down` | 启停本地基础设施 |

完整发布前验证：

```bash
go vet ./...
go test -race ./...
cd python && python -m pytest
cd ../web && npm test -- --run
npm run lint
npm run build
```

真实基础设施验收还必须运行统一 Demo、容量、恢复和 Playwright 桌面/移动端测试。

## 设计与实施基线

- [Phase 4 实时可观测、可信评测与生产治理设计](docs/superpowers/specs/2026-07-24-phase4-realtime-evaluation-governance-design.md)
- [Phase 4 实施计划](docs/superpowers/plans/2026-07-24-phase4-realtime-evaluation-governance.md)
- [容量与恢复测试](docs/phase4-capacity-testing.md)

后续修改必须保持项目隔离、脱敏、不可变快照、Outbox 最终一致性和存储边界，不得为了
演示绕过真实 API 或直接向数据库写测试结果。
