-- Migration 00003: Semantic trace storage for agent thoughts and final answers.
--
-- Adds dedicated tables for agent THOUGHT and FINAL_ANSWER spans,
-- extracted from span attributes during ingestion (parallel to the existing
-- trace_llm_calls, trace_tool_calls, trace_retrieval_calls, trace_skill_calls).

-- Agent thought records: each row is one agent "thinking" step.
CREATE TABLE IF NOT EXISTS trace_thoughts (
    project_id        UUID,
    run_id            UUID,
    trace_id          String,
    span_id           String,
    agent_id          String,
    agent_instance_id String,
    stage             String,
    summary           String,
    content           String,
    confidence        Nullable(Float64),
    content_captured  Bool,
    start_time        DateTime64(6),
    duration_ms       Nullable(Int64),
    ingested_at       DateTime64(6)
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(start_time)
ORDER BY (project_id, run_id, trace_id, start_time)
TTL start_time + INTERVAL 30 DAY;

-- Agent final answer records: each row is one agent final answer.
CREATE TABLE IF NOT EXISTS trace_final_answers (
    project_id        UUID,
    run_id            UUID,
    trace_id          String,
    span_id           String,
    answer_summary    String,
    answer_output     String,
    structured_output String,
    start_time        DateTime64(6),
    duration_ms       Nullable(Int64),
    ingested_at       DateTime64(6)
)
ENGINE = MergeTree()
PARTITION BY toYYYYMM(start_time)
ORDER BY (project_id, run_id, trace_id, start_time)
TTL start_time + INTERVAL 30 DAY;

-- Update ingest stats aggregation to include new record types.
ALTER TABLE trace_ingest_stats
    ADD COLUMN IF NOT EXISTS thought_count    UInt64 DEFAULT 0,
    ADD COLUMN IF NOT EXISTS final_answer_count UInt64 DEFAULT 0;
