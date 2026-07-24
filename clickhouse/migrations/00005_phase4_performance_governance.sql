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
