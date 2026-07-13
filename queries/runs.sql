-- Run / span / span-event queries.

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

-- name: CreateSpan :one
INSERT INTO spans (run_id, trace_id, span_id, parent_span_id, name, kind,
                   status_code, status_message, start_time, end_time, attributes, truncated)
VALUES ($1, $2, $3, $4, $5, $6,
        $7, $8, $9, $10, $11, $12)
RETURNING *;

-- name: GetSpanByID :one
SELECT * FROM spans WHERE id = $1;

-- name: ListSpansByRunID :many
SELECT * FROM spans WHERE run_id = $1 ORDER BY start_time ASC;

-- name: CreateSpanEvent :one
INSERT INTO span_events (span_id, name, time, attributes, truncated)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListSpanEventsBySpanID :many
SELECT * FROM span_events WHERE span_id = $1 ORDER BY time ASC;
