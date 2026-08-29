-- Migration 00025: immutable experiment evaluation snapshots and task semantics.
-- All additions are nullable/defaulted for backwards compatibility.

ALTER TABLE dataset_cases
    ADD COLUMN IF NOT EXISTS expected_behavior JSONB,
    ADD COLUMN IF NOT EXISTS success_criteria JSONB,
    ADD COLUMN IF NOT EXISTS environment JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS allowed_tools JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS graders JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE TABLE IF NOT EXISTS evaluation_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    experiment_id UUID NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    schema_version INTEGER NOT NULL DEFAULT 1,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    content_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(experiment_id),
    UNIQUE(experiment_id, content_hash)
);

ALTER TABLE experiments
    ADD COLUMN IF NOT EXISTS evaluation_snapshot_id UUID;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'experiments_evaluation_snapshot_fk'
    ) THEN
        ALTER TABLE experiments
            ADD CONSTRAINT experiments_evaluation_snapshot_fk
            FOREIGN KEY (evaluation_snapshot_id)
            REFERENCES evaluation_snapshots(id)
            ON DELETE SET NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_evaluation_snapshots_hash
    ON evaluation_snapshots(content_hash);
CREATE INDEX IF NOT EXISTS idx_experiments_evaluation_snapshot
    ON experiments(evaluation_snapshot_id);

-- Explicitly distinguish execution-only results from quality evaluation in the
-- existing JSON details column without changing the legacy numeric schema.
COMMENT ON TABLE evaluation_snapshots IS
    'Immutable normalized Task/Grader/Harness configuration captured at Experiment creation';
