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
