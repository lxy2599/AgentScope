# OpenHands 接入 AgentScope

本文定义 OpenHands 接入 AgentScope 的两条路径，并以本机维护的 Hermes 原生 fork 为参考架构。

1. **外置 `OpenHandsAdapter` 兼容路径**：当前已实现，并由真实 Demo/回归覆盖；不修改 OpenHands
   源码，只使用 `Conversation(callbacks=..., token_callbacks=...)` 的公开回调。
2. **OpenHands 原生 fork 路径**：目标生产接入路径；在受控 OpenHands fork 的真实 Conversation、LLM
   transport 与 Tool dispatch 执行点直接调用 AgentScope SDK。该路径尚未在 OpenHands fork 中实现，不能
   把当前 Adapter Demo 表述为原生埋点验收。

Hermes 的原生实现在本机维护 fork `hermes-agent-agentscope` 中：由 `hermes_agentscope/bridge.py` 管理
每个会话的 SDK Trace Session，并由 `agent/conversation_loop.py`、`agent/tool_executor.py` 与
`agent/agent_init.py` 在真实运行边界调用。OpenHands 原生路径应复用这个架构与语义契约，而不是复用
`OpenHandsAdapter` 的 callback 事件作为原生事件源。

## 固定版本

- 官方仓库：`https://github.com/OpenHands/software-agent-sdk`
- 固定提交：`a375c66aaf29c8d3b6553c9164e65ed917149e81`
- 对应 SDK 版本：`1.37.0`
- 外置 Adapter 的本地官方源码：`.external/openhands`

`setup-openhands-agent.sh` 只服务于外置 Adapter Demo：它会验证固定提交与官方上游工作区清洁度。原生
路径应在**单独维护的 OpenHands fork** 中保留可回放的上游基线和清晰的 patch 历史；不把 fork 源码提交到
AgentScope 仓库。

## 统一 Trace 契约

两条路径都必须使用 AgentScope Runtime、OTLP、脱敏、背压和语义 Trace 协议，并表达相同层级：

```text
agent.run
└── agent.turn                         # 一次 decision loop，不等于一次 LLM 请求
    ├── llm.call
    ├── tool.call（可多个、可并行、可恢复失败）
    ├── thought / skill / internal event
    └── agent.answer（最终 Turn）
```

Turn 表示一次 Agent 决策循环，而非一次 LLM 请求；一个 Turn 可包含多次模型调用、多个工具、重试或并行
工具。Tool Span 必须显式挂在活动 Turn 下，不可因为隐式 OpenTelemetry Context 而错误挂在 LLM Span 下。

## OpenHands 原生 fork 路径（目标实现）

原生实现应像 Hermes 一样提供一个薄的 `openhands_agentscope` bridge。bridge 只调用公开的
`agentscope_sdk.RuntimeTraceSession` API；它不拥有 OpenHands 的调度、消息状态、LLM transport 或工具
执行，也不应维护第二套 Runtime client、OTLP exporter 或 Trace 协议。

一个 conversation 对应一个 bridge session；bridge 的 registry 必须以稳定的 conversation/session key 隔离
并发运行。最小埋点顺序如下：

```text
真实用户输入
-> bridge.start_run(...)
-> 真实 Agent decision loop: bridge.start_turn(...)
-> 真实 LLM transport: bridge.start_llm(...) / finish_llm(...)
-> 真实 Tool dispatch: bridge.start_tool(...) / finish_tool(...)
-> bridge.finish_turn(...)
-> 最终回答: record_final_answer(...) + finish_run(...)
   或未恢复异常: fail_run(...)
```

具体修改点应落在固定 OpenHands fork 的以下真实边界，而不是订阅回调后再推断：

- Conversation 接收一条新的用户任务并开始本次 `run()` / `arun()` 时创建 Run；
- Agent 执行一次 decision loop 时创建和关闭 Turn；
- `LLM` 的实际请求/流式响应边界记录模型、请求/响应摘要、TTFT、用量、成本和异常；
- Action 对应的 Tool 真正开始和返回 Observation（含异常）时记录 Tool；
- Finish、conversation error、取消或未恢复异常关闭活动 Turn 和 Run。

与 Hermes 原生 bridge 一致，OpenHands bridge 必须：默认关闭、lazy import、fail-open、观测异常绝不传播到
主执行链、每个并发 conversation 独立 session、默认不在 Run 结束时同步 flush。启用配置和凭据处理也应沿用
Hermes 的显式 opt-in 模式，不能把 token、完整 prompt 或敏感工具参数写入日志、Snapshot 或结果文件。

原生路径完成后，`OpenHandsAdapter` 仍可保留为不改源码的兼容模式；但同一 Run 只能选择一种事件源。原生
bridge 启用时不得同时注册 Adapter 来重复创建 Run、Turn、LLM 或 Tool Span。

## 外置 Adapter 兼容路径（当前已实现）

此路径适用于不能维护 OpenHands fork 的集成方。应用进程初始化 AgentScope Runtime 和 Tracing 后，在**创建**
`Conversation` 时把 Adapter 的两个公开回调列表与宿主已有回调合并。一个 `AdapterSession` 只能服务一个物理
Run；Run 结束后请为下一次 `Conversation.run()` 创建新的 Session 和 Adapter。

```python
from openhands.sdk import Conversation

from agentscope_sdk.adapters import (
    AdapterSession,
    OPENHANDS_UPSTREAM_COMMIT,
    OpenHandsAdapter,
)
from agentscope_sdk.runtime import init as init_runtime
from agentscope_sdk.tracing import configure_tracing

tracer = configure_tracing(
    "http://agentscope:8080",
    project_token,
    service_name="my-openhands-agent",
)
runtime = init_runtime(
    "http://agentscope:8080",
    project_token,
    agent_name="my-openhands-agent",
    framework="openhands",
    source="adapter",
)

session = AdapterSession(
    runtime,
    tracer,
    metadata={
        "adapter_name": "openhands",
        "framework": "openhands",
        "openhands.upstream_commit": OPENHANDS_UPSTREAM_COMMIT,
    },
)
adapter = OpenHandsAdapter(
    session,
    capture_reasoning=False,
    capture_system_prompt=False,
    metrics_provider=lambda: llm.metrics,  # 可选：使用公开 LLM 指标
    metrics_cost_is_estimate=True,
)

conversation = Conversation(
    agent=agent,
    workspace=workspace,
    callbacks=[*existing_event_callbacks, *adapter.callbacks],
    token_callbacks=[*existing_token_callbacks, *adapter.token_callbacks],
)
answer = adapter.run(conversation, user_prompt, task_id="support-123")
run_id = session.last_run_id
```

`adapter.run()` 负责依次调用 `conversation.send_message()`、`conversation.run()`，并在成功或失败时关闭
AgentScope Run；不要在同一任务中再手动调用这些生命周期方法。若宿主自行驱动 Conversation，则改用
`adapter.start(...)`、宿主执行 Conversation、最后 `adapter.finish()` 或 `adapter.fail()`，且仍只使用公开
回调作为事件源。

Action/Observation 按公开 `tool_call_id` 关联，LLM 响应按公开 `llm_response_id` 去重。因此同一轮中的
多个工具、以及迟到或重复的 Observation 都不会关闭或污染后一轮 Turn。回调中的任何异常都会被 Adapter
吞掉并只记 debug 日志，不会影响 OpenHands 主执行链。

## OpenTelemetry exporter 边界

固定版本的 OpenHands 会在检测到 `LMNR_PROJECT_API_KEY`、`OTEL_ENDPOINT`、
`OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` 或 `OTEL_EXPORTER_OTLP_ENDPOINT` 时启用其内置
Laminar/OpenTelemetry 路径。无论使用原生 bridge 还是外置 Adapter，一个 OpenHands 进程都只能有一个
负责 AgentScope 数据的 exporter / Trace root：

- 外置 Adapter 使用 `configure_tracing()`，不要再启用 OpenHands 内置观测；
- 原生 bridge 复用 AgentScope SDK 的 Runtime 与 exporter，并在 fork 内明确禁止或隔离 OpenHands 的
  Laminar exporter；
- 若业务必须保留 OpenHands 自身观测，应将其作为独立后端，不能把两条 exporter 的数据混作同一个
  AgentScope Run。

## 事件映射

| OpenHands 公开事件 | AgentScope 记录 |
| --- | --- |
| `MessageEvent` | 用户输入、LLM 响应、最终答案、Skill 激活 |
| `ActionEvent` | Thought、LLM 响应组、Tool 开始 |
| `ObservationEvent` | Tool 结果、退出码与失败状态 |
| `AgentErrorEvent` | 可恢复 Tool 失败 |
| `ConversationErrorEvent` | Run 失败与错误 Span |
| `ConversationStateUpdateEvent` | 实时 Run 状态 |
| `PauseEvent` / `InterruptEvent` | `waiting_user` |
| `SystemPromptEvent` | 提示词哈希、工具清单；内容按采集策略决定 |
| `Condensation*` | 上下文压缩 Thought |
| Token Callback | 首 Token 时间与流式输出 |
| 未知事件 | `openhands.event.<ClassName>` 兼容 Span |

事件由 `id` 去重，Action/Observation 使用 `tool_call_id` 关联，并行 Tool Call 不会
互相覆盖。Adapter 内部异常只写 debug 日志，不会改变 OpenHands 的执行。

## 运行

先启动完整平台：

```bash
make dev
```

配置 GLM：

```bash
export GLM_API_KEY="..."
export GLM_MODEL="glm-4.7-flash"
make demo-openhands-agentops
```

或配置 DeepSeek：

```bash
export DEEPSEEK_API_KEY="..."
export DEEPSEEK_MODEL="deepseek-chat"
make demo-openhands-agentops
```

也可直接设置 `OPENHANDS_LLM_API_KEY`、`OPENHANDS_LLM_BASE_URL`、
`OPENHANDS_LLM_MODEL` 与 `OPENHANDS_LLM_PROVIDER`。模型名未带 LiteLLM provider
前缀时，脚本会自动补为 `openai/<model>`。

Demo 使用真实 OpenHands `Agent`、`TerminalTool` 与 `FileEditorTool` 完成工作区
检查、一次可恢复失败、文件创建和命令验证。结束时按精确 Run ID 验证 PostgreSQL、
ClickHouse、Redis Stream、SSE 与不可变 Snapshot；任意一项缺失都会以非零状态退出。

## 采集边界

系统提示默认只保存 SHA-256 与工具清单。只有显式设置
`capture_system_prompt=True` 才采集提示词摘要。API Key 不会写入 Snapshot、Span、
SSE 或结果文件。公开的模型思考内容是否采集由 `capture_reasoning` 控制。
