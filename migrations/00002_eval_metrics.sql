-- Migration 00002: Multi-dimensional evaluation scores.
-- Adds eval_metric_definitions and eval_scores tables.
-- Does not modify existing eval_case_results for backwards compatibility.

-- ============================================================
-- Eval metric definitions (optional metadata for known metrics)
-- ============================================================
CREATE TABLE IF NOT EXISTS eval_metric_definitions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    kind TEXT NOT NULL,
    config JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(project_id, name)
);

-- ============================================================
-- Eval scores: multi-dimensional scoring for runs and traces
-- ============================================================
CREATE TABLE IF NOT EXISTS eval_scores (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    trace_id TEXT NOT NULL DEFAULT '',
    span_id TEXT NOT NULL DEFAULT '',
    metric_name TEXT NOT NULL,
    metric_kind TEXT NOT NULL,
    score NUMERIC(6,4) NOT NULL CHECK (score >= 0.0 AND score <= 1.0),
    passed BOOLEAN NOT NULL DEFAULT false,
    reason TEXT NOT NULL DEFAULT '',
    details JSONB NOT NULL DEFAULT '{}',
    evaluator_run_id UUID REFERENCES runs(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================
-- Indexes for eval_scores
-- ============================================================
CREATE INDEX IF NOT EXISTS idx_eval_scores_project_id ON eval_scores(project_id);
CREATE INDEX IF NOT EXISTS idx_eval_scores_run_id ON eval_scores(run_id);
CREATE INDEX IF NOT EXISTS idx_eval_scores_metric_kind ON eval_scores(metric_kind);
CREATE INDEX IF NOT EXISTS idx_eval_scores_created_at ON eval_scores(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_eval_scores_project_metric ON eval_scores(project_id, metric_kind);
CREATE INDEX IF NOT EXISTS idx_eval_scores_run_metric ON eval_scores(run_id, metric_kind);
