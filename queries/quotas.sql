-- name: GetProjectQuota :one
SELECT * FROM project_quotas WHERE project_id = $1;

-- name: UpsertProjectQuota :one
INSERT INTO project_quotas (
    project_id,
    ingest_requests_per_minute,
    ingest_spans_per_minute,
    ingest_bytes_per_minute,
    sse_connections,
    tokens_per_day,
    cost_micros_per_day,
    updated_by
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (project_id) DO UPDATE SET
    ingest_requests_per_minute = EXCLUDED.ingest_requests_per_minute,
    ingest_spans_per_minute = EXCLUDED.ingest_spans_per_minute,
    ingest_bytes_per_minute = EXCLUDED.ingest_bytes_per_minute,
    sse_connections = EXCLUDED.sse_connections,
    tokens_per_day = EXCLUDED.tokens_per_day,
    cost_micros_per_day = EXCLUDED.cost_micros_per_day,
    version = project_quotas.version + 1,
    updated_by = EXCLUDED.updated_by,
    updated_at = now()
RETURNING *;

-- name: CreateProjectQuotaEvent :one
INSERT INTO project_quota_events (
    project_id,
    dimension,
    requested,
    quota_limit,
    remaining,
    outcome,
    fail_mode,
    reason,
    occurred_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING *;

-- name: ListProjectQuotaEvents :many
SELECT * FROM project_quota_events
WHERE project_id = $1
ORDER BY occurred_at DESC
LIMIT $2;
