# OpenHands 原生接入 AgentScope

> **接入决策**：OpenHands 仅使用受控 fork 中的原生 AgentScope SDK bridge 接入。`OpenHandsAdapter`
> 及其基于 `Conversation(callbacks=..., token_callbacks=...)` 的 callback 方式已退役并从 SDK、Demo、脚本和
> 测试中删除。OpenHands 的生产与验收路径只能使用 native bridge。

本文定义 OpenHands 的唯一接入方式，并以仓库内 `integrations/hermes-agent/` 的 Hermes 原生 fork 为参考架构。
截至 **2026-07-30**，受控 fork、可选 bridge、Run/Turn、LLM、Tool 和收尾边界已纳入源码；真实模型的完整平台
E2E 仍须在配置凭据和平台服务后执行。

## 项目级无感使用方式

最终交付不是一个需要业务代码包裹的 SDK 示例，而是一个安装到项目依赖中的受控 OpenHands 分发包。业务项目继续
使用原来的 OpenHands 写法、启动命令和部署模型：HTTP server、CLI、ACP、后台 worker、Agent、Conversation 与
workspace 均无需因为 AgentScope 改造。业务代码**不得**导入 `agentscope_sdk`、注册 callback、创建 bridge，或
显式调用 Run/Turn 生命周期。

项目只在部署层提供配置；以下变量为拟定接口，Phase 0 冻结后才成为正式契约：

```bash
OPENHANDS_AGENTSCOPE_ENABLED=true
AGENTSCOPE_SERVER_URL=https://agentscope.example
AGENTSCOPE_PROJECT_TOKEN=...             # Secret/环境注入，不进入源码
AGENTSCOPE_ENVIRONMENT=prod
OPENHANDS_AGENTSCOPE_CAPTURE_LEVEL=L0    # 默认 L0
```

启用后，bridge 在第一个真实用户任务进入 `Conversation.run()` / `arun()` 时自动初始化。AgentScope Runtime 在进程内
只初始化一次；每条用户任务创建独立的 `RuntimeTraceSession`。未启用、SDK 缺失、配置无效或平台不可达时，bridge
退化为 NoOp：项目应以原命令正常启动和运行，观测失败不能影响响应、工具、重试、取消或异常语义。

## Benchmark 关联

Benchmark worker 可以在启动受控 OpenHands 进程时传入下列可选环境变量：

```bash
AGENTSCOPE_EXPERIMENT_ID=...
AGENTSCOPE_EVAL_JOB_ID=...
AGENTSCOPE_EVAL_TRIAL_ID=...
AGENTSCOPE_BENCHMARK_KEY=tau2-bench
AGENTSCOPE_BENCHMARK_TASK_ID=retail-001
AGENTSCOPE_TRIAL_INDEX=0
```

所有变量均为可选。bridge 只读取非空、最长 512 字符的值；`AGENTSCOPE_TRIAL_INDEX` 必须为不超过 JavaScript 安全整数
上限的非负整数。缺失或无效的值会被忽略，且不会改变普通 OpenHands Run 的 Runtime metadata、Trace 或默认 Snapshot。

当至少一个有效值存在且原生 bridge 已启用时，bridge 会使用既有 `RuntimeTraceSession` 协议将上下文写入：

- Runtime Run metadata：`experiment_id`、`eval_job_id`、`eval_trial_id`、`benchmark_key`、`benchmark_task_id`、`trial_index`；
- `agent.run` root span：对应的 `agentscope.experiment.id`、`agentscope.eval.job.id`、`agentscope.eval.trial.id`、
  `agentscope.benchmark.key`、`agentscope.benchmark.task.id`、`agentscope.trial.index`；
- Run Snapshot 的 `benchmark` extra。

这些值应是 Worker 生成的受控、不含用户内容的 opaque identifier。L0 继续只保存输入哈希和内容摘要，不将 Prompt、工具参数、
工具结果或最终回复写入此 benchmark context；bridge 保持 lazy import、fail-open，且 Run 完成时不进行同步 flush。

## 当前 native Demo

在平台和模型凭据准备好后，使用：

```bash
make demo-openhands-native-agentops
```

该命令从 `integrations/openhands-agent/` 安装受控 OpenHands fork，示例仅以原生 OpenHands API 创建
`LLM`、`Agent` 和 `Conversation`，再执行 `send_message()` 与 `run()`；它不导入 `agentscope_sdk`、不注册
callback，也不显式调用 Run/Turn 生命周期。部署到真实项目时应通过私有 wheel、私有索引或基础镜像交付同一 fork，
并由环境变量/Secret 提供 AgentScope 配置。

## 固定基线与 fork 管理

- 官方仓库：`https://github.com/OpenHands/software-agent-sdk`
- 固定上游提交：`a375c66aaf29c8d3b6553c9164e65ed917149e81`
- 对应 SDK 版本：`1.37.0`
- 目标受控目录：`integrations/openhands-agent/`

受控 fork 必须在仓库中记录官方 URL、上游 base commit、fork commit、AgentScope 修改文件清单、SDK 版本、同步方式和
回归命令。`.external/openhands` 只能作为只读上游调研工作区，不能承载原生埋点、Demo 或验收。

## 原生 bridge 的边界

OpenHands fork 内提供一个薄的 `openhands_agentscope` bridge。它只调用公开的
`agentscope_sdk.RuntimeTraceSession` API，不拥有或改变 OpenHands 的调度、消息状态、LLM transport 和工具执行；
也不维护第二套 Runtime client、OTLP exporter、队列或 Trace 协议。

bridge 必须满足：

- 默认关闭、lazy import、显式 opt-in；
- fail-open：任何观测异常不传播到 OpenHands 主执行链，不改变返回值或异常语义；
- 每个并发 Conversation/任务拥有独立 session，不依赖全局或 thread-local 当前 Run；
- Run 结束默认不做同步 flush；
- 只使用显式 `parent_context` 建树；
- 对一个 OpenHands Run，native bridge 是唯一事件 owner；不得注册 callback Adapter 作为第二数据源。

## Run 与 Trace 契约

一个长寿命 `Conversation` 可处理多个用户任务，因此一个用户任务对应一个 AgentScope Run，而不是整个
Conversation。建议 session key：

```text
conversation_id + 当前用户任务/run 序号
```

```text
agent.run
└── agent.turn                         # 一次 Agent decision loop，而非一次 LLM 请求
    ├── llm.call                        # 可多个：sync / async / streaming / Responses API
    ├── tool.call                       # 可多个：并行、重试和恢复失败
    ├── thought / skill / memory / retrieval / guardrail / internal event
    └── agent.answer                    # 最终回答
```

Turn、LLM、Tool、Final Answer 都必须有唯一的真实执行点。Tool 必须显式挂在活动 Turn 下，不能因隐式
OpenTelemetry Context 误成为 LLM child。

## 源码级埋点边界

固定基线的候选真实边界为：

| 运行语义 | OpenHands 源码边界 | AgentScope 记录 |
| --- | --- | --- |
| Run | `LocalConversation.run()` / `arun()` | start/finish/fail Run、用户消息、状态变化 |
| Turn | `Agent.step()` | start/finish Turn、决策结果、重试和终止原因 |
| LLM | `LLM.completion()` / `acompletion()` / `responses()` / `aresponses()` | 请求、流式 TTFT、Token、缓存、成本、finish reason、异常 |
| Tool | `Agent._execute_actions()` / `_aexecute_actions()` | Tool start/finish/error、并发 worker、退出码、风险与工作区影响 |
| 收尾 | Finish/final response/cancellation/stuck/budget/error 分支 | Final Answer、Run/Turn 最终状态与错误链摘要 |

实现时必须在当前固定 fork 中重新冻结调用图；不能用公开 callback 事件事后推断这些边界。

## 观测内容与采集等级

### 内容采集等级

- **L0（默认）**：元数据、摘要、哈希、Token、耗时、成本；不保存原始敏感内容。
- **L1（显式授权）**：限长、脱敏后的输入输出、Prompt、工具参数与结果。
- **L2（可见 reasoning）**：仅保存 OpenHands 已向用户可见的 reasoning；严禁采集隐藏 CoT、
  `encrypted_content` 或凭据原文。

### 必采语义

| 类别 | 必采内容 |
| --- | --- |
| Run / Turn | run、conversation、turn 标识；状态历史；取消、stuck、预算/迭代上限、错误和恢复 |
| 消息与上下文 | 用户/Agent 消息摘要；system prompt、动态上下文、工具与 skill schema 指纹；上下文压缩与预算 |
| LLM | provider、model、请求模式、TTFT、时长、输入/输出/缓存 Token、`actual`/`estimated` 成本、重试和异常 |
| Tool / Workspace | tool call ID、参数/结果摘要、成功状态、exit code、并发 worker、风险、文件和终端影响摘要 |
| Agent 语义 | thought、可见 reasoning、skill、memory、retrieval、condensation、guardrail、approval/user reject |
| SDK 健康 | 采样、队列深度、背压、丢弃、导出重试、导出失败 |

所有 Span 至少带 `run_id`、`trace_id`、`framework=openhands` 和 `integration=native`。高基数字段只作为
Trace 属性或脱敏摘要，不能作为 metrics label；成本必须注明真实账单或估算来源。

## Exporter 与平台投影

OpenHands 检测到 `LMNR_PROJECT_API_KEY`、`OTEL_ENDPOINT`、`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` 或
`OTEL_EXPORTER_OTLP_ENDPOINT` 时，可能启用内置 Laminar/OpenTelemetry 路径。原生 bridge 复用 AgentScope SDK
Runtime/exporter，并必须在 fork 中明确禁用或隔离会写入同一 AgentScope Run 的内置 exporter，以避免双 root、
重复 Span 和成本翻倍。

原始 Span 写入 `trace_spans`；LLM、Tool、Thought、Skill 和 Final Answer 写入既有结构化投影。Memory、Retrieval、
Guardrail 至少保留稳定、可检索的语义 Span。SSE 与 Snapshot 必须遵循同一内容采集等级，且 Run 完成前可见非终态
状态与语义事件。

## SDK 前置接口

当前原生 bridge 不能访问 `AdapterSession` 私有字段，也不能复制历史 Adapter 的 span 构造逻辑。实施前应在
`RuntimeTraceSession` 提供公开、fail-open 的等价接口：

```python
session.record_thought(...)
session.record_skill(...)
session.record_memory(...)
session.record_guardrail(...)
session.record_internal_event(...)
```

或提供受 `SemanticKind` 限制的通用 `record_semantic_event()`。

## 验收与退役条件

原生 E2E 必须使用受控 fork 和真实 LLM、`TerminalTool`、`FileEditorTool` 覆盖直接回答、正常工具、并发工具、
可恢复失败、文件验证、最终中文回答、取消、预算限制和异常路径。验收应交叉检查 API、PostgreSQL、ClickHouse、
Redis Stream、SSE、Snapshot、Trace 树及语义投影。

完成原生 E2E 后，删除以下历史 Adapter 资产：

- 已删除 `OpenHandsAdapter` 公开导出和实现；
- 已删除 Adapter 单元测试；
- 已删除 `make demo-openhands-agentops` 与其脚本；
- 已删除仅为该 Demo 保留的 `.external/openhands` 安装和校验逻辑；
- 已将活跃文档改为 native-only，并以 `make demo-openhands-native-agentops` 作为 native Demo 入口。

详细分阶段计划见
`docs/superpowers/plans/2026-07-30-openhands-native-sdk-integration.md`。
