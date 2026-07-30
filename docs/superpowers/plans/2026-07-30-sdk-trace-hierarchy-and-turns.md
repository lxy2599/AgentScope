# Python SDK Trace 层级与轮次改造计划

> 状态：设计计划
>
> 范围：Python SDK、Python Adapter、OTLP 语义协议兼容
>
> 不包含：OpenClaw 插件改造、前端完整交互改造、独立 `trace_turns` 表的第一阶段实现

## 1. 背景

当前 SDK 已经能够将 Agent 执行过程采集为标准 OpenTelemetry Span，并通过
`agentscope.span.kind` 为 Span 赋予 `LLM`、`TOOL`、`THOUGHT`、`FINAL_ANSWER` 等
Agent 语义。所有 Span 进入 `trace_spans`，部分语义类型再投影到结构化表。

当前主要问题集中在三个方面：

1. **父子关系依赖隐式 Context。**
   `AdapterSession.start_run()` 手动进入 `agent.run`，之后 `trace_llm()`、
   `trace_tool()` 和通用事件直接调用 `start_as_current_span()`。在同步串行场景
   中可以工作，但跨回调、异步 Task 或并发工具场景下，Span 可能挂到回调发生时
   恰好处于 current 的 Span，而不是业务上真正的父节点。
2. **固定协议字符串散落在 SDK、Adapter 和后端。**
   语义类型、属性名、Span 名称和事件映射规则分散在多个文件中，容易产生拼写
   不一致，也会让 Python SDK 与 Go ingest 的协议演进缺少单一约束。
3. **Run 内没有显式轮次模型。**
   当前系统可以记录多个 LLM、Tool 和 Thought Span，但没有明确表示它们属于
   第几轮 Agent 决策，也没有记录一轮的开始、结束、耗时、结果和重试情况。

这三个问题应作为一次相关联的 SDK 追踪模型升级处理，而不是分别增加几个字段。

## 2. 目标与非目标

### 2.1 目标

- 在不改变 OpenTelemetry Span 底层模型的前提下，引入稳定的 Run → Turn → 行为
  Span 层级。
- 让 SDK 内部创建 LLM、Tool、Thought、Memory、Guardrail 等 Span 时显式选择
  父 Context。
- 保留公共 tracing helper 的兼容性：未传 `parent_context` 时，继续继承当前
  OTel Context。
- 将固定语义值、属性名和命名规则集中管理，避免 Adapter 手写协议字符串。
- 用 `turn_id`、`turn_index` 和 Turn Span 支持多轮耗时、行为归属、重试和并行工具
  分析。
- 保证现有 `trace_spans`、结构化语义表和旧 Adapter 调用方式继续可用。
- 为同步、跨回调、异步和并发场景补充可验证的父子关系测试。

### 2.2 非目标

- 不修改 OTel 的 `trace_id`、`span_id`、`parent_span_id` 数据模型。
- 不把 `LLM`、`TOOL` 等实现成不同的 Span 子类；它们仍然是普通 OTel Span。
- 不在第一阶段新增 `trace_turns` ClickHouse 表；Turn 先作为 raw Span 和属性保存。
- 不要求 SDK 根据任意事件自动猜测轮次；轮次边界由具体 Adapter 通过统一接口上报。
- 不改动已有语义值的协议字符串，例如 `LLM`、`TOOL`、`FINAL_ANSWER`。
- 不在本计划中改造 OpenClaw 或其他非 Python SDK 实现。

## 3. 当前实现基线

### 3.1 当前 Span 创建方式

Python SDK 的语义 helper 位于 `python/agentscope_sdk/tracing.py`，包括：

- `trace_llm()`
- `trace_tool()`
- `trace_retrieval()`
- `trace_skill()`
- `trace_thought()`
- `record_final_answer()`

这些 helper 的共同模式是：

```python
with tracer.start_as_current_span(name) as span:
    set_redacted_attribute(span, "agentscope.span.kind", "LLM")
    ...
```

`AdapterSession` 位于 `python/agentscope_sdk/adapters/base.py`，当前负责：

- `start_run()` 创建并长期保持 `agent.run`；
- `on_llm_start()` / `on_llm_finish()` 管理跨回调 LLM Span；
- `on_tool_start()` / `on_tool_finish()` 管理跨回调 Tool Span；
- `on_thought()`、`on_skill_call()` 和通用事件创建一次性 Span；
- `finish_run()` / `fail_run()` 关闭残留子 Span、关闭 root 并 flush。

### 3.2 当前父子关系风险

当前子 Span 创建时不传显式 parent：

```python
tracer.start_as_current_span("llm.call")
```

因此关系由创建时的 current Context 决定。对于跨回调 Span，
`on_llm_start()` 通过 `__enter__()` 将 LLM Span 置为 current，直到
`on_llm_finish()` 调用 `__exit__()`。如果期间有其他事件创建 Span，可能得到：

```text
agent.run
└── llm.call
    └── tool.search
```

这不一定符合业务希望的：

```text
agent.run
└── agent.turn
    ├── llm.call
    └── tool.search
```

### 3.3 当前轮次缺口

当前 Trace 可以通过多个 LLM/Tool Span 和时间顺序大致还原多轮，但没有：

- Turn Span；
- Turn 开始和结束接口；
- `turn_id` 和 `turn_index`；
- 一轮的 outcome、耗时和统计；
- LLM 重试、Tool 重试与轮次的明确归属。

`call_id` 和 `tool_id` 目前只负责跨回调配对，不能代替轮次。

## 4. 目标数据模型

### 4.1 目标 Span 树

```text
agent.run
├── agent.turn
│   ├── llm.call
│   ├── thought.reasoning
│   └── tool.search
└── agent.turn
    ├── llm.call
    └── agent.answer
```

定义如下：

- `agent.run`：一次完整任务的根 Span。
- `agent.turn`：一次 Agent 决策循环，父节点固定为 Run。
- `LLM`、`TOOL`、`THOUGHT`、`MEMORY`、`GUARDRAIL`：当前 Turn 内的行为 Span。
- `FINAL_ANSWER`：通常挂在最后一个 Turn 下；没有活动 Turn 时退回挂在 Run 下。

Turn 不等于一次 LLM 调用。一个 Turn 可以包含多个 LLM、并行 Tool、Tool 重试或
其他内部行为；一次 LLM 重试也不自动产生新 Turn。

### 4.2 Span 属性协议

所有业务 Span 继续使用 OTel 的标准字段，同时增加统一 AgentScope 属性：

```text
agentscope.span.kind
agentscope.run.id
agentscope.trace.id
agentscope.turn.id
agentscope.turn.index
```

Turn Span 额外允许记录：

```text
agentscope.turn.outcome       continue / final / error / cancelled
agentscope.turn.llm_count
agentscope.turn.tool_count
agentscope.turn.retry_count
```

行为 Span 至少复制当前 Turn 的：

```text
agentscope.run.id
agentscope.turn.id
agentscope.turn.index
```

复制这些属性是为了方便 ClickHouse 查询；真正的树关系仍由 OTel 的
`parent_span_id` 表达。

### 4.3 Context 关系

SDK 内部显式保存两类 Context：

```text
root_context  → agent.run
turn_context  → agent.turn.N
```

创建行为 Span 时按以下规则选择父节点：

1. 调用方显式提供 `parent_context`：使用它；
2. 当前 AdapterSession 有活动 Turn：使用 `turn_context`；
3. 当前 Run 存在但没有 Turn：使用 `root_context`；
4. 公共 helper 未被 AdapterSession 管理且没有传 Context：继承当前 OTel Context。

## 5. 协议常量与命名治理

### 5.1 固定值分类

需要集中管理的固定值包括：

- 语义类型：`AGENT`、`TURN`、`LLM`、`TOOL`、`RETRIEVER`、`SKILL`、`THOUGHT`、
  `MEMORY`、`GUARDRAIL`、`FINAL_ANSWER`、`INTERNAL`；
- 属性名：`agentscope.span.kind`、`agentscope.turn.id` 等；
- 固定 Span 名称：`agent.run`、`agent.turn`、`llm.call`、`agent.answer`；
- 事件前缀到语义类型的映射规则。

动态值不应被伪装成常量，包括：

- `tool_name`、`provider`、`model`；
- `run_id`、`turn_id`、`call_id`、`tool_id`；
- 动态工具 Span 名称中的具体工具名。

### 5.2 Python SDK 方案

新增一个小型协议模块，例如：

```text
python/agentscope_sdk/tracing_contract.py
```

集中定义：

```python
class SemanticKind(StrEnum): ...
class SpanAttribute: ...
class SpanName: ...
```

`tracing.py` 和 `adapters/events.py` 使用该模块，不再直接散落固定字符串。

### 5.3 统一 Span Factory

在 `tracing.py` 内抽取内部工厂，统一处理：

- `parent_context` 解析；
- Span 创建；
- `agentscope.span.kind` 写入；
- 通用 Run/Turn 属性；
- 脱敏；
- Span 名称规范化。

语义 helper 保留现有公共接口，但变为薄封装：

```python
trace_llm(..., parent_context=None)
trace_tool(..., parent_context=None)
trace_thought(..., parent_context=None)
```

`parent_context=None` 的兼容规则必须明确：不传时继承当前 OTel Context。

### 5.4 Python 与 Go 的协议一致性

Python Enum 和 Go 常量可以分别实现，但字符串值必须保持一致。Go ingest 中的
`internal/ingest/structured.go` 应使用集中定义的常量，而不是在多个 `switch` 或
测试中重复书写协议值。

本阶段不做跨语言代码生成；先以文档、常量和契约测试保证一致性。

## 6. 详细实施阶段

### Phase 0：冻结协议与基线行为

**目标：** 在改代码前确认兼容边界，防止重构过程中改变已有数据含义。

**任务：**

1. 梳理 `tracing.py` 中所有语义 helper、属性和固定 Span 名称。
2. 梳理 `adapters/events.py` 的事件归一化规则。
3. 梳理 `internal/ingest/structured.go` 的结构化类型识别规则。
4. 用现有测试固化以下行为：
   - 未传 `parent_context` 时继承当前 Context；
   - `agentscope.span.kind` 与 `openinference.span.kind` 的优先级；
   - LLM/Tool 的结构化投影不受新增 Turn 属性影响；
   - 未知语义类型仍保留在 `trace_spans`。

**验收：**

- 输出一份固定语义值清单；
- 现有 Python 和 Go 测试通过；
- 明确不删除、不重命名现有语义值。

### Phase 1：集中协议常量与统一命名

**目标：** 消除固定语义字符串和属性名散落，同时保持序列化值不变。

**任务：**

1. 新增 Python tracing contract 模块。
2. 将 `tracing.py` 中的固定语义值迁移到 Enum/常量。
3. 将属性 key 迁移到集中常量。
4. 将 `events.py` 的语义映射改为使用 Enum。
5. 为动态工具名、技能名增加统一命名函数和长度/字符规范化。
6. 在 Go ingest 中集中定义语义常量，替换结构化投影中的裸字符串。

**兼容要求：**

- Enum 的 `.value` 必须仍然是原来的大写字符串；
- OTLP attributes 的 key/value 不发生变化；
- 外部调用方仍可传入字符串，SDK 在入口处归一化为 Enum。

**验收：**

- `rg` 检查固定协议值只出现在 contract、兼容解析和测试断言中；
- 所有既有语义 helper 测试通过；
- Go 结构化投影测试通过。

### Phase 2：引入显式 Parent Context

**目标：** 让 SDK 内部的业务 Span 不再依赖回调时的隐式 current Span。

**任务：**

1. 在公共 tracing helper 中增加可选 `parent_context` 参数。
2. 实现统一的 parent Context 解析逻辑：
   - 显式 Context 优先；
   - 未传时退回当前 OTel Context。
3. 在 `AdapterSession.start_run()` 中保存 `_root_context`。
4. 抽取 `AdapterSession._start_span()` 或等价内部工厂。
5. 一次性 Span 使用带显式 Context 的 `start_as_current_span()`。
6. 跨回调 LLM/Tool 使用 `start_span(context=...)`，保存 Span 引用，结束时直接
   `span.end()`，不再跨回调持有 `__enter__()` 状态。
7. 保留 `call_id`、`tool_id` 的配对职责，不让它们参与父节点推断。

**父节点规则：**

```text
有活动 Turn  → Turn Context
无 Turn       → Root Context
公共 helper 未绑定 Session 且未传 Context → 当前 OTel Context
```

**验收：**

- 同步串行回调的父子关系与改造前语义一致；
- LLM 未结束时创建 Tool，不会因为 current Context 自动误挂到 LLM 下；
- LLM/Tool 在不同执行上下文结束时仍能正确 `end()`；
- 父子关系由 `parent_span_id` 验证，而不是通过 Span name 推断。

### Phase 3：引入 Turn 生命周期

**目标：** 增加 Run 与行为 Span 之间的业务控制流层。

**任务：**

1. 在 `AdapterSession` 中增加 `_active_turn` 和 Turn 序号管理。
2. 增加：
   - `start_turn(turn_id: str = "", turn_index: int | None = None, ...)`
   - `finish_turn(outcome: str = "continue", ...)`
3. `start_turn()` 创建 `agent.turn`，父节点固定为 `_root_context`。
4. 保存 `turn_context` 和 Turn 元数据。
5. 所有 AdapterSession 内部行为 Span 自动继承当前 Turn Context。
6. 将 `turn_id`、`turn_index` 复制到行为 Span。
7. Turn 结束时关闭残留 LLM/Tool，并设置 Turn 的状态和统计属性。
8. `finish_run()` 在结束 Final Answer 前后按统一规则关闭最后一个 Turn。
9. `fail_run()` 标记活动 Turn 和残留子 Span 为 ERROR，再关闭 Run。

**轮次边界原则：**

- 轮次边界由具体 Adapter 事件决定；
- SDK 不根据 LLM 数量自动猜测 Turn；
- LLM 重试仍属于当前 Turn；
- 并行 Tool 必须全部结束后才能结束 Turn；
- 没有明确 Turn 的旧调用可以不创建 Turn，行为 Span 退回 Root。

**验收：**

- 一个多轮 Run 可以产生多个 `agent.turn`；
- 每个 LLM/Tool/Thought 都能通过 `turn_id` 找到所属 Turn；
- Turn duration、outcome、LLM/Tool count 可从 raw Span 读取；
- 没有 Turn 的旧 Adapter 调用仍然可以正常落库。

### Phase 4：Adapter 适配与兼容迁移

**目标：** 让具体 Adapter 上报真实轮次，而不是让基础 Session 猜测。

**任务：**

1. 设计 Adapter 的 Turn 适配约定：框架的决策循环开始时调用 `start_turn()`，
   决策循环结束时调用 `finish_turn()`。
2. 优先改造已有 Python Adapter 中轮次边界最明确的入口。
3. 对无法可靠识别 Turn 的事件，使用“无 Turn 退回 Root”的兼容策略。
4. 继续保留现有 LLM/Tool callback 的 `call_id`、`tool_id` 匹配。
5. 为每个 Adapter 增加重复回调、缺失 finish、异常退出和跨轮次测试。

**不做的事情：**

- 不把每一次 `on_llm_start()` 都强行当成新 Turn；
- 不把 Tool retry 当成新 Turn；
- 不要求不同 Agent 框架共享相同的外部事件名。

### Phase 5：后端、查询和验证闭环

**目标：** 确保新增 Turn Span 不破坏现有 ingest 和结构化投影，并让数据可查询。

**任务：**

1. 确认 `trace_spans` 已能保存 Turn Span，无需改表结构。
2. 确认 `ExtractStructuredCalls()` 忽略 `TURN`，但继续提取 LLM/Tool 等类型。
3. 在 ingest 测试中增加 Turn 与 `turn_id` 属性的透传验证。
4. 增加按 `run_id + turn_index` 查询原始 Span 的查询样例。
5. 评估前端是否需要按 Turn 分组展示；第一阶段只保证 API/raw 数据完整。
6. 当 Turn 查询成为高频路径后，再单独评估 `trace_turns` 投影表。

**验收：**

- 新增 Turn Span 不产生错误的结构化业务记录；
- 原有 LLM/Tool/Thought/Final Answer 表数据不受影响；
- 原始 Span 可以重建 Run → Turn → 行为树。

## 7. 兼容性策略

### 7.1 公共 API 兼容

现有代码：

```python
with trace_llm(tracer, "llm.call"):
    ...
```

继续有效，且 `parent_context` 默认值为 `None`。当它为 `None` 时，helper
继承当前 OTel Context，保持原有行为。

新增能力：

```python
with trace_llm(
    tracer,
    "llm.call",
    parent_context=turn_context,
):
    ...
```

不建议第一阶段删除旧的 context-manager API。

### 7.2 数据兼容

- 旧 Span 没有 `turn_id` 时，后端和前端按无 Turn 处理；
- 旧 Span 的 `agentscope.span.kind` 值保持不变；
- 新增 `TURN` 只进入 `trace_spans`，不触发已有业务结构化表；
- `trace_spans.kind` 继续保存 OTel `SpanKind`，不能改成 Agent 语义类型。

### 7.3 失败与恢复

- `start_turn()` 创建失败时，不影响 Run 继续采集；可以退回 Root Context；
- `finish_turn()` 重复调用应幂等或明确抛出可诊断错误；
- Run 失败时，所有未结束 Turn、LLM、Tool 都必须设置错误状态并关闭；
- 退出进程前仍由现有 `force_flush()` 和 `shutdown_tracing()` 负责发送数据。

## 8. 测试计划

### 8.1 Contract 测试

- Enum/常量序列化值与历史字符串完全一致；
- 属性 key 不发生变化；
- 事件归一化得到预期 `SemanticKind`；
- 未知事件仍为 `INTERNAL` 或既有默认语义。

### 8.2 Parent Context 测试

- Root → child 的 `parent_span_id` 正确；
- Root → Turn → child 的两级关系正确；
- 显式 `parent_context` 优先于 current Context；
- 未传 `parent_context` 时继承 current Context；
- LLM active 时创建 Tool，Tool 按业务规则挂 Turn，而不是自动挂 LLM；
- 跨线程/异步结束 Span 时仍使用创建时的 parent，不重新读取 current Context。

### 8.3 Turn 生命周期测试

- 单轮 Run：一个 Turn、一个 LLM、一个 Final Answer；
- 多轮 Run：多个 Turn，`turn_index` 单调递增；
- 一轮多个 Tool：所有 Tool 共享同一个 Turn；
- LLM retry：同一 Turn 下多个 LLM Span；
- Tool retry：同一 Turn 下多个 Tool Span；
- 缺失 finish：Run 结束时残留 Span 被关闭并标记；
- Turn 异常：Turn、子 Span 和 Run 的状态一致；
- 无 Turn 兼容路径：行为 Span 直接挂 Root。

### 8.4 Ingest 回归测试

- `TURN` 不进入 LLM/Tool/Retriever/Skill/Thought/Final Answer 结构化表；
- 新增 Turn 属性能在 `trace_spans.attributes` 中完整保存；
- 旧 Span 无 Turn 属性时仍能被结构化提取；
- `parent_span_id`、`trace_id`、`run_id` 保持原有解码方式。

## 9. 风险与取舍

### 风险一：Turn 边界不统一

不同框架对“轮次”的定义不同。解决方案是由 Adapter 显式上报，基础 SDK 不猜测；
无法识别时允许无 Turn 运行。

### 风险二：显式 Context 破坏已有嵌套行为

公共 helper 默认仍继承 current Context；SDK 内部只有在明确进入 Run/Turn 管理时
才使用显式 Context。对于确实需要嵌套的内部 Span，提供显式 parent 参数。

### 风险三：抽象过度

第一阶段不引入复杂的 `RunTrace`、`TurnTrace` 服务对象。优先让
`AdapterSession` 保存 root/turn 状态，并用轻量 `ActiveTurn` 和统一 Span factory
完成改造。

### 风险四：协议常量跨语言漂移

Python 和 Go 分别维护常量，但必须通过固定值清单和契约测试保证一致；后续可再
引入代码生成，不在本阶段扩大范围。

## 10. 推荐实施顺序

```text
Phase 0  冻结协议和基线
   ↓
Phase 1  集中常量、属性和命名
   ↓
Phase 2  显式 Parent Context
   ↓
Phase 3  Turn 生命周期
   ↓
Phase 4  Adapter 上报轮次
   ↓
Phase 5  Ingest 回归和查询验证
```

必须先完成 Phase 2 再做 Phase 3：如果先加 `turn_id`，但父节点仍依赖隐式 current
Context，最终只能得到“标记了轮次但树关系仍不稳定”的半成品。

## 11. 最终验收标准

改造完成后，一个多轮 Run 至少应能稳定表达：

```text
agent.run
├── agent.turn(index=1)
│   ├── llm.call(turn=1)
│   ├── tool.search(turn=1)
│   └── tool.search.retry(turn=1)
└── agent.turn(index=2)
    ├── llm.call(turn=2)
    └── agent.answer(turn=2)
```

并满足：

- `parent_span_id` 由显式 Run/Turn Context 确定；
- 不传 `parent_context` 的旧 helper 行为不变；
- 固定语义值和属性名由统一协议常量管理；
- `call_id`/`tool_id` 只负责生命周期配对；
- Turn 不等于 LLM 调用，重试和并行工具仍可归属于同一 Turn；
- 所有 Span 仍是标准 OTel Span，结构化表逻辑无需重写；
- 异常、缺失回调和异步结束不会导致未关闭或误挂的 Span。

