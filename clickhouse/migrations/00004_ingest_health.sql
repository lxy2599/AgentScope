-- Content-free operational health events for authenticated OTLP ingestion.
CREATE TABLE IF NOT EXISTS trace_ingest_health_events (
    project_id UUID,
    agent_name String,
    outcome LowCardinality(String),
    http_status Int32,
    span_count UInt64,
    event_count UInt64,
    semantic_count UInt64,
    missing_run_id Bool,
    missing_trace_id Bool,
    error_code LowCardinality(String),
    duration_ms Int64,
    event_time DateTime64(6),
    ingested_at DateTime64(6)
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(event_time)
ORDER BY (project_id, event_time, agent_name)
TTL event_time + INTERVAL 90 DAY;

CREATE TABLE IF NOT EXISTS trace_ingest_health_rollup_15m (
    project_id UUID,
    agent_name String,
    bucket DateTime,
    accepted_count UInt64,
    rejected_count UInt64,
    storage_failed_count UInt64,
    missing_run_id_count UInt64,
    missing_trace_id_count UInt64,
    span_count UInt64,
    semantic_count UInt64
) ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(bucket)
ORDER BY (project_id, agent_name, bucket)
TTL bucket + INTERVAL 90 DAY;

CREATE MATERIALIZED VIEW IF NOT EXISTS trace_ingest_health_rollup_15m_mv
TO trace_ingest_health_rollup_15m AS
SELECT
    project_id,
    agent_name,
    toStartOfInterval(event_time, INTERVAL 15 MINUTE) AS bucket,
    countIf(outcome = 'accepted') AS accepted_count,
    countIf(outcome != 'accepted') AS rejected_count,
    countIf(outcome = 'storage_failed') AS storage_failed_count,
    countIf(missing_run_id) AS missing_run_id_count,
    countIf(missing_trace_id) AS missing_trace_id_count,
    sum(span_count) AS span_count,
    sum(semantic_count) AS semantic_count
FROM trace_ingest_health_events
GROUP BY project_id, agent_name, bucket;
