-- Durable, project-scoped benchmark job cancellation audit.
--
-- A cancellation is terminal for queued/running eval_jobs. We retain the
-- operator-provided (non-secret) reason on an immutable per-job audit row,
-- rather than overloading eval_jobs.error or deleting queued work.
CREATE TABLE benchmark_eval_job_cancellation_audits (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    experiment_id UUID NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    eval_job_id UUID NOT NULL REFERENCES eval_jobs(id) ON DELETE CASCADE,
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    reason TEXT NOT NULL,
    previous_status TEXT NOT NULL CHECK (previous_status IN ('queued', 'running')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT benchmark_eval_job_cancellation_audits_one_per_job UNIQUE (eval_job_id)
);

CREATE INDEX idx_benchmark_eval_job_cancellation_audits_experiment_created
    ON benchmark_eval_job_cancellation_audits(experiment_id, created_at DESC);
CREATE INDEX idx_benchmark_eval_job_cancellation_audits_project_created
    ON benchmark_eval_job_cancellation_audits(project_id, created_at DESC);
