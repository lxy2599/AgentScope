-- Benchmark control-plane extensions.
--
-- This migration deliberately reuses experiments and eval_jobs as the durable
-- evaluation queue. Benchmark suites provide immutable-ish source metadata,
-- while each eval_job records the execution contract for one independent trial.

CREATE TABLE benchmark_suites (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    dataset_id UUID NOT NULL REFERENCES datasets(id) ON DELETE RESTRICT,
    benchmark_key TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    version TEXT NOT NULL,
    harness_kind TEXT NOT NULL,
    execution_kind TEXT NOT NULL DEFAULT 'function',
    source_uri TEXT,
    source_commit TEXT,
    default_execution_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    required_capabilities JSONB NOT NULL DEFAULT '[]'::jsonb,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT benchmark_suites_execution_config_object
        CHECK (jsonb_typeof(default_execution_config) = 'object'),
    CONSTRAINT benchmark_suites_required_capabilities_array
        CHECK (jsonb_typeof(required_capabilities) = 'array'),
    CONSTRAINT benchmark_suites_metadata_object
        CHECK (jsonb_typeof(metadata) = 'object'),
    CONSTRAINT benchmark_suites_project_key_version_unique
        UNIQUE (project_id, benchmark_key, version)
);

CREATE INDEX idx_benchmark_suites_project_created_at
    ON benchmark_suites(project_id, created_at DESC);
CREATE INDEX idx_benchmark_suites_dataset
    ON benchmark_suites(dataset_id);

ALTER TABLE experiments
    ADD COLUMN benchmark_suite_id UUID REFERENCES benchmark_suites(id) ON DELETE SET NULL,
    ADD COLUMN benchmark_key TEXT,
    ADD COLUMN benchmark_version TEXT,
    ADD COLUMN benchmark_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN trials_per_case INT NOT NULL DEFAULT 1;

ALTER TABLE experiments
    ADD CONSTRAINT experiments_benchmark_config_object
        CHECK (jsonb_typeof(benchmark_config) = 'object'),
    ADD CONSTRAINT experiments_trials_per_case_positive
        CHECK (trials_per_case > 0),
    ADD CONSTRAINT experiments_benchmark_snapshot_complete
        CHECK (
            benchmark_suite_id IS NULL
            OR (benchmark_key IS NOT NULL AND benchmark_version IS NOT NULL)
        );

CREATE INDEX idx_experiments_benchmark_suite
    ON experiments(benchmark_suite_id)
    WHERE benchmark_suite_id IS NOT NULL;

ALTER TABLE eval_jobs
    ADD COLUMN execution_kind TEXT NOT NULL DEFAULT 'function',
    ADD COLUMN execution_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN trial_index INT NOT NULL DEFAULT 0,
    ADD COLUMN seed BIGINT;

ALTER TABLE eval_jobs
    ADD CONSTRAINT eval_jobs_execution_config_object
        CHECK (jsonb_typeof(execution_config) = 'object'),
    ADD CONSTRAINT eval_jobs_trial_index_non_negative
        CHECK (trial_index >= 0);

CREATE UNIQUE INDEX idx_eval_jobs_experiment_case_trial
    ON eval_jobs(experiment_id, dataset_case_id, trial_index);
CREATE INDEX idx_eval_jobs_execution_claim
    ON eval_jobs(execution_kind, status, created_at);
