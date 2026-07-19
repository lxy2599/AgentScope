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

-- name: UpdateRunStatus :one
UPDATE runs
SET
    status = $3,
    error = $4,
    updated_at = now()
WHERE id = $1 AND project_id = $2
RETURNING *;

-- name: StartRuntimeRun :one
INSERT INTO runs (
    project_id, source, status, trace_id, agent_name, agent_version,
    input_summary, input, metadata, started_at
) VALUES (
    $1, 'live', $2, $3, $4, '', $5, $5, $6, now()
)
RETURNING *;

-- name: CreateRunStateEvent :one
INSERT INTO run_state_events (
    project_id,
    run_id,
    agent_id,
    agent_instance_id,
    from_status,
    to_status,
    message,
    attributes
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING *;

-- name: ListRunStateEventsByRun :many
SELECT *
FROM run_state_events
WHERE run_id = $1
ORDER BY created_at ASC;

-- name: ListActiveRunsByProject :many
SELECT
    r.id,
    r.project_id,
    r.status,
    r.input_summary,
    r.output_summary,
    r.error,
    r.trace_id,
    r.latency_ms,
    r.created_at,
    r.updated_at,
    a.id AS agent_id,
    a.name AS agent_name,
    ai.id AS agent_instance_id,
    ai.instance_name AS agent_instance_name,
    rse.created_at AS last_state_event_at,
    rse.to_status AS last_state
FROM runs r
LEFT JOIN agent_instances ai ON ai.active_run_id = r.id
LEFT JOIN agents a ON a.id = ai.agent_id
LEFT JOIN LATERAL (
    SELECT created_at, to_status
    FROM run_state_events
    WHERE run_id = r.id
    ORDER BY created_at DESC
    LIMIT 1
) rse ON true
WHERE r.project_id = $1
  AND r.status IN ('queued', 'running', 'waiting_llm', 'waiting_tool', 'waiting_retrieval', 'retrying')
ORDER BY r.updated_at DESC
LIMIT $2 OFFSET $3;

-- name: GetRunRuntimeDetail :one
SELECT
    r.id,
    r.project_id,
    r.status,
    r.input_summary,
    r.output_summary,
    r.error,
    r.trace_id,
    r.latency_ms,
    r.created_at,
    r.updated_at,
    a.id AS agent_id,
    a.name AS agent_name,
    ai.id AS agent_instance_id,
    ai.instance_name AS agent_instance_name
FROM runs r
LEFT JOIN agent_instances ai ON ai.active_run_id = r.id
LEFT JOIN agents a ON a.id = ai.agent_id
WHERE r.id = $1 AND r.project_id = $2;

-- name: CompleteRuntimeRun :one
UPDATE runs
SET
    status = 'completed',
    output_summary = $3,
    output = $3,
    ended_at = now(),
    latency_ms = $4,
    updated_at = now()
WHERE id = $1 AND project_id = $2
RETURNING *;

-- name: FailRuntimeRun :one
UPDATE runs
SET
    status = 'failed',
    error = $3,
    ended_at = now(),
    latency_ms = $4,
    updated_at = now()
WHERE id = $1 AND project_id = $2
RETURNING *;
