# Phase 2.7: 高并发采集链路

## 概述

Phase 2.7 将 AgentScope 的 trace ingest 链路从 demo 级升级到生产可用级别，覆盖 Python SDK 批量导出、gzip 压缩、服务端限流、ClickHouse 存储优化和采集健康可视化。

## 架构

```
Python SDK                               Go Backend
┌──────────────────┐                    ┌────────────────────────┐
│ BatchProcessor   │  gzip OTLP/HTTP    │                        │
│ + RedactingProc  │ ──────────────────▶│  gzip Decompress       │
│                  │                    │  Body Size Limit (10MB)│
│ AGENTSCOPE_*     │                    │  Span Count Limit(5000)│
│ env 配置         │                    │  Project Rate Limiter  │
│ capture_content  │                    │  TraceBatchWriter 抽象 │
│ enable_redaction │                    │  IngestStats API       │
└──────────────────┘                    └───────┬────────────────┘
                                                │
                                                ▼
                                        ┌──────────────────┐
                                        │   ClickHouse     │
                                        │  TTL 30 天       │
                                        │  聚合查询        │
                                        └──────────────────┘
```

## 1. Python SDK 批量配置

### 环境变量

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `AGENTSCOPE_EXPORT_BATCH_SIZE` | `512` | 每批最大 span 数。设为 `0` 或 `1` 启用 SimpleSpanProcessor（同步发送） |
| `AGENTSCOPE_EXPORT_DELAY_MS` | `5000` | BatchProcessor 调度延迟（毫秒） |
| `AGENTSCOPE_EXPORT_TIMEOUT_MS` | `30000` | 导出超时（毫秒） |
| `AGENTSCOPE_EXPORT_COMPRESSION` | `gzip` | 压缩算法：`gzip` 或 `none` |
| `AGENTSCOPE_CAPTURE_CONTENT` | `false` | 是否捕获完整 LLM 输入输出内容 |
| `AGENTSCOPE_SESSION_SECRET` | - | 服务端 Session 密钥（必需） |
| `AGENTSCOPE_INGEST_MAX_BODY_SIZE` | `10485760` | 服务端请求 body 上限（字节） |
| `AGENTSCOPE_INGEST_MAX_SPANS` | `5000` | 服务端单次请求 span 数上限 |

### Python SDK 使用示例

```python
from agentscope_sdk.tracing import configure_tracing, shutdown_tracing

# 使用默认 BatchSpanProcessor（推荐生产用）
tracer = configure_tracing(
    server_url="http://127.0.0.1:8080",
    project_token="ascope_your_token",
    service_name="my-agent",
)

# 可以覆盖 batch 参数
tracer = configure_tracing(
    server_url="http://127.0.0.1:8080",
    project_token="ascope_your_token",
    export_batch_size=256,
    export_delay_ms=2000,
    export_timeout_ms=15000,
    export_compression="gzip",
)

# 使用 SimpleSpanProcessor（调试用，每 span 立即发送）
tracer = configure_tracing(
    server_url="http://127.0.0.1:8080",
    project_token="ascope_your_token",
    export_batch_size=0,
)

# 关闭时 flush 所有待发送 span
shutdown_tracing()

# 主动 flush
from agentscope_sdk.tracing import force_flush
force_flush()
```

### Deep Trace 示例

```python
from agentscope_sdk.tracing import trace_llm, trace_tool

with trace_llm(
    tracer, "llm-call",
    provider="anthropic",
    model="claude-3-opus",
    input_summary="用户提问",
    output_summary="回答内容",
    input_tokens=100,
    output_tokens=200,
    cost=0.003,
) as span:
    # agent 发起 LLM 调用...
    pass

with trace_tool(
    tracer, "search_order",
    tool_type="api",
    arguments_summary='{"order_id": "12345"}',
    result_summary="找到订单",
    success=True,
) as span:
    # agent 调用工具...
    pass
```

### 数据脱敏

Python SDK 内置双层脱敏机制，确保敏感信息不会泄露到 ClickHouse：

**第一层：RedactingSpanProcessor（on_start）**

`RedactingSpanProcessor` 包装了 `BatchSpanProcessor`，在 `on_start` 回调中扫描 span 所有已有 attribute，对敏感 key（`api_key`、`token`、`password`、`authorization` 等）替换为 `"[REDACTED]"`，同时对字符串值中的 PII 模式（Email、电话、身份证号）进行模糊化。

**第二层：set_redacted_attribute（在设置时脱敏）**

所有 deep trace helper（`trace_llm`、`trace_tool`、`trace_retrieval`、`trace_skill`）内部使用 `set_redacted_attribute` 替代原始的 `span.set_attribute`。该函数在属性设置时就执行脱敏，`**attrs` 中的任意自定义 key-value 也会经过脱敏处理。

```python
# trace_llm 所有参数自动脱敏 — api_key 会被替换
with trace_llm(tracer, "call", provider="openai",
               api_key="sk-abc123",
               input_summary="user@example.com") as span:
    pass
```

**脱敏边界（重要）：**

| 场景 | 是否脱敏 | 说明 |
|------|---------|------|
| `trace_llm(..., api_key="sk-xxx")` | ✅ 自动脱敏 | `set_redacted_attribute` 处理了 `**attrs` |
| `trace_llm(..., input_summary="user@example.com")` | ✅ 自动脱敏 | PII 正则匹配后在字符串值中模糊化 |
| `span.set_attribute("raw_key", "secret")` | ❌ 不保证脱敏 | 绕过 `set_redacted_attribute`，仅受 `on_start` 保护 |
| `span.set_attribute("api_key", "sk-xxx")` | ✅ `on_start` 脱敏 | RedactingSpanProcessor 在 `on_start` 扫描全量 attribute |
| `RedactingSpanProcessor` 未加载 | ❌ 不脱敏 | `enable_redaction=False` 时两层脱敏均关闭 |

**推荐做法：** 始终使用 deep trace helper 的 `**attrs` 参数注入自定义属性，或手动调用 `set_redacted_attribute(span, key, value)` 函数。不要直接调用 `span.set_attribute`。

```python
from agentscope_sdk.tracing import set_redacted_attribute

with tracer.start_as_current_span("custom") as span:
    set_redacted_attribute(span, "my_key", "sensitive_value")  # ✅ 自动脱敏
```

## 2. Gzip Ingest

Python SDK 默认启用 gzip 压缩（`export_compression="gzip"`）。

服务端自动检测 `Content-Encoding: gzip` 并解压。解压后仍然执行 body size limit（默认 10MB）。

### curl 示例

```bash
# 使用 gzip 压缩的 OTLP payload
curl -X POST http://localhost:8080/v1/traces \
  -H "Authorization: Bearer ascope_your_token" \
  -H "Content-Encoding: gzip" \
  -H "Content-Type: application/x-protobuf" \
  --data-binary @trace_payload.gz

# 不压缩的请求
curl -X POST http://localhost:8080/v1/traces \
  -H "Authorization: Bearer ascope_your_token" \
  -H "Content-Type: application/x-protobuf" \
  --data-binary @trace_payload.bin
```

## 3. 请求限制

### Body Size Limit

默认 10MB。超过返回 413 Payload Too Large。

环境变量：`AGENTSCOPE_INGEST_MAX_BODY_SIZE`（默认 `10485760`）

### Span Count Limit

默认 5000。超过返回 413 Payload Too Large。

环境变量：`AGENTSCOPE_INGEST_MAX_SPANS`（默认 `5000`）

### Rate Limit / Quota

基于 token bucket 算法的项目级限流。默认禁用（向后兼容）。

启用后对 `/v1/traces` 生效。超出返回 429 Too Many Requests。

| 环境变量 | 默认值 | 说明 |
|----------|--------|------|
| `AGENTSCOPE_INGEST_RATE_LIMIT_ENABLED` | `false` | 启用限流 |
| `AGENTSCOPE_INGEST_RATE_LIMIT_CAPACITY` | `100` | bucket 容量 |
| `AGENTSCOPE_INGEST_RATE_LIMIT_REFILL` | `100` | 每次 refill 补充 token 数 |
| `AGENTSCOPE_INGEST_RATE_LIMIT_INTERVAL` | `1s` | refill 间隔 |

> **注意：** 限流 key 为 projectID，不同项目之间隔离。

## 4. ClickHouse TTL

所有 trace 表（`trace_spans`、`trace_span_events`、`trace_llm_calls`、`trace_tool_calls`、`trace_retrieval_calls`、`trace_skill_calls`）默认保留 **30 天**。

新增 `trace_ingest_stats` 汇总表（SummingMergeTree）保留 **90 天**。

TTL 写死在 migration `clickhouse/migrations/00002_ttl_aggregation.sql`，后续可通过配置中心化控制。

```sql
-- 手动修改 TTL 示例
ALTER TABLE trace_spans MODIFY TTL start_time + INTERVAL 90 DAY;
```

## 5. 聚合指标含义

| 指标 | 来源 | 说明 |
|------|------|------|
| span_count | `trace_spans` | 时间窗口内 span 总数 |
| error_count | `trace_spans` | status_code = 'ERROR' 的 span 数 |
| event_count | `trace_span_events` | span event 总数 |
| llm_count | `trace_llm_calls` | LLM 调用次数 |
| tool_count | `trace_tool_calls` | 工具调用次数 |
| retrieval_count | `trace_retrieval_calls` | 知识库检索次数 |
| skill_count | `trace_skill_calls` | Skill 调用次数 |
| total_tokens | `trace_llm_calls` | 输入+输出 token 总数 |
| total_cost | `trace_llm_calls` | LLM 调用总费用 |
| p95_latency | `trace_spans` | P95 延迟（按 agent 分组） |

## 6. API 端点

### 采集健康统计

```
GET /api/projects/{projectID}/ingest-stats?minutes=15
GET /api/projects/{projectID}/ingest-stats/agents?minutes=15
```

需要 session cookie（用户认证）。`minutes` 可选，默认 15，可选值 5/15/60。

### 返回格式

```json
{
  "project_id": "uuid",
  "minutes": 15,
  "collected_at": "2026-07-19T12:00:00Z",
  "spans": 1234,
  "errors": 5,
  "llms": 100,
  "tools": 50,
  "tokens": 50000,
  "cost": 0.15
}
```

## 7. 本地验证

### 启动服务

```bash
make db-up          # 启动 PostgreSQL 和 ClickHouse
make server         # 启动 Go 服务端
```

### 验证 API

```bash
# 健康检查
curl http://localhost:8080/healthz

# 注册用户
curl -X POST http://localhost:8080/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"email":"test@test.com","password":"test1234","display_name":"测试用户"}'

# 创建项目
# ... (登录后获取 cookie)

# 创建 token
# ...

# 发送 trace（gzip 压缩示例）
echo "..." | gzip | curl -X POST http://localhost:8080/v1/traces \
  -H "Authorization: Bearer ascope_xxxx" \
  -H "Content-Encoding: gzip" \
  -H "Content-Type: application/x-protobuf" \
  --data-binary @-
```

### 运行测试

```bash
# Go 测试
go test ./... -count=1
go test -race ./internal/httpapi ./internal/ingest ./internal/tracestore -count=1
go vet ./...

# Python 测试
cd python && python -m pytest -q

# 前端构建（若 node_modules 已安装）
cd web && npm test -- --run && npm run build

# 检查空白字符
git diff --check
```

## 8. 压测方式

建议使用 [vegeta](https://github.com/tsenart/vegeta) 或 [wrk](https://github.com/wg/wrk) 对 `/v1/traces` 端点进行压测。

```bash
# vegeta 示例（需安装 vegeta）
echo "POST http://localhost:8080/v1/traces" | \
  vegeta attack -rate=100 -duration=10s \
  -header "Authorization: Bearer ascope_test" \
  -header "Content-Type: application/x-protobuf" \
  -body ./test_trace.bin | vegeta report
```

压测关注指标：
- 请求成功率（预期 99%+）
- P95/P99 延迟
- 429 限流触发率
- ClickHouse 写入吞吐

## 9. 遗留问题和下一步建议

### 已知限制

1. **TraceBatchWriter** 仅实现了 ClickHouse 直接写入，未配置 Kafka/NATS 后端。
2. **RateLimiter** 基于内存 token bucket，重启丢失状态。生产环境建议使用 Redis 等外部存储。
3. **前端** 采集健康仪表盘页面的样式可能需要与设计系统对齐。由于 `node_modules` 在 worktree 中未安装，无法执行 TypeScript 类型检查。
4. **TTL** 天数写死在 migration SQL，未通过配置中心下放。

### 下一步建议

1. **Kafka/NATS 后端**：实现 `TraceBatchWriter` 的消息队列变体。
2. **外部限流**：将 token bucket 后端改为 Redis，支持分布式限流。
3. **多 writer 路由**：支持同时写入 ClickHouse + 消息队列的扇出模式。
4. **前端的正式设计**：将 IngestDashboard 与项目主页的侧边栏导航整合。
