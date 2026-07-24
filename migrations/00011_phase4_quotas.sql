-- Phase 4: versioned per-project quotas and auditable enforcement events.

CREATE TABLE IF NOT EXISTS project_quotas (
    project_id UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    ingest_requests_per_minute BIGINT NOT NULL DEFAULT 6000
        CHECK (ingest_requests_per_minute > 0),
    ingest_spans_per_minute BIGINT NOT NULL DEFAULT 250000
        CHECK (ingest_spans_per_minute > 0),
    ingest_bytes_per_minute BIGINT NOT NULL DEFAULT 1073741824
        CHECK (ingest_bytes_per_minute > 0),
    sse_connections BIGINT NOT NULL DEFAULT 100
        CHECK (sse_connections > 0),
    tokens_per_day BIGINT NOT NULL DEFAULT 100000000
        CHECK (tokens_per_day > 0),
    cost_micros_per_day BIGINT NOT NULL DEFAULT 100000000000
        CHECK (cost_micros_per_day > 0),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version > 0),
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS project_quota_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    dimension TEXT NOT NULL,
    requested BIGINT NOT NULL,
    quota_limit BIGINT NOT NULL,
    remaining BIGINT NOT NULL,
    outcome TEXT NOT NULL CHECK (outcome IN ('allowed', 'rejected', 'degraded')),
    fail_mode TEXT NOT NULL CHECK (fail_mode IN ('fail_open', 'fail_closed')),
    reason TEXT NOT NULL DEFAULT '',
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_project_quota_events_recent
    ON project_quota_events(project_id, occurred_at DESC);
