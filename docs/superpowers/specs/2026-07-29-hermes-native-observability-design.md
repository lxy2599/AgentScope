# Hermes 原生内置 AgentScope 可观测设计

- **状态**：已确认方案，待实现
- **日期**：2026-07-29
- **适用上游版本**：NousResearch/hermes-agent `d9165d7a678d4105f42921a7fc1886df3804531b`
- **决策**：在 Hermes 源码内部直接采集；仅使用通用 OpenTelemetry SDK/OTLP HTTP 协议库；不使用 AgentScope SDK、`HermesAdapter`、`AdapterSession`、Hermes callback 或 Plugin observer hook。

## 1. 背景

当前仓库已经证明了真实 Hermes 能通过外部 Adapter 接入 AgentScope，能够采集 Run、LLM、Tool、Memory、Skill、Thought、最终回答，并跑通线上评测和失败 Run 自动人工回流。

该验证入口是一次性 Demo：调用方显式执行 `adapter.run(agent, prompt)`，运行结束后退出。它不满足将 Hermes 作为本地长期使用 Agent 时的工程目标：用户不能因为接入可观测而改变原有 Hermes 的启动方式、Dashboard/Gateway/CLI 使用方式、会话方式或工具行为。

本设计将观测能力下沉到 Hermes 源码的真实执行点。AgentScope 只作为 Runtime API 与 OTLP 的接收端，不参与 Hermes 的控制流和决策。

## 2. 决策摘要

### 2.1 采用

1. 建立独立的本地 Hermes fork/worktree，固定上游 commit 后在本地分支维护改动；不在 AgentScope 的 `.tmp/hermes-agent` 缓存目录长期开发。
2. 在 Hermes 源码内部新增 `hermes_observability` 模块，拥有事件模型、Runtime HTTP Client、OTLP exporter、后台队列、脱敏和配置。
3. 在 Hermes 的真实执行点直接产生观测事件：Turn 生命周期、LLM 传输、工具执行、Memory、Skill、审批、错误等。
4. 使用通用 `opentelemetry-api`、`opentelemetry-sdk`、`opentelemetry-exporter-otlp-proto-http` 实现 TraceContext、Span、批处理和 OTLP 传输。
5. 使用 Hermes 内部实现的轻量 Runtime HTTP Client 对接 AgentScope 的 Agent 注册、心跳及 Run 生命周期 API。
6. 默认关闭、异步发送、有界缓冲、失败隔离。Hermes 现有 Dashboard、Gateway、CLI、ACP 和会话机制保持原状。

### 2.2 明确不采用

- 不使用 `agentscope_sdk`。
- 不使用 `HermesAdapter` 或 `AdapterSession`。
- 不使用 `tool_progress_callback`、`reasoning_callback`、`step_callback`、`stream_delta_callback`、`event_callback`。
- 不使用 Hermes Plugin observer hook，例如 `pre_api_request`、`post_tool_call`。
- 不增加新的 Hermes HTTP 服务、代理服务、启动入口或替代 Dashboard。
- 不在用户请求路径同步等待 AgentScope Runtime API、OTLP 导出、Redis、PostgreSQL 或 ClickHouse。

## 3. 目标与非目标

### 3.1 目标

- 用户继续以原生方式使用 Hermes，例如 `hermes dashboard`、原有 Gateway、CLI 或 ACP。
- 每个真实用户 Turn 产生一个可关联的 AgentScope Run 与 Trace。
- 原生采集 LLM、Tool、Memory、Skill、Final Answer、审批和错误，不依赖外围回调推断。
- 常驻 Hermes 进程持续注册 Agent、维护实例心跳，并将观测数据异步发送到 AgentScope。
- AgentScope 继续复用现有的 Run 详情、实时 SSE、TraceAware 在线评测和人工标注回流。
- AgentScope 不可用、Token 错误、网络失败或 exporter 错误时，Hermes 的原有结果、异常和工具行为保持不变。

### 3.2 非目标

- 不改写 Hermes 的 Prompt、messages、provider、model、base URL、重试、工具列表、工具参数、工具结果、审批逻辑或 session 逻辑。
- 不把 AgentScope 作为 Hermes 的控制面、配置中心或鉴权层。
- 不在第一阶段采集完整 reasoning 文本；完整 reasoning 仅在未来确认存在稳定的 Hermes 内部真实生成点且显式开启后考虑。
- 不保证 AgentScope 不可用时零数据丢失；优先保证 Hermes 可用性和行为一致性。
- 不在本设计中改变 AgentScope 后端协议或新增后端服务。

## 4. 第一原则：Hermes 使用行为不变

### 4.1 不变项

启用或禁用观测均不得改变以下事项：

```text
Hermes 原有启动命令
Dashboard/Gateway/CLI/ACP 入口
会话 ID 与会话持久化
模型、provider、base URL 与请求内容
工具选择、工具参数、工具结果与审批
重试、fallback、异常类型与异常文本
最终回答内容与返回对象结构
原有 callback 的存在、顺序、参数和调用次数
```

### 4.2 实现约束

每一个埋点点都必须满足：

```text
观测代码失败 -> 吞掉观测异常，仅记录本地 debug 日志/计数
观测队列满   -> 有界丢弃，不阻塞 Hermes
网络不可用   -> 后台重试或丢弃，不影响当前 Turn
后端不可用   -> Hermes 原结果照常返回、原异常照常抛出
```

日常 Turn 结束时不得调用同步 `force_flush()`。仅在 Hermes 进程优雅退出时，以有上限的时间窗口尝试 flush。

## 5. 总体架构

```text
原生 Hermes Dashboard / Gateway / CLI / ACP
                    |
                    v
          AIAgent.run_conversation()
                    |
       +------------+-------------+
       |                          |
       v                          v
Hermes 原有执行链                Hermes 内置观测旁路
LLM / Tool / Memory / Skill      hermes_observability
       |                          |
       v                          +-- Runtime control queue
原始结果或原始异常                |     register / heartbeat / run lifecycle
                                  |
                                  +-- OTel BatchSpanProcessor
                                        OTLP HTTP /v1/traces
                                                |
                                                v
                                      AgentScope 后端
                                      PostgreSQL / ClickHouse / Redis
                                                |
                                                v
                               Run 详情 / 实时观测 / 在线评测 / 标注回流
```

观测旁路只能读取执行状态并提交副本，不能向 Hermes 主执行链返回任何控制信息。

## 6. Hermes fork 与依赖策略

### 6.1 源码承载

建立独立本地 fork/worktree：

```text
/home/luxinyu/GolandProjects/hermes-agent-agentscope
branch: agentscope-native-observability
base: d9165d7a678d4105f42921a7fc1886df3804531b
```

上游更新通过 rebase/cherry-pick 管理。所有对 Hermes 的侵入式改动应位于可审查的本地分支，不能依赖 AgentScope `.tmp/` 缓存目录。

### 6.2 通用依赖

在 Hermes fork 中增加**可选**依赖组，例如：

```toml
[project.optional-dependencies]
agentscope-observability = [
  "opentelemetry-api>=1.25,<2",
  "opentelemetry-sdk>=1.25,<2",
  "opentelemetry-exporter-otlp-proto-http>=1.25,<2",
]
```

要求：

- 默认 Hermes 安装不新增该依赖，也不 import 这些包；
- 只有启用 `HERMES_AGENTSCOPE_ENABLED=true` 的本地环境安装该 extra；
- 不引入 `agentscope_sdk` 依赖。

## 7. 内部模块设计

建议新增：

```text
hermes_observability/
├── __init__.py
├── config.py
├── models.py
├── redact.py
├── runtime_client.py
├── runtime_worker.py
├── tracing.py
├── buffer.py
└── collector.py
```

### 7.1 `config.py`

解析配置并保证默认关闭：

| 配置 | 默认 | 说明 |
|---|---:|---|
| `HERMES_AGENTSCOPE_ENABLED` | `false` | 是否启用旁路观测 |
| `AGENTSCOPE_SERVER_URL` | 无 | AgentScope 后端地址 |
| `AGENTSCOPE_PROJECT_TOKEN` | 无 | 项目 Bearer Token |
| `HERMES_AGENTSCOPE_AGENT_NAME` | `hermes-local` | 运行时 Agent 名称 |
| `HERMES_AGENTSCOPE_HEARTBEAT_SECONDS` | `5` | 心跳间隔 |
| `HERMES_AGENTSCOPE_QUEUE_SIZE` | `2048` | Trace 队列上限 |
| `HERMES_AGENTSCOPE_CONTROL_QUEUE_SIZE` | `256` | Runtime 控制事件上限 |
| `HERMES_AGENTSCOPE_FLUSH_INTERVAL_MS` | `500` | 运行时批量发送周期 |
| `HERMES_AGENTSCOPE_CAPTURE_CONTENT` | `false` | 是否保存 Prompt/输出/工具内容 |
| `HERMES_AGENTSCOPE_CAPTURE_REASONING` | `false` | 未来 reasoning 采集开关，第一阶段不启用 |

项目 Token 应使用 Hermes 原有本地环境文件/部署环境保存，并确保文件权限为 `0600`。不新增用户日常运行命令，不把 Token 写入源码、日志、Trace 或 Git。

### 7.2 `models.py`

定义 Hermes 内部事件模型：

```text
RunStarted
RunFinished
RunFailed
LLMStarted
LLMFinished
LLMFailed
ToolStarted
ToolFinished
ToolFailed
MemoryRead
MemoryWrite
SkillLoaded
SkillCalled
ApprovalRequested
ApprovalResolved
```

所有事件至少带：

```text
project token 对应的身份（不进入 Trace）
agent_id / instance_id（注册后获得）
run_id
trace_id
span_id / parent_span_id（Trace 事件）
session_id
conversation_id
task_id
发生时间
```

### 7.3 `runtime_client.py`

Hermes 内置普通 HTTP Client，Bearer Token 鉴权，直接调用现有 AgentScope Runtime 控制面：

```text
POST /api/v1/agents/register
POST /api/v1/agents/{agentID}/heartbeat
POST /api/v1/agents/{agentID}/capabilities
POST /api/sdk/runs/start
POST /api/sdk/runs/{run_id}/state
POST /api/sdk/runs/{run_id}/finish
POST /api/sdk/runs/{run_id}/fail
```

职责：

- 进程首次启用时异步注册 Agent 与能力；
- 创建/刷新 Agent Instance；
- 周期性 heartbeat；
- 保持 `run_id` 与 OTel `trace_id` 的一一关联；
- 不抛出会影响 Hermes 的异常。

### 7.4 `tracing.py`

使用通用 OTel SDK：

```text
service.name = hermes-native
endpoint = {AGENTSCOPE_SERVER_URL}/v1/traces
Authorization = Bearer {AGENTSCOPE_PROJECT_TOKEN}
```

Span 语义与现有 AgentScope ingest 兼容：

| Hermes 内部事件 | Span kind / 表 |
|---|---|
| Turn | `agent.run` 根 Span / `trace_spans` |
| LLM | `agentscope.span.kind=LLM` / `trace_llm_calls` |
| Tool | `agentscope.span.kind=TOOL` / `trace_tool_calls` |
| Memory | Memory 语义 Span；工具表中保留 `tool_name=memory` |
| Skill | `agentscope.span.kind=SKILL` / `trace_skill_calls` |
| Final Answer | `agentscope.span.kind=FINAL_ANSWER` / `trace_final_answers` |
| Approval | `agentscope.span.kind=GUARDRAIL` |

Trace exporter 使用 BatchSpanProcessor；不在单个 Turn 结束时同步 flush。

### 7.5 `buffer.py` 与 `runtime_worker.py`

采用两条独立、有界后台通道：

1. **Runtime 控制队列**：Agent 注册、心跳、Run start/finish/fail。优先级高，必须保持同一 Run 内的顺序；若 `start` 永久失败，则该 Run 之后的控制事件被本地丢弃并计数，避免产生无主 finish/fail。
2. **Trace 队列**：OTel BatchSpanProcessor 自己维护批处理队列。队列满时允许丢弃 Span 并累积本地 dropped counter。

队列策略必须是 fail-open：队列已满不得阻塞模型请求、工具执行或结果返回。可选的本地磁盘 spool 不属于第一阶段，以避免新的 I/O 行为和故障面。

### 7.6 `redact.py`

默认只保存元数据和摘要：

```text
模型/provider、Token、耗时、状态
工具名、耗时、成功失败
错误摘要
Memory/Skill 名称
```

默认不保存：

```text
完整 prompt
完整输出
完整 tool 参数/结果
完整 reasoning
密钥、Authorization、Cookie、环境变量值
```

开启 `HERMES_AGENTSCOPE_CAPTURE_CONTENT=true` 后仍需递归脱敏常见密钥字段；完整 reasoning 不是第一阶段范围。

## 8. 源码级埋点位置

本节定义逻辑位置；实现时必须通过调用链复核具体函数，不允许为了方便退回 callback 或 observer。

### 8.1 Turn 生命周期

稳定入口：

```text
run_agent.py
AIAgent.run_conversation(...)
```

改动模式：

```text
原有上下文设置
  -> collector.start_turn(...)            # best effort，仅入控制队列
  -> 原有 agent.conversation_loop.run_conversation(...)
  -> collector.finish_turn(result)        # best effort，仅入队
  -> 原样返回 result

原有异常路径
  -> collector.fail_turn(original_error)  # best effort，仅入队
  -> 原样抛出 original_error
```

不得改变原有 `try/finally` 中的 accounting 与 portal context 恢复顺序。

### 8.2 LLM 传输

候选调用链从：

```text
agent/conversation_loop.py::run_conversation
```

进入 Hermes 的 provider/transport 实现。实现前应完成一次调用图确认，找到所有真实模型发送、流式响应完成、usage 解析与最终错误归并点。

埋点要求：

```text
发送前：创建 LLM Span
成功响应：记录 provider/model/usage/latency/TTFT（若原始实现提供）
错误：记录原始错误摘要与最终失败状态
重试：每次实际请求是独立 LLM Span；不改变原重试策略
```

### 8.3 工具执行

在 Hermes 的统一工具调度/执行函数真实前后采集：

```text
执行前  -> ToolStarted
成功后  -> ToolFinished
异常后  -> ToolFailed
```

必须覆盖普通工具、MCP 工具和内置工具，并以实际 `tool_call_id` 关联开始/结束事件。

### 8.4 Memory 与 Skill

Memory 和 Skill 在其真实实现/分发点生成语义事件：

```text
Memory read/write  -> MemoryRead / MemoryWrite
Skill load/list/use -> SkillLoaded / SkillCalled
```

第一阶段可同时保留其 Tool Span，以确保现有 `trace_tool_calls` 和 `trace_skill_calls` 查询可用；但语义判断必须来自 Hermes 内部真实类型/路径，不依赖外围 callback 名称。

### 8.5 审批、会话压缩与子 Agent

第一阶段：审批进入 Guardrail Span；会话压缩和子 Agent 仅记录为普通事件/元数据。

第二阶段：

```text
approval request / allow / deny / timeout
context compression
model fallback
subagent/delegation parent-child trace relationship
```

所有第二阶段事件也必须在各自真实执行点产生。

## 9. 常驻 Hermes 的运行模型

不新增 Agent 服务。用户继续运行 Hermes 原生入口，例如：

```bash
hermes dashboard
```

启用观测只需要一次性配置原生 Hermes 环境：

```bash
export HERMES_AGENTSCOPE_ENABLED=true
export AGENTSCOPE_SERVER_URL=http://127.0.0.1:8080
export AGENTSCOPE_PROJECT_TOKEN='...'
```

启动后：

```text
Hermes 原 Dashboard/Gateway/CLI 进程
  -> Hermes 内置 Runtime worker 注册 Agent/Instance
  -> 后台 heartbeat
  -> 每个真实用户 Turn 创建一个 Run
  -> Trace 与 Runtime 事件异步写入 AgentScope
```

不要求用户改为新命令、新 API、`adapter.run()` 或新服务端口。

## 10. 在线评测与标注闭环

Hermes 原生埋点产出的 `completed` / `failed` Run 与 Trace 会进入现有 AgentScope 后端链路：

```text
Run 终态
-> PostgreSQL online_eval_jobs
-> Sampling Rule 决策
-> TraceAware evaluator 读取 Run Detail/ClickHouse 证据
-> eval_scores
-> failed 或低分 Run 自动写 annotation_items
```

推荐为长期 Hermes Project 在 AgentScope UI 中创建规则：

```text
初始调试：sample_rate = 1.0
稳定日常使用：按预算、失败率、高成本或低置信度调整
```

TraceAware evaluator 仍只代表执行健康，不等价于答案正确性、事实性、安全性或人工 Gold。

## 11. 验收标准

### 11.1 行为兼容

| 场景 | 验收 |
|---|---|
| 未设置 `HERMES_AGENTSCOPE_ENABLED` | 不 import OTel，不创建线程，不发网络请求；Hermes 原测试与交互行为不变 |
| 启用观测且 AgentScope 后端不可达 | Hermes 仍正常聊天、调用工具、返回原结果；仅观测丢失/本地计数增加 |
| Token 无效 | Hermes 仍正常工作；后台记录初始化/发送失败 |
| 原 callback 已安装 | callback 顺序、参数、次数不变；原 callback 不被替换或包装 |
| 原生 Dashboard/Gateway/CLI/ACP | 不改启动命令、不要求调用方改代码 |
| 模型/Tool 抛异常 | Hermes 原异常语义不变，同时产生 best-effort failed Run/Trace |

### 11.2 数据正确性

对一次真实 Hermes Turn 验证：

```text
Agent 与 Instance 已注册且有心跳
Run 与根 Span 共享 trace_id
至少一条 trace_llm_calls
至少一条 trace_tool_calls（任务实际使用工具时）
Memory/Skill 任务有对应语义数据
trace_final_answers 存在
Run completed 或 failed 与 Hermes 原结果一致
```

对失败 Turn 验证：

```text
failed Run
LLM/Tool error Span（适用时）
online_eval_job 进入终态
失败 Run 自动人工回流（匹配 Sampling Rule 时）
```

### 11.3 性能与可靠性

- 开启观测后，固定 mock provider 的主流程结果与异常完全一致；
- 后端不可达时，主线程不得因观测重试等待；
- 队列满时，主线程不得阻塞；
- 对真实模型的 P95 Turn 延迟增量应单独基线测试并记录；
- 常驻 30 分钟至少保持 heartbeat，后台线程无泄漏，优雅退出可在限定时间内结束。

## 12. 实施阶段

### Phase 0：fork 与调用图确认

- 建立独立 Hermes fork/worktree；
- 固定上游版本；
- 绘制真实 LLM transport、Tool dispatch、Memory、Skill 和审批调用图；
- 不修改执行逻辑。

### Phase 1：最小原生观测

- 可选 OTel extra；
- 内置配置、脱敏、Runtime Client、后台队列；
- `AIAgent.run_conversation` 的 start/finish/fail；
- LLM 与 Tool 真实执行点埋点；
- Agent 注册、心跳、Run/Trace 关联；
- 后端不可达和 disabled compatibility 测试。

### Phase 2：语义与长期使用验证

- Memory、Skill、Approval 语义事件；
- 原生 Dashboard/Gateway/CLI 实测；
- 长期项目 Sampling Rule；
- 在线评测、失败标注回流与前端展示核验。

### Phase 3：可选高级能力

- 子 Agent/delegation 关系；
- Context compression、fallback、retry 详细事件；
- 受控的 reasoning 采集；
- 可选本地磁盘 spool 与诊断页。

## 13. 回滚方案

运行时最快回滚：

```bash
unset HERMES_AGENTSCOPE_ENABLED
# 或 export HERMES_AGENTSCOPE_ENABLED=false
```

此时不启动任何观测能力，Hermes 走原始路径。

代码回滚：

```text
在独立 Hermes fork 上 revert 本地 observability commits
回到固定上游 commit
```

不需要回滚 AgentScope 主仓库、数据库 schema 或 Hermes 用户会话数据。

## 14. 风险与应对

| 风险 | 应对 |
|---|---|
| 上游 Hermes 调用点随版本变化 | 固定 commit、独立 fork、调用图测试、升级时 rebase 审查 |
| 观测代码造成延迟 | 仅入队、后台批量导出、禁用逐 Turn flush、性能基线 |
| AgentScope 不可用 | fail-open、有界队列、后台失败隔离 |
| 采集敏感内容 | 默认仅摘要、显式内容开关、递归脱敏 |
| 长期运行后后台线程泄漏 | 生命周期测试、daemon worker、优雅 shutdown |
| Runtime start/finish 乱序 | 控制队列按 Run 保序；start 失败则抑制后续同 Run 控制事件 |
| 多入口/多线程并发 | 将 collector context 绑定到 ContextVar/线程上下文；避免全局当前 Run |

## 15. 实施前检查清单

- [ ] 确认本地 Hermes fork 路径与分支命名。
- [ ] 确认目标原生使用入口优先级（Dashboard、Gateway、CLI、ACP）。
- [ ] 创建长期 `Hermes Local` AgentScope Project 与最小权限 Project Token。
- [ ] 确认 Token 存放在 Hermes 原有环境文件并设置 `0600`。
- [ ] 对上游源码完成 LLM/Tool/Memory/Skill 实际调用图定位。
- [ ] 设计并先通过 disabled、backend-down、queue-full 三类兼容测试。
- [ ] 仅在上述检查通过后开始 Phase 1 代码改动。
