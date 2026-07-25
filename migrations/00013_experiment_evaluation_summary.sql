-- Backfill experiment quality summaries from persisted evaluation outcomes.
--
-- eval_jobs.status describes runner execution, while
-- eval_case_results.passed describes evaluation quality. Older code copied
-- succeeded/failed job counts into passed_cases/failed_cases, which made a
-- successfully executed but low-quality answer appear as passed.

WITH summaries AS (
    SELECT
        e.id AS experiment_id,
        COUNT(DISTINCT ej.id) FILTER (
            WHERE ej.status = 'succeeded' AND ecr.passed IS TRUE
        )::integer AS passed_cases,
        COUNT(DISTINCT ej.id) FILTER (
            WHERE ej.status = 'failed'
               OR (ej.status = 'succeeded' AND ecr.passed IS FALSE)
        )::integer AS failed_cases,
        AVG(ecr.score)::numeric(10,4) AS aggregate_score
    FROM experiments e
    LEFT JOIN eval_jobs ej ON ej.experiment_id = e.id
    LEFT JOIN eval_case_results ecr ON ecr.eval_job_id = ej.id
    GROUP BY e.id
)
UPDATE experiments e
SET passed_cases = summaries.passed_cases,
    failed_cases = summaries.failed_cases,
    aggregate_score = summaries.aggregate_score,
    updated_at = now()
FROM summaries
WHERE e.id = summaries.experiment_id;
