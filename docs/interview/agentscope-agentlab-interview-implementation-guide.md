# AgentScope / AgentLab 面试实现讲解

> **用途**：这是针对简历中四条项目经历的面试准备稿。它不是产品宣传稿：本文明确区分已经落地并验证的能力、可靠性语义和当前边界，避免把单机压测、Smoke Benchmark 或执行健康评估夸大成生产级全量能力或语义正确性评估。
>
> **建议使用方式**：先记住第 0 节的 30 秒概述；技术面按第 1～4 节逐层展开；遇到深挖时使用每节末尾的“追问与答法”。不要背诵所有字段和表名，而要说清楚“问题—设计—可靠性—取舍—证据”这条主线。

---

## 0. 30 秒项目概述

我做的是一个面向 Agent 的 **可观测和评测闭环平台**。核心思路是把一次 Agent 执行抽象成有稳定 `run_id` 的 Run，再把 LLM 调用、工具调用、思考过程和最终答案以 OTel Trace 的形式采集下来。

- 在采集侧，Python SDK 不让业务线程等待网络 I/O，而是通过有界队列和异步批量导出发送 OTLP；Go 服务完成鉴权、Run 关联、Span 标准化和 ClickHouse 落库。
- 在展示侧，Run 状态先持久化到 PostgreSQL，同一事务通过 Outbox 产生实时事件；后台转发到 Redis Streams，前端通过 SSE 订阅。这样 Redis 出问题不会导致“数据库状态变了但永远没有事件可补”。
- 在质量侧，真实线上 Run 终态后进入持久化在线评测队列，按规则抽样，再从 Trace 中提取执行证据做自动评估；失败、低分或高风险样本进入人工标注队列，沉淀为下一轮离线数据集。
- 在离线侧，评测把数据集、评估规则、Agent 镜像、模型配置和执行环境做成不可变快照。Benchmark 任务在 Harbor 隔离 Sandbox 中运行，只接受官方 verifier 的结果，最后做版本回归对比。

**一句话价值**：不是只“看日志”，而是把 **线上真实行为采集 → 实时定位 → 自动筛选问题样本 → 人工回标 → 离线回归** 连成一个可追溯闭环。

---

## 1. 全局架构：先建立面试官脑中的数据流

```text
OpenHands / Hermes 等真实 Agent 源码
        │
        │  Python AgentScope SDK：Runtime Run + OTel Trace
        ▼
Go AgentScope 服务
  ├─ PostgreSQL：项目、Agent、Run、状态机、评测任务、配置快照（权威业务状态）
  ├─ ClickHouse：原始 Span + LLM/Tool/Thought/Final Answer 等高维 Trace 数据
  └─ realtime_outbox：与业务状态同事务产生的事件
        │
        ▼
Redis Streams（低延迟事件分发、按项目隔离）
        │
        ▼
SSE → Web 控制台（在线 Agent、Run 状态、Trace 语义事件）

Run 结束
  │
  ▼
PostgreSQL online_eval_jobs（持久化在线评测队列）
  │
  ├─ 抽样规则：失败 / 高成本 / 低置信度 / 分歧 / 随机分层
  ├─ TraceAware 自动评估：读取 ClickHouse Trace 证据
  └─ 低分或失败 → annotation_items（人工标注队列）
                              │
                              ▼
                    版本化离线数据集 / Benchmark 回归
                              │
                              ▼
        Harbor Sandbox + Agent task runner + official verifier
```

### 1.1 为什么要分 PostgreSQL、ClickHouse、Redis 三类存储

| 组件 | 放什么 | 为什么放这里 | 不能替代什么 |
|---|---|---|---|
| PostgreSQL | 用户/项目权限、Run 状态、任务状态、抽样决定、评测任务、目录版本、Outbox | 需要事务、关联查询、幂等约束和强业务语义 | 不适合存海量 Trace 明细和复杂时序分析 |
| ClickHouse | 原始 Span、LLM 调用、工具调用、Thought、Final Answer、聚合分析 | 面向大吞吐追加写和列式分析 | 不作为 Run 状态机或任务队列的权威来源 |
| Redis Streams | 实时事件流、低延迟订阅、短期流式 replay | 延迟低，适合 SSE 的“新事件到达”场景 | 不作为唯一真相；断线补偿仍需 PostgreSQL Outbox |

面试中可以主动强调：**这不是三者做分布式事务。** 我把“必须强一致的业务事实”收敛在 PostgreSQL；ClickHouse 是 Trace 分析存储，Redis 是事件投影。这个边界能明显降低系统复杂度。

---

# 2. Trace 采集：Python SDK + Go OTLP 服务如何做到高吞吐

对应简历：

> **Trace 采集：**针对高吞吐采集瓶颈，基于 OTel 构建 Python SDK 与 Go 服务，采用有界队列、批量写入并关联 Run；单机压测 **30 万 Span**，吞吐达 **4.66k Span/s**，P95 延迟 **558 ms**。

## 2.1 问题与目标

Agent 的一次任务不是单个请求，而是一串异构操作：规划、LLM 调用、工具调用、检索、重试、最终回复。若只记录文本日志，会有四个问题：

1. **无法还原因果关系**：哪次工具调用属于哪次 LLM 调用、哪个 Run；
2. **没有统一关联键**：Runtime 上报、OTLP Trace 和评测数据容易变成三份孤立数据；
3. **采集拖慢业务**：在业务线程里同步 HTTP 写 Trace，会把网络和存储抖动传递给 Agent；
4. **高频小写导致 ClickHouse 开销高**：一次请求一个小批次，多表落库时固定开销被反复支付。

设计目标是：

- 用 OpenTelemetry 复用行业标准的 trace/span/context 模型；
- 给每次 Agent 任务分配平台 Run，并把 `run_id` 传播为 Trace attribute；
- 业务执行路径**不阻塞等待 Trace 网络写入**；
- 正常情况下批量传输和批量落库；饱和时可观测地降级，而不是拖垮 Agent；
- 服务端既保留原始 Span，又提取 LLM/Tool/Thought 等适合查询的语义表。

## 2.2 采集模型：Run 是业务主键，Trace 是执行证据

### Run、Trace、Span 的关系

```text
Project
  └─ Agent / Agent Instance
       └─ Run（一次可观测的任务执行，PostgreSQL 权威状态）
            └─ Trace（一个分布式追踪图）
                 ├─ Span: agent.run
                 ├─ Span: llm.generate
                 ├─ Span: tool.execute
                 ├─ Span: thought / plan
                 └─ Span: final.answer
```

- `run_id`：平台生成的 UUID，是控制台、在线评测、标注项和 Benchmark 结果共同使用的业务关联键；
- `trace_id`：OTel Trace 的 128 位追踪 ID，用于串联跨调用 Span；
- `span_id` / `parent_span_id`：描述同一 Trace 内的父子因果关系。

SDK 创建 Runtime Run 后，通过 `associate_run_with_trace` 或当前上下文属性将 `agentscope.run.id` 写入 Trace。服务端接收 OTLP 后根据该属性查找或创建关联 Run，并校验项目与 Token 的边界。这样后面查询一个 Run 时，可以同时拿到：状态、最终结果、Trace、评测 Job 和人工标注，而不依赖“按时间猜测是哪一条日志”。

### 为什么 Run 不直接等于 Trace

一个 Run 在复杂场景下可能跨线程、跨重试或跨多个 Trace；反过来某些初始化 Span 也未必属于业务 Run。因此 Run 是**产品/业务实体**，Trace 是**执行观测实体**。这也是为什么 Run 等核心状态放 PostgreSQL、Span 明细放 ClickHouse。

## 2.3 SDK 侧实现：不阻塞业务的有界队列 + 批量导出

关键实现位于：

- `python/agentscope_sdk/tracing.py`
- `python/agentscope_sdk/export_metrics.py`
- `python/agentscope_sdk/runtime.py`
- `python/agentscope_sdk/runtime_trace.py`

### 2.3.1 为什么不用同步 export

朴素做法是在 `span.end()` 时直接发 HTTP：

```text
Agent 业务线程 → 结束 Span → HTTP 发 OTLP → 等服务端/网络/ClickHouse → 才继续 Agent
```

问题是后端短暂抖动会直接增大 Agent 工具调用或回答的 P95，极端情况下大量线程被阻塞，形成级联雪崩。

实际实现使用自定义 `ObservableBatchSpanProcessor`，仅依赖 OTel 公共 `SpanProcessor` / `SpanExporter` 接口：

```text
业务线程调用 on_end(span)
        │
        ├─ queue.put_nowait(span)        # 不等待网络
        ├─ 队列已满：丢弃该 Span，递增 drops 指标
        └─ 唤醒后台 exporter 线程

后台线程
        ├─ 达到 batch size 或 schedule delay
        ├─ 一次取出多个 Span
        ├─ exporter.export(batch)
        ├─ 记录 latency / 成功数 / 失败数 / retry
        └─ force_flush / shutdown 时尽力排空
```

默认实现体现了三个工程原则：

1. **有界而非无界**：队列容量有限，避免后端不可用时内存无限增长；
2. **非阻塞而非“绝不丢数据”**：Trace 是辅助观测数据，优先保护 Agent 主链路；一旦满了，显式记录 `spans_dropped`、`queue_drops`，而不是静默丢失；
3. **批量而非逐条**：通过 `max_export_batch_size` 和定时 flush 摊薄请求、序列化与服务端解析成本。

### 2.3.2 背压策略如何解释

面试官常问：“为什么允许丢 Span？”

可回答：

> 我把数据分级了。对 Run 创建、终态和 Benchmark 官方结果这类业务事实，走 PostgreSQL 的可恢复/幂等链路，不能随便丢；对高频 Span，若改成阻塞重试，最坏情况下会影响 Agent 执行本身。因此 SDK 的策略是有界队列 + 非阻塞投递 + 丢失可观测。运维上看 queue depth、high watermark、drop 数、export latency，超过阈值再调 batch、采样率或扩容。它是受控降级，而不是假装可靠。

这里不要说“Trace 永不丢失”。压测中是 **0 丢失**，说明指定环境、指定负载和指定配置下验证通过；设计语义上，队列满时仍允许受控丢弃。

### 2.3.3 SDK 自身健康度为什么也要采

`ExportMetricsSnapshot` 记录了队列深度、容量、高水位、入队数、成功/失败 batch、导出延迟、丢失数、重试数等。它们以低基数、无内容字段的形式随 heartbeat/health 事件上报。

这样在控制台发现 Trace 少时，不需要猜是 Agent 没执行、网络失败还是服务端慢，而可以先看：

```text
SDK queue_depth / queue_high_watermark
→ queue_drops
→ batch_failed / last_error
→ server ingest accepted / rejected
→ ClickHouse 行数
```

## 2.4 Go 服务端：OTLP 接收、鉴权、Run 关联、语义拆分与批写

关键实现位于：

- `internal/ingest/otlp.go`
- `internal/adapter/tracestore.go`
- `internal/tracestore/`
- `clickhouse/` 下的 Trace 表定义

服务器端路径可以这样讲：

```text
OTLP HTTP 请求
  → Protobuf 解码
  → 从 Bearer/Project Token 做项目级鉴权
  → 解析 resource / span attributes
  → 读取 agentscope.run.id，查询或创建 PostgreSQL Run
  → 标准化 Span（trace_id、span_id、父子关系、时间、状态、属性）
  → 根据 agentscope.span.kind 提取 LLM / Tool / Thought 等结构化语义
  → 批写 ClickHouse：trace_spans + 语义表
  → 异步/批量记录 Ingest Health
```

### 2.4.1 原始表与语义表并存的原因

- `trace_spans` 保存通用 OTel 原始维度，便于保留完整上下文与未来重解析；
- `trace_llm_calls` 等语义表把高频分析字段抽出来，例如 provider、model、token、TTFT、成本、工具名、错误、最终答案等；
- 查询“某模型的 TTFT P95”“某工具失败率”“Run 是否有最终答案”时，不必扫描大 JSON 属性。

这是典型的 **raw + derived** 设计：原始数据保证可追溯，衍生表服务高频分析。

### 2.4.2 Run 关联和跨请求聚合优化

性能报告中专门修正了压测模型：同一 OTLP 请求内的 100 个 Span 使用同一个 `trace_id` 与同一个 `agentscope.run.id`。服务端因此可做请求内/Trace 内聚合，避免对同一个 Run 做重复 PostgreSQL 查找或创建，也避免把一批 Span 拆成大量 ClickHouse 小 Insert。

优化重点不是“把单条 Insert 变得神奇更快”，而是：

- 聚合多个 Span，减少 ClickHouse Insert 次数；
- 健康事件从同步单行写改为有界队列异步批写；
- LLM 语义记录也随批次写入；
- 优雅关闭时 flush，尽力排空内存中待写数据。

## 2.5 压测结果：必须讲清口径

简历中的 **4.66k Span/s、P95 558 ms** 对应的是 Raw Trace 的最终优化结果，完整口径为：

| 项目 | 口径 |
|---|---|
| 测试时间 | 2026 年 7 月 25 日 |
| 机器 | Intel i5-1035G1，4 核 8 线程，15 GiB 内存 |
| 部署 | 单个 Go AgentScope 进程 + 本地 Docker PostgreSQL / ClickHouse / Redis；容器未显式限额 |
| 并发 | 固定 16 |
| 单请求 | 100 Span，同一请求共享一个 Trace 和一个 Run |
| 正式轮次 | 3 轮，每轮 100,000 Span，取中位数 |
| 总量 | 300,000 Span |
| 压测范围 | OTLP 接收、鉴权、Run 关联、Span 标准化、ClickHouse `trace_spans`；测试时关闭 Runtime / SSE 干扰 |
| 最终结果 | 4,657.27 Span/s，P50 312.33 ms，P95 558.39 ms，P99 627.79 ms |
| 完整性 | HTTP 错误率 0，Span 落库丢失率 0 |

对比未优化基线：

| 工作负载 | 吞吐 | P95 | 变化 |
|---|---:|---:|---:|
| Raw Trace 基线 | 2,223.27 Span/s | 971.87 ms | - |
| Raw Trace 优化后 | 4,657.27 Span/s | 558.39 ms | 吞吐 +109.48%，P95 -42.55% |
| 高密度 LLM Semantic 优化后 | 2,680.58 Span/s | 866.91 ms | 同时写 `trace_spans` 和 `trace_llm_calls` |

**面试表述模板**：

> 我不会把 4.66k 说成集群 SLO。它是 2026 年 7 月 25 日在单机 Docker、16 并发、每请求 100 Span、三轮共 30 万 Span 的 Raw Trace 压测中得到的中位吞吐，P95 是 558ms。它证明了优化后的端到端采集链路和全量对账在该环境下可行；跨机网络、更多 Trace 语义类型和大规模多租户仍需要继续压测。

## 2.6 Trace 采集高频追问

### Q1：为什么选择 OTel，而不是自己定义 JSON 协议？

OTel 解决了 Span 上下文传播、父子关系、Trace ID、采样和生态兼容问题。自定义 JSON 虽然开始快，但后续要自己维护 SDK、跨服务透传和工具兼容。平台保留自己的 `agentscope.*` attribute 约定，用来表达 Run、Agent、语义类型和脱敏策略；即“标准骨架 + 领域语义”。

### Q2：ClickHouse 多表写入如何保证原子性？

目前不是跨表事务。Span 表写成功、后续语义表写失败时，请求会失败，客户端重试可能造成重复。压测通过了全量对账，但这不等于实现了跨表 exactly-once。若业务要求更强保证，下一步会引入持久化 WAL/消息队列、稳定幂等键和消费端去重；当前将其作为 Trace 分析链路的明确边界。

### Q3：如何防止采集 Prompt、Token 等敏感内容？

默认优先采结构化、低敏感字段，例如模型、token 数、耗时、错误码和摘要；内容采集必须显式配置，并配合脱敏/截断。性能压测的 LLM workload 也明确 `content_captured=false`，不把真实 Prompt 当测试数据写入。

---

# 3. 实时在线观测：Outbox + Redis Streams + SSE 如何保证可恢复

对应简历：

> **实时在线观测：**针对事件投递与实时展示问题，基于 Outbox、Redis Streams 与 SSE 构建事件链路，保障**落库与投递一致**；推送 Run 状态与 Trace 语义事件，支持去重、断线重连。

## 3.1 问题：为什么不能“数据库更新后直接发 Redis”

最常见的错误流程：

```text
更新 Run 状态到 PostgreSQL
        │
        └─ 调 Redis XADD
```

中间一旦进程崩溃或 Redis 不可用，就会出现两种不一致：

1. 数据库已显示 Run `completed`，但前端永远收不到完成事件；
2. 先发 Redis 后事务失败，前端看到一个不存在或未提交的状态。

这就是经典的“双写”问题。解决目标不是魔法般做到 PostgreSQL 和 Redis 的全局强一致，而是做到：**只要业务状态已经提交，就一定有一条可恢复的待投递记录；投递允许至少一次，但消费端可去重。**

## 3.2 Outbox：把“状态变化”和“待发布事件”放进同一数据库事务

关键结构在 `migrations/00008_phase4_realtime.sql` 与 `queries/realtime.sql`：

```text
业务状态表：runs / run_state_events / agent_heartbeats / agents
                 │
                 │ 同一 PostgreSQL 事务内
                 ▼
realtime_outbox
  - id（单调递增、可作为 durable cursor）
  - project_id / run_id / agent_id / instance_id
  - event_type / payload（上限 16 KiB、默认 redacted）
  - available_at / claimed_at / published_at
  - attempts / last_error
```

当前实现通过 PostgreSQL trigger 在插入 `run_state_events`、`agents` 注册、`agent_heartbeats` 时自动写入 `realtime_outbox`。因此只要原业务事务提交，Outbox 事件也已经落库；业务代码不必在每个状态分支手工重复调用发消息函数。

事件类型包括：

```text
agent.registered / agent.heartbeat / agent.status.changed
run.started / run.state.changed / run.semantic.appended
run.completed / run.failed
ingest.accepted / ingest.rejected / eval.completed
quota.warning / quota.exceeded / operational.anomaly
```

事件只承载用于刷新 UI 的低敏感 envelope，最大 payload 16 KiB；完整 Trace 内容仍从受权限保护的详情 API 读取，避免把原始 Prompt/工具输出通过实时通道广播。

## 3.3 Relay：批量领取、发布、失败退避

后台 `Relay` 周期性执行：

```text
SELECT pending outbox
FOR UPDATE SKIP LOCKED
LIMIT batch_size
        │
        ▼
标记 claimed_at
        │
        ▼
逐条/批次发布 Redis Streams
        │                  │
        │成功              │失败
        ▼                  ▼
published_at = now      attempts + 1
claimed_at = NULL       available_at = now + 指数退避（最多 60s）
```

`SKIP LOCKED` 的意义是可以多实例并发 Relay 而不重复领取同一条 Outbox；同时，超过 30 秒未完成的 `claimed_at` 会被再次领取，防止发布进程崩溃后永久卡死。

这里的投递语义是 **at-least-once（至少一次）**：如果 Redis 实际已写入但 Relay 在标记 `published_at` 前崩溃，重试时会重复投递。因此下一层 Redis Publisher 和前端都需要幂等策略。

## 3.4 Redis Streams：项目隔离、去重与实时读取

每个项目一个 Stream，例如：

```text
agentscope:events:<project_id>
```

发布时使用 Lua 脚本把“检查去重键 → XADD → 记录 event_id 到去重 Hash → 设置 TTL”放在 Redis 内原子执行：

```text
if event_id 已发布过：返回已有 stream_id
else：XADD stream + HSET(event_id → stream_id) + EXPIRE
```

因此 Relay 重试不会把同一个 `realtime_outbox.id` 无限复制到 Redis Stream。默认 Stream 长度和去重 TTL 有配置上限，避免实时数据无限增长。

### 为什么仍保留 PostgreSQL 回放

Redis Stream 是低延迟路径，但会按 MAXLEN 裁剪，Redis 也可能重启、故障或丢失历史 cursor。因此 SSE 断线重连时，不把 Redis Stream ID 当作唯一真相；将数字 `Last-Event-ID` 解释为 **PostgreSQL Outbox 的 durable id**，优先从 `realtime_outbox` 补历史事件。

`HybridSubscriber` 的策略是：

```text
首次连接（没有 Last-Event-ID）
  → 首选 Redis 实时订阅
  → Redis 中断后切换 PostgreSQL 回放

重连（携带 Last-Event-ID）
  → 直接 PostgreSQL 按 outbox id 补事件
```

这不是“Redis 永不丢消息”，而是“Redis 的可用性不决定用户是否最终能补到已提交的状态事件”。

## 3.5 SSE：为什么选择它，如何断线重连

SSE endpoint 为项目成员建立 `text/event-stream` 连接，支持按 `run_id`、`agent_id` 和事件类型过滤。核心点：

- 后端先验证登录态与项目成员权限；
- `Last-Event-ID` 传入订阅器作为回放 cursor；
- 每 15 秒发注释型 heartbeat，避免代理/负载均衡因空闲断连；
- `X-Accel-Buffering: no` 禁止反向代理把流式响应缓冲成“任务结束才显示”；
- 每条 SSE event 使用稳定的 Outbox ID 作为 `id`，浏览器原生 EventSource 断线后会自动携带最后 ID；
- 前端在渲染层仍按事件 ID 去重，处理网络重连、页面刷新和至少一次投递。

**为什么不是 WebSocket？** 当前主要是服务端向浏览器单向推送状态和 Trace 语义事件，SSE 更简单：原生支持自动重连和 `Last-Event-ID`，HTTP 鉴权与反向代理配置更直接。若以后需要控制台反向实时控制 Agent、多人协同编辑或高频双向指令，再引入 WebSocket。

## 3.6 “落库与投递一致”到底是什么意思

这是最容易被面试官挑错的词。准确说法：

> 我保障的是 PostgreSQL 业务状态与 Outbox 记录的原子提交：业务状态一旦可见，对应事件就有持久化凭据可重试发布；Relay 至少一次投递，Redis 端基于 event ID 去重，SSE 重连可由持久化 Outbox cursor 回放。因此实现的是**可恢复的最终一致 + 幂等消费**，不是跨 PostgreSQL 与 Redis 的二阶段提交，也不是端到端 exactly-once。

## 3.7 实时观测高频追问

### Q1：Redis 宕机了会怎样？

Run 状态和 Outbox 都在 PostgreSQL，业务执行不依赖 Redis。Relay 将失败记录到 Outbox，按退避重试；已有 SSE 连接会切换到 PostgreSQL fallback 或在重连后回放。实时性会下降，但状态不会因 Redis 单点故障变成不可恢复。

### Q2：Outbox 会无限增长吗？

不会。已发布且超过保留期的事件由 janitor 分批清理，清理同样使用 `SKIP LOCKED` 降低对在线读写的影响。保留窗口和 Redis MAXLEN 共同决定可回放范围；超过保留窗口则需要从 Run/Trace 详情 API 重建当前页面状态。

### Q3：SSE 慢客户端会拖垮服务吗？

订阅链路有 channel 缓冲、连接配额以及按项目/Run 过滤；服务端对连接断开和 heartbeat 做处理。更大规模时可进一步做事件合并、只推摘要、限制每项目连接数、将 UI polling 降级为补充手段。

---

# 4. 在线评测闭环：如何从真实 Run 沉淀可用数据

对应简历：

> **在线评测闭环：**对 Agent 线上质量难量化、样本难沉淀的问题，基于自定义规则抽样真实运行，结合 Trace 自动评测；将失败或低分 Run 投递标注队列，**高价值样本回灌离线数据源**，形成闭环。

## 4.1 问题：为什么线上不能全量 LLM Judge

真实线上 Run 通常量大、成本敏感，且大部分正常样本的信息增益低。若所有 Run 都送 LLM Judge：

- 成本和延迟不可控；
- 正常样本淹没失败边界样本；
- Judge 自身噪声会被放大；
- 线上数据如果直接进入训练/回归集，容易混入隐私、错误标签和分布偏差。

因此我将线上评测拆成四层：

```text
Run 完成
  → 持久化评测 Job
  → 抽样/预算/分层选中高价值样本
  → 基于 Trace 的自动评估
  → 低分、失败或高风险样本进入人工标注
  → 审核后的 Gold 样本写入版本化离线数据源
  → 新版本 Agent 在离线集 / Benchmark 上回归
```

## 4.2 任务生成：为什么先“入队”，再做抽样

在线评测 Job 是 PostgreSQL 权威任务，关键类型位于 `internal/onlinerval/jobs.go`。一个 Job 关联：

```text
project_id + run_id + trace_id + evaluator_version
+ status + attempt_count + max_attempts
+ worker lease token / claimed_by / timestamps
+ sampling_rule_id / sampling_reason
+ evaluation_source / fallback_reason
+ annotation_item_id
```

Run 到终态后创建 `online_eval_jobs`，worker 领取后才执行抽样。这样做有两个好处：

1. 抽样决策、规则版本、跳过原因和执行结果都有持久审计，而不是在请求线程里随机决定后丢失证据；
2. 规则、预算或 evaluator 短暂异常时，可以重试或人工复跑，不影响 Agent 主请求。

Job 生命周期包括：

```text
queued → sampling → running → succeeded
                    └→ retry_wait → running
queued / sampling → skipped
running → failed / dead_letter
```

worker 通过 claim / lease token 领取任务，并周期性恢复超时 claim；失败按指数退避重试，到最大次数进入 dead letter。这与实时链路同样遵循“业务主流程和异步耗时工作解耦”。

## 4.3 抽样规则：不只是 `random < p`

抽样规则可配置在项目级或 Agent 级，包含：

- `sample_rate`：抽样率；
- `filters`：状态、Agent、时延、错误等过滤条件；
- `strategy`：`random`、`failure`、`high_cost`、`low_confidence`、`high_disagreement` 或 `combined`；
- `daily_budget`：日预算；
- `stratification`：按模型、场景、用户类型等维度的分层预算。

对于非 100% 的随机抽样，代码使用 `SHA-256(project_id, agent_id, rule_id, run_id, trace_id)` 生成稳定的 `[0,1)` 值，而不是每次 `rand()`：

- 同一个 Run 在重试时得到同一选择结果，具有可复现性；
- 对比版本时可以减少随机波动；
- 再通过数据库的 `ReserveSamplingDecision` 原子预占日预算/分层预算，避免多个 worker 并发超采。

**举例**：

```text
规则 A：失败 Run 全量采样
规则 B：成功 Run 中 5% 随机抽样
规则 C：成本高于阈值、或置信度低于阈值的 Run 全量采样
规则 D：按 model 字段分层，每种模型每日最多 N 条
```

这种组合让有限评测预算优先覆盖失败、成本异常、边界与新版本样本，而不是只得到一堆“正常完成”的数据。

## 4.4 Trace 自动评测：当前已实现什么，不能夸大什么

当前 `TraceAwareEvaluator` 从 ClickHouse 中读取 RunDetail，使用 Trace 证据计算**执行健康启发式**，例如：

- Run 是否完成；
- 是否存在最终答案；
- 是否执行过工具调用；
- Span 是否出现错误；
- Trace 中是否有可用的执行信号。

它的作用是把“Agent 运行是否健康、是否明显失败、是否缺少关键链路”量化并输出 `eval_scores`，作为线上筛选和告警证据。

必须如实说明：

> 当前 TraceAware evaluator 不是对答案事实正确性、任务语义正确性或安全性的完整判断。它解决的是“从真实运行中自动发现执行异常并筛出高价值样本”。语义质量评估需要在离线评测中结合任务规则、Rubric + LLM-as-a-Judge、人工 Gold 标注和校准集。

这句实话反而会让面试官更信任你对评测系统边界的理解。

## 4.5 评分、标注队列与回灌

worker 完成评估时，在一个原子持久化动作中完成：

```text
写入 eval_scores
+ 更新 online_eval_job 终态
+ 如果 shouldAnnotate(run, scores) 为真，创建 annotation_item
```

标注项关联 `project_id`、`run_id`、`trace_id`、分数快照与 `online_eval_job_id`，而不是由临时脚本“另造一份数据”。因此人工标注者可以回到同一个 Run 查看 Trace、工具调用与最终回答。

回灌不是把所有线上数据直接加入离线集，而是：

```text
自动筛选 → 人工判定 / 修订标签 → 脱敏与去重 → 定义 case / expected outcome
→ 生成数据集版本 → 作为 candidate / baseline 的离线回归输入
```

这能避免“线上 LLM Judge 自己打的分又被当作绝对真值”的自训练偏差。

## 4.6 在线评测高频追问

### Q1：LLM Judge 不稳定怎么办？

先区分两层：线上 Trace health 不依赖 LLM Judge；离线语义 Judge 才用 Rubric + LLM。对后者要记录 evaluator/model/prompt/rubric version、temperature、原始结构化 verdict（经脱敏）、失败原因，并使用校准集和人工 Gold 样本监控与人类的一致性。重要版本至少做多次 trial/置信区间或配对对比，而不是仅看一次平均分。

### Q2：为什么低分就直接送人工？

不会只看低分。应组合：失败状态、低分、判断不确定、评委分歧、高成本、长时延、新版本/新场景覆盖不足等信号。标注容量有限，所以要根据信息增益和风险排序。

### Q3：如何保护真实线上用户数据？

项目隔离、Project Token 权限、最小化内容采集、脱敏和截断是前置条件。进入标注和离线数据集前还应经过权限控制、敏感字段清洗、保留策略和审计；不能因为“需要数据闭环”就把生产原文无限制扩散。

---

# 5. 离线评测与 Harbor Benchmark：如何做到可信、可回归

对应简历：

> **离线评测：**针对 Agent 迭代缺少量化回归依据，实现数据集评测与 Benchmark 执行：规则匹配、LLM-as-a-Judge、多指标加权；每题以**不可变镜像沙箱执行、仅认官方评分**，输出版本回归对比。

## 5.1 把“评测”拆成两类，不混为一谈

| 类型 | 输入 | 评分方式 | 适用问题 |
|---|---|---|---|
| 普通离线数据集评测 | 输入样本、参考答案/规则、Agent 输出 | 规则匹配、LLM-as-a-Judge、多指标加权 | 文本质量、格式、事实性、偏好、业务 Rubric |
| 环境型 Benchmark | 任务镜像、初始环境、Agent 工具交互轨迹 | Harness/官方 verifier | 终端操作、代码修改、系统任务等可执行结果 |

前者适合快速低成本迭代，后者更接近真实操作能力。二者的共同点是版本化、可重复、可比较；不能因为 Agent 最后说“我完成了”就当作通过。

## 5.2 离线数据集评测：规则、Judge 与加权

一个离线 experiment 通常需要固定：

```text
数据集版本
+ Agent / Prompt / 模型版本
+ evaluator / rubric 版本
+ 指标定义及权重
+ trial 次数 / seed
+ 执行配置快照
```

### 规则匹配

适合有明确客观约束的题：JSON schema、字段存在、关键词、数值范围、正则、文件内容、单元测试结果。优点是低成本、确定性高；缺点是覆盖不了开放性质量。

### LLM-as-a-Judge

用于开放回答或复杂任务结果。正确做法不是“把问题和回答扔给模型要一个分数”，而是：

```text
定义 Rubric（维度、权重、扣分项、示例）
→ 限制 Judge 输入为任务必要证据
→ 要求结构化 JSON verdict（每维 score/reason/pass）
→ 固定 evaluator 与 rubric 版本
→ 对关键集做 Gold 校准、重复试验或多评委投票
```

### 多指标加权

示例：

```text
总分 = 0.45 × task_success
     + 0.25 × answer_quality
     + 0.15 × tool_safety
     + 0.10 × latency_score
     + 0.05 × cost_score
```

权重不是“数学上天然正确”，必须由业务目标决定。例如 coding agent 的任务成功应主导，客服 Agent 可能更重安全和用户满意度。面试中可以说：**保留每个子指标和权重版本，避免只看一个平均分掩盖成本或安全退化。**

## 5.3 Benchmark 控制面：从目录到 Job 的不可变快照

关键目录/模块：

- `internal/benchmarks/catalog.go`
- `internal/benchmarks/catalog_postgres.go`
- `internal/httpapi/benchmark_*.go`
- `migrations/00021_benchmark_control_plane.sql`
- `migrations/00022_benchmark_catalog_persistence.sql`
- `migrations/00024_benchmark_worker_registry.sql`

Benchmark catalog 描述以下不可变对象：

```text
Suite（题库、版本、默认执行配置）
Case（题目、任务环境、期望能力）
AgentArtifact（Agent 构建镜像，必须 image digest）
ModelProfile（模型/推理配置版本）
ExecutionProfile（Harbor 等执行器配置）
RunTemplate（把上述引用组合为可选择模板）
Catalog revision（整份不可变 payload）
```

创建 experiment 时，系统解析模板并把 catalog revision、镜像 digest、试次、seed、执行配置等固化到 `eval_jobs.execution_config`，而不是只保存一个“模板名”。原因是：模板明天可能更新，但历史结果必须还能回答“这 80 分究竟是在什么镜像、什么模型和什么题目版本下跑出来的”。

## 5.4 Worker、Lease、Heartbeat：Benchmark 任务如何可靠执行

评测 Job 在 PostgreSQL 中排队，worker 按项目注册并上报短租约：

```text
benchmark_workers(project_id, worker_name, execution_kinds,
                  capacity, version, last_seen_at, expires_at)
```

- worker 只领取自己项目、自己支持的执行类型任务，避免跨项目误执行；
- Job 领取使用 lease，worker 心跳续租；
- worker 崩溃或心跳过期后，任务可由其他 worker 恢复/重试；
- 用户取消 experiment 时，只取消未完成任务或撤销 lease，保留不可变执行快照和审计原因；
- 结果写回时带任务/lease 身份，防止旧 worker 把过期结果覆盖新一轮结果。

这套机制解决的是“外部 Sandbox 运行很久、worker 会挂、用户会取消”的现实问题，而不是简单的 for 循环跑题。

## 5.5 Harbor 执行链路：为什么只认 official verifier

Harbor 将环境、Agent 生命周期和 verifier 统一纳入 harness。链路是：

```text
AgentScope 创建 Benchmark eval job
        ↓
Harbor worker 领取 job，加载不可变 execution snapshot
        ↓
Harbor 创建独立 Sandbox / task environment
        ↓
启动 OpenHands 或 Hermes task-runner sidecar
        ↓
Agent 通过 Harbor Tool Gateway 使用 terminal / filesystem / git
        ↓
Harbor official verifier 检查最终环境与任务结果
        ↓
解析 verifier_result.rewards，写回 official_score / official_passed / artifact / trace
```

SDK 的 benchmark result contract 明确区分：

```text
环境错误（environment_error）
Agent/目标错误（target_error）
Harness 错误（harness_error）
Verifier 错误（verifier_error）
协议错误（benchmark_contract_error）
```

通过条件必须是：Harbor 返回可解析、有限数值的 official score 和 official passed。以下都**不能**替代官方评分：

- Agent 的最终自然语言回答；
- HTTP 200；
- task-runner CLI 退出码；
- 人工从日志看起来“似乎做对了”。

### 为什么需要不可变镜像

若任务运行时 `pip install`、`apt install` 或使用浮动 tag，今天和明天的题目环境就不同，分数不可比。当前约束是：

- Agent artifact 用固定 `sha256` image digest；
- Task/Sandbox 使用题目定义的环境；
- runtime 不允许临时安装依赖；
- 不允许 task runner 本地 fallback 绕过 Harbor；
- 凭据不进入 catalog、镜像、artifact 或日志。

这样一个 Benchmark 分数才是“可复验实验结果”，而不是一次不可重放的 Demo。

## 5.6 当前验证范围：必须诚实

当前仓库验证了 Harbor 端到端执行链路，但**没有声称已经跑完官方 Terminal-Bench 的全量大题库**。当前实际接入的是两个独立 smoke fixture：

| suite | 独立题目 | 使用模板 |
|---|---|---|
| `agentlab-harbor-smoke` | `harbor-agent-smoke` | OpenHands / Hermes 各 1 个模板 |
| `terminal-bench-2.0` | `smoke-task-001` | OpenHands / Hermes 各 1 个模板 |

因此当前是 **2 道独立 Smoke 题、4 个 Agent×题目组合、每题 1 trial**，用于证明控制面、Sandbox、真实 Agent、官方 verifier 和结果回写完整可走通。

正确表述：

> 我已实现并验证可扩展的 Benchmark 执行框架和 Harbor official-verifier 链路；当前仓库用 Smoke fixture 做端到端验证。完整接入官方 Terminal-Bench 大题库、进行更大规模多 trial 稳定性对比，是下一阶段工作，不能把 Smoke 成绩夸大成完整题库榜单。

## 5.7 离线评测 / Benchmark 高频追问

### Q1：为何要把 catalog revision 也持久化，不只存 Git commit？

Git commit 能标识代码，但评测还依赖镜像 digest、题目版本、模型配置、执行参数、trial/seed 等。catalog revision 保存的是解析前后的完整选择语义；历史 revision payload 不可改，head 指向当前可用版本。两者结合才利于复现实验。

### Q2：Agent 有随机性，怎么做版本回归？

固定可固定部分：数据集版本、镜像 digest、模型参数、seed、执行配置；对不可完全固定的模型采样，增加 trials，比较 pass rate/均分及置信区间，并用同一任务集做 paired comparison。不要只用一次运行的平均分宣布“版本提升”。

### Q3：为什么 Benchmark 和普通 Eval 共用 Job/lease 模型？

两者都需要任务排队、领取、续租、取消、重试、审计和结果回写。复用可靠的调度模型能减少基础设施重复；执行器通过 `execution_kind` 区分 in-process function、Harbor 等具体实现，避免把外部 harness 逻辑塞进 HTTP handler。

---

# 6. 真实 Agent 如何接入：可以作为延伸回答

当前路线不是给上游做“外挂插件”，而是在使用的 OpenHands / Hermes 源码的真实执行路径中直接导入 AgentScope Python SDK：

```text
Agent 初始化
  → init / heartbeat（Agent Instance 在线）
  → configure_tracing（建立 OTel Provider / exporter）
  → Conversation / agent loop 内创建 Runtime Run
  → trace_llm / trace_tool / trace_thought / record_final_answer
  → Run 完成或失败时结束 Runtime Run
  → force_flush / shutdown 时尽力上报并停止心跳
```

例如：

- Hermes 在 `hermes/agent/agent_init.py` 初始化 SDK 和 tracing，在 `hermes/agent/conversation_loop.py` 将实际 conversation 绑定为 Runtime Trace Session；
- OpenHands 有 Agent server 的 presence 生命周期和实际 SDK 调用路径，用于 Agent 在线/离线、Run 和 Trace 关联；
- 两者都不是“写一个旁路日志抓包器”，而是将埋点放在 LLM、工具执行和最终结果真实发生的位置。

面试中不用展开全部框架源码，但要抓住：**SDK 是标准化采集层；Agent 的本地业务逻辑仍由自身执行；平台只接收明确的 Runtime/Trace 契约。**

---

# 7. 5 分钟完整讲法（可直接练习）

> 我先做了统一的 Run 和 Trace 模型。一次 Agent 任务在 PostgreSQL 中有 `run_id`，Python SDK 在真实 Agent 执行路径上基于 OTel 采集 LLM、Tool、Thought 和最终答案，并把 `run_id` 关联到 Trace。SDK 不在业务线程同步写网络，而是用有界队列和后台批量 exporter；满了会记录丢失指标来保护主业务。Go 服务做项目 Token 鉴权、OTLP 解码、Run 关联和 ClickHouse 批量落库，同时把通用 Span 和 LLM/Tool 语义表分开，兼顾追溯和分析。
>
> 性能上，我针对 ClickHouse 高频小批次写入做了健康事件异步批写和 Trace 跨请求聚合。在 2026 年 7 月 25 日的单机 Docker 环境，16 并发、每请求 100 Span、三轮共 30 万 Span 的 Raw Trace 压测下，中位吞吐是 4.657k Span/s，P95 558ms，HTTP 和落库对账丢失为 0；这个结论我会限定在测试环境，不把它泛化为集群容量。
>
> 在线展示没有直接把数据库和 Redis 双写。我让 Run 状态变更和 `realtime_outbox` 在同一个 PostgreSQL 事务里提交，Relay 用 `SKIP LOCKED` 批量领取并发布 Redis Streams，失败指数退避。因为投递语义是至少一次，所以 Redis 用 event ID Lua 去重；SSE 的 Last-Event-ID 对应持久化 Outbox ID，重连时可从 PostgreSQL 回放，Redis 不可用时实时性下降但不会丢失已提交状态。
>
> 质量闭环上，Run 结束会产生持久化在线评测 Job，worker 按失败、高成本、低置信度、分歧和随机分层等规则抽样，并从 Trace 计算执行健康分数。当前自动评估用于筛选异常执行，而不是宣称能判断答案语义正确性。失败或低分样本原子地关联到人工标注队列，审核后的高价值案例再进入版本化离线数据集。离线评测结合规则、Rubric + LLM Judge 和多指标加权，最后用 Harbor 在不可变镜像 Sandbox 中跑 Agent；只接受 Harbor official verifier 的结果，确保版本回归分数可复验。

---

# 8. 简历 bullet 与证据映射

| 简历表述 | 可展开的实现证据 | 需要避免的夸大 |
|---|---|---|
| Python SDK + Go + OTel + Run 关联 | `agentscope_sdk`、OTLP ingest、`runs`、ClickHouse Trace 表 | 不说所有框架都已无侵入自动支持 |
| 有界队列、批量写入 | `ObservableBatchSpanProcessor`、export metrics、Trace 聚合、Health 异步批写 | 不说队列满仍绝不丢 Trace |
| 30 万、4.66k、P95 558ms | 2026-07-25 性能报告，16 并发、三轮中位数、Raw Trace | 不说生产集群极限或所有语义负载性能 |
| Outbox、Redis Streams、SSE | `realtime_outbox`、Relay、Lua dedupe、HybridSubscriber、Last-Event-ID | 不说跨库 exactly-once 或分布式事务 |
| 在线抽样和自动评测 | `online_eval_jobs`、sampling rule、lease、TraceAware evaluator、`eval_scores` | 不说当前 Trace heuristic 等于语义正确性 Judge |
| 标注回灌 | `annotation_items` 与 Run/Trace/online job 关联、版本化离线数据集 | 不说所有线上原始数据直接可用于训练 |
| Sandbox + 官方评分 | Harbor execution、image digest、official verifier result | 不说已经全量跑通官方 Terminal-Bench 大题库 |

---

# 9. 代码与材料导航

| 主题 | 主要位置 |
|---|---|
| SDK Trace / 有界批处理 | `python/agentscope_sdk/tracing.py`、`python/agentscope_sdk/export_metrics.py`、`python/agentscope_sdk/runtime_trace.py` |
| OTLP 接收与 Trace 存储 | `internal/ingest/otlp.go`、`internal/adapter/tracestore.go`、`internal/tracestore/`、`clickhouse/` |
| 采集压测证据 | `docs/otlp-ingest-performance-report.md` |
| 实时事件 | `migrations/00008_phase4_realtime.sql`、`queries/realtime.sql`、`internal/realtime/`、`internal/httpapi/realtime_handlers.go` |
| 在线评测 | `internal/onlinerval/`、`internal/sampling/`、`internal/adapter/runtime_online_eval.go`、`migrations/00018_online_eval_jobs.sql` |
| 标注队列 | `queries/annotation_queues.sql`、相关 migrations / adapters |
| Benchmark 控制面 | `internal/benchmarks/`、`internal/httpapi/benchmark_*.go`、`migrations/00021`～`00024` |
| Harbor 结果契约 | `python/agentscope_sdk/executors.py`、`python/agentscope_sdk/harbor.py`、`python/agentscope_sdk/runner.py` |
| Agent 源码接入 | `docs/integrations/openhands.md`、`docs/integrations/hermes-agent.md`、`openhands/`、`hermes/` |

---

# 10. 最后提醒：面试时的表达原则

1. **先讲问题，再讲组件**：不要上来堆 OTel、Redis、ClickHouse；先说明“同步采集会拖慢 Agent”“双写会丢实时事件”“全量 Judge 成本失控”“自然语言自报完成不可信”。
2. **说清一致性级别**：Outbox = DB 内原子 + 至少一次投递 + 幂等；不是全链路 exactly-once。
3. **说清数据分级**：Run/Job/评测结果是业务事实，Trace 是高频分析证据，Redis 是可重建的实时投影。
4. **说清量化口径**：4.66k Span/s 与 P95 558ms必须带上单机、16 并发、30 万 Span、三轮中位数的限定。
5. **说清当前边界**：Trace 自动评测当前是执行健康筛选；Harbor 当前是 Smoke 链路验证，不是全量官方题库排行榜。
6. **最后落业务价值**：平台真正的价值是缩短 Agent 问题定位与迭代验证的周期，并把线上失败案例沉淀成可复用的离线回归资产。
