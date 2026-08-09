-- Benchmark suite, benchmark experiment, and benchmark runner queries.

-- name: CreateBenchmarkSuite :one
INSERT INTO benchmark_suites (
    project_id,
    dataset_id,
    benchmark_key,
    name,
    description,
    version,
    harness_kind,
    execution_kind,
    source_uri,
    source_commit,
    default_execution_config,
    required_capabilities,
    metadata
)
SELECT
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13
WHERE EXISTS (
    SELECT 1
    FROM datasets d
    WHERE d.id = $2
      AND d.project_id = $1
)
RETURNING *;

-- name: GetBenchmarkSuiteByProject :one
SELECT *
FROM benchmark_suites
WHERE id = $1
  AND project_id = $2;

-- name: ListBenchmarkSuitesByProject :many
SELECT *
FROM benchmark_suites
WHERE project_id = $1
ORDER BY created_at DESC;

-- name: CreateBenchmarkExperiment :one
INSERT INTO experiments (
    project_id,
    dataset_id,
    evaluator_id,
    target_name,
    target_version,
    status,
    aggregate_score,
    total_cases,
    passed_cases,
    failed_cases,
    metadata,
    model_provider,
    model_name,
    model_version,
    prompt_version,
    harness_version,
    dataset_version,
    feature_flags,
    benchmark_suite_id,
    benchmark_key,
    benchmark_version,
    benchmark_config,
    trials_per_case
)
SELECT
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11,
    $12, $13, $14, $15, $16, $17, COALESCE(sqlc.narg('feature_flags')::jsonb, '{}'::jsonb),
    suite.id, suite.benchmark_key, suite.version, $19, $20
FROM benchmark_suites suite
WHERE suite.id = $18
  AND suite.project_id = $1
  AND suite.dataset_id = $2
RETURNING *;

-- name: CreateBenchmarkEvalJob :one
INSERT INTO eval_jobs (
    experiment_id,
    dataset_case_id,
    status,
    execution_kind,
    execution_config,
    trial_index,
    seed
)
SELECT $1, $2, $3, $4, $5, $6, $7
FROM experiments e
JOIN dataset_cases dc
  ON dc.id = $2
 AND dc.dataset_id = e.dataset_id
WHERE e.id = $1
  AND e.benchmark_suite_id IS NOT NULL
RETURNING *;

-- name: ClaimNextBenchmarkEvalJob :one
UPDATE eval_jobs
SET status = 'running',
    runner_name = $2,
    lease_expires_at = $3,
    attempts = attempts + 1,
    updated_at = now()
WHERE id = (
    SELECT ej.id
    FROM eval_jobs ej
    JOIN experiments e ON e.id = ej.experiment_id
    WHERE e.project_id = $1
      AND ej.execution_kind = ANY($4::text[])
      AND (
          ej.status = 'queued'
          OR (ej.status = 'running' AND ej.lease_expires_at < now())
      )
    ORDER BY ej.created_at ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: ClaimNextBenchmarkEvalJobForExperiment :one
UPDATE eval_jobs
SET status = 'running',
    runner_name = $3,
    lease_expires_at = $4,
    attempts = attempts + 1,
    updated_at = now()
WHERE id = (
    SELECT ej.id
    FROM eval_jobs ej
    JOIN experiments e ON e.id = ej.experiment_id
    WHERE e.project_id = $1
      AND e.id = $2
      AND ej.execution_kind = ANY($5::text[])
      AND (
          ej.status = 'queued'
          OR (ej.status = 'running' AND ej.lease_expires_at < now())
      )
    ORDER BY ej.created_at ASC
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: GetBenchmarkClaimContext :one
SELECT
    ej.id AS eval_job_id,
    ej.experiment_id,
    ej.dataset_case_id,
    ej.status AS job_status,
    ej.runner_name,
    ej.lease_expires_at,
    ej.attempts,
    ej.error AS job_error,
    ej.execution_kind,
    ej.execution_config,
    ej.trial_index,
    ej.seed,
    e.target_name,
    e.target_version,
    e.benchmark_suite_id,
    e.benchmark_key,
    e.benchmark_version,
    e.benchmark_config,
    e.trials_per_case,
    dc.input AS case_input,
    dc.expected AS case_expected,
    dc.metadata AS case_metadata,
    COALESCE(evaluator.config, '{}'::jsonb) AS evaluator_config,
    suite.name AS benchmark_name,
    suite.harness_kind,
    suite.source_uri,
    suite.source_commit,
    suite.required_capabilities
FROM eval_jobs ej
JOIN experiments e ON e.id = ej.experiment_id
JOIN dataset_cases dc ON dc.id = ej.dataset_case_id
LEFT JOIN evaluators evaluator ON evaluator.id = e.evaluator_id
LEFT JOIN benchmark_suites suite ON suite.id = e.benchmark_suite_id
WHERE ej.id = $1
  AND e.project_id = $2;
