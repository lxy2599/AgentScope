-- Dataset, experiment, eval job, and eval case result queries.

-- name: CreateDataset :one
INSERT INTO datasets (project_id, name, description)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ListDatasetsByProject :many
SELECT * FROM datasets WHERE project_id = $1 ORDER BY created_at DESC;

-- name: CreateDatasetCase :one
INSERT INTO dataset_cases (dataset_id, input, expected, metadata, sort_order)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListDatasetCasesByDataset :many
SELECT * FROM dataset_cases WHERE dataset_id = $1 ORDER BY sort_order ASC;

-- name: CreateEvaluator :one
INSERT INTO evaluators (project_id, name, type, config)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: CreateExperiment :one
INSERT INTO experiments (project_id, dataset_id, evaluator_id, target_name, target_version,
                         status, aggregate_score, total_cases, passed_cases, failed_cases, metadata)
VALUES ($1, $2, $3, $4, $5,
        $6, $7, $8, $9, $10, $11)
RETURNING *;

-- name: GetExperimentByID :one
SELECT * FROM experiments WHERE id = $1;

-- name: ListExperimentsByProject :many
SELECT * FROM experiments WHERE project_id = $1 ORDER BY created_at DESC;

-- name: GetEvalJobByID :one
SELECT * FROM eval_jobs WHERE id = $1;

-- name: CreateEvalJob :one
INSERT INTO eval_jobs (experiment_id, dataset_case_id, status)
VALUES ($1, $2, $3)
RETURNING *;

-- name: ClaimEvalJob :one
UPDATE eval_jobs
SET status       = 'running',
    runner_name  = $2,
    lease_expires_at = $3,
    attempts     = attempts + 1,
    updated_at   = now()
WHERE id = $1
  AND $3::timestamptz IS NOT NULL
  AND (status = 'queued'
       OR (status = 'running' AND lease_expires_at < now()))
RETURNING *;

-- name: UpdateEvalJobStatus :one
UPDATE eval_jobs SET status = $2, error = $3, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CreateEvalCaseResult :one
INSERT INTO eval_case_results (eval_job_id, run_id, score, passed, reason, details,
                               actual_output, latency_ms, cost)
VALUES ($1, $2, $3, $4, $5, $6,
        $7, $8, $9)
RETURNING *;

-- name: ListEvalResultsByExperiment :many
SELECT ecr.* FROM eval_case_results ecr
JOIN eval_jobs ej ON ej.id = ecr.eval_job_id
WHERE ej.experiment_id = $1
ORDER BY ecr.created_at DESC;

-- ============================================================
-- Additional queries for Task 6: Dataset, Experiment, Runner
-- ============================================================

-- name: GetDatasetByID :one
SELECT * FROM datasets WHERE id = $1;

-- name: GetDatasetCaseByID :one
SELECT * FROM dataset_cases WHERE id = $1;

-- name: CountDatasetCases :one
SELECT COUNT(*)::bigint FROM dataset_cases WHERE dataset_id = $1;

-- name: ListEvalJobsByExperiment :many
SELECT * FROM eval_jobs WHERE experiment_id = $1 ORDER BY created_at ASC;

-- name: ClaimNextEvalJob :one
UPDATE eval_jobs
SET status = 'running',
    runner_name = $2,
    lease_expires_at = $3,
    attempts = attempts + 1,
    updated_at = now()
WHERE id = (
  SELECT ej.id FROM eval_jobs ej
  JOIN experiments e ON e.id = ej.experiment_id
  WHERE e.project_id = $1
    AND (ej.status = 'queued' OR (ej.status = 'running' AND ej.lease_expires_at < now()))
  ORDER BY ej.created_at ASC
  LIMIT 1
  FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: HeartbeatEvalJob :one
UPDATE eval_jobs
SET lease_expires_at = $2, updated_at = now()
WHERE id = $1 AND runner_name = $3 AND status = 'running'
RETURNING *;

-- name: GetExperimentJobStatusCounts :many
SELECT status, COUNT(*)::bigint AS count
FROM eval_jobs
WHERE experiment_id = $1
GROUP BY status;

-- name: UpdateExperimentStatus :one
UPDATE experiments
SET status = $2, passed_cases = $3, failed_cases = $4, updated_at = now()
WHERE id = $1
RETURNING *;
