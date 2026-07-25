# AgentScope OTLP Trace 与 LLM 语义采集性能报告

测试日期：2026-07-25（Asia/Shanghai）  
测试分支：`perf/otlp-ingest-batching`  
未优化基线：`921b2f4`  
最终优化实现：`8b7ec27`  
LLM 压测工具：`bed89f7`

## 1. 测试结论

本次测试覆盖两类负载：

1. Raw OTLP Trace：只写原始 Span，用于测量 OTLP 解码、鉴权、Run 关联和 ClickHouse Span 落库能力；
2. LLM Semantic：每个 Span 都携带完整 LLM 语义字段，同时写入 `trace_spans` 和 `trace_llm_calls`，用于测量高密度 LLM 语义提取与双表落库能力。

在固定 16 并发、每请求 100 Span、每轮 10 万 Span、三轮取中位数的条件下：

| 工作负载 | 版本 | 吞吐量 | P50 | P95 | P99 | HTTP 错误率 | Span 落库丢失率 | 语义落库丢失率 |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| Raw Trace | 未优化基线 | 2,223.27 Span/s | 709.65 ms | 971.87 ms | 1,138.75 ms | 0 | 0 | 不适用 |
| Raw Trace | 最终优化 | 4,657.27 Span/s | 312.33 ms | 558.39 ms | 627.79 ms | 0 | 0 | 不适用 |
| LLM Semantic | 未优化基线 | 1,712.44 Span/s | 933.32 ms | 1,165.23 ms | 1,258.37 ms | 0 | 0 | 0 |
| LLM Semantic | 最终优化 | 2,680.58 Span/s | 562.08 ms | 866.91 ms | 1,007.93 ms | 0 | 0 | 0 |

最终优化相对未优化基线：

| 工作负载 | 吞吐提升 | P50 下降 | P95 下降 | P99 下降 |
|---|---:|---:|---:|---:|
| Raw Trace | 109.48% | 55.99% | 42.55% | 44.87% |
| LLM Semantic | 56.54% | 39.78% | 25.60% | 19.90% |

LLM Semantic 在优化后的 16 并发标准档位达到约 **2.68k LLM Span/s**。扩展性测试中，64 并发达到约 **5.79k LLM Span/s**，但 P95 上升到约 **1.43s**，因此它属于吞吐优先档，不作为默认低延迟指标。

## 2. 测试环境

| 项目 | 配置 |
|---|---|
| CPU | Intel Core i5-1035G1，4 核 / 8 线程 |
| 内存 | 15 GiB |
| Go | go1.26.5 linux/amd64 |
| 后端 | 单个 AgentScope Go 进程 |
| PostgreSQL | postgres:16.14 |
| ClickHouse | clickhouse/clickhouse-server:26.6.1.1193 |
| Redis | redis:7.4-alpine |
| 部署方式 | 本地 Docker，容器未显式设置 CPU/内存限制 |

测试使用独立数据库和独立项目，不复用主服务业务数据：

- 未优化 LLM 基线：`agentscope_llm_baseline_test`；
- 最终优化 LLM：`agentscope_llm_optimized_test`；
- Redis 使用对应的独立 namespace；
- 临时后端监听 `127.0.0.1:18080` 或 `127.0.0.1:18081`；
- Runtime 和 SSE 在正式 OTLP 性能测试中关闭；
- 测试结束后删除隔离数据库、Redis key、目标凭证文件、临时二进制和临时 worktree。

## 3. 负载模型

### 3.1 通用请求模型

```text
每个 OTLP 请求：100 Span
一个请求：一个 trace_id
一个请求：一个 agentscope.run.id
批内 Span：span_id 唯一
正式轮次：3
每轮请求数：1,000
每轮 Span 数：100,000
正式验证总量：300,000 Span
并发数：16
HTTP 超时：30s
非重叠随机种子：每轮独立
```

该模型修复了旧压测工具“同一请求内每个 Span 使用不同 Trace/Run ID”的语义问题，使请求级 Run 关联和数据库计数可以进行强校验。

### 3.2 Raw Trace

Raw Span 只包含 Run、Agent 和环境维度，不包含 `agentscope.span.kind`，因此不会生成结构化语义记录。它主要覆盖：

```text
HTTP OTLP 接收
→ Protobuf 解码
→ Token/项目鉴权
→ PostgreSQL Run 查询或创建
→ 原始 Span 标准化
→ ClickHouse trace_spans 写入
→ Ingest Health 记录
```

### 3.3 LLM Semantic

LLM 工作负载中的每个 Span 都包含：

```text
agentscope.span.kind = LLM
llm.provider = openai
llm.model = load-model
llm.input_tokens = 128
llm.output_tokens = 32
llm.total_tokens = 160
llm.cache_read_tokens = 16
llm.cache_write_tokens = 0
llm.ttft_ms = 12
llm.estimated_cost = 0.00016
llm.cost_source = estimated
llm.input_summary = redacted load prompt
llm.output_summary = redacted load response
agentscope.content_captured = false
```

服务端必须通过生产代码 `ExtractStructuredCalls` 将每个 Span 提取为一条 `LLMCallRecord`。因此 LLM 请求链路为：

```text
HTTP OTLP 接收
→ Protobuf 解码
→ Token/项目鉴权
→ PostgreSQL Run 查询或创建
→ Attributes JSON 解析
→ LLM 语义识别与字段提取
→ ClickHouse trace_spans 写入
→ ClickHouse trace_llm_calls 写入
→ Ingest Health 记录
```

LLM 冒烟验证发送 1,000 Span 后得到：

```text
trace_llm_calls                  1,000
唯一 trace_id                       10
唯一 run_id                         10
input_tokens 汇总              128,000
output_tokens 汇总              32,000
total_tokens 汇总              160,000
provider 唯一值                  openai
model 唯一值                 load-model
content_captured=true 数量            0
```

这证明测试流量真实经过 LLM 语义提取和语义表写入，并且没有采集原始 Prompt/Response 内容。

## 4. 优化内容

### 4.1 阶段一：Health Event 异步批写

未优化版本在每个 OTLP 请求结束前同步向 ClickHouse 写入一条 Health Event。优化后，Handler 仅将 Health Event 非阻塞写入有界队列，后台按数量或时间批量刷新。

默认参数：

```text
Health Queue Capacity = 4096
Health Batch Size = 500
Health Flush Interval = 200ms
Health Write Timeout = 5s
```

队列记录 enqueued、written、dropped、failed 和 batches 指标；队列满时不会阻塞 Trace 接收；关闭时会排空队列。

### 4.2 阶段二：Trace 跨请求聚合

阶段二将多个请求级 `TraceBatch` 合并后写入 ClickHouse：

```text
多个 OTLP 请求
       ↓
有界 Trace Queue
       ↓
8 个 Batch Worker
       ↓
合并 Span / Event / LLM / Tool / Retrieval / Skill / Thought / Final Answer
       ↓
批量写入 ClickHouse
       ↓
写入成功后通知该批次的所有 HTTP 请求
```

默认参数：

```text
Trace Queue Capacity = 1024 请求
Max Batch Spans = 1000
Trace Flush Interval = 10ms
Trace Write Timeout = 30s
Trace Workers = 8
```

请求不是“进入内存队列就返回成功”，而是等待所属批次完成底层写入。因此仍然保持：

```text
HTTP 2xx ⇒ 请求所属 Trace 批次已经成功写入 ClickHouse
```

如果合并批次写入失败，同一批次中的所有请求都会收到失败结果。

### 4.3 有序关闭

应用关闭顺序调整为：

```text
停止接受新请求
→ 等待活跃 Handler 完成
→ 排空 Trace Writer
→ 排空 Health Writer
→ 关闭 PostgreSQL 和 ClickHouse
```

避免进程在后台批次尚未落库时提前退出。

## 5. Raw Trace 正式结果

### 5.1 未优化基线

| 轮次 | 吞吐量 | P50 | P95 | P99 |
|---:|---:|---:|---:|---:|
| 1 | 2,200.80 Span/s | 715.78 ms | 1,025.07 ms | 1,159.45 ms |
| 2 | 2,223.27 Span/s | 709.65 ms | 960.94 ms | 1,137.86 ms |
| 3 | 2,234.80 Span/s | 708.25 ms | 971.87 ms | 1,138.75 ms |
| 中位数 | **2,223.27 Span/s** | **709.65 ms** | **971.87 ms** | **1,138.75 ms** |

### 5.2 最终优化

| 轮次 | 吞吐量 | P50 | P95 | P99 |
|---:|---:|---:|---:|---:|
| 1 | 4,650.24 Span/s | 314.90 ms | 558.39 ms | 624.32 ms |
| 2 | 4,693.10 Span/s | 310.07 ms | 567.73 ms | 627.79 ms |
| 3 | 4,657.27 Span/s | 312.33 ms | 552.01 ms | 635.29 ms |
| 中位数 | **4,657.27 Span/s** | **312.33 ms** | **558.39 ms** | **627.79 ms** |

正式测试中 300,000 Span 全部落库，HTTP 错误数和 ClickHouse 丢失数均为 0。

## 6. LLM Semantic 并发矩阵

优化版每档发送 300 请求，即 30,000 LLM Span：

| 并发数 | 吞吐量 | P50 | P95 | P99 | HTTP 错误率 | 语义丢失率 |
|---:|---:|---:|---:|---:|---:|---:|
| 4 | 982.29 Span/s | 491.11 ms | 522.44 ms | 535.37 ms | 0 | 0 |
| 8 | 1,651.47 Span/s | 518.90 ms | 572.30 ms | 583.63 ms | 0 | 0 |
| 16 | 2,701.47 Span/s | 554.56 ms | 921.08 ms | 1,025.72 ms | 0 | 0 |
| 32 | 3,934.34 Span/s | 714.19 ms | 1,210.88 ms | 1,242.73 ms | 0 | 0 |
| 48 | 4,586.20 Span/s | 966.29 ms | 1,326.48 ms | 1,364.86 ms | 0 | 0 |
| 64 | 5,792.42 Span/s | 1,085.60 ms | 1,434.56 ms | 1,512.50 ms | 0 | 0 |

观察：

- 4 到 64 并发范围内吞吐持续增长，测试尚未观察到错误或落库丢失；
- 32 并发以后，吞吐提升伴随明显排队延迟；
- 16 并发适合作为与 Raw Trace 对比的标准档；
- 64 并发是本次测试的吞吐优先档，不代表推荐生产默认值；
- 若生产目标要求 P95 小于 1 秒，本机更合适的并发范围是 16 左右。

## 7. LLM Semantic 正式结果

### 7.1 未优化基线

| 轮次 | 吞吐量 | P50 | P95 | P99 |
|---:|---:|---:|---:|---:|
| 1 | 1,746.82 Span/s | 917.44 ms | 1,165.23 ms | 1,245.75 ms |
| 2 | 1,712.44 Span/s | 934.33 ms | 1,164.64 ms | 1,258.37 ms |
| 3 | 1,696.22 Span/s | 933.32 ms | 1,192.57 ms | 1,269.54 ms |
| 中位数 | **1,712.44 Span/s** | **933.32 ms** | **1,165.23 ms** | **1,258.37 ms** |

### 7.2 最终优化

| 轮次 | 吞吐量 | P50 | P95 | P99 |
|---:|---:|---:|---:|---:|
| 1 | 2,680.58 Span/s | 562.08 ms | 883.72 ms | 1,007.93 ms |
| 2 | 2,755.57 Span/s | 539.81 ms | 852.38 ms | 953.91 ms |
| 3 | 2,671.00 Span/s | 567.39 ms | 866.91 ms | 1,031.62 ms |
| 中位数 | **2,680.58 Span/s** | **562.08 ms** | **866.91 ms** | **1,007.93 ms** |

### 7.3 提升比例

| 指标 | 基线 | 优化后 | 变化 |
|---|---:|---:|---:|
| 吞吐量 | 1,712.44 Span/s | 2,680.58 Span/s | **+56.54%** |
| P50 | 933.32 ms | 562.08 ms | **-39.78%** |
| P95 | 1,165.23 ms | 866.91 ms | **-25.60%** |
| P99 | 1,258.37 ms | 1,007.93 ms | **-19.90%** |

## 8. 数据完整性验证

未优化和最终优化正式轮次均满足：

```text
发送请求                   3,000
发送 Span                300,000
ClickHouse Span 增量     300,000
ClickHouse LLM Call 增量 300,000
PostgreSQL Run 增量        3,000
ClickHouse Health 增量     3,000
HTTP 错误                      0
Span 落库丢失                  0
LLM Call 落库丢失              0
```

最终优化版关闭时的内部统计：

```text
Trace:
  enqueued_requests = 3000
  written_requests  = 3000
  failed_requests   = 0
  written_spans     = 300000
  flushes           = 1888

Health:
  enqueued = 3000
  written  = 3000
  dropped  = 0
  failed   = 0
  batches  = 458
```

## 9. ClickHouse Insert 分析

### 9.1 Insert 次数

| 写入目标 | 未优化 | 优化后 | 减少比例 |
|---|---:|---:|---:|
| `trace_spans` | 3,000 | 1,888 | 37.07% |
| `trace_llm_calls` | 3,000 | 1,888 | 37.07% |
| `trace_ingest_health_events` | 3,000 | 458 | 84.73% |
| 总计 | 9,000 | 4,234 | **52.96%** |

### 9.2 `system.query_log` 耗时

| 版本 | 表 | Insert 次数 | 写入行数 | 平均耗时 | P50 | P95 |
|---|---|---:|---:|---:|---:|---:|
| 未优化 | `trace_spans` | 3,011 | 301,001 | 209.62 ms | 233 ms | 291 ms |
| 未优化 | `trace_llm_calls` | 3,010 | 301,000 | 168.43 ms | 193 ms | 234 ms |
| 未优化 | `trace_ingest_health_events` | 3,011 | 3,011 | 178.92 ms | 202 ms | 251 ms |
| 优化后正式轮次 | `trace_spans` | 1,888 | 300,000 | 191.96 ms | 193 ms | 301 ms |
| 优化后正式轮次 | `trace_llm_calls` | 1,888 | 300,000 | 165.56 ms | 192 ms | 234 ms |
| 优化后正式轮次 | `trace_ingest_health_events` | 458 | 3,000 | 83.61 ms | 86 ms | 139 ms |

未优化查询日志包含 1 个 provisioning 请求和 10 个 LLM 冒烟请求，因此比正式轮次多 11 次 Span/Health Insert 和 10 次 LLM Insert；正式增量通过前后表计数单独验证为 3,000/3,000/3,000。

这些数据说明：

- 单次 Span 或 LLM Insert 的耗时没有数量级下降；
- 主要收益来自减少 Insert 次数、摊薄固定开销并允许多个聚合 worker 并发写入；
- Health Event 从同步单行写变成异步批写后，既减少请求关键路径耗时，也显著降低 Insert 数量；
- LLM 路径需要顺序写 Span 和 LLM 两张表，因此相同 16 并发下仍比 Raw 路径吞吐低 42.44%，P95 高 55.25%。

## 10. 可复现命令

目标文件只需要包含隔离项目的 `project_id` 和具有 `otlp:write` 权限的 Token。示例：

```bash
go run ./cmd/agentscope-loadtest \
  --server-url http://127.0.0.1:18081 \
  --targets /path/to/isolated-targets.json \
  --requests 1000 \
  --concurrency 16 \
  --batch-size 100 \
  --workload llm \
  --seed 910000 \
  --http-timeout 30s \
  --output terminal
```

三轮测试必须使用不重叠 seed：

```bash
for seed in 910000 920000 930000; do
  go run ./cmd/agentscope-loadtest \
    --server-url http://127.0.0.1:18081 \
    --targets /path/to/isolated-targets.json \
    --requests 1000 \
    --concurrency 16 \
    --batch-size 100 \
    --workload llm \
    --seed "$seed" \
    --http-timeout 30s \
    --output terminal
done
```

测试前后至少校验：

```sql
-- ClickHouse
SELECT count() FROM trace_spans;
SELECT count() FROM trace_llm_calls;
SELECT count() FROM trace_ingest_health_events;

-- PostgreSQL
SELECT count(*) FROM runs;
```

## 11. 结果边界

1. 本次 LLM Semantic 是高密度压力模型：100 个 Span 全部是 LLM Span。真实 Agent Run 通常还包含 Thought、Tool、Retrieval 和普通内部 Span，因此实际结构化记录比例可能更低；
2. 测试覆盖 LLM 语义提取与 `trace_spans`、`trace_llm_calls` 双表写入，没有同时混入 Tool/Retrieval/Thought 等更多语义表；
3. 测试运行在单机 Docker 环境，没有网络跨机延迟，也没有设置容器资源上限；
4. 64 并发结果用于展示扩展性，不应与 16 并发正式中位数混为同一 SLO；
5. ClickHouse 多表写入不是跨表事务。若 Span 表写成功而后续语义表写失败，请求会返回失败，客户端重试可能带来重复 Span；如需严格跨表原子性，需要引入持久化日志或消息队列并设计幂等键；
6. Health Event 是辅助运行证据，采用有界内存队列和最终一致写入；进程在优雅关闭时会排空，但强制崩溃仍可能丢失尚未刷新的 Health Event；
7. 当前压测数字适合描述“单机 OTLP Trace/LLM 语义采集能力”，不应表述为分布式集群容量或完整 Agent 多语义链路上限。

## 12. 简历建议

如果简历只保留一个采集点，建议使用覆盖范围更完整但不过度堆数字的版本：

```latex
\item \textbf{Trace 采集：}基于 OpenTelemetry/OTLP 实现 Python SDK 与 Go 采集服务，通过有界队列、Health 事件异步批写和 Trace 跨请求聚合降低 ClickHouse 高频小批次写入开销；在单机环境下完成 \textbf{30 万 Span} 端到端验证，Raw Trace 三轮中位吞吐达到 \textbf{4.66k Span/s}，较基线提升约 \textbf{109\%}，并完成高密度 LLM 语义双表落库测试，HTTP 错误率和数据丢失率均为 \textbf{0}
```

若面试官追问 LLM 语义性能，可以补充：

```text
16 并发、100 LLM Span/请求下：
未优化 1.71k Span/s → 优化后 2.68k Span/s，提升 56.54%；
P95 由 1.17s 降至 0.87s；
30 万 Span、30 万 LLM Call、3,000 Run、3,000 Health 全量对账，丢失为 0。
```
