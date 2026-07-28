-- Durable, PostgreSQL-authoritative online evaluation work queue.
CREATE TABLE IF NOT EXISTS online_eval_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    trace_id TEXT NOT NULL DEFAULT '',
    evaluator_version TEXT NOT NULL DEFAULT 'trace-aware-v1',
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','sampling','running','retry_wait','succeeded','skipped','failed','dead_letter')),
    sampling_rule_id UUID REFERENCES trace_sampling_rules(id) ON DELETE SET NULL,
    sampling_reason TEXT NOT NULL DEFAULT '',
    evaluation_source TEXT NOT NULL DEFAULT '',
    fallback_reason TEXT NOT NULL DEFAULT '',
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    max_attempts INTEGER NOT NULL DEFAULT 5 CHECK (max_attempts >= 1),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_by TEXT,
    claimed_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    finished_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    annotation_item_id UUID REFERENCES annotation_items(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(project_id, run_id, evaluator_version)
);

CREATE INDEX IF NOT EXISTS idx_online_eval_jobs_claim
    ON online_eval_jobs(status, available_at, created_at)
    WHERE status IN ('queued', 'retry_wait');
CREATE INDEX IF NOT EXISTS idx_online_eval_jobs_project_run ON online_eval_jobs(project_id, run_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_online_eval_jobs_stale_claim ON online_eval_jobs(status, claimed_at)
    WHERE status = 'running';

-- Tie online scores and human-review items to a durable job so retries are idempotent.
ALTER TABLE eval_scores ADD COLUMN IF NOT EXISTS online_eval_job_id UUID REFERENCES online_eval_jobs(id) ON DELETE CASCADE;
CREATE UNIQUE INDEX IF NOT EXISTS idx_eval_scores_online_job_metric
    ON eval_scores(online_eval_job_id, metric_name, metric_kind)
    WHERE online_eval_job_id IS NOT NULL;

ALTER TABLE annotation_items ADD COLUMN IF NOT EXISTS online_eval_job_id UUID REFERENCES online_eval_jobs(id) ON DELETE SET NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_annotation_items_online_job
    ON annotation_items(online_eval_job_id)
    WHERE online_eval_job_id IS NOT NULL;
