-- Trace sampling rule queries.

-- name: CreateTraceSamplingRule :one
INSERT INTO trace_sampling_rules (project_id, agent_id, name, enabled, sample_rate, filters)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetTraceSamplingRuleByID :one
SELECT * FROM trace_sampling_rules WHERE id = $1;

-- name: ListTraceSamplingRulesByProject :many
SELECT * FROM trace_sampling_rules WHERE project_id = $1 ORDER BY created_at DESC;

-- name: UpdateTraceSamplingRuleEnabled :one
UPDATE trace_sampling_rules SET enabled = $2, updated_at = now() WHERE id = $1 RETURNING *;

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
