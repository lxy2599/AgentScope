-- ClickHouse trace storage schema for AgentScope.
-- Stores OpenTelemetry span data that was previously in PostgreSQL.
-- Tables are created in the current database (set via connection DSN).

-- ============================================================
-- trace_spans
-- ============================================================
CREATE TABLE IF NOT EXISTS trace_spans (
    project_id UUID,
    run_id UUID,
    trace_id String,
    span_id String,
    parent_span_id Nullable(String),
    name String,
    kind String,
    status_code String,
    status_message Nullable(String),
    start_time DateTime64(6, 'UTC'),
    end_time Nullable(DateTime64(6, 'UTC')),
    duration_ms Nullable(Int64),
    attributes String,
    truncated Bool,
    ingested_at DateTime64(6, 'UTC')
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(start_time)
ORDER BY (project_id, run_id, trace_id, start_time);

-- ============================================================
-- trace_span_events
-- ============================================================
CREATE TABLE IF NOT EXISTS trace_span_events (
    project_id UUID,
    run_id UUID,
    trace_id String,
    span_id String,
    event_name String,
    event_time DateTime64(6, 'UTC'),
    attributes String,
    truncated Bool,
    ingested_at DateTime64(6, 'UTC')
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(event_time)
ORDER BY (project_id, run_id, trace_id, event_time);
