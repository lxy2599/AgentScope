-- Trace sampling rule queries.

-- name: CreateTraceSamplingRule :one
INSERT INTO trace_sampling_rules (
    project_id, agent_id, name, enabled, sample_rate, filters,
    strategy, daily_budget, stratification
)
SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9
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

-- name: ReserveTraceSamplingDecision :one
INSERT INTO trace_sampling_decisions (rule_id, run_id, stratum)
SELECT sqlc.arg(rule_id), sqlc.arg(run_id), sqlc.arg(stratum)
WHERE (
    sqlc.arg(daily_budget)::int = 0
    OR (
        SELECT count(*)
        FROM trace_sampling_decisions
        WHERE rule_id = sqlc.arg(rule_id)
          AND sampled_at >= date_trunc('day', now())
    ) < sqlc.arg(daily_budget)::int
)
AND (
    sqlc.arg(stratum_budget)::int = 0
    OR (
        SELECT count(*)
        FROM trace_sampling_decisions
        WHERE rule_id = sqlc.arg(rule_id)
          AND stratum = sqlc.arg(stratum)
          AND sampled_at >= date_trunc('day', now())
    ) < sqlc.arg(stratum_budget)::int
)
ON CONFLICT (rule_id, run_id) DO NOTHING
RETURNING true;
