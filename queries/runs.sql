-- Run queries (spans/span-events moved to ClickHouse tracestore).

-- name: CreateRun :one
INSERT INTO runs (project_id, source, status, trace_id, agent_name, agent_version,
                  input_summary, output_summary, metadata, error,
                  started_at, ended_at, latency_ms, cost)
VALUES ($1, $2, $3, $4, $5, $6,
        $7, $8, $9, $10,
        $11, $12, $13, $14)
RETURNING *;

-- name: GetRunByID :one
SELECT * FROM runs WHERE id = $1;

-- name: ListRunsByProject :many
SELECT * FROM runs WHERE project_id = $1 ORDER BY created_at DESC;

-- name: ListRunsByProjectAndSource :many
SELECT * FROM runs WHERE project_id = $1 AND source = $2 ORDER BY created_at DESC;

-- name: GetRunByTraceID :one
SELECT * FROM runs WHERE trace_id = $1 AND project_id = $2 LIMIT 1;

-- name: UpdateRun :one
UPDATE runs SET
    agent_name = $2,
    agent_version = $3,
    status = $4,
    ended_at = $5,
    latency_ms = $6,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateRunAgentInfo :one
UPDATE runs SET
    agent_name = $2,
    agent_version = $3,
    updated_at = now()
WHERE id = $1
RETURNING *;
