-- Phase 2.7: TTL 策略和聚合优化
-- 所有 trace 表默认保留 30 天（ALTER TABLE 方式，对已有表也生效）
-- TTL 天数硬编码在 migration；后续可通过配置中心化控制。
-- 注意：TTL 具体天数写死后，后续可通过客户端修改 Config 中的 TTL 值实现配置化。
-- 这些 ALTER 在 00001 创建表后执行；再次执行时 ClickHouse 会静默忽略已有 TTL。
-- ClickHouse 26.x 不支持 ALTER TABLE IF EXISTS，因此直接 ALTER。

-- 1. trace_spans TTL
ALTER TABLE trace_spans
MODIFY TTL start_time + INTERVAL 30 DAY;

-- 2. trace_span_events TTL
ALTER TABLE trace_span_events
MODIFY TTL event_time + INTERVAL 30 DAY;

-- 3. trace_llm_calls TTL
ALTER TABLE trace_llm_calls
MODIFY TTL start_time + INTERVAL 30 DAY;

-- 4. trace_tool_calls TTL
ALTER TABLE trace_tool_calls
MODIFY TTL start_time + INTERVAL 30 DAY;

-- 5. trace_retrieval_calls TTL
ALTER TABLE trace_retrieval_calls
MODIFY TTL start_time + INTERVAL 30 DAY;

-- 6. trace_skill_calls TTL
ALTER TABLE trace_skill_calls
MODIFY TTL start_time + INTERVAL 30 DAY;

-- 7. trace_ingest_stats 汇总表（SummingMergeTree 模式）
-- 每小时为每个 project_id 汇总 ingest 数据
CREATE TABLE IF NOT EXISTS trace_ingest_stats (
    project_id UUID,
    hour DateTime,
    span_count UInt64 DEFAULT 0,
    event_count UInt64 DEFAULT 0,
    error_count UInt64 DEFAULT 0,
    llm_count UInt64 DEFAULT 0,
    tool_count UInt64 DEFAULT 0,
    retrieval_count UInt64 DEFAULT 0,
    skill_count UInt64 DEFAULT 0,
    total_tokens UInt64 DEFAULT 0,
    total_cost Float64 DEFAULT 0.0
) ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(hour)
ORDER BY (project_id, hour)
TTL hour + INTERVAL 90 DAY;
