# CLAUDE.md

本文件约束 Claude Code、Codex 和其他代码代理在 AgentScope 仓库中的开发行为。

## 当前阶段

AgentScope 已完成 Phase 1–3 基础能力，并在独立分支实现 Phase 4：真实 OpenHands
接入、Redis Streams + SSE 实时事件、不可变 Run Snapshot、多 Judge 与人工校准、
实验显著性分析、分布式配额、SDK 背压、成本分析、Rollup 和异常检测。

当前权威文档：

- `docs/superpowers/specs/2026-07-24-phase4-realtime-evaluation-governance-design.md`
- `docs/superpowers/plans/2026-07-24-phase4-realtime-evaluation-governance.md`
- `docs/phase4-enterprise-demo.md`
- `docs/phase4-verification-report.md`

旧计划只用于追溯历史决策，不得用旧 MVP 状态判断当前功能。

## 开工规则

1. 先运行 `git branch --show-current && git status --short && git worktree list`。
2. 功能开发必须在隔离 worktree 中进行，禁止直接在 `main` 或 `master` 修改。
3. 先读相关设计和计划，再读现有实现；修复缺陷时先复现并添加回归测试。
4. 每个任务按计划边界单独提交，提交信息使用中文。
5. 完成代码后必须跑通真实流程并核对数据库，不能只跑单元测试。
6. 不覆盖用户已有改动，不使用 `git reset --hard`、`git checkout -- .` 或
   `git clean -fd`。

## 架构边界

核心架构是 Go 单体服务 + PostgreSQL + ClickHouse + Redis + Python SDK/Runner +
React 控制台。

工具链基线为 Go 1.26.5、基础 SDK Python 3.11+、Node.js 20.19+，前端使用 React
Router 7、Vite 8 和 Vitest 4；OpenHands 集成要求 Python 3.12+，OpenClaw 插件测试
要求 Node.js 22.22.3+。

- PostgreSQL 保存用户、权限、项目、Run 状态机、Snapshot、数据集、评测、校准、
  实验对比、配额配置和 Outbox。
- ClickHouse 保存 Span、语义事件、Token、TTFT、成本、延迟和 Rollup。
- Redis 保存可重建的实时 Stream、SSE Cursor 和短期分布式计数。
- `Run` 是在线执行、Trace、评测和人工反馈的统一业务锚点。

禁止事项：

- handler 直接写 SQL；
- Redis 保存不可恢复的业务事实；
- store 包含 HTTP 语义；
- 服务端执行用户 Agent 代码；
- 在 PostgreSQL 保存 Trace 明细，或在 ClickHouse 保存鉴权和业务状态机；
- 在非受控 fork 中修改 Hermes、OpenHands 等上游 Agent 源码，或在受控 fork 中将观测失败传播到主执行链；
- 将 API Key、Project Token、密码、完整敏感 Prompt 写入数据库、日志或结果文件；
- 在 SSE Payload 或 Snapshot 中绕过脱敏。

## SDK 与 Adapter 约束

- Python SDK 负责 Agent 注册、心跳、Run 生命周期、语义埋点、OTLP 批量导出和背压；
- Adapter 使用上游公开生命周期事件，将框架事件映射为统一语义；
- Hermes 已是受控的原生 fork 接入；OpenHands 也只允许在固定 commit、可回放的 AgentScope 维护 fork 中，于真实执行点直接调用 AgentScope SDK。OpenHands 不再提供或维护 `OpenHandsAdapter`；原生路径不得使用 `HermesAdapter.run()`、callback 或 observer hook 冒充事件源，且必须默认关闭、fail-open、行为兼容；
- 原生 Hermes/OpenHands 必须复用 SDK 的 Runtime、OTLP、背压、脱敏与 `tracing_contract.py`，不维护第二套 Runtime client / exporter / Trace 协议；Run 结束默认不得同步 flush；
- 对 agentic loop，Span 树必须显式表达 `agent.run -> agent.turn -> LLM/Tool/Final Answer`；Turn 不等于一次 LLM 调用，Tool 不得因隐式 Context 误挂在 LLM 下；
- LLM 调用必须记录真实或可解释的 duration、Token、TTFT 和成本来源；
- 估算成本使用显式模型单价，并标记为 estimated，不能伪装成供应商账单；
- 内部系统事件不能伪装成 LLM 调用；
- 多 Agent、多 Token 并存时，Tracer Provider 和资源属性必须项目隔离；
- 外置 Adapter 使用的官方上游工作区必须固定版本且保持干净；原生 fork 必须记录可回放的上游基线和 fork commit，并保持应用 patch 后的工作区干净。

## 实时与一致性约束

- PostgreSQL/ClickHouse 成功写入后再产生 Outbox 事件；
- Relay 将事件投递到项目级 Redis Stream，SSE 只读取当前项目；
- Redis 故障不能使权威写入失败，恢复后必须重放并最终一致；
- 前端按事件 ID 去重，支持 Cursor 重连、reset 和轮询降级；
- Run 完成前必须能观察到非终态状态和语义事件；
- 每个新 Run 都应生成脱敏、不可变、带 SHA-256 的 Snapshot。

## 评测约束

- 数据集、Rubric、Judge 版本和 Run Snapshot 必须可追溯；
- 每个 Judge 的投票、理由、置信度和错误独立保存；
- 聚合结果保存分歧度、置信区间和安全 veto；
- 人工 Gold 用于校准 Judge，不得覆盖原始 Judge 投票；
- Baseline/Candidate 对比必须报告效应量、显著性和回归门禁；
- 演示和测试必须通过公开 API 创建评测数据，不允许直接造库。

## 关键命令

```bash
make dev
make db-up
make db-down

make test-all
make test-race
make python-lint
make web-lint
make openclaw-test

make demo-phase4-agentops
make phase4-load-smoke
make phase4-recovery-test
```

浏览器 E2E：

```bash
cd web
AGENTSCOPE_API_URL=http://127.0.0.1:8080 \
PHASE4_RESULT_FILE=../.external/phase4-agentops-result.json \
npm run test:e2e
```

统一 Demo 是当前主验收路径。旧 Demo 只能作为兼容性回归，不能替代 Phase 4 的真实
OpenHands、SSE、数据库、容量和恢复验证。

## 完成标准

提交前至少完成与改动对应的测试；宣布阶段完成前必须逐项验证设计文档第 14 节，并将
命令、真实数据和未解决风险写入 `docs/phase4-verification-report.md`。测试输出、
数据库查询和浏览器 E2E 是证据，代码存在或代理口头报告不是证据。

真实存储测试只允许连接名称以 `_test` 结尾的隔离数据库。新增或修改外部调用时还必须
验证跨项目隔离、对外错误脱敏、密钥变量名和目标 Origin 白名单，并维护 Go、Python、
Web 与 OpenClaw 的静态检查、依赖审计、测试和构建门禁。
