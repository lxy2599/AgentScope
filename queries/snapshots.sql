-- name: CreateRunSnapshot :one
INSERT INTO run_snapshots (
    run_id, project_id, schema_version, canonical_payload, payload_hash
) VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (run_id) DO NOTHING
RETURNING *;

-- name: GetRunSnapshotByRunID :one
SELECT * FROM run_snapshots WHERE run_id = $1;

-- name: GetProjectRunSnapshot :one
SELECT * FROM run_snapshots WHERE run_id = $1 AND project_id = $2;
