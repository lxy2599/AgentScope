# OpenHands 原生 AgentScope SDK 接入实施计划

- **状态**：实施中；已纳入受控 fork、native bridge 和核心生命周期埋点，待完成真实平台 E2E
- **日期**：2026-07-30
- **目标**：以 `integrations/hermes-agent/` 的原生 bridge 为参考，在受控 OpenHands fork 的真实执行点直接调用 AgentScope Python SDK；交付为项目级无感能力——业务项目按原有方式启动和运行 OpenHands，不包装 `Conversation`、不注册 callback、不显式创建/结束 Run；OpenHands 后续只保留原生接入，不再提供或维护 `OpenHandsAdapter` 路径。
- **当前上游基线**：OpenHands Software Agent SDK `a375c66aaf29c8d3b6553c9164e65ed917149e81`（SDK `1.37.0`）
- **不属于本计划**：直接修改当前 `.external/openhands` Demo 工作区、维持或扩展 OpenHands Adapter、修改 Go 后端/ClickHouse 表结构、将 OpenHands 运行时托管到 AgentScope 服务端。

## 1. 背景与决策

当前仓库中仍存在历史外置 `OpenHandsAdapter`：它通过 `Conversation(callbacks=..., token_callbacks=...)`
将公开事件映射为 AgentScope Run、Turn、LLM、Tool、Thought 和 Final Answer。该实现只作为迁移前的历史代码，
不再作为 OpenHands 的集成、Demo 或回归入口，也不得继续扩展。

callback 只能在事件产生后推断执行边界，无法成为原生生产接入的事件源。原生路径应像已纳入
`integrations/hermes-agent/` 的 Hermes fork 一样，在真实的 conversation、LLM transport 和 Tool dispatch
边界直接调用 `RuntimeTraceSession`。这样可以保证：

- 一个真实用户任务对应一个 Run，而不是整个长寿命 Conversation；
- 一个 Agent decision loop 对应一个 Turn，而不是一次 LLM 请求；
- LLM、Tool 和 Final Answer 显式挂在活动 Turn 下；
- 并发工具和异步任务不依赖隐式 OpenTelemetry current context；
- OpenHands/AgentScope 任一观测失败均不影响用户任务、工具调度或回答。

**决策**：建立受控、可回放的 OpenHands fork，在源码中增加薄 bridge，并将历史 `OpenHandsAdapter`
从 OpenHands 的运行、Demo、测试和公开导出中退役。原生路径必须复用 AgentScope SDK 的 Runtime、OTLP exporter、
脱敏、背压和 `tracing_contract.py`，不得维护第二套 client、队列或 span 协议。一个 OpenHands Run 只有原生
bridge 一个事件 owner。

## 2. 受控源码与发布策略

### 2.1 Fork 承载

实施前创建并纳入受控 OpenHands 源码目录，建议目标路径：

```text
integrations/openhands-agent/
```

该目录应基于固定官方提交 `a375c66aaf29c8d3b6553c9164e65ed917149e81`，记录：

- 官方仓库 URL、上游基线 commit、fork commit；
- AgentScope 修改文件清单；
- SDK 版本/commit；
- 源码同步方式和回归命令。

不得在 `.external/openhands` 中开发原生埋点；该目录不再作为 OpenHands 集成 Demo 或验收入口，并应在
Adapter 退役阶段随其专用脚本、测试与文档一并移除。官方上游调研工作区必须保持干净。

### 2.2 Bridge 包位置（Phase 0 决策项）

优先把 bridge 放在 OpenHands SDK 可选安装边界，避免无 AgentScope 配置的普通用户导入 SDK 时产生依赖或
网络行为。候选方案：

1. `openhands-sdk/openhands/sdk/agentscope/`：随 SDK 安装，模块路径稳定；
2. 独立 `openhands_agentscope/` 包：与 Hermes 的 `hermes_agentscope/` 同构，但需要确认 OpenHands
   monorepo 的打包、editable install 和发布流程；
3. OpenHands 现有 observability 子包中的可选 bridge：仅在能明确隔离 Laminar 内置 exporter 时采用。

Phase 0 必须选择一个方案，并以以下标准裁决：默认不 import `agentscope_sdk`、不会改变 OpenHands 默认
observability 行为、可在 fork 测试中单独 import、更新时修改范围小且不依赖私有 AgentScope API。

## 3. 目标运行模型

### 3.1 Run、Conversation 与并发隔离

OpenHands `Conversation` 可以跨多个用户任务长期存在，因此不能把一个 Conversation 生命周期等同于一个
AgentScope Run。原生 bridge 要以稳定的 key 管理 session：

```text
session key = conversation_id + 当前用户任务/run 序号
```

- 用户消息被消费并开始本次 `Conversation.run()` / `Conversation.arun()` 时创建 Run；
- 同一 Conversation 的下一条用户任务创建新的 Run 和新的 `RuntimeTraceSession`；
- 多个 Conversation、并发 `arun()`、并行 Tool worker 必须各自持有独立 session；
- 不允许 thread-local 或全局“当前 Run”作为跨任务关联手段。

### 3.2 项目级无感运行模型

原生接入的交付单位是**整个 OpenHands 项目/进程**，不是一段被 SDK 包裹的 demo 代码。业务方安装受控
OpenHands 分发包后，继续按原有方式构建 Agent、Conversation、workspace、server 或 CLI，并照常调用
`send_message()`、`run()` / `arun()`；不得要求业务代码导入 AgentScope、创建 `RuntimeTraceSession`、包裹
`Conversation`、调用 `start_run()` / `finish_run()`，或注册 callback/token callback。

bridge 在 OpenHands 进程启动后保持惰性：只有第一个真实用户任务进入 `run()` / `arun()` 时才按配置初始化
AgentScope Runtime、OTLP exporter 和该任务的 session。Runtime 是**每个进程一次**的共享基础设施；
`RuntimeTraceSession` 是**每个用户任务一个**的隔离对象。业务项目可以同时拥有 HTTP server、CLI、ACP、
后台 worker、多 Conversation 和 sync/async 调用，原生 bridge 均应在真实执行点自动生效。

项目配置应仅来自部署配置（环境变量、容器/Helm Secret 或 OpenHands 正式配置文件），建议：

```text
OPENHANDS_AGENTSCOPE_ENABLED=true|false       # 默认 false
AGENTSCOPE_SERVER_URL=...
AGENTSCOPE_PROJECT_TOKEN=...                  # 仅 Secret 注入，绝不写入项目源码
AGENTSCOPE_ENVIRONMENT=dev|staging|prod
OPENHANDS_AGENTSCOPE_CAPTURE_LEVEL=L0|L1|L2   # 默认 L0
```

未启用、依赖未安装、凭据缺失、AgentScope 不可达或 SDK 初始化失败时，项目必须与未集成版本等价运行：不要求
修改启动命令、不阻塞启动、不改变 OpenHands 返回值/异常/重试/并发语义，只丢失本次观测并记录安全的 debug 信息。

### 3.3 Trace 树

```text
agent.run
└── agent.turn                         # 一次 Agent decision loop
    ├── llm.call                        # 可多个；同步、异步、streaming 或 Responses API
    ├── tool.call                       # 可多个、可并行、可重试
    ├── thought / skill / internal event
    └── agent.answer                    # 最终 Turn
```

所有 child span 都使用活动 Turn 的显式 `parent_context`。Turn 关闭时必须关闭属于该 Turn 的未完成 LLM/Tool
span；Run 失败、取消、stuck、预算/迭代上限或未恢复异常时同样必须关闭 Run，且保持 OpenHands 原始异常语义。

## 4. 观测范围与数据契约

原生接入不应只采集“模型调用 + 工具调用”。它需要同时回答：**任务为何开始、Agent 如何做出决策、
调用了什么、改变了什么、是否受到安全约束、为何结束、数据是否完整以及采集自身是否健康**。以下内容是
native bridge 的最低观测契约；所有动态值必须经 SDK 脱敏、截断和采样策略处理。

### 4.1 内容采集等级

| 等级 | 默认 | 可记录内容 | 禁止记录内容 |
| --- | --- | --- | --- |
| L0：元数据与摘要 | 是 | ID、状态、模型/工具名称、长度、哈希、Token、耗时、成本、错误类型 | 完整 prompt、完整响应、文件正文、命令输出、密钥、Cookie、加密 reasoning |
| L1：经授权的业务内容 | 否 | 已脱敏并限长的用户输入、模型输出、工具参数/结果、文件 diff 摘要 | API Key、Project Token、密码、Cookie、未脱敏 PII、二进制文件内容 |
| L2：可见 reasoning | 否 | 模型公开返回的可见 reasoning / thought，带 `capture_reasoning=true` | Responses API 的 `encrypted_content`、隐藏 reasoning、供应商内部 chain-of-thought |

Snapshot、SSE、Runtime API、OTLP 和本地结果文件必须使用同一采集等级；不得出现“Span 脱敏但 Snapshot
保留原文”或“关闭内容采集但 debug 日志输出完整工具参数”的旁路。

### 4.2 必采观测矩阵

| 领域 | 记录内容 | 语义 Span / 事件 | 关键属性与指标 | 触发边界 |
| --- | --- | --- | --- | --- |
| Run 身份与来源 | agent、fork/SDK 版本、环境、conversation/task、配置版本和 prompt/tool schema 指纹 | `agent.run` | `run_id`、`trace_id`、agent instance、framework、integration、conversation ID、task ID、上游/fork commit、配置哈希 | 用户任务开始 |
| Run 状态机 | running、thinking、waiting_user、retrying、completed、failed、cancelled，以及状态原因 | Runtime status + `INTERNAL` event | 状态、原因码、时间戳、重试/迭代计数、预算/上限命中标志 | Conversation 状态变化、暂停、取消、异常 |
| Turn 决策 | Turn 序号、开始/结束、outcome、LLM/Tool 数量、是否恢复 | `agent.turn` | turn ID/index、outcome、LLM/Tool count、iteration、retry attempt、termination reason | 每次 `Agent.step()` |
| 用户/Agent 消息 | 消息 role、sender、长度、摘要/哈希、可选捕获内容 | `INTERNAL` event；最终消息另记 Final Answer | message ID、role、sender、content length、SHA-256、captured 标志 | 用户消息消费、Agent message 产生 |
| System Prompt / 工具清单 | prompt、dynamic context、工具 schema 与可用 skill 的指纹 | `INTERNAL` event | SHA-256、工具名/数量、skill 名、context token 估计、captured 标志 | Conversation 初始化、工具集变化 |
| LLM 请求 | Chat / Responses 路径、provider、model、request/response ID、finish reason、重试 | `llm.call` | call ID、provider/model、API 类型、stream、attempt、duration、TTFT、input/output/total/cache token、成本与来源、错误分类 | 四个 LLM transport 入口 |
| 流式输出 | 首 token、可见 token 数、流完成/中断、partial output | LLM 属性 + 低频 `INTERNAL` progress event | TTFT、first-token flag、chunk count、finish/cancel reason | 首可见 token、流结束 |
| Thought / reasoning | 工具规划、可见 reasoning、压缩摘要 | `THOUGHT` | stage、content length/hash、capture 标志、来源（Chat/Responses/Condenser） | 可见 thought/reasoning 或 condensation |
| Tool 执行 | tool name/type、tool call ID、参数/结果摘要、退出码、side-effect risk、重试、并发 worker | `tool.call` | tool ID、name/type、arguments/result hash、duration、success、error、exit code、risk、worker index | 实际 action dispatch 与 Observation 返回 |
| Workspace 影响 | 文件创建/修改/删除摘要、命令/终端影响、工作区相对路径 | Tool 属性或 `INTERNAL` artifact event | operation、路径哈希/脱敏路径、diff size、artifact count、command exit code | File/Terminal tool 完成 |
| Memory / retrieval / skill | memory read/write、检索 query/result count、skill load/activate/call | `MEMORY`、`RETRIEVER`、`SKILL` | operation、目标/索引、结果数、skill 名/版本、延迟、脱敏摘要 | 对应真实组件边界；不可仅从 callback 猜测 |
| 安全与确认 | security risk、approval request/allow/deny/timeout、用户拒绝、策略命中 | `GUARDRAIL` | risk level、policy/confirmation ID、decision、reason code、等待时长 | action 授权、拒绝、确认超时 |
| 上下文治理 | context compression、token 预算、stuck detector、缓存命中、上下文窗口 | `THOUGHT` / `INTERNAL` | before/after token、summary hash、compression reason、budget、cache token、stuck rule | Condensation、预算检查、stuck 判定 |
| 失败与恢复 | provider/工具/会话错误、retry、fallback、异常链摘要 | 当前 LLM/Tool span + Run/Turn 错误 | error type/code、recoverable、attempt、fallback path、原始异常类（非原文） | catch/retry/final failure 边界 |
| SDK 采集健康 | queue depth、dropped spans、export retry、export failure、flush 行为 | `SDK_EXPORT_METRICS` | queue/dropped/retry/export latency、sampling decision | SDK 指标周期与 Run 收尾 |

### 4.3 属性、关联和低基数规则

1. 每个原生 Span 至少携带 `agentscope.run.id`、`agentscope.trace.id`、`framework=openhands`、
   `integration=native`、agent identity 和 fork provenance；Turn child 还必须携带 `agentscope.turn.id`、
   `agentscope.turn.index` 与显式 outcome。
2. 低基数资源属性仅包含 environment、framework、agent version、OpenHands upstream/fork commit、
   provider、model、工具/skill 名称等。conversation ID、task ID、tool call ID、request ID、路径、错误摘要
   和用户内容只能作为单次 span/event 属性，不能作为 metrics label。
3. request ID 与 tool call ID 只用于 lifecycle 配对与查询；它们不能取代 `parent_context`、Turn 或 Run。
4. 需要跨系统关联时记录摘要/哈希和安全 ID，不复制凭据。例如记录 `prompt.sha256`、`tool.args.sha256`、
   `workspace.relative_path` 的脱敏形式，而非原始秘密。
5. 所有 duration 使用真实单调时钟；成本必须标记 `actual` 或 `estimated` 来源，估算单价和供应商账单不得混淆。

### 4.4 SDK 公共接口前置项

当前 `RuntimeTraceSession` 已公开 Run、Turn、LLM、Tool、Final Answer 和状态更新接口；完整 OpenHands
原生观测还需要一个**公开且 fail-open** 的语义事件表面。Phase 1 需要先选择并实现以下其中一种方式：

```python
session.record_thought(stage, content, capture_content=False, attributes=...)
session.record_skill(name, action, arguments=..., result=..., attributes=...)
session.record_memory(operation, target, arguments=..., result=..., attributes=...)
session.record_guardrail(name, decision, attributes=...)
session.record_internal_event(name, input_summary=..., output_summary=..., attributes=...)
```

或提供等价的、受 `SemanticKind` 约束的通用 `record_semantic_event()`。OpenHands bridge 不得导入
`AdapterSession` 私有字段、调用以下划线开头的 SDK 方法，或复制 Adapter 的 span 构造逻辑。

### 4.5 存储、实时与验收投影

- 所有原始 Span 写入 `trace_spans`；LLM、Tool、Thought、Skill、Final Answer 等由现有 ingest 投影写入
  对应结构化语义表；Memory/Retrieval/Guardrail 至少保留可查询的语义 Span 和稳定属性。
- PostgreSQL Run、状态历史、Snapshot、Sampling Rule、online-eval job 和 annotation item 必须通过同一
  `run_id` / `trace_id` 关联；Snapshot 只保存 L0/L1 允许内容。
- SSE 在 Run 完成前至少发送一个 non-terminal 状态事件和一个语义事件，但不得发送 L1/L2 未授权内容。
- 平台查询必须能回答：某次失败在哪个 Turn、哪个模型/工具、哪个 retry、哪个安全决策和哪个上下文限制处发生；
  同时能解释是否存在 SDK 丢弃或 exporter 失败。

## 5. OpenHands 源码级埋点点

以下是针对固定 SDK 提交的初步调用图；实施前必须重新用真实源码与测试确认行级位置和异常分支。

| 生命周期 | 候选真实边界 | 原生 bridge 调用 | 关键约束 |
| --- | --- | --- | --- |
| 用户任务开始 | `LocalConversation.send_message()` 后由 `run()` / `arun()` 消费用户消息 | `start_run()` | 只为本次用户任务创建一次；不对 agent/system/tool 消息重复创建 |
| Decision Turn | `LocalConversation.run()` / `arun()` 中调用 `agent.step()` 的每次迭代 | `start_turn()` / `finish_turn()` | Turn 不等于 LLM；工具、重试、并行 worker 仍归属此 Turn |
| Chat LLM | `LLM.completion()` / `LLM.acompletion()` | `start_llm()` / `finish_llm()` | 覆盖普通和 streaming 路径，记录模型、用量、真实耗时、异常 |
| Responses LLM | `LLM.responses()` / `LLM.aresponses()` | `start_llm()` / `finish_llm()` | 保持与 Chat 路径相同 call ID、Token、成本和错误语义 |
| 首 Token | 流事件处理（例如 `_process_stream_event()` 的第一个可见 token） | 更新 TTFT | 仅记录一次；不采集被加密或按策略禁止的 reasoning 内容 |
| Tool | `Agent._execute_actions()` / `_aexecute_actions()` 及实际 action 执行边界 | `start_tool()` / `finish_tool()` | 以 `tool_call_id` 配对；并发 Tool 显式传递 session/turn 上下文 |
| 最终回答 | Finish action / final agent response 确认处 | `record_final_answer()` + `finish_run()` | Final Answer 必须属于最终 Turn；不要把普通 agent Message 提前当作最终答案 |
| 错误与取消 | Conversation error、interrupt/pause、stuck、预算/迭代限制、未恢复异常 | `finish_turn(error)` + `fail_run()` | 观测调用 fail-open；不得吞掉或替换 OpenHands 的原始异常 |

除 LLM 和 Tool 外，System Prompt、Skill、上下文压缩和状态变化可按内容采集策略记录为 SDK 语义事件；它们不是
启动或结束 Run/Turn 的依据。

## 6. Bridge 接口与配置

### 6.1 最小 bridge 接口

接口应与 `integrations/hermes-agent/hermes_agentscope/bridge.py` 对齐，但使用 OpenHands 的会话标识：

```python
bridge.on_user_task_started(session_key, input_text, conversation_id=..., task_id=...)
bridge.on_decision_turn_started(session_key, turn_id=..., turn_index=...)
bridge.on_llm_started(session_key, name, call_id=..., ...)
bridge.on_llm_finished(session_key, name, call_id=..., ...)
bridge.on_tool_started(session_key, name, arguments, tool_id=..., ...)
bridge.on_tool_finished(session_key, name, result, tool_id=..., ...)
bridge.record_thought(session_key, stage, content, ...)
bridge.record_skill(session_key, name, action, ...)
bridge.record_memory(session_key, operation, target, ...)
bridge.record_guardrail(session_key, name, decision, ...)
bridge.record_internal_event(session_key, name, ...)
bridge.on_final_answer(session_key, output, ...)
bridge.on_run_failed(session_key, error, ...)
```

bridge 内部只使用公开的 `agentscope_sdk.RuntimeTraceSession`、`configure_tracing()`、`init()` 和
`get_runtime()`。SDK import 必须 lazy；初始化、注册、心跳、OTLP 或 Runtime 失败必须返回同形 NoOp session。

### 6.2 配置与隐私

首期使用显式 opt-in，默认关闭。具体配置名在 Phase 0 与 OpenHands 配置体系对齐后确定；语义上至少包括：

- 启用开关；
- AgentScope server URL、Project Token、environment；
- 内容采集和 reasoning 采集开关（均默认关闭）。

Project Token、API Key、完整 prompt、未脱敏工具参数/结果、加密 reasoning、Cookie 和环境变量不得进入日志、
Run Snapshot、SSE payload、结果文件或 bridge `repr`。所有 span 属性继续通过 SDK 脱敏和长度限制。

### 6.3 与 OpenHands 内置 observability 的边界

固定版本 OpenHands 在 `LMNR_PROJECT_API_KEY`、`OTEL_ENDPOINT`、
`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` 或 `OTEL_EXPORTER_OTLP_ENDPOINT` 存在时可启用 Laminar/OTEL。
原生 AgentScope bridge 必须在 Phase 0 明确采取以下之一：

1. 禁止该进程同时启用 OpenHands Laminar exporter；或
2. 明确隔离为独立 trace/backend，绝不把它与 AgentScope Run 混用。

不得让两个 exporter 向同一 Run 产生两个 root span 或重复 LLM/Tool span。

## 7. 分阶段实施计划

### Phase 0：冻结调用图与 ADR

1. 固定官方 OpenHands commit，创建受控 fork 与 provenance 文档；
2. 逐行确认 `LocalConversation.run/arun`、`Agent.step`、LLM 四种调用入口、同步/异步 Tool、Finish、错误、取消和并行 worker 的调用图；
3. 决定 bridge 包位置、配置来源、session key 和内置 Laminar exporter 的互斥策略；
4. 写 ADR/设计文档，列出每个源码写点、预期 Span 树、异常责任边界、项目级无感启动方式和不变量；
5. 决定受控分发方式（fork wheel / 私有索引 / 镜像）、可选依赖 extras 与环境变量/正式配置文件入口；
6. 在未启用状态运行完整 OpenHands 项目回归，证明不 import AgentScope SDK 且启动、HTTP/CLI/worker 行为不变。

**完成条件**：没有模糊的“在 callback 中补一个 span”；每一个 Run/Turn/LLM/Tool/Final Answer 都有唯一真实调用点和唯一 owner。

### Phase 1：SDK 与 bridge 骨架

1. 确认并补齐 `RuntimeTraceSession` 的 native 需求：显式 parent context、Run/Turn 关闭、NoOp、异步导出、`flush_on_run_finish=False`，以及公开的 Thought/Skill/Memory/Guardrail/Internal 语义事件接口；
2. 新增 OpenHands bridge、config、session registry 和 NoOp；
3. 单元测试默认关闭、lazy import、缺 Token/URL、SDK 初始化失败、并发 session 隔离、token 不出现在 `repr`，以及 L0/L1/L2 采集等级；
4. 不接入任何 OpenHands 执行点前，先验证 bridge 独立测试和结构化语义事件投影。

**完成条件**：bridge 可被正常 OpenHands 安装忽略；启用失败不会改变任何业务返回值或异常。

### Phase 2：Run 与 Turn 原生接入

1. 在用户任务被 `run/arun` 消费时创建 Run；
2. 在每次 `agent.step()` 周围建立/关闭 Turn；
3. 为直接回答、单工具、多工具、工具失败后恢复、连续多个用户任务、取消和迭代上限编写源码级回归；
4. 验证原生 bridge 是唯一事件 owner，并能产生正确的 Run/Turn 树。

**完成条件**：一个 Conversation 多任务不会复用 Run；一个 Turn 不会被错误切成多条 Run 或挂到上一任务。

### Phase 3：LLM 原生接入

1. 覆盖 `completion/acompletion/responses/aresponses`；
2. 建立稳定 call ID，记录 provider、model、开始/结束时间、usage、actual/estimated cost、错误；
3. 从真实 streaming 首 token 记录 TTFT；
4. 对 retry、超时、取消、空响应、Responses API 和异常路径闭合 open LLM span；
5. 将内容采集、reasoning 和加密 reasoning 的边界纳入测试。

**完成条件**：LLM span 始终属于正确 Turn；TTFT 只写一次；失败不会泄漏 span 或吞掉 OpenHands 异常。

### Phase 4：Tool、并发与最终回答

1. 在实际 action dispatch 边界记录 Tool start/finish/error；
2. 使用公开 `tool_call_id` 进行 start/finish 配对，但以显式 Turn context 决定 parent；
3. 覆盖 `parallel_executor` / 异步工具并发，验证多个 Tool 共享当前 Turn 而不串 Run；
4. 在真正的 Finish/final response 边界记录 Final Answer；
5. 覆盖 Tool 失败后恢复、用户拒绝、pause/interrupt、conversation error、stuck 与预算限制。

**完成条件**：工具不会成为 LLM child；并发工具和迟到 Observation 不会关闭后续 Turn；最终回答只有一个。

### Phase 5：平台 E2E、发布与 Adapter 退役（进行中）

1. 新增 native OpenHands 项目级 E2E，使用受控 fork，而不是 `.external/openhands`；验收应用代码不得包含 AgentScope import、wrapper、callback 或显式 Run 生命周期调用；
2. 用原有启动方式运行真实 OpenHands 项目（HTTP server/CLI 或 worker 之一）和真实 LLM + TerminalTool + FileEditorTool，覆盖正常工具、可恢复失败、文件验证和最终中文回答；
3. 在同一项目中验证多个用户任务、多 Conversation、sync/async 和并发 Tool，证明 Runtime 只初始化一次而每个任务都有独立 Run；
4. 交叉验证 API、PostgreSQL、ClickHouse、Redis Stream、SSE、Snapshot、Trace 树、LLM/Tool/Final Answer 语义表；
5. 验证 Run 完成前已收到非终态 SSE 语义事件；
6. 验证 disabled、缺依赖、缺凭据、后端不可达时项目仍按原命令启动、任务仍返回原结果；
7. 删除 `OpenHandsAdapter` 的公开导出、单元测试、Demo 脚本、Make target 和仅为该 Demo 保留的 `.external/openhands` 安装/校验逻辑；
8. 将 fork 基线、结果文件位置、E2E 命令、升级流程和“项目级无感、仅原生接入”的边界写入 `docs/integrations/openhands.md`。

**完成条件**：只有 native OpenHands 路径拥有可复现验收命令；仓库中没有 OpenHands Adapter 的运行入口、公开 API、Demo 或活跃文档。

## 8. 测试矩阵

| 层级 | 必测场景 |
| --- | --- |
| bridge 单元测试 | disabled、lazy import、NoOp、缺配置、SDK 失败、并发 registry、脱敏、L0/L1/L2 采集等级 |
| 项目级无感运行 | 业务应用零 AgentScope import / wrapper / callback；原启动命令覆盖 HTTP/CLI/worker；Runtime 每进程一次、任务 session 每任务一个；disabled/缺依赖/缺凭据/后端不可达均不影响启动与原业务结果 |
| Run / Conversation 生命周期 | 多用户任务、直接回答、消息角色、状态历史、取消、stuck、预算/迭代上限、conversation error |
| Turn 树 | Run → Turn → LLM/Tool/Answer，Tool 不成为 LLM child，Turn 计数、retry、并发 worker parent 正确 |
| LLM | Chat/Responses、sync/async、streaming TTFT、retry、usage/cache token、actual/estimated cost、finish reason、异常闭合 |
| Tool / Workspace | 单个、批量并发、失败恢复、用户拒绝、迟到结果、Finish action、退出码、文件/终端影响摘要、side-effect risk |
| Agent 语义 | system prompt/tool schema hash、thought、可见 reasoning、skill、memory、retrieval、condensation、guardrail、内部状态事件 |
| 数据策略 | API Key/Token/Cookie/加密 reasoning 永不出现；关闭内容采集时 Snapshot、SSE、Span、日志均无原文 |
| SDK 健康 | 采样、队列、丢弃、export retry/failure、Run 结束不做同步 flush |
| 非侵入性与退役 | 未启用时 OpenHands 原生回归不变；native E2E 覆盖完成后，`OpenHandsAdapter` 的公开导出、Demo、脚本和测试已移除 |
| 平台 E2E | Runtime、OTLP、PostgreSQL、ClickHouse、Redis、SSE、Snapshot、语义表、在线评测与人工回流证据 |

## 9. 风险与控制

| 风险 | 控制措施 |
| --- | --- |
| 长寿命 Conversation 误复用 Run | 以用户任务/run 序号构建 session key；添加连续任务回归 |
| sync/async/Responses 路径漏埋 | 四个 LLM 入口分别测试；公共 helper 只做共享属性整理 |
| 并发工具 Context 丢失 | worker 显式传 session key / Turn context，不依赖 current span |
| 双 exporter / 双 Trace root | 原生 bridge 与 OpenHands Laminar/OTEL exporter 互斥配置和验收；不存在 Adapter 作为第二事件源 |
| 观测影响 OpenHands 主流程 | 每个 bridge 调用独立 try/except；NoOp 同形返回；不修改异常/返回值 |
| 上游升级破坏调用图 | 固定基线；每次升级复核 Phase 0 调用图和完整回归 |
| fork 体积和维护成本 | 记录 provenance、最小写集、同步流程；避免在 bridge 外散布 AgentScope 条件分支 |

## 10. 实施前的决策门

开始改 OpenHands 源码前，必须确认以下问题：

1. 受控 OpenHands fork 的仓库/目录和同步责任人；
2. bridge 包位置与可选依赖安装方式；
3. OpenHands 配置系统中默认关闭的正式配置入口；
4. Laminar/OpenTelemetry 内置 exporter 的互斥或隔离方案；
5. 每个用户任务的稳定 run 序号来源；
6. native 平台 E2E 的模型、工作区、结果文件和数据库验收脚本；
7. 是否同时把 native OpenHands fork 纳入本仓库 `integrations/`，并接受其源码体积与升级成本；
8. 受控 fork 如何以不修改业务代码的方式交付到真实项目（wheel/私有索引/镜像）以及 HTTP/CLI/worker 的项目级 E2E 入口。

在这些决策完成前，不得把 callback 事件映射作为 OpenHands 集成或验收路径；历史 `OpenHandsAdapter` 仅可在
受控退役过程中保留，不得用于新的功能、Demo 或发布。
