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
SELECT ecr.id, ecr.eval_job_id, ecr.run_id, ecr.score, ecr.passed, ecr.reason, ecr.details, ecr.actual_output, ecr.latency_ms, ecr.cost, ecr.created_at,
       ej.dataset_case_id
FROM eval_case_results ecr
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

-- name: ListDatasetCaseIDsByDataset :many
SELECT id FROM dataset_cases WHERE dataset_id = $1 ORDER BY sort_order ASC;

-- name: HeartbeatEvalJobByProject :one
UPDATE eval_jobs
SET lease_expires_at = $2, updated_at = now()
WHERE eval_jobs.id = $1
  AND runner_name = $3
  AND status = 'running'
  AND EXISTS (
    SELECT 1 FROM experiments e
    WHERE e.id = eval_jobs.experiment_id AND e.project_id = $4
  )
RETURNING *;

-- name: UpdateEvalJobStatusByProject :one
UPDATE eval_jobs
SET status = $2, error = $3, updated_at = now()
WHERE eval_jobs.id = $1
  AND eval_jobs.runner_name = $5
  AND eval_jobs.status = 'running'
  AND EXISTS (
    SELECT 1 FROM experiments e
    WHERE e.id = eval_jobs.experiment_id AND e.project_id = $4
  )
RETURNING *;

-- name: GetEvalJobLockedByProject :one
SELECT ej.* FROM eval_jobs ej
JOIN experiments e ON e.id = ej.experiment_id
WHERE ej.id = $1 AND e.project_id = $2
FOR UPDATE OF ej;

-- name: UpdateExperimentStatus :one
UPDATE experiments
SET status = $2, passed_cases = $3, failed_cases = $4, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: ListEvaluatorsByProject :many
SELECT * FROM evaluators WHERE project_id = $1 ORDER BY created_at DESC;

-- name: GetEvaluatorByID :one
SELECT * FROM evaluators WHERE id = $1;

-- ============================================================
-- Queries for Phase 2.5: Multi-dimensional evaluation scores
-- ============================================================

-- name: CreateMetricDefinition :one
INSERT INTO eval_metric_definitions (project_id, name, kind, config)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListMetricDefinitionsByProject :many
SELECT * FROM eval_metric_definitions WHERE project_id = $1 ORDER BY created_at DESC;

-- name: GetMetricDefinitionByID :one
SELECT * FROM eval_metric_definitions WHERE id = $1;

-- name: CreateEvalScore :one
INSERT INTO eval_scores (project_id, run_id, trace_id, span_id,
                         metric_name, metric_kind, score, passed, reason, details,
                         evaluator_run_id)
SELECT $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11
WHERE EXISTS (SELECT 1 FROM runs WHERE id = $2 AND project_id = $1)
RETURNING *;

-- name: ListEvalScoresByRun :many
SELECT es.* FROM eval_scores es
JOIN runs r ON r.id = es.run_id
WHERE es.run_id = $1 AND r.project_id = $2
ORDER BY es.created_at DESC;

-- name: ListEvalScoresByProject :many
SELECT * FROM eval_scores WHERE project_id = $1 ORDER BY created_at DESC;

-- name: ListEvalScoresByProjectAndMetricKind :many
SELECT * FROM eval_scores
WHERE project_id = $1 AND metric_kind = $2
ORDER BY created_at DESC;

-- name: AggregateEvalScoresByExperiment :many
SELECT es.metric_kind,
       COUNT(*)::bigint AS total_count,
       COUNT(*) FILTER (WHERE es.passed = true)::bigint AS passed_count,
       ROUND(AVG(es.score)::numeric, 4) AS avg_score
FROM eval_scores es
JOIN runs r ON r.id = es.run_id
JOIN eval_case_results ecr ON ecr.run_id = r.id
JOIN eval_jobs ej ON ej.id = ecr.eval_job_id
JOIN experiments e ON e.id = ej.experiment_id
WHERE ej.experiment_id = $1 AND e.project_id = $2
GROUP BY es.metric_kind
ORDER BY es.metric_kind;

-- name: ListEvalScoresByRunAndKind :many
SELECT es.* FROM eval_scores es
JOIN runs r ON r.id = es.run_id
WHERE es.run_id = $1 AND es.metric_kind = $2 AND r.project_id = $3
ORDER BY es.created_at DESC;

-- name: CountEvalScoresByRun :one
SELECT COUNT(*)::bigint FROM eval_scores WHERE run_id = $1;

-- name: ListEvalScoresByRunIDs :many
SELECT es.* FROM eval_scores es
WHERE es.project_id = $1 AND es.run_id = ANY($2::uuid[])
ORDER BY es.run_id, es.created_at DESC;

-- ============================================================
-- Phase 3.6: Experiment Comparison queries
-- ============================================================

-- name: CreateExperimentComparison :one
INSERT INTO experiment_comparisons (
    project_id, baseline_experiment_id, candidate_experiment_id,
    comparison_type, created_by, status, summary, regression_gates
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: GetExperimentComparisonByID :one
SELECT * FROM experiment_comparisons WHERE id = $1;

-- name: ListExperimentComparisonsByProject :many
SELECT * FROM experiment_comparisons WHERE project_id = $1 ORDER BY created_at DESC;

-- name: ListExperimentComparisonsByExperiment :many
SELECT * FROM experiment_comparisons
WHERE baseline_experiment_id = $1 OR candidate_experiment_id = $1
ORDER BY created_at DESC;

-- name: CreateComparisonCaseResult :one
INSERT INTO comparison_case_results (
    comparison_id, dataset_case_id,
    baseline_run_id, candidate_run_id,
    baseline_score, candidate_score,
    baseline_passed, candidate_passed,
    score_diff, status,
    baseline_latency_ms, candidate_latency_ms,
    baseline_cost, candidate_cost,
    details
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
RETURNING *;

-- name: ListComparisonCaseResults :many
SELECT * FROM comparison_case_results WHERE comparison_id = $1 ORDER BY created_at ASC;

-- name: DeleteComparisonCaseResults :exec
DELETE FROM comparison_case_results WHERE comparison_id = $1;

-- name: DeleteExperimentComparison :exec
DELETE FROM experiment_comparisons WHERE id = $1;

-- name: UpdateExperimentComparison :one
UPDATE experiment_comparisons
SET status = $2, summary = $3, regression_gates = $4, updated_at = now()
WHERE id = $1
RETURNING *;

-- name: UpdateExperimentHarnessMetadata :one
UPDATE experiments
SET model_provider = $2, model_name = $3, model_version = $4,
    prompt_version = $5, harness_version = $6, dataset_version = $7,
    feature_flags = $8, updated_at = now()
WHERE id = $1
RETURNING *;
