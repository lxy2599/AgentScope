# Hermes Agent 全面接入实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use `executing-plans` task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **状态（2026-07-30 同步）**：这是外置 `HermesAdapter` 兼容路径的历史实施计划；该路径已实现并由
> `make demo-hermes-agentops` 覆盖。它不是当前 Hermes 原生 fork 的实现说明。原生实现已在
> `integrations/hermes-agent/` 完成，权威现状见 `docs/integrations/hermes-agent.md` 与
> `docs/superpowers/specs/2026-07-29-hermes-native-observability-design.md`。

**Goal:** 将 NousResearch Hermes Agent 通过稳定回调完整接入 AgentScope，使一个真实 Hermes 常驻实例持续心跳、执行真实 LLM+工具任务，并在平台中形成可追溯的 run、trace、语义事件和失败标注闭环。

**Architecture:** AgentScope SDK 增加框架无关的 `AdapterSession` 与事件归一化层；Hermes 集成仅在创建 Hermes `AIAgent` 时绑定其公开回调，不修改或复制 Hermes 主循环。集成运行器以固定上游提交检出 Hermes，使用环境中已配置的 OpenAI 兼容模型端点，运行真实任务并对 API、PostgreSQL 和 ClickHouse 进行同一轮数据验证。

**Tech Stack:** Python 3.11+、OpenTelemetry、AgentScope Python SDK、Hermes Agent upstream、Go HTTP API、PostgreSQL、ClickHouse、Docker Compose。

## Global Constraints

- 代码在 `.claude/worktrees/phase3-8-hermes-integration` 独立 worktree 开发；不得修改根工作区用户的 `docker-compose.yml`。
- 锁定 Hermes 上游提交 `d9165d7a678d4105f42921a7fc1886df3804531b`，运行器拒绝未验证的检出目录。
- 不读取、不打印、不写入 ccswitch 的任何明文凭据；仅通过环境变量传递模型端点与密钥。
- 不重写 Hermes 主逻辑；仅使用 `agent_init` 的 `tool_progress_callback`、`reasoning_callback`、`step_callback`、`stream_delta_callback` 和 `event_callback`。
- 工具参数、模型输出和错误文本统一使用 SDK 脱敏和长度限制逻辑。
- 所有新增行为先写失败测试，确认 RED 后才写实现；每个任务独立提交且提交信息使用中文。

---

## 文件职责

- `python/agentscope_sdk/adapters/events.py`：外部 Agent 事件的数据模型和安全摘要归一化。
- `python/agentscope_sdk/adapters/base.py`：管理 runtime/run/root span、即时 span 生命周期和语义事件上报。
- `python/agentscope_sdk/adapters/hermes.py`：Hermes 回调到 `AdapterSession` 的无侵入绑定。
- `python/agentscope_sdk/adapters/__init__.py`：稳定公开导入入口。
- `python/tests/test_adapter_events.py`：归一化与语义事件单元测试。
- `python/tests/test_hermes_adapter.py`：使用最小 Hermes 形状的 fake agent 验证回调生命周期，不要求安装 Hermes。
- `python/examples/hermes_agentops_demo.py`：真实 Hermes 任务、持续心跳、annotation 闭环和 DB/API 验证。
- `scripts/setup-hermes-agent.sh`：锁定提交检出和依赖安装，不将上游源码纳入仓库。
- `scripts/demo-hermes-agentops.sh`、`Makefile`：一键入口。
- `docs/integrations/hermes-agent.md`：环境、映射表、隐私边界、验证与故障排查。

## Task 1: 事件模型与通用 AdapterSession

**Files:**
- Create: `python/agentscope_sdk/adapters/__init__.py`
- Create: `python/agentscope_sdk/adapters/events.py`
- Create: `python/agentscope_sdk/adapters/base.py`
- Test: `python/tests/test_adapter_events.py`

**Interfaces:**
- Produces `NormalizedEvent(kind, name, input_summary, output_summary, attributes, success, error)`。
- Produces `AdapterSession.start_run()`、`on_thought()`、`on_llm_call()`、`on_tool_start()`、`on_tool_finish()`、`on_memory_read()`、`on_memory_write()`、`on_skill_call()`、`on_guardrail()`、`finish_run()`、`fail_run()`。

- [ ] **Step 1: 写失败测试**：覆盖 tool/memory/skill/guardrail 归一化、跨事件 trace 上下文和错误关闭 run。
- [ ] **Step 2: 运行测试确认失败**：`cd python && python -m pytest tests/test_adapter_events.py -q`，期望因模块不存在失败。
- [ ] **Step 3: 实现最小事件模型和会话**：以 OTel root span 将 runtime run 与 trace ID 对齐，工具 span 跨 started/completed 成对关闭，memory/guardrail 使用显式 semantic span 属性。
- [ ] **Step 4: 运行目标测试确认通过**：同上，期望全通过。
- [ ] **Step 5: 提交**：`feat: 添加通用 Agent 事件适配层`。

## Task 2: Hermes 无侵入回调适配

**Files:**
- Create: `python/agentscope_sdk/adapters/hermes.py`
- Create: `python/tests/test_hermes_adapter.py`
- Modify: `python/agentscope_sdk/adapters/__init__.py`

**Interfaces:**
- Produces `HermesAdapter(session).bind(agent)` 和 `HermesAdapter.run(agent, prompt, session_id)`。
- `tool.started` / `tool.completed` 映射为 tool；`memory` 按 action 映射为 memory read/write；`skill_view` / `skill_manage` 映射为 skill；guardrail 拒绝事件映射为 guardrail；reasoning 映射为 thought；LLM 回合由 `step_callback` 与 agent metadata 映射为 LLM；最终文本映射为 final answer。

- [ ] **Step 1: 写失败测试**：fake Hermes agent 触发各回调，断言调用顺序和 normalized metadata，不依赖 Hermes 安装。
- [ ] **Step 2: 运行测试确认失败**：`cd python && python -m pytest tests/test_hermes_adapter.py -q`。
- [ ] **Step 3: 实现回调绑定**：保存已有回调并链式调用；任何观测错误不得中断 Hermes；所有事件携带 `framework=hermes`、上游提交与 session 标识。
- [ ] **Step 4: 运行 Task 1/2 测试**：`cd python && python -m pytest tests/test_adapter_events.py tests/test_hermes_adapter.py -q`。
- [ ] **Step 5: 提交**：`feat: 接入 Hermes 运行时回调`。

## Task 3: 真实 Hermes 检出、运行与验证闭环

**Files:**
- Create: `scripts/setup-hermes-agent.sh`
- Create: `scripts/demo-hermes-agentops.sh`
- Create: `python/examples/hermes_agentops_demo.py`
- Create: `python/tests/test_hermes_demo_structure.py`
- Modify: `Makefile`
- Create: `docs/integrations/hermes-agent.md`

**Interfaces:**
- `make demo-hermes-agentops`：启动前检查后端和环境变量，检出固定 Hermes commit，执行真实任务并输出单个账户/项目的链接。
- 运行结果 JSON 包含 `project_id`、`agent_id`、`run_ids`、`trace_ids`、`annotation_item_id`。
- `python/examples/hermes_agentops_demo.py --verify-only <result-json>` 对同一轮数据执行 API、PostgreSQL、ClickHouse 验证。

- [ ] **Step 1: 写失败结构测试**：验证脚本固定上游 commit、拒绝空模型配置、输出单一登录上下文、检查 run/trace/annotation 证据。
- [ ] **Step 2: 运行测试确认失败**：`cd python && python -m pytest tests/test_hermes_demo_structure.py -q`。
- [ ] **Step 3: 实现检出与 demo**：运行真实 Hermes `agent.oneshot` 或官方 `agent_init` 路径，执行至少一个实际 LLM+工具任务；在同一 runtime 保持心跳至少两个间隔；从实际失败任务创建 annotation item，不伪造 span。
- [ ] **Step 4: 运行结构和 SDK 全量测试**：`cd python && python -m pytest`。
- [ ] **Step 5: 启动依赖并执行真实 E2E**：用隔离端口启动当前 worktree 后端，运行 `make demo-hermes-agentops`，检查结果 JSON；查询 PostgreSQL 的 agent/instance/run/annotation 与 ClickHouse 的 spans、thought、LLM、tool、skill/memory/guardrail、final answer。
- [ ] **Step 6: 提交**：`feat: 提供 Hermes 真实接入演示`。

## Task 4: 全项目回归与完成审计

**Files:**
- Modify: `docs/integrations/hermes-agent.md`（只记录经验证的命令和证据）

- [ ] **Step 1: 执行全量后端测试**：`go test ./...`。
- [ ] **Step 2: 执行 Python 全量测试**：`cd python && python -m pytest`。
- [ ] **Step 3: 执行前端测试和构建**：`cd web && npm test -- --run && npm run build`。
- [ ] **Step 4: 复跑真实 Hermes E2E 并核对数据**：确认在线实例、至少一个成功任务、至少一个失败任务、trace 语义表及 annotation 均属于本轮项目。
- [ ] **Step 5: 提交**：`test: 验证 Hermes AgentOps 端到端闭环`。

## OpenClaw 后续判定

仅在 Hermes 全链路验证通过后评估 OpenClaw。OpenClaw 是 Node.js 运行时且事件模型不同，不能把 Hermes 适配器复制过去；应另建 worktree，实现 channel/session、permission/guardrail 与持续 ingest 映射，并运行独立真实 E2E。它不是本次 Hermes 验收的替代品。
