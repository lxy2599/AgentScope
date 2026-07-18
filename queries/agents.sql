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
UPDATE agents SET status = $2, updated_at = now() WHERE id = $1 RETURNING *;

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
