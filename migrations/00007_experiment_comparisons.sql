-- Migration 00007: Experiment Comparisons and Harness Metadata.
--
-- Adds:
--   - Harness metadata columns on experiments (nullable for backward compat)
--   - experiment_comparisons – baseline vs candidate comparison definition
--   - comparison_case_results – per-case comparison results
--
-- Safety: each ALTER TABLE ADD COLUMN uses IF NOT EXISTS for idempotency.

-- ============================================================
-- Harness metadata columns on experiments (nullable, backward-compatible)
-- ============================================================

ALTER TABLE experiments ADD COLUMN IF NOT EXISTS model_provider TEXT;
ALTER TABLE experiments ADD COLUMN IF NOT EXISTS model_name TEXT;
ALTER TABLE experiments ADD COLUMN IF NOT EXISTS model_version TEXT;
ALTER TABLE experiments ADD COLUMN IF NOT EXISTS prompt_version TEXT;
ALTER TABLE experiments ADD COLUMN IF NOT EXISTS harness_version TEXT;
ALTER TABLE experiments ADD COLUMN IF NOT EXISTS dataset_version TEXT;
ALTER TABLE experiments ADD COLUMN IF NOT EXISTS toolset_version TEXT;
ALTER TABLE experiments ADD COLUMN IF NOT EXISTS skillset_version TEXT;
ALTER TABLE experiments ADD COLUMN IF NOT EXISTS rag_config_version TEXT;
ALTER TABLE experiments ADD COLUMN IF NOT EXISTS feature_flags JSONB NOT NULL DEFAULT '{}';

-- ============================================================
-- Experiment comparisons (baseline vs candidate)
-- ============================================================

CREATE TABLE IF NOT EXISTS experiment_comparisons (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    baseline_experiment_id UUID NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    candidate_experiment_id UUID NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    comparison_type TEXT NOT NULL DEFAULT 'general'
        CHECK (comparison_type IN ('model_swap', 'ablation', 'general')),
    created_by UUID NOT NULL REFERENCES users(id),
    status TEXT NOT NULL DEFAULT 'completed',
    summary JSONB NOT NULL DEFAULT '{}',
    regression_gates JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_comparisons_project ON experiment_comparisons(project_id);
CREATE INDEX IF NOT EXISTS idx_comparisons_baseline ON experiment_comparisons(baseline_experiment_id);
CREATE INDEX IF NOT EXISTS idx_comparisons_candidate ON experiment_comparisons(candidate_experiment_id);

-- ============================================================
-- Per-case comparison results
-- ============================================================

CREATE TABLE IF NOT EXISTS comparison_case_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    comparison_id UUID NOT NULL REFERENCES experiment_comparisons(id) ON DELETE CASCADE,
    dataset_case_id UUID NOT NULL REFERENCES dataset_cases(id),
    baseline_run_id UUID REFERENCES runs(id),
    candidate_run_id UUID REFERENCES runs(id),
    baseline_score NUMERIC(10,4),
    candidate_score NUMERIC(10,4),
    baseline_passed BOOLEAN,
    candidate_passed BOOLEAN,
    score_diff NUMERIC(10,4),
    status TEXT NOT NULL DEFAULT 'unchanged'
        CHECK (status IN ('improved', 'regressed', 'unchanged', 'incomparable')),
    baseline_latency_ms BIGINT,
    candidate_latency_ms BIGINT,
    baseline_cost NUMERIC(10,6),
    candidate_cost NUMERIC(10,6),
    details JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_comparison_case_results_comparison ON comparison_case_results(comparison_id);
CREATE INDEX IF NOT EXISTS idx_comparison_case_results_status ON comparison_case_results(status);
