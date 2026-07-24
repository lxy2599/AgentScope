-- Phase 4: nullable performance/cost fields preserve compatibility with old rows.
ALTER TABLE trace_llm_calls ADD COLUMN IF NOT EXISTS cache_read_tokens Nullable(Int64) AFTER total_tokens;
ALTER TABLE trace_llm_calls ADD COLUMN IF NOT EXISTS cache_write_tokens Nullable(Int64) AFTER cache_read_tokens;
ALTER TABLE trace_llm_calls ADD COLUMN IF NOT EXISTS ttft_ms Nullable(Int64) AFTER cache_write_tokens;
ALTER TABLE trace_llm_calls ADD COLUMN IF NOT EXISTS estimated_cost Nullable(Float64) AFTER cost;
ALTER TABLE trace_llm_calls ADD COLUMN IF NOT EXISTS actual_cost Nullable(Float64) AFTER estimated_cost;
ALTER TABLE trace_llm_calls ADD COLUMN IF NOT EXISTS cost_source LowCardinality(String) DEFAULT '' AFTER actual_cost;

ALTER TABLE trace_ingest_health_events ADD COLUMN IF NOT EXISTS sdk_queue_depth Nullable(Int64) AFTER duration_ms;
ALTER TABLE trace_ingest_health_events ADD COLUMN IF NOT EXISTS sdk_queue_capacity Nullable(Int64) AFTER sdk_queue_depth;
ALTER TABLE trace_ingest_health_events ADD COLUMN IF NOT EXISTS sdk_spans_dropped Nullable(Int64) AFTER sdk_queue_capacity;
ALTER TABLE trace_ingest_health_events ADD COLUMN IF NOT EXISTS sdk_export_retries Nullable(Int64) AFTER sdk_spans_dropped;
ALTER TABLE trace_ingest_health_events ADD COLUMN IF NOT EXISTS sdk_export_latency_ms Nullable(Float64) AFTER sdk_export_retries;

-- Raw telemetry is intentionally short lived. Rollups retain operational
-- evidence without retaining prompts, tool payloads, or other content.
ALTER TABLE trace_spans MODIFY TTL start_time + INTERVAL 30 DAY;
ALTER TABLE trace_span_events MODIFY TTL event_time + INTERVAL 30 DAY;
ALTER TABLE trace_llm_calls MODIFY TTL start_time + INTERVAL 30 DAY;
ALTER TABLE trace_tool_calls MODIFY TTL start_time + INTERVAL 30 DAY;
ALTER TABLE trace_retrieval_calls MODIFY TTL start_time + INTERVAL 30 DAY;
ALTER TABLE trace_skill_calls MODIFY TTL start_time + INTERVAL 30 DAY;
ALTER TABLE trace_ingest_health_events MODIFY TTL event_time + INTERVAL 30 DAY;

CREATE TABLE IF NOT EXISTS trace_governance_rollup_hourly (
    project_id UUID,
    agent_name String,
    agent_version String,
    environment String,
    bucket DateTime('UTC'),
    metric LowCardinality(String),
    value_sum Float64,
    sample_count UInt64
) ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(bucket)
ORDER BY (project_id, agent_name, agent_version, environment, metric, bucket)
TTL bucket + INTERVAL 180 DAY;

CREATE TABLE IF NOT EXISTS trace_governance_rollup_daily (
    project_id UUID,
    agent_name String,
    agent_version String,
    environment String,
    bucket DateTime('UTC'),
    metric LowCardinality(String),
    value_sum Float64,
    sample_count UInt64
) ENGINE = SummingMergeTree()
PARTITION BY toYYYYMM(bucket)
ORDER BY (project_id, agent_name, agent_version, environment, metric, bucket)
TTL bucket + INTERVAL 730 DAY;

CREATE MATERIALIZED VIEW IF NOT EXISTS trace_governance_run_latency_hourly_mv
TO trace_governance_rollup_hourly AS
SELECT
    project_id,
    if(JSONExtractString(attributes, 'agentscope.agent.name') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.agent.name')) AS agent_name,
    if(JSONExtractString(attributes, 'agentscope.agent.version') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.agent.version')) AS agent_version,
    if(JSONExtractString(attributes, 'agentscope.environment') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.environment')) AS environment,
    toStartOfHour(start_time) AS bucket,
    'run_latency_ms' AS metric,
    sum(toFloat64(assumeNotNull(duration_ms))) AS value_sum,
    count() AS sample_count
FROM trace_spans
WHERE name = 'agent.run' AND duration_ms IS NOT NULL
GROUP BY project_id, agent_name, agent_version, environment, bucket;

CREATE MATERIALIZED VIEW IF NOT EXISTS trace_governance_llm_tokens_hourly_mv
TO trace_governance_rollup_hourly AS
SELECT
    project_id,
    if(JSONExtractString(attributes, 'agentscope.agent.name') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.agent.name')) AS agent_name,
    if(JSONExtractString(attributes, 'agentscope.agent.version') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.agent.version')) AS agent_version,
    if(JSONExtractString(attributes, 'agentscope.environment') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.environment')) AS environment,
    toStartOfHour(start_time) AS bucket,
    'llm_total_tokens' AS metric,
    sum(JSONExtractFloat(attributes, 'llm.total_tokens')) AS value_sum,
    count() AS sample_count
FROM trace_spans
WHERE JSONExtractString(attributes, 'agentscope.span.kind') = 'LLM'
  AND JSONHas(attributes, 'llm.total_tokens')
GROUP BY project_id, agent_name, agent_version, environment, bucket;

CREATE MATERIALIZED VIEW IF NOT EXISTS trace_governance_llm_cost_hourly_mv
TO trace_governance_rollup_hourly AS
SELECT
    project_id,
    if(JSONExtractString(attributes, 'agentscope.agent.name') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.agent.name')) AS agent_name,
    if(JSONExtractString(attributes, 'agentscope.agent.version') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.agent.version')) AS agent_version,
    if(JSONExtractString(attributes, 'agentscope.environment') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.environment')) AS environment,
    toStartOfHour(start_time) AS bucket,
    'llm_cost' AS metric,
    sum(if(
        JSONHas(attributes, 'llm.actual_cost'),
        JSONExtractFloat(attributes, 'llm.actual_cost'),
        JSONExtractFloat(attributes, 'llm.cost')
    )) AS value_sum,
    count() AS sample_count
FROM trace_spans
WHERE JSONExtractString(attributes, 'agentscope.span.kind') = 'LLM'
  AND (JSONHas(attributes, 'llm.actual_cost') OR JSONHas(attributes, 'llm.cost'))
GROUP BY project_id, agent_name, agent_version, environment, bucket;

CREATE MATERIALIZED VIEW IF NOT EXISTS trace_governance_llm_ttft_hourly_mv
TO trace_governance_rollup_hourly AS
SELECT
    project_id,
    if(JSONExtractString(attributes, 'agentscope.agent.name') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.agent.name')) AS agent_name,
    if(JSONExtractString(attributes, 'agentscope.agent.version') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.agent.version')) AS agent_version,
    if(JSONExtractString(attributes, 'agentscope.environment') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.environment')) AS environment,
    toStartOfHour(start_time) AS bucket,
    'llm_ttft_ms' AS metric,
    sum(JSONExtractFloat(attributes, 'llm.ttft_ms')) AS value_sum,
    count() AS sample_count
FROM trace_spans
WHERE JSONExtractString(attributes, 'agentscope.span.kind') = 'LLM'
  AND JSONHas(attributes, 'llm.ttft_ms')
GROUP BY project_id, agent_name, agent_version, environment, bucket;

CREATE MATERIALIZED VIEW IF NOT EXISTS trace_governance_tool_error_hourly_mv
TO trace_governance_rollup_hourly AS
SELECT
    project_id,
    if(JSONExtractString(attributes, 'agentscope.agent.name') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.agent.name')) AS agent_name,
    if(JSONExtractString(attributes, 'agentscope.agent.version') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.agent.version')) AS agent_version,
    if(JSONExtractString(attributes, 'agentscope.environment') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.environment')) AS environment,
    toStartOfHour(start_time) AS bucket,
    'tool_error_rate' AS metric,
    sum(if(JSONExtractBool(attributes, 'tool.success'), 0.0, 1.0)) AS value_sum,
    count() AS sample_count
FROM trace_spans
WHERE JSONExtractString(attributes, 'agentscope.span.kind') = 'TOOL'
  AND JSONHas(attributes, 'tool.success')
GROUP BY project_id, agent_name, agent_version, environment, bucket;

CREATE MATERIALIZED VIEW IF NOT EXISTS trace_governance_ingest_reject_hourly_mv
TO trace_governance_rollup_hourly AS
SELECT
    project_id,
    if(agent_name = '', 'unknown', agent_name) AS agent_name,
    'unknown' AS agent_version,
    'unknown' AS environment,
    toStartOfHour(event_time) AS bucket,
    'ingest_reject_rate' AS metric,
    sum(if(outcome = 'accepted', 0.0, 1.0)) AS value_sum,
    count() AS sample_count
FROM trace_ingest_health_events
GROUP BY project_id, agent_name, bucket;

CREATE MATERIALIZED VIEW IF NOT EXISTS trace_governance_run_latency_daily_mv
TO trace_governance_rollup_daily AS
SELECT
    project_id,
    if(JSONExtractString(attributes, 'agentscope.agent.name') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.agent.name')) AS agent_name,
    if(JSONExtractString(attributes, 'agentscope.agent.version') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.agent.version')) AS agent_version,
    if(JSONExtractString(attributes, 'agentscope.environment') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.environment')) AS environment,
    toStartOfDay(start_time) AS bucket,
    'run_latency_ms' AS metric,
    sum(toFloat64(assumeNotNull(duration_ms))) AS value_sum,
    count() AS sample_count
FROM trace_spans
WHERE name = 'agent.run' AND duration_ms IS NOT NULL
GROUP BY project_id, agent_name, agent_version, environment, bucket;

CREATE MATERIALIZED VIEW IF NOT EXISTS trace_governance_llm_tokens_daily_mv
TO trace_governance_rollup_daily AS
SELECT
    project_id,
    if(JSONExtractString(attributes, 'agentscope.agent.name') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.agent.name')) AS agent_name,
    if(JSONExtractString(attributes, 'agentscope.agent.version') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.agent.version')) AS agent_version,
    if(JSONExtractString(attributes, 'agentscope.environment') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.environment')) AS environment,
    toStartOfDay(start_time) AS bucket,
    'llm_total_tokens' AS metric,
    sum(JSONExtractFloat(attributes, 'llm.total_tokens')) AS value_sum,
    count() AS sample_count
FROM trace_spans
WHERE JSONExtractString(attributes, 'agentscope.span.kind') = 'LLM'
  AND JSONHas(attributes, 'llm.total_tokens')
GROUP BY project_id, agent_name, agent_version, environment, bucket;

CREATE MATERIALIZED VIEW IF NOT EXISTS trace_governance_llm_cost_daily_mv
TO trace_governance_rollup_daily AS
SELECT
    project_id,
    if(JSONExtractString(attributes, 'agentscope.agent.name') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.agent.name')) AS agent_name,
    if(JSONExtractString(attributes, 'agentscope.agent.version') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.agent.version')) AS agent_version,
    if(JSONExtractString(attributes, 'agentscope.environment') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.environment')) AS environment,
    toStartOfDay(start_time) AS bucket,
    'llm_cost' AS metric,
    sum(if(
        JSONHas(attributes, 'llm.actual_cost'),
        JSONExtractFloat(attributes, 'llm.actual_cost'),
        JSONExtractFloat(attributes, 'llm.cost')
    )) AS value_sum,
    count() AS sample_count
FROM trace_spans
WHERE JSONExtractString(attributes, 'agentscope.span.kind') = 'LLM'
  AND (JSONHas(attributes, 'llm.actual_cost') OR JSONHas(attributes, 'llm.cost'))
GROUP BY project_id, agent_name, agent_version, environment, bucket;

CREATE MATERIALIZED VIEW IF NOT EXISTS trace_governance_llm_ttft_daily_mv
TO trace_governance_rollup_daily AS
SELECT
    project_id,
    if(JSONExtractString(attributes, 'agentscope.agent.name') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.agent.name')) AS agent_name,
    if(JSONExtractString(attributes, 'agentscope.agent.version') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.agent.version')) AS agent_version,
    if(JSONExtractString(attributes, 'agentscope.environment') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.environment')) AS environment,
    toStartOfDay(start_time) AS bucket,
    'llm_ttft_ms' AS metric,
    sum(JSONExtractFloat(attributes, 'llm.ttft_ms')) AS value_sum,
    count() AS sample_count
FROM trace_spans
WHERE JSONExtractString(attributes, 'agentscope.span.kind') = 'LLM'
  AND JSONHas(attributes, 'llm.ttft_ms')
GROUP BY project_id, agent_name, agent_version, environment, bucket;

CREATE MATERIALIZED VIEW IF NOT EXISTS trace_governance_tool_error_daily_mv
TO trace_governance_rollup_daily AS
SELECT
    project_id,
    if(JSONExtractString(attributes, 'agentscope.agent.name') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.agent.name')) AS agent_name,
    if(JSONExtractString(attributes, 'agentscope.agent.version') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.agent.version')) AS agent_version,
    if(JSONExtractString(attributes, 'agentscope.environment') = '', 'unknown',
       JSONExtractString(attributes, 'agentscope.environment')) AS environment,
    toStartOfDay(start_time) AS bucket,
    'tool_error_rate' AS metric,
    sum(if(JSONExtractBool(attributes, 'tool.success'), 0.0, 1.0)) AS value_sum,
    count() AS sample_count
FROM trace_spans
WHERE JSONExtractString(attributes, 'agentscope.span.kind') = 'TOOL'
  AND JSONHas(attributes, 'tool.success')
GROUP BY project_id, agent_name, agent_version, environment, bucket;

CREATE MATERIALIZED VIEW IF NOT EXISTS trace_governance_ingest_reject_daily_mv
TO trace_governance_rollup_daily AS
SELECT
    project_id,
    if(agent_name = '', 'unknown', agent_name) AS agent_name,
    'unknown' AS agent_version,
    'unknown' AS environment,
    toStartOfDay(event_time) AS bucket,
    'ingest_reject_rate' AS metric,
    sum(if(outcome = 'accepted', 0.0, 1.0)) AS value_sum,
    count() AS sample_count
FROM trace_ingest_health_events
GROUP BY project_id, agent_name, bucket;
