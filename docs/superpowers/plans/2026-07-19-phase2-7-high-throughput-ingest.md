# Phase 2.7 高并发采集链路实现计划

> **For agentic workers:** 每个 Task 使用 subagent-driven 或 TDD 方式实现，提交前执行验证命令。

**目标:** 将现有 SDK/ingest 从 demo 级升级到生产可用级别，支持批量、压缩、限流、背压、ClickHouse 查询优化。

**架构:**
- Python SDK: BatchSpanProcessor + 配置化导出 + 敏感字段脱敏
- Go 后端 ingest: gzip 解压 + body/span 限制 + 项目级 token bucket 限流 + IngestWriter 抽象
- ClickHouse: TTL + 聚合查询 + materialized view
- 前端: 采集健康中文 dashboard

**Tech Stack:** Go + Chi + ClickHouse + Python OTel SDK + React + Ant Design

## 全局约束

- 不要引入 Kafka/NATS（留抽象即可）
- 不要破坏现有 demo、runner、online eval、annotation queue
- ClickHouse migration 必须是幂等的（IF NOT EXISTS）
- 所有 handler 错误不能暴露内部路径
- Python SDK 兼容现有配置（新配置项默认值 = 旧行为）
- 前端页面保持中文

---

### Task 1: 基线测试 — 为高并发 ingest 增加测试覆盖

**说明：** 在修改代码前，先写测试覆盖需要测试的行为，确保后续实现的正确性。

**Files:**
- Modify: `internal/httpapi/ingest_handlers_test.go`
- Modify: `internal/ingest/otlp_test.go`
- Create: `internal/ingest/ratelimit_test.go`
- Modify: `internal/tracestore/clickhouse_test.go`
- Create: `python/tests/test_tracing_batch.py`

- [ ] **Step 1: 编写 ingest handler 测试（gzip、body limit、span limit）**

```go
// internal/httpapi/ingest_handlers_test.go — 追加测试
func TestIngestGzipBody(t *testing.T) {
    // 验证 Content-Encoding: gzip 能被正确处理
    // 验证 gzip 解压失败返回 400
    // 验证 body > 10MB 返回 413
}
func TestIngestMaxSpans(t *testing.T) {
    // 验证超过 maxSpans 的请求被拒绝
}
```

- [ ] **Step 2: 编写 ratelimit 测试**

```go
// internal/ingest/ratelimit_test.go
func TestTokenBucketBasic(t *testing.T) {
    // 验证 token bucket 的基本获取和等待
}
func TestTokenBucketRefill(t *testing.T) {
    // 验证 token 按速率补充
}
func TestProjectRateLimiter(t *testing.T) {
    // 验证不同 project 的隔离
    // 验证单个 project 超限返回 false
}
```

- [ ] **Step 3: 编写 ClickHouse TTL 和聚合查询测试**

- 验证 migration SQL 包含 TTL 子句
- 编写 go 测试验证 QueryProjectIngestStats / QueryAgentErrorStats 返回正确

- [ ] **Step 4: 编写 Python batch tracing 测试**

```python
# python/tests/test_tracing_batch.py
def test_configure_tracing_uses_batch_processor():
    # 验证默认使用 BatchSpanProcessor 或可配置
def test_batch_config_params():
    # 验证 AGENTSCOPE_EXPORT_BATCH_SIZE 等配置生效
def test_force_flush_and_shutdown():
    # 验证 force_flush 和 shutdown 正常工作
def test_redact_token():
    # 验证 token 字段被脱敏
def test_redact_pii():
    # 验证邮箱、手机号被脱敏
def test_capture_content_default_false():
    # 验证 capture_content 默认关闭
```

- [ ] **Step 5: 运行所有测试验证基线**

```bash
cd /home/luxinyu/GolandProjects/AgentScope
go test ./... -count=1 2>&1 | tail -20
cd python && python -m pytest tests/test_tracing_batch.py -q 2>&1
```

- [ ] **Step 6: 提交**

```bash
git add internal/httpapi/ingest_handlers_test.go internal/ingest/otlp_test.go internal/tracestore/clickhouse_test.go python/tests/
git commit -m "test: 为高并发 ingest 增加基线测试"
```

---

### Task 2: 增强 Python SDK 批量导出与脱敏

**Files:**
- Modify: `python/agentscope_sdk/tracing.py`
- Create: `python/agentscope_sdk/redact.py`

#### 具体改动

- [ ] **Step 1: 新建 `python/agentscope_sdk/redact.py`**

```python
"""Sensitive data redaction utilities for AgentScope tracing."""

import re
from typing import Any

# 敏感字段 key 列表（大小写不敏感匹配）
SENSITIVE_KEYS = {
    "authorization", "api_key", "apikey", "token", "secret",
    "password", "passwd", "credential", "access_key", "private_key",
}

# PII 匹配模式
PII_PATTERNS: list[tuple[re.Pattern, str]] = [
    (re.compile(r'\b[\w.+-]+@[\w-]+\.[\w.-]+\b'), '[EMAIL_REDACTED]'),
    (re.compile(r'\b1[3-9]\d{9}\b'), '[PHONE_REDACTED]'),
    (re.compile(r'\b\d{18}[\dXx]\b'), '[ID_CARD_REDACTED]'),  # 中国身份证
]

def should_redact_key(key: str) -> bool:
    """Returns True if the attribute key indicates sensitive data."""
    lower = key.lower()
    # 检查完整 key 或 key 的最后一段
    parts = lower.replace("-", "_").split(".")
    for part in parts:
        if part in SENSITIVE_KEYS:
            return True
    return False

def redact_value(value: str) -> str:
    """Replace sensitive values with a placeholder."""
    result = value
    for pattern, replacement in PII_PATTERNS:
        result = pattern.sub(replacement, result)
    return result

def maybe_redact(key: str, value: Any) -> Any:
    """Redact a value if its key or content indicates sensitivity."""
    if should_redact_key(key):
        return "[REDACTED]"
    if isinstance(value, str):
        return redact_value(value)
    return value
```

- [ ] **Step 2: 修改 `tracing.py` — 添加配置参数和 BatchSpanProcessor**

```python
# 在 configure_tracing 中添加新参数
def configure_tracing(
    server_url: str,
    project_token: str,
    *,
    service_name: str = "agentscope-runner",
    export_batch_size: int = 512,
    export_delay_ms: int = 5000,
    export_timeout_ms: int = 30000,
    export_compression: str = "gzip",
    capture_content: bool = False,
    enable_redaction: bool = True,
) -> trace.Tracer:
```

**实现细节:**
- 当 export_batch_size > 1 时使用 `BatchSpanProcessor`，否则继续使用 `SimpleSpanProcessor`
- `BatchSpanProcessor` 参数通过 `BatchSpanProcessorOptions` 设置
- 注入 `SpanProcessor` 包装器，在 `on_start` 中对敏感属性做 redaction
- `capture_content` 参数控制 `agentscope.content_captured` 属性

Span processor 包装器实现：

```python
class RedactingSpanProcessor(SpanProcessor):
    """A SpanProcessor that redacts sensitive attributes before export."""

    def __init__(self, inner: SpanProcessor, enable: bool = True):
        self._inner = inner
        self._enable = enable

    def on_start(self, span, parent_context=None):
        if self._enable:
            self._redact_attributes(span)
        self._inner.on_start(span, parent_context)

    def on_end(self, span_data):
        self._inner.on_end(span_data)

    def force_flush(self, timeout_millis=30000):
        return self._inner.force_flush(timeout_millis)

    def shutdown(self):
        self._inner.shutdown()

    def _redact_attributes(self, span):
        """Iterate over span attributes and redact sensitive ones."""
        # 通过 span._attributes 或 set_attribute 实现
        # OTel SDK 不直接暴露 attributes 修改，需要从 Resource 或 Span 层面处理
        pass  # 具体实现使用 OTel SDK 的 AttributeMap 操作
```

注意：由于 OTel Python SDK 的 Span 不直接暴露 attributes 写入 API，redaction 的最佳实践是在设置 attribute 之前就处理。所以在 deep trace helpers 中做 redaction。

- [ ] **Step 3: 在 deep trace helpers 中集成 redaction**

在 `trace_llm`, `trace_tool`, `trace_retrieval`, `trace_skill` 中，对 `input_summary`, `output_summary`, `**attrs` 里的内容做 redaction。

对于额外参数 `**attrs`，遍历时检查 key 是否敏感。

- [ ] **Step 4: 运行 Python 测试**

```bash
cd /home/luxinyu/GolandProjects/AgentScope/python
python -m pytest tests/test_tracing_batch.py tests/test_tracing.py -q 2>&1
```

- [ ] **Step 5: 提交**

```bash
git add python/agentscope_sdk/redact.py python/agentscope_sdk/tracing.py
git commit -m "feat: 增强 Python SDK 批量导出与脱敏"
```

---

### Task 3: 支持 gzip ingest 和请求限制

**Files:**
- Modify: `internal/httpapi/ingest_handlers.go`
- Modify: `internal/ingest/otlp.go`
- Modify: `internal/config/config.go`
- Modify: `internal/app/app.go`

- [ ] **Step 1: Config 增加新配置项**

```go
// internal/config/config.go — 追加
type IngestConfig struct {
    MaxBodySize       int  // 默认 10MB (10*1024*1024)
    MaxSpans          int  // 默认 5000
}

// 在 Config 中追加
type Config struct {
    // ... 现有字段
    Ingest IngestConfig
}
```

环境变量：
- `AGENTSCOPE_INGEST_MAX_BODY_SIZE` — 默认 10485760 (10MB)
- `AGENTSCOPE_INGEST_MAX_SPANS` — 默认 5000

- [ ] **Step 2: 实现 gzip body reader**

在 `internal/ingest/` 包中新增或在 otlp.go 中扩展：

```go
// ReadAndDecodeBody 读取请求 body，处理 gzip 解压。
// body 在解压后仍然受 maxSize 限制。
func ReadAndDecodeBody(r *http.Request, maxSize int64) ([]byte, error) {
    var reader io.Reader = r.Body
    if r.Header.Get("Content-Encoding") == "gzip" {
        gr, err := gzip.NewReader(r.Body)
        if err != nil {
            return nil, fmt.Errorf("gzip decode: %w", err)
        }
        defer gr.Close()
        reader = gr
    }
    body, err := io.ReadAll(io.LimitReader(reader, maxSize+1))
    if err != nil {
        return nil, fmt.Errorf("read body: %w", err)
    }
    if int64(len(body)) > maxSize {
        return nil, ErrBodyTooLarge
    }
    return body, nil
}
```

- [ ] **Step 3: 实现 span 数量限制**

```go
// DecodeOTLPTraceRequest 增加 maxSpans 参数
func DecodeOTLPTraceRequest(body []byte, maxSpans int) ([]SpanRecord, []SpanEventRecord, error) {
    // 解析后检查 span + event 总数
    // 超过 maxSpans 返回 ErrTooManySpans
}
```

- [ ] **Step 4: 更新 handler 使用新函数**

```go
// 在 handleIngest 中使用 ReadAndDecodeBody 替代原有 readBody
// 使用 DecodeOTLPTraceRequest(body, cfg.MaxSpans) 替代原有调用
// 超限时返回 413 Payload Too Large 和 422 Unprocessable Entity
```

- [ ] **Step 5: 运行测试**

```bash
go test ./internal/ingest/... ./internal/httpapi/... -count=1 -v 2>&1 | tail -20
```

- [ ] **Step 6: 提交**

```bash
git add internal/config/config.go internal/ingest/otlp.go internal/httpapi/ingest_handlers.go
git commit -m "feat: 支持 gzip ingest 和请求限制"
```

---

### Task 4: 项目级 ingest 限流

**Files:**
- Create: `internal/ingest/ratelimit.go`
- Modify: `internal/httpapi/ingest_handlers.go`
- Modify: `internal/app/app.go`

- [ ] **Step 1: 创建 `internal/ingest/ratelimit.go`**

```go
package ingest

import (
    "sync"
    "time"
)

// ErrRateLimited is returned when a project has exceeded its ingest quota.
var ErrRateLimited = fmt.Errorf("rate limited: too many ingest requests")

// TokenBucket implements a simple token bucket rate limiter.
type TokenBucket struct {
    mu        sync.Mutex
    capacity  int
    tokens    int
    refill    int       // tokens per refill interval
    interval  time.Duration
    lastRefill time.Time
}

func NewTokenBucket(capacity, refill int, interval time.Duration) *TokenBucket {
    return &TokenBucket{
        capacity:   capacity,
        tokens:     capacity,
        refill:     refill,
        interval:   interval,
        lastRefill: time.Now(),
    }
}

func (tb *TokenBucket) Allow() bool { ... }  // 获取一个 token
func (tb *TokenBucket) AllowN(n int) bool { ... }  // 获取 n 个 token

// ProjectRateLimiter manages per-project rate limiters.
type ProjectRateLimiter struct {
    mu       sync.RWMutex
    buckets  map[string]*TokenBucket
    capacity int
    refill   int
    interval time.Duration
}

// NewProjectRateLimiter creates a new per-project rate limiter.
// Default: capacity=100, refill=100, interval=1s (100 spans/sec per project)
func NewProjectRateLimiter(capacity, refill int, interval time.Duration) *ProjectRateLimiter { ... }

func (prl *ProjectRateLimiter) Allow(projectID string, n int) bool { ... }
```

- [ ] **Step 2: 在 Config 中增加限流配置**

```go
// config.go IngestConfig 追加
type IngestConfig struct {
    // ... 现有
    RateLimitEnabled   bool          // 默认 false（向后兼容）
    RateLimitCapacity  int           // 默认 100
    RateLimitRefill    int           // 默认 100
    RateLimitInterval  time.Duration // 默认 1s
}
```

环境变量：
- `AGENTSCOPE_INGEST_RATE_LIMIT_ENABLED` — 默认 false
- `AGENTSCOPE_INGEST_RATE_LIMIT_CAPACITY` — 默认 100
- `AGENTSCOPE_INGEST_RATE_LIMIT_REFILL` — 默认 100
- `AGENTSCOPE_INGEST_RATE_LIMIT_INTERVAL` — 默认 1s

- [ ] **Step 3: 在 handler 中注入并使用**

在 `Dependencies` 中增加 `RateLimiter *ingest.ProjectRateLimiter`
在 `handleIngest` 中，如果 rate limiter 不为 nil，调用 `Allow`

- [ ] **Step 4: 创建 IngestWriter 抽象**

```go
// internal/ingest/writer.go
// TraceBatchWriter 抽象，为后续 Kafka/NATS 留接口
type TraceBatchWriter interface {
    WriteSpans(ctx context.Context, spans []SpanRecord, events []SpanEventRecord,
        llm []LLMCallRecord, tools []ToolCallRecord,
        retrievals []RetrievalCallRecord, skills []SkillCallRecord) error
    Close() error
}

// ClickHouseTraceWriter 是当前实现
type ClickHouseTraceWriter struct {
    tracestore TracestoreWriter
}

// TracestoreWriter 是 tracestore.Store 的子集接口
type TracestoreWriter interface {
    InsertSpans(ctx context.Context, spans []SpanRecord) error
    InsertSpanEvents(ctx context.Context, events []SpanEventRecord) error
    InsertLLMCalls(ctx context.Context, calls []LLMCallRecord) error
    InsertToolCalls(ctx context.Context, calls []ToolCallRecord) error
    InsertRetrievalCalls(ctx context.Context, calls []RetrievalCallRecord) error
    InsertSkillCalls(ctx context.Context, calls []SkillCallRecord) error
}
```

然后在 handler 中使用 `TraceBatchWriter` 而不是直接调用 `Tracestore`。后端写操作抽象化。

- [ ] **Step 5: 运行测试**

```bash
go test ./internal/ingest/... ./internal/httpapi/... -count=1 -v 2>&1 | tail -20
```

- [ ] **Step 6: 提交**

```bash
git add internal/ingest/ratelimit.go internal/ingest/writer.go internal/httpapi/ingest_handlers.go
git commit -m "feat: 增加项目级 ingest 限流和 TraceBatchWriter 抽象"
```

---

### Task 5: 增加 ClickHouse TTL 和聚合查询

**Files:**
- Create: `clickhouse/migrations/00002_ttl_aggregation.sql`
- Modify: `internal/tracestore/clickhouse.go`
- Modify: `internal/tracestore/clickhouse_test.go`
- Create: `internal/tracestore/aggregation.go`

- [ ] **Step 1: 创建 ClickHouse TTL migration**

```sql
-- clickhouse/migrations/00002_ttl_aggregation.sql
-- Phase 2.7: TTL 策略和聚合优化
-- 所有 trace 表默认保留 30 天

-- trace_spans TTL
ALTER TABLE trace_spans MODIFY TTL start_time + INTERVAL 30 DAY;

-- trace_span_events TTL
ALTER TABLE trace_span_events MODIFY TTL event_time + INTERVAL 30 DAY;

-- trace_llm_calls TTL
ALTER TABLE trace_llm_calls MODIFY TTL start_time + INTERVAL 30 DAY;

-- trace_tool_calls TTL
ALTER TABLE trace_tool_calls MODIFY TTL start_time + INTERVAL 30 DAY;

-- trace_retrieval_calls TTL
ALTER TABLE trace_retrieval_calls MODIFY TTL start_time + INTERVAL 30 DAY;

-- trace_skill_calls TTL
ALTER TABLE trace_skill_calls MODIFY TTL start_time + INTERVAL 30 DAY;
```

注意：TTL 只对 MergeTree 引擎有效。也可以考虑在 CREATE TABLE 中加 TTL，但对于已有生产数据的系统，ALTER TABLE 更安全。

- [ ] **Step 2: 创建物化视图和聚合表（可选）**

考虑是否需要物化视图。先选择直接用查询方式实现聚合，后续如需优化再建 MV。

可以添加一个 ingest_stats 汇总表：

```sql
-- trace_ingest_stats 汇总表（用于快速查询采集健康数据）
CREATE TABLE IF NOT EXISTS trace_ingest_stats (
    project_id UUID,
    hour DateTime,
    span_count UInt64,
    error_count UInt64,
    llm_count UInt64,
    tool_count UInt64,
    total_tokens UInt64,
    total_cost Float64
) ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(hour)
ORDER BY (project_id, hour);
```

- [ ] **Step 3: 在 `internal/tracestore/` 中创建 aggregation.go**

```go
package tracestore

import "context"

// ProjectIngestStats 表示项目级采集统计
type ProjectIngestStats struct {
    SpanCount    uint64  `ch:"span_count"`
    ErrorCount   uint64  `ch:"error_count"`
    LLMCount     uint64  `ch:"llm_count"`
    TotalTokens  uint64  `ch:"total_tokens"`
    TotalCost    float64 `ch:"total_cost"`
}

// AgentErrorStats 表示 agent 维度错误统计
type AgentErrorStats struct {
    AgentName  string  `ch:"agent_name"`
    ErrorCount uint64  `ch:"error_count"`
    RunCount   uint64  `ch:"run_count"`
    P95Latency float64 `ch:"p95_latency"`
}

// QueryProjectIngestMinutes 查询最近 N 分钟项目级 ingest 统计
func (s *Store) QueryProjectIngestStats(ctx context.Context, projectID uuid.UUID, minutes int) (ProjectIngestStats, error) {
    // SELECT
    //   count() as span_count,
    //   countIf(status_code = 'ERROR') as error_count,
    //   ... FROM trace_spans WHERE project_id = $1 AND start_time >= now() - INTERVAL $2 MINUTE
}

// QueryAgentErrorStatsMinutes 查询最近 N 分钟 agent 维度错误统计
func (s *Store) QueryAgentErrorStats(ctx context.Context, projectID uuid.UUID, minutes int) ([]AgentErrorStats, error) {
    // 从 trace_spans 按 agent name 聚合
    // P95 latency 使用 quantile(0.95)(duration_ms)
}
```

- [ ] **Step 4: 添加聚合查询 API**

在 `internal/httpapi/` 中给 `/api/projects/{projectID}/ingest-stats` 和 `/api/projects/{projectID}/ingest-stats/agents` 添加 handler（放在 auth 分组内）。

- [ ] **Step 5: 运行测试**

```bash
go test ./internal/tracestore/... ./internal/httpapi/... -count=1 -v 2>&1 | tail -20
```

- [ ] **Step 6: 提交**

```bash
git add clickhouse/migrations/00002_ttl_aggregation.sql internal/tracestore/aggregation.go internal/tracestore/clickhouse.go internal/httpapi/
git commit -m "feat: 增加 ClickHouse TTL 和聚合查询"
```

---

### Task 6: 采集健康中文页面

**Files:**
- Create: `web/src/features/ingest/IngestDashboard.tsx`
- Create: `web/src/features/ingest/IngestHealthStats.tsx`
- Create: `web/src/features/ingest/index.ts`
- Modify: `web/src/App.tsx` 或对应的 router 文件
- Modify: `web/src/api/types.ts`

- [ ] **Step 1: API types 扩展**

```typescript
// web/src/api/types.ts 追加
export interface IngestStats {
  span_count: number;
  error_count: number;
  llm_count: number;
  total_tokens: number;
  total_cost: number;
}

export interface AgentStats {
  agent_name: string;
  error_count: number;
  run_count: number;
  p95_latency: number;
}
```

- [ ] **Step 2: React 组件实现**

`IngestDashboard.tsx` — 中文采集健康仪表盘，包含：
- 时间选择器（最近 5/15/60 分钟）
- Span 总数卡片
- Agent 维度错误率表格
- P95 延迟
- Token/cost 汇总
- 429/限流状态提示

使用 Ant Design 的 Card、Table、Statistic 组件。

注意：429 状态不从前端主动触发，但 API 返回类型中要包含 429 处理。

- [ ] **Step 3: 路由注册**

在 web 的路由配置中添加 `/ingest` 路径。

- [ ] **Step 4: 验证前端构建**

```bash
cd web && npm test -- --run && npm run build
```

- [ ] **Step 5: 提交**

```bash
git add web/src/features/ingest/ web/src/api/types.ts
git commit -m "feat: 增加采集健康中文页面"
```

---

### Task 7: 补充文档

**Files:**
- Create: `docs/superpowers/phase2.7-high-throughput-ingest.md`

- [ ] **Step 1: 编写 Phase 2.7 文档**

```markdown
# Phase 2.7: 高并发采集链路

## 概述

Phase 2.7 将 AgentScope 的 trace ingest 链路从 demo 级升级到生产可用级别...

// SDK batch 配置说明
// gzip ingest curl 示例
// rate limit / quota 说明
// ClickHouse TTL 说明
// 聚合指标含义
// 本地验证和压测方式
```

- [ ] **Step 2: 提交**

```bash
git add docs/superpowers/phase2.7-high-throughput-ingest.md
git commit -m "docs: 补充 Phase 2.7 高并发采集文档"
```

---

## 验证命令（最终）

每个 commit 后运行对应包的测试。最终在 worktree 中运行：

```bash
cd /home/luxinyu/GolandProjects/AgentScope
sqlc generate 2>&1
go test ./... -count=1 2>&1 | grep -E "(ok|FAIL|---)"
go test -race ./internal/httpapi ./internal/ingest ./internal/tracestore -count=1 2>&1 | tail -10
go vet ./... 2>&1 | head -20
cd python && python -m pytest -q 2>&1 | tail -5
cd web && npm test -- --run 2>&1 | tail -10 && npm run build 2>&1 | tail -5
git diff --check
```

---

## 架构图

```
Python SDK                               Go Backend
┌──────────────────┐                    ┌──────────────────────┐
│ BatchProcessor   │  gzip OTLP/HTTP    │                      │
│ + RedactingProc  │ ──────────────────▶│  gzip Decompress     │
│                  │                    │  Body Size Limit     │
│ AGENTSCOPE_*     │                    │  Span Count Limit    │
│ env config       │                    │  Project Rate Limiter│
└──────────────────┘                    │  TraceBatchWriter    │
                                        │       │              │
                                        └───────┼──────────────┘
                                                │
                                                ▼
                                        ┌──────────────────┐
                                        │   ClickHouse     │
                                        │  TTL 30d Auto    │
                                        │  Aggregation     │
                                        └──────────────────┘
```
