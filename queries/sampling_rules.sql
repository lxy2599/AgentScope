-- Trace sampling rule queries.

-- name: CreateTraceSamplingRule :one
INSERT INTO trace_sampling_rules (project_id, agent_id, name, enabled, sample_rate, filters)
SELECT $1, $2, $3, $4, $5, $6
WHERE $2::uuid IS NULL
   OR EXISTS (
       SELECT 1 FROM agents a
       WHERE a.id = $2 AND a.project_id = $1
   )
RETURNING *;

-- name: GetTraceSamplingRuleByID :one
SELECT * FROM trace_sampling_rules WHERE id = $1;

-- name: ListTraceSamplingRulesByProject :many
SELECT * FROM trace_sampling_rules WHERE project_id = $1 ORDER BY created_at DESC;

-- name: UpdateTraceSamplingRuleByProject :one
UPDATE trace_sampling_rules
SET name = COALESCE(sqlc.narg('name')::text, name),
    enabled = COALESCE(sqlc.narg('enabled')::boolean, enabled),
    sample_rate = COALESCE(sqlc.narg('sample_rate')::real, sample_rate),
    filters = COALESCE(sqlc.narg('filters')::jsonb, filters),
    updated_at = now()
WHERE id = sqlc.arg('id')
  AND project_id = sqlc.arg('project_id')
RETURNING *;

-- name: DeleteTraceSamplingRule :one
DELETE FROM trace_sampling_rules WHERE id = $1 RETURNING id;

-- name: ListEnabledSamplingRulesForRun :many
SELECT * FROM trace_sampling_rules
WHERE project_id = $1
  AND enabled = true
  AND (
    -- Match any agent_id = NULL (applies to all agents) OR specific agent
    agent_id IS NULL OR agent_id = $2::uuid
  )
ORDER BY sample_rate DESC;
