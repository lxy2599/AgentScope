# Hermes 原生内置 AgentScope 可观测设计

- **状态**：已在受控 fork 实现；平台原生 E2E 证据需与外置 Adapter Demo 分开维护
- **首次日期**：2026-07-29
- **最后更新**：2026-07-30
- **适用原生 fork 基线**：`hermes-agent-agentscope` 上游基线 `015718066ab8e9499c3caea3cda9f7ea469036fc`；当前 fork commit `02a438eb5b04009bebfc3c23c60d03b347e6b133`
- **决策**：在受控的 Hermes fork 源码内部、真实执行点直接调用 **AgentScope Python SDK**。不以 `HermesAdapter.run()`、Hermes callback 或 Plugin observer hook 作为原生路径；外置 Adapter 仅保留为兼容和回归路径。


## 实施状态与证据（2026-07-30）

设计已落实在维护 fork：`hermes_agentscope/bridge.py` 以 `RuntimeTraceSession` 管理 Run/Turn/LLM/Tool/
Final Answer/Failure，`agent/agent_init.py` 初始化可选 bridge，`agent/conversation_loop.py` 与
`agent/tool_executor.py` 在真实生命周期边界调用。配置实际只使用
`HERMES_AGENTSCOPE_ENABLED`、`AGENTSCOPE_SERVER_URL`、`AGENTSCOPE_PROJECT_TOKEN` 与
`AGENTSCOPE_ENVIRONMENT`；未启用、缺少配置或 SDK 初始化失败均退化为 NoOp。

fork 内 `tests/test_agentscope_bridge.py` 和 `tests/agent/test_agentscope_native_lifecycle.py` 覆盖并发会话隔离、
真实 loop、LLM、工具、最终回答、异常和并行工具。AgentScope 仓库的 `make demo-hermes-agentops` 是外置
Adapter 的平台 E2E，不构成 native fork 的平台 E2E 证据。

## 1. 背景与问题

现有 `HermesAdapter` 已验证：真实 Hermes 可以通过公开 callback / observer hook 接入
AgentScope，采集 Run、LLM、Tool、Memory、Skill、Thought、最终回答，并跑通在线评测和失败
Run 自动回流人工标注。这是重要的兼容性方案，但其入口是调用方主动执行
`adapter.run(agent, prompt)`，不适合 Hermes 作为长期运行的 Dashboard、Gateway、CLI 或 ACP
服务时使用。

原生方案的目标是把观测下沉到 Hermes 的真实 conversation loop、LLM transport 和 Tool dispatch
等执行点；用户的启动方式、会话模型、工具行为和回答结果均不因观测改变。AgentScope SDK 负责
Runtime 控制面、OTLP Trace、统一语义协议、脱敏和导出背压，Hermes fork 只负责将自身真实生命周期
映射给 SDK。

## 2. 决策摘要

### 2.1 采用

1. 建立固定上游版本的独立 Hermes fork/worktree；仅在该受控 fork 中加入原生埋点。
2. Hermes fork 可选依赖 `agentscope-sdk`；启用时直接调用 SDK 的 `Runtime`、Trace helper 与
   原生 Trace Session，不重复实现 Runtime HTTP client、OTLP exporter、Span buffer 或语义协议。
3. 每个**真实用户 Turn**创建一个 AgentScope Run 和根 Span；在 Run 内显式建立一个或多个
   `agent.turn` Span，表达 Hermes 的 agentic decision loop，而不是把每次 LLM 调用误当作 Turn。
4. 在真实执行点记录 LLM、Tool、Memory、Skill、Approval、错误和 Final Answer；各 Span 通过
   显式 `parent_context` 挂在正确的 Turn 下。
5. 默认关闭、fail-open、内容默认脱敏；SDK 或后端故障绝不改变 Hermes 的控制流、返回值或异常。
6. 原生 Run 终态继续进入已有 Sampling Rule、在线评测、低分/失败 Run 人工标注和 Gold 样本回灌链路。

### 2.2 明确不采用

- 不在 fork 内重新实现 `runtime_client.py`、OTLP exporter、独立 Trace queue 或重复的
  `models.py` 事件协议；这些职责属于 AgentScope SDK。
- 不在 Hermes 原生路径中调用 `HermesAdapter.run()` 或用 `AdapterSession` 包装整个
  `run_conversation()`；否则仍然依赖外围 callback，且容易与真实埋点重复生成 Span。
- 不以 `tool_progress_callback`、`reasoning_callback`、`step_callback`、`stream_delta_callback`、
  `event_callback` 或 Plugin observer hook 作为原生事件源。
- 不增加新的 Hermes HTTP 服务、代理服务、启动命令或用户侧调用方式。
- 不在用户请求路径同步等待 AgentScope Runtime API、OTLP 导出、Redis、PostgreSQL 或 ClickHouse。

## 3. 目标与非目标

### 3.1 目标

- 原有 `hermes dashboard`、Gateway、CLI、ACP 和会话操作保持不变。
- 每个真实用户 Turn 有可关联的 `Run`、`trace_id` 与结构完整的 Trace。
- 在 Run 内准确区分 Turn、LLM、Tool 和 Final Answer，并支持重试、多个 Tool 与异步结束。
- 长驻 Hermes 进程通过 SDK 注册 Agent、维护实例心跳，并以 SDK 的批处理能力异步导出 Trace。
- AgentScope 现有 Run 详情、实时 SSE、在线评测和人工标注无需为 Hermes 新建平行链路。
- 后端不可用、Token 无效、SDK 初始化/导出失败时，Hermes 原有结果与异常语义保持不变。

### 3.2 非目标

- 不改变 Hermes 的 Prompt、messages、provider、model、base URL、重试、工具列表、工具参数、
  工具结果、审批逻辑或 session 逻辑。
- 不把 AgentScope 变成 Hermes 的控制面、配置中心或鉴权层。
- 第一阶段不采集完整 reasoning 文本；仅在 Hermes 存在稳定真实生成点、用户明确开启内容采集后再实现。
- 不承诺 AgentScope 不可用时零数据丢失；优先级永远是 Hermes 可用性和行为兼容。
- 不在本阶段新增 AgentScope 后端协议或服务。

## 4. 第一原则：行为不变与 fail-open

启用或禁用观测均不得改变：启动命令、会话 ID、模型请求内容、工具选择及结果、审批、重试、
callback 的存在/顺序/参数/次数、最终回答、返回对象和原异常。

每个埋点点必须满足：

```text
观测调用异常 -> 就地捕获，仅增加本地 debug 日志/计数
SDK 初始化失败 -> 返回 NoOp session，Hermes 继续执行
后端/网络错误 -> SDK 后台重试或丢弃，不阻塞当前 Turn
队列已满       -> 有界丢弃 Span，绝不阻塞模型、工具或结果返回
```

日常 Turn 结束时不得同步 `force_flush()`；仅在 Hermes 进程优雅退出、显式诊断或测试中以有上限的时间
尝试 flush。原生常驻服务必须使用 SDK 的 BatchSpanProcessor 批处理策略，而不是每个 Run 强制导出。

## 5. 总体架构

```text
Hermes 原生 Dashboard / Gateway / CLI / ACP
                    |
                    v
          Hermes conversation loop（真实执行点）
                    |
       +------------+-------------+
       |                          |
       v                          v
Hermes 原有执行链                hermes_agentscope bridge（薄层）
LLM / Tool / Memory / Skill      延迟加载 SDK、会话注册、fail-open
       |                          |
       v                          v
原始结果或原始异常      AgentScope SDK: Runtime + Trace Session + OTel exporter
                                      |
                                      v
                             AgentScope 后端
                    PostgreSQL / ClickHouse / Redis
                                      |
                                      v
               Run 详情 / 实时观测 / 在线评测 / 标注回流
```

桥接层只传递执行事实的副本，不能向 Hermes 执行链返回控制信息。SDK 是 Runtime、Trace、协议和导出
的唯一实现来源；Hermes bridge 不拥有第二套 Span 或 Run 状态机。

## 6. Hermes fork 与 SDK 依赖策略

### 6.1 源码承载

```text
/home/luxinyu/GolandProjects/hermes-agent-agentscope
branch: main
upstream base: 015718066ab8e9499c3caea3cda9f7ea469036fc
current fork commit: 02a438eb5b04009bebfc3c23c60d03b347e6b133
```

上游更新用 rebase/cherry-pick 管理。侵入式改动只能在这个可审查、可复放的 fork 分支中维护，
不能在 AgentScope 的 `.tmp/` 缓存目录长期开发。

### 6.2 可选 SDK 依赖

Hermes 默认安装不能增加 AgentScope 依赖或网络行为。fork 增加一个可选 extra，例如：

```toml
[project.optional-dependencies]
agentscope-observability = [
  "agentscope-sdk==<经验证版本>",
]
```

开发阶段允许与本仓库 `python/` 以 editable 方式安装；发布/复验阶段必须锁定经过 E2E 验证的 SDK
版本或 commit。启用 `HERMES_AGENTSCOPE_ENABLED=true` 时才延迟 import bridge 和 SDK；未启用时
不得 import SDK、创建后台线程或发送网络请求。

### 6.3 SDK 接口边界

`AdapterSession` 已具备 Root/Turn 状态、显式父 Context 和语义 helper；原生 fork 使用的
框架中性公共会话接口已经以 **`RuntimeTraceSession`** 提供，并默认使用
`flush_on_run_finish=False`：

```python
session = RuntimeTraceSession.best_effort(
    runtime_config=..., tracer=..., metadata={"framework": "hermes", ...},
    flush_on_run_finish=False,
)
run = session.start_run(...)
session.start_turn(turn_id=..., turn_index=...)
session.trace_llm(..., parent_context=session.active_parent_context)
session.trace_tool(..., parent_context=session.active_parent_context)
session.finish_turn(outcome="continue" | "final" | "error")
session.finish_run(output=...)
```

该接口复用 SDK 现有 Runtime、OTLP、`tracing_contract.py` 与 Span helper；它不是对 Hermes 的
Adapter 包装，也不能要求 callback。`HermesAdapter` 可以继续复用同一会话内核，但它仍是兼容路径。

`best_effort` 必须保证 Runtime 注册、初始心跳或 tracer 配置失败时返回 NoOp session，调用方无需
分支处理。Run finish/fail 仅关闭 Span 和异步提交 Runtime 终态，默认不 force flush。

## 7. Hermes bridge 模块

Hermes fork 已包含以下薄模块，不重复 SDK 内部实现：

```text
hermes_agentscope/
├── __init__.py
├── config.py
├── bridge.py
├── session_registry.py
└── noop.py
```

| 模块 | 职责 | 不应承担的职责 |
|---|---|---|
| `config.py` | 环境变量解析、默认关闭、内容采集开关 | Runtime HTTP、OTLP 配置细节 |
| `bridge.py` | 延迟加载 SDK；将真实 Hermes 生命周期映射为 SDK 调用；逐点隔离异常 | 改写 Hermes 控制流、维护第二套 Span |
| `session_registry.py` | 用 Hermes `session_id` / conversation / task 维护并发安全的 session 映射 | 全局“当前 Run”单例 |
| `noop.py` | SDK 未安装、未启用或初始化失败时的同形 no-op 对象 | 日志洪泛或重试阻塞 |

配置建议：

| 配置 | 默认 | 说明 |
|---|---:|---|
| `HERMES_AGENTSCOPE_ENABLED` | `false` | 是否启用原生旁路观测 |
| `AGENTSCOPE_SERVER_URL` | 无 | AgentScope 后端地址 |
| `AGENTSCOPE_PROJECT_TOKEN` | 无 | 项目 Bearer Token |
| `AGENTSCOPE_ENVIRONMENT` | `dev` | Runtime 环境标识 |

Token 仅存于 Hermes 原有的本地环境/部署密钥系统；禁止写入代码、日志、Trace、Git 或结果文件。

## 8. Trace、Turn 与父子 Span 协议

### 8.1 目标 Span 树

一个真实用户输入对应一个 Run。一次 Hermes 决策循环对应一个 Turn；一轮可以含多次 LLM 调用、
Tool 重试或并发 Tool，不能等同于一次模型请求。

```text
agent.run                         # 一个真实用户 Turn / Runtime Run
├── agent.turn(index=1)           # 第一次 decision loop
│   ├── llm.call
│   ├── tool.search
│   └── tool.search.retry
└── agent.turn(index=2)           # 使用工具结果后的下一次 decision loop
    ├── llm.call
    └── agent.answer
```

`agent.run` 是根 Span，`agent.turn` 的父节点固定为 Run；LLM、Tool、Memory、Skill、Approval、
Thought 与 Final Answer 均优先挂到**活动 Turn**。没有活动 Turn 时才回退到 Run。绝不能依赖隐式
OTel current context 让 Tool 误挂在 LLM 下：

```text
错误：agent.run -> agent.turn -> llm.call -> tool.search
正确：agent.run -> agent.turn -> { llm.call, tool.search }
```

### 8.2 固定协议

必须使用 SDK `tracing_contract.py` 中的常量，禁止在 Hermes 代码手写跨系统依赖的协议字符串：

```text
SpanName.RUN                  = agent.run
SpanName.TURN                 = agent.turn
SpanName.ANSWER               = agent.answer
SpanAttribute.SEMANTIC_KIND   = agentscope.span.kind
SpanAttribute.RUN_ID          = agentscope.run.id
SpanAttribute.TRACE_ID        = agentscope.trace.id
SpanAttribute.TURN_ID         = agentscope.turn.id
SpanAttribute.TURN_INDEX      = agentscope.turn.index
```

Turn Span 还应写入：

```text
agentscope.turn.outcome       continue / final / error / cancelled
agentscope.turn.llm_count
agentscope.turn.tool_count
```

这项“字符串优化”是**可持久化 Trace 协议治理**，不是微观性能优化：后端 ingest、ClickHouse 查询、
前端和不同语言 SDK 都依赖这些稳定字段。Hermes 自身动态值（model、tool name、session id、
run id）仍是普通数据，不能伪装为常量。

### 8.3 上下文与并发

每个原生 SDK session 显式保存：

```text
root_context        -> agent.run
active_turn.context -> agent.turn.N
```

创建语义 Span 时的父节点优先级为：调用方显式传入 `parent_context`、活动 Turn context、Root
context、最后才是 SDK helper 的兼容 current context。每个 Hermes session/task 必须拥有独立 session
对象或可验证的 registry key；禁止在多会话、异步 Tool 或子 Agent 场景使用全局 current Run。

`call_id`、`tool_id` 只用于 start/finish 配对，不能取代 Turn 或 parent span 关系。

## 9. 源码级映射（先以调用图复核为准）

下表的逻辑映射已经在受控 fork 的真实函数与异常路径中实现；后续上游升级必须重新复核调用图，且不得为了方便退回 callback / observer。

| Hermes 真实位置 | SDK 调用 | 结果 |
|---|---|---|
| 用户消息进入 `conversation loop` | `start_run()` + `start_turn()` | Run 根 Span + Turn 1 |
| loop 决定继续，准备下一次模型决策 | `finish_turn("continue")` + `start_turn()` | 同 Run 的下一个 Turn |
| LLM transport 请求开始/结束/异常 | `trace_llm(..., parent_context=active_turn)` | `LLM` Span，记录真实耗时、Token、TTFT、错误 |
| Tool dispatch 开始/结束/异常 | `trace_tool(..., parent_context=active_turn)` | `TOOL` Span，记录参数/结果摘要、状态、耗时 |
| Memory read/write | `trace_tool("memory", ...)` 或 SDK memory helper | 可查询的 Memory 语义 |
| Skill load/call | `trace_skill(..., parent_context=active_turn)` | `SKILL` Span |
| approval allow/deny/timeout | Guardrail 语义 helper / generic span | `GUARDRAIL` Span |
| 最终正常回复 | `record_final_answer(..., parent_context=active_turn)`，再 `finish_turn("final")`、`finish_run()` | Final Answer + completed Run |
| 模型/工具/循环异常或取消 | 关闭活动 child/Turn，`fail_run()` 或 cancelled finish | failed/cancelled Run 与 error Span |

首期最小闭环仅实现：**Run → Turn → LLM → Tool → Final Answer / Error**。Memory、Skill 和 Approval
在最小闭环正确、可验证后再加入，避免在调用图未确认时扩大侵入面。

## 10. 常驻运行与在线评测闭环

用户继续运行：

```bash
hermes dashboard
```

只需配置原生 Hermes 环境：

```bash
export HERMES_AGENTSCOPE_ENABLED=true
export AGENTSCOPE_SERVER_URL=http://127.0.0.1:8080
export AGENTSCOPE_PROJECT_TOKEN='...'
```

启动后 SDK best-effort 注册 Agent/Instance 并维护 heartbeat。每个真实用户 Turn 创建的 Run 在终态进入
既有链路：

```text
Run completed / failed
-> Sampling Rule 决策
-> PostgreSQL online_eval_jobs
-> TraceAware evaluator 读取 Run + ClickHouse Trace 证据
-> eval_scores
-> 失败或低分 Run 自动写 annotation_items
-> 人工 Gold 标注 / Judge 校准 / 高价值样本回灌 Dataset Case
```

TraceAware evaluator 仅表示执行健康（完成状态、Final Answer、Tool、Span error 等），不等同于
语义正确性、事实性、安全性或人工 Gold。线上采样和人工回流依赖 Run 终态，因此 bridge 必须以
best-effort 方式保证 start → finish/fail 的顺序；如果 Run start 永久失败，后续控制事件应丢弃并计数，
不能发无主终态。

## 11. 验收标准

### 11.1 行为兼容

| 场景 | 验收 |
|---|---|
| 默认未启用 | 不 import SDK、不创建线程、不发网络请求；Hermes 原测试与交互不变 |
| SDK 缺失/初始化失败 | 返回 NoOp bridge；Hermes 正常聊天、调用工具、返回原结果 |
| 后端不可达或 Token 无效 | 主流程不等待重试；仅观测丢失/计数增加 |
| 原 callback 已安装 | callback 顺序、参数和次数不变；不替换或包装 callback |
| Dashboard/Gateway/CLI/ACP | 不改命令、不要求用户改为 `adapter.run()` |
| 模型/Tool 抛异常 | 原异常语义不变，同时 best-effort 产生 failed Run/Trace |

### 11.2 数据正确性

对多轮真实 Hermes 对话验证：

```text
Agent 与 Instance 已注册且存在 heartbeat
每个真实用户 Turn 对应一个 Run，Run 与根 Span 共用 trace_id
Root -> Turn -> LLM/Tool/Final Answer 的 parent_span_id 正确
turn_index 单调递增；一轮多 LLM/Tool 和重试仍归属同一 Turn
不存在 Tool 作为 LLM 子 Span 的误挂关系
trace_llm_calls、实际使用的 trace_tool_calls、trace_final_answers 均存在
Run completed/failed 与 Hermes 原结果一致
```

对失败 Turn 还应验证：failed Run、适用的 LLM/Tool error Span、online eval job 终态，以及匹配
Sampling Rule 时失败 Run 自动进入人工标注队列。

### 11.3 性能与可靠性

- disabled、SDK 未安装、后端不可达三种场景均不改变 mock provider 下的结果和异常；
- 日常 Run finish/fail 不触发同步 `force_flush()`；
- SDK 队列/Runtime 背压不能阻塞模型请求、工具执行或结果返回；
- 对真实模型记录开启观测后的 P95 用户 Turn 延迟增量；
- 常驻 30 分钟保持 heartbeat，后台资源无泄漏，优雅退出在限定时间内完成 bounded flush。

## 12. 分阶段实施

### Phase 0：协议与调用图

- 确认 SDK 暴露框架中性 Trace Session（或从现有 `AdapterSession` 提取而来）；
- 增加 `flush_on_run_finish=False`、best-effort / NoOp 行为并以测试固化；
- 固定 Hermes fork，绘制 conversation loop、LLM transport、Tool dispatch、异常和最终回复调用图；
- 仅做读取和验证，不改 Hermes 执行逻辑。

### Phase 1：最小原生链路

- 新增 `hermes_agentscope` bridge、配置和并发安全 registry；
- 在真实用户入口创建 Run + Turn，在 LLM/Tool/Final Answer/异常点调用 SDK；
- 用显式 `parent_context` 验证 Root → Turn → child；
- 验证 disabled、SDK 缺失和后端不可达的 fail-open 行为。

### Phase 2：长期可用与评测闭环

- Agent 注册、心跳、Run 终态与在线评测完整 E2E；
- Dashboard/Gateway/CLI 实测，多 session / 并发 Tool / retry 回归；
- 以实际 Run 校验 Sampling Rule、online evaluation 与失败回流人工标注。

### Phase 3：扩展语义

- Memory、Skill、Approval；
- 内容采集和脱敏策略；
- 子 Agent 的独立 Run 与 trace link / parent-child 关系；
- reasoning 仅在稳定真实执行点存在且显式开启后评估。

## 13. 回滚与风险

立即回滚仅需移除 Hermes fork 的原生埋点提交或设置：

```bash
export HERMES_AGENTSCOPE_ENABLED=false
```

主要风险与约束：

- **SDK 抽象尚偏 Adapter 化**：先完成最小公共 Trace Session，避免 Hermes 调用私有字段。
- **同步初始化/flush 干扰长驻服务**：初始化必须有界、失败返回 NoOp；Run 结束默认异步导出。
- **Turn 边界理解错误**：由 Hermes loop 显式上报；LLM retry 不自动新建 Turn。
- **并发 context 串线**：禁止全局 Run，所有 Span 用 session-local root/turn context。
- **协议漂移**：复用 `tracing_contract.py`；固定值变更要同步 SDK、ingest 与契约测试。
- **fork 与上游分叉**：固定 commit、小而清晰的 bridge 写集、每次升级回放调用图和 E2E。

## 14. 后续上游升级与平台验收检查清单

- [ ] Hermes fork 从固定 commit 创建，工作目录不是 `.tmp/` 缓存；
- [ ] SDK 的框架中性 session、NoOp/best-effort 和 non-blocking finish 设计已评审；
- [ ] `TURN` 与显式 parent-context 协议已在 SDK/ingest 测试中稳定；
- [ ] 已确认真实 conversation loop、LLM、Tool、final/error 位置；
- [ ] bridge 默认关闭，SDK 为可选依赖；
- [ ] 未启用 / SDK 缺失 / 后端不可达测试先于功能 E2E；
- [ ] 凭据、完整 Prompt、工具结果和 reasoning 不进入源码、日志或结果文件；
- [ ] 只在最小链路验收后再扩展 Memory、Skill、Approval 和子 Agent。
