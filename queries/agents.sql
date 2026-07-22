-- Agent Control Center queries.

-- name: CreateAgent :one
INSERT INTO agents (project_id, name, description, metadata)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetAgentByID :one
SELECT * FROM agents WHERE id = $1;

-- name: GetAgentByProjectAndName :one
SELECT * FROM agents WHERE project_id = $1 AND name = $2;

-- name: ListAgentsByProject :many
SELECT * FROM agents WHERE project_id = $1 ORDER BY created_at DESC;

-- name: UpdateAgentStatus :one
UPDATE agents SET status = $2, last_heartbeat_at = now(), updated_at = now() WHERE id = $1 RETURNING *;

-- name: CreateAgentVersion :one
INSERT INTO agent_versions (agent_id, version, description, metadata)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateAgentInstance :one
INSERT INTO agent_instances (agent_id, project_id, instance_id, instance_name, status, hostname, pid, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: UpdateAgentInstanceHeartbeat :one
UPDATE agent_instances
SET status = $3, last_heartbeat_at = now()
WHERE agent_id = $1 AND instance_id = $2
RETURNING *;

-- name: GetAgentInstanceByAgentAndInstanceID :one
SELECT * FROM agent_instances
WHERE agent_id = $1 AND instance_id = $2;

-- name: UpsertAgentInstance :one
INSERT INTO agent_instances (agent_id, project_id, instance_id, instance_name, status, hostname, pid, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (agent_id, instance_id) DO UPDATE SET
    status = EXCLUDED.status,
    last_heartbeat_at = now()
RETURNING *;

-- name: ListAgentInstancesByAgent :many
SELECT * FROM agent_instances WHERE agent_id = $1 ORDER BY last_heartbeat_at DESC;

-- name: CreateAgentHeartbeat :one
INSERT INTO agent_heartbeats (agent_id, instance_id, status, payload)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateAgentCapability :one
INSERT INTO agent_capabilities (agent_id, name, description, metadata)
VALUES ($1, $2, $3, $4)
ON CONFLICT (agent_id, name) DO UPDATE SET
    description = EXCLUDED.description,
    metadata = EXCLUDED.metadata
RETURNING *;

-- name: ListAgentCapabilitiesByAgent :many
SELECT * FROM agent_capabilities WHERE agent_id = $1 ORDER BY created_at DESC;

-- name: SetAgentInstanceActiveRun :exec
UPDATE agent_instances
SET active_run_id = $3, last_heartbeat_at = now(), updated_at = now()
WHERE id = $1 AND project_id = $2;

-- name: ClearAgentInstanceActiveRun :exec
UPDATE agent_instances
SET active_run_id = NULL, last_heartbeat_at = now(), updated_at = now()
WHERE id = $1;

-- name: SetAgentInstanceActiveRunByExternalID :one
UPDATE agent_instances
SET active_run_id = $4, last_heartbeat_at = now(), updated_at = now()
WHERE project_id = $1 AND agent_id = $2 AND instance_id = $3
RETURNING id;

-- name: ClearAgentInstanceActiveRunByExternalID :one
UPDATE agent_instances
SET active_run_id = NULL, last_heartbeat_at = now(), updated_at = now()
WHERE project_id = $1 AND agent_id = $2 AND instance_id = $3
RETURNING id;

-- name: GetAgentInstanceDBID :one
SELECT id FROM agent_instances
WHERE project_id = $1 AND agent_id = $2 AND instance_id = $3;

-- name: ListAgentSummariesByProject :many
SELECT
    a.id::text,
    a.project_id::text,
    a.name,
    a.description,
    a.status,
    a.version,
    a.metadata,
    a.created_at::timestamptz,
    a.updated_at::timestamptz,
    COALESCE(inst_stats.instance_count, 0)::bigint AS instance_count,
    COALESCE(inst_stats.online_instance_count, 0)::bigint AS online_instance_count,
    inst_stats.last_heartbeat_at,
    COALESCE(run_stats.active_run_count, 0)::bigint AS active_run_count,
    COALESCE(run_stats.recent_run_count, 0)::bigint AS recent_run_count,
    COALESCE(run_stats.recent_error_count, 0)::bigint AS recent_error_count,
    run_stats.last_run_at,
    run_stats.p95_latency_ms,
    COALESCE(run_stats.error_rate, 0)::numeric AS error_rate,
    run_stats.total_cost
FROM agents a
LEFT JOIN LATERAL (
    SELECT
        COUNT(*)::bigint AS instance_count,
        COUNT(*) FILTER (WHERE ai.last_heartbeat_at >= NOW() - INTERVAL '90 seconds')::bigint AS online_instance_count,
        MAX(ai.last_heartbeat_at)::timestamptz AS last_heartbeat_at
    FROM agent_instances ai
    WHERE ai.agent_id = a.id
) inst_stats ON true
LEFT JOIN LATERAL (
    SELECT
        COUNT(DISTINCT r_inner.id) FILTER (WHERE r_inner.status NOT IN ('completed','failed','timeout','cancelled'))::bigint AS active_run_count,
        COUNT(DISTINCT r_inner.id) FILTER (WHERE r_inner.created_at >= NOW() - INTERVAL '24 hours')::bigint AS recent_run_count,
        COUNT(DISTINCT r_inner.id) FILTER (WHERE r_inner.status = 'failed' AND r_inner.created_at >= NOW() - INTERVAL '24 hours')::bigint AS recent_error_count,
        MAX(r_inner.created_at)::timestamptz AS last_run_at,
        ROUND(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY r_inner.latency_ms))::bigint AS p95_latency_ms,
        CASE WHEN COUNT(r_inner.id) > 0
            THEN ROUND(COUNT(r_inner.id) FILTER (WHERE r_inner.status = 'failed')::numeric / COUNT(r_inner.id)::numeric, 4)
            ELSE 0
        END AS error_rate,
        SUM(r_inner.cost)::numeric AS total_cost
    FROM runs r_inner
    WHERE r_inner.project_id = a.project_id AND r_inner.agent_name = a.name
) run_stats ON true
WHERE a.project_id = $1
ORDER BY a.name ASC;
