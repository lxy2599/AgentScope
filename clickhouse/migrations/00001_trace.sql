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

-- ============================================================
-- trace_llm_calls
-- Structured LLM call records extracted from span attributes.
-- ============================================================
CREATE TABLE IF NOT EXISTS trace_llm_calls (
    project_id UUID,
    run_id UUID,
    trace_id String,
    span_id String,
    agent_id Nullable(String),
    agent_instance_id Nullable(String),
    provider String,
    model String,
    input_tokens Nullable(Int64),
    output_tokens Nullable(Int64),
    total_tokens Nullable(Int64),
    cost Nullable(Float64),
    input_summary String,
    output_summary String,
    content_captured Bool,
    status String,
    error Nullable(String),
    start_time DateTime64(6, 'UTC'),
    duration_ms Nullable(Int64),
    ingested_at DateTime64(6, 'UTC')
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(start_time)
ORDER BY (project_id, run_id, trace_id, start_time);

-- ============================================================
-- trace_tool_calls
-- Structured tool call records extracted from span attributes.
-- ============================================================
CREATE TABLE IF NOT EXISTS trace_tool_calls (
    project_id UUID,
    run_id UUID,
    trace_id String,
    span_id String,
    agent_id Nullable(String),
    agent_instance_id Nullable(String),
    tool_name String,
    tool_type String,
    arguments_summary String,
    result_summary String,
    success Bool,
    error Nullable(String),
    retry_count Int64,
    side_effect_risk Nullable(String),
    start_time DateTime64(6, 'UTC'),
    duration_ms Nullable(Int64),
    ingested_at DateTime64(6, 'UTC')
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(start_time)
ORDER BY (project_id, run_id, trace_id, start_time);

-- ============================================================
-- trace_retrieval_calls
-- Structured knowledge base retrieval records extracted from span attributes.
-- ============================================================
CREATE TABLE IF NOT EXISTS trace_retrieval_calls (
    project_id UUID,
    run_id UUID,
    trace_id String,
    span_id String,
    agent_id Nullable(String),
    agent_instance_id Nullable(String),
    knowledge_base String,
    query_text String,
    top_k Nullable(Int64),
    returned_count Nullable(Int64),
    document_ids Array(String),
    scores Array(Float64),
    start_time DateTime64(6, 'UTC'),
    duration_ms Nullable(Int64),
    ingested_at DateTime64(6, 'UTC')
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(start_time)
ORDER BY (project_id, run_id, trace_id, start_time);

-- ============================================================
-- trace_skill_calls
-- Structured skill invocations extracted from span attributes.
-- ============================================================
CREATE TABLE IF NOT EXISTS trace_skill_calls (
    project_id UUID,
    run_id UUID,
    trace_id String,
    span_id String,
    agent_id Nullable(String),
    agent_instance_id Nullable(String),
    skill_name String,
    skill_version String,
    input_summary String,
    output_summary String,
    success Bool,
    error Nullable(String),
    start_time DateTime64(6, 'UTC'),
    duration_ms Nullable(Int64),
    ingested_at DateTime64(6, 'UTC')
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(start_time)
ORDER BY (project_id, run_id, trace_id, start_time);
