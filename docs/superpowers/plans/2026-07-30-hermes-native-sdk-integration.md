# Hermes 原生 AgentScope SDK 接入实施计划

- **状态**：原生 fork 源码实现完成；原生平台 E2E 证据待单独归档
- **日期**：2026-07-30
- **前置设计**：`docs/superpowers/specs/2026-07-29-hermes-native-observability-design.md`
- **SDK 前置计划**：`docs/superpowers/plans/2026-07-30-sdk-trace-hierarchy-and-turns.md`
- **目标**：在固定 Hermes fork 的真实执行点直接使用 AgentScope Python SDK，跑通“在线观测 → 在线评测 → 失败/低分人工回流”最小闭环。


> **实施记录（2026-07-30）**：SDK 前置能力已合入 AgentScope `master`；原生 bridge 与真实
> conversation loop / LLM / Tool 生命周期改动已在 `integrations/hermes-agent/` 实现，fork HEAD 为
> `02a438eb5b04009bebfc3c23c60d03b347e6b133`。fork 回归测试已覆盖 native lifecycle。
> `make demo-hermes-agentops` 仍是外置 Adapter 的平台 E2E，不能替代 native fork 的平台证据；本计划
> 中涉及 PostgreSQL、ClickHouse、在线评测和人工回流的 native 真实验收应以独立结果文件继续追踪。

## 1. 范围与成功定义

本计划的首期范围是：一个真实用户输入启动一个 Runtime Run，并在 Hermes 的真实 decision loop 中建立
`agent.turn`，采集 LLM、Tool、Final Answer 和错误。最终交叉验证 PostgreSQL、ClickHouse 和 API：

```text
真实用户输入
-> agent.run
-> agent.turn(1..N)
-> llm.call / tool.call / agent.answer
-> Run completed 或 failed
-> Sampling Rule
-> online_eval_job
-> eval_scores
-> failed/low-score annotation_item
```

不在首期实现完整 reasoning、Memory、Skill、Approval 或子 Agent；这些只能在最小树关系与
fail-open 行为验收后扩展。

## 2. 先决条件与边界

1. 已建立 Hermes 专用 fork/worktree；当前 fork 上游基线为 `015718066ab8e9499c3caea3cda9f7ea469036fc`，fork commit 为 `02a438eb5b04009bebfc3c23c60d03b347e6b133`。
2. 不使用 `HermesAdapter.run()`、callback、observer hook 作为原生事件源。
3. Hermes fork 只新增 `hermes_agentscope` 薄 bridge；Runtime HTTP、OTLP exporter、协议常量、
   脱敏、导出背压均复用 `agentscope-sdk`。
4. 不允许 bridge 引用 SDK 私有 API（以下划线开头）；先补齐公共 session API 再开始侵入 Hermes。
5. 所有观测调用 fail-open；默认关闭，未启用时不 import SDK。

## 3. Phase 0：SDK 接口收敛

### 3.1 提取框架中性会话

`RuntimeTraceSession` 已从 Adapter 场景中提取为公共会话接口，可同时服务外置 Adapter 与 Hermes
原生代码。已实现的最低公共能力：

```python
session = RuntimeTraceSession.best_effort(..., flush_on_run_finish=False)
session.start_run(...)
session.start_turn(turn_id, turn_index)
session.finish_turn(outcome, error=None)
session.trace_llm(..., parent_context=session.active_parent_context)
session.trace_tool(..., parent_context=session.active_parent_context)
session.record_final_answer(..., parent_context=session.active_parent_context)
session.finish_run(output)
session.fail_run(error)
```

必须保持现有 `AdapterSession` API 与测试兼容；Adapter 可迁移到同一内核，但不是本阶段阻塞项。

### 3.2 必要的 SDK 语义

- `agent.run` 为 root，`agent.turn` 固定为 root child；
- LLM、Tool、Final Answer 的显式父节点是 active Turn context；
- `TURN`、`agentscope.turn.id`、`agentscope.turn.index`、outcome/counts 均来自
  `tracing_contract.py`；
- `parent_context` 显式优先；无 Turn 时回退到 root；不传时保持已有 helper 的 OTel current-context
  兼容语义；
- `finish_run()` / `fail_run()` 默认不 `force_flush()`，而由 BatchSpanProcessor 异步导出；
- Runtime init、Agent register、初始 heartbeat 失败必须可得到 NoOp session，不向 Hermes 抛出异常。

### 3.3 SDK 验收

- Root → Turn → LLM/Tool/Answer 的 `parent_span_id` 测试；
- Tool 不得成为 LLM child；重试和多 Tool 仍属于同一 Turn；
- `flush_on_run_finish=False` 不调用同步 flush；
- `best_effort` 在 SDK 配置错误、网络错误、Token 错误时可安全 no-op；
- 现有 Adapter hierarchy、Hermes Adapter、OpenHands Adapter 测试全部通过。

## 4. Phase 1：Hermes 调用图与薄 bridge（已完成）

### 4.1 调用图确认

在不修改执行逻辑的前提下，针对固定 Hermes commit 记录以下事实：

| 需要确认的事实 | 产出 |
|---|---|
| 用户输入如何进入 conversation loop | Run/Turn start 位置 |
| loop 何时决定继续下一轮 | Turn finish/start 边界 |
| LLM 请求、stream、重试和异常位置 | LLM Span 生命周期与真实 Token/TTFT 来源 |
| Tool dispatch、结果、失败、并发位置 | Tool Span 生命周期与 context 传递方式 |
| 最终回答和取消/异常出口 | finish/fail/cancel Run 位置 |
| session / task / conversation 标识 | registry key 与 Run metadata |

### 4.2 Bridge 写集

仅在 Hermes fork 中新增：

```text
hermes_agentscope/config.py
hermes_agentscope/bridge.py
hermes_agentscope/session_registry.py
hermes_agentscope/noop.py
```

桥接 API 应接近真实业务语言，例如：

```python
bridge.on_user_turn_started(...)
bridge.on_decision_turn_started(...)
bridge.trace_llm(...)
bridge.trace_tool(...)
bridge.on_final_answer(...)
bridge.on_turn_failed(...)
```

每个 API 内部独立捕获 SDK 异常；`session_registry` 以 Hermes session/task key 找到对应 SDK session。
禁止 thread-local/global “当前 Run”作为跨 session 状态来源。

### 4.3 配置与依赖

- fork 的 `agentscope-observability` extra 依赖已验证的 `agentscope-sdk`；
- `HERMES_AGENTSCOPE_ENABLED=false` 是默认；
- 启用后要求 `AGENTSCOPE_SERVER_URL` 与 `AGENTSCOPE_PROJECT_TOKEN`；缺失或无效时降级 NoOp；
- Prompt、输出、tool 参数/结果、reasoning 默认不采集；
- 不在 Hermes 侧实现额外 Runtime worker、HTTP client 或 exporter queue。

## 5. Phase 2：最小原生 Trace 链路

按调用图的真实位置接入，顺序如下：

1. 用户输入：创建 Run root 与 Turn 1；Run metadata 写入 session/conversation/task 和 `framework=hermes`。
2. LLM：将 request start / response finish / error 映射到 `trace_llm`；记录真实 duration、Token、TTFT、
   provider、model 和脱敏摘要。
3. Tool：将 dispatch start / result / error 映射到 `trace_tool`；所有 Tool 使用 active Turn context，
   不能以 LLM context 作为父节点。
4. loop 继续：结束当前 Turn（`continue`），启动下一个 `turn_index`。
5. 最终回答：`record_final_answer` 挂在 active Turn；结束 Turn（`final`），完成 Run。
6. 异常/取消：关闭未完成 child 与 Turn，并写 failed/cancelled Run；Hermes 原异常原样传播。

最小 E2E 至少覆盖：单轮纯回答、多轮含 Tool、LLM retry、Tool retry、Tool error、LLM error、取消（若
上游支持）。

## 6. Phase 3：在线评测和人工回流 E2E

为独立临时 Project 配置 `sample_rate=1.0`，避免污染日常项目。执行成功和失败各一个真实 Hermes Run，
轮询并交叉验证：

| 层 | 证据 |
|---|---|
| Runtime API / PostgreSQL | Agent/Instance heartbeat、Run、Sampling Rule、online_eval_job、eval_scores、annotation item |
| ClickHouse | `trace_spans` 的 Root/Turn/child 树，`trace_llm_calls`、实际使用的 `trace_tool_calls`、`trace_final_answers` |
| UI / SSE | Run 时间线与 Tree 可展示；评测/标注状态可追溯 |

失败 Run 的 annotation item 必须由在线评测链路自动生成，并关联 `online_eval_job_id`；演示脚本不得手工伪造。
TraceAware evaluator 仅验证执行健康，不能表述为语义正确性判断。

## 7. Phase 4：扩展语义（后续）

最小链路验收后再实现：

- Memory read/write；
- Skill load/call；
- approval allow/deny/timeout（Guardrail）；
- 经过脱敏与明确同意的内容采集；
- 子 Agent 独立 Run 及 trace link / 父子关系；
- 仅在真实、稳定生成点存在时的 reasoning。

每增加一种语义，都先增加 SDK/ingest 契约测试与 Hermes 真实调用图回归。

## 8. 回归与提交策略

每个阶段单独中文提交，建议顺序：

```text
1. SDK：提取原生 Trace Session 与 non-blocking finish
2. SDK：Turn/parent-context/no-op/flush 回归测试
3. Hermes fork：bridge、配置和 disabled compatibility
4. Hermes fork：Run/Turn/LLM/Tool/Answer 原生埋点
5. AgentScope：真实 E2E、数据库证据与文档
```

每次 Hermes 上游升级均重新执行调用图确认、disabled compatibility、最小 Trace 树和在线评测 E2E。
