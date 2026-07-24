-- Phase 4: versioned rubrics, independent judge votes and calibration storage.

CREATE TABLE IF NOT EXISTS rubric_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(project_id, name)
);

CREATE TABLE IF NOT EXISTS rubric_template_versions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    template_id UUID NOT NULL REFERENCES rubric_templates(id) ON DELETE CASCADE,
    version INT NOT NULL CHECK (version > 0),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),
    rubric JSONB NOT NULL,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    UNIQUE(template_id, version)
);

ALTER TABLE eval_metric_definitions
    ADD COLUMN IF NOT EXISTS rubric_version_id UUID
        REFERENCES rubric_template_versions(id) ON DELETE RESTRICT;

CREATE TABLE IF NOT EXISTS eval_judge_votes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    metric_name TEXT NOT NULL,
    metric_kind TEXT NOT NULL,
    evaluator_id UUID REFERENCES evaluators(id) ON DELETE SET NULL,
    rubric_version_id UUID REFERENCES rubric_template_versions(id) ON DELETE RESTRICT,
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    prompt_version TEXT NOT NULL DEFAULT '',
    score NUMERIC(6,4) CHECK (score IS NULL OR (score >= 0 AND score <= 1)),
    passed BOOLEAN,
    reason TEXT NOT NULL DEFAULT '',
    raw_response_hash TEXT NOT NULL DEFAULT '',
    latency_ms BIGINT,
    input_tokens BIGINT,
    output_tokens BIGINT,
    cost NUMERIC(18,8),
    error TEXT NOT NULL DEFAULT '',
    essential_failed BOOLEAN NOT NULL DEFAULT false,
    veto_triggered BOOLEAN NOT NULL DEFAULT false,
    veto_reason TEXT NOT NULL DEFAULT '',
    details JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (
        (error <> '' AND score IS NULL AND passed IS NULL)
        OR
        (error = '' AND score IS NOT NULL AND passed IS NOT NULL)
    )
);

CREATE INDEX IF NOT EXISTS idx_eval_judge_votes_run_metric
    ON eval_judge_votes(project_id, run_id, metric_name, created_at);

CREATE TABLE IF NOT EXISTS eval_score_aggregations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    metric_name TEXT NOT NULL,
    metric_kind TEXT NOT NULL,
    rubric_version_id UUID REFERENCES rubric_template_versions(id) ON DELETE RESTRICT,
    method TEXT NOT NULL CHECK (method IN ('mean', 'median', 'trimmed_mean', 'majority')),
    required_votes INT NOT NULL CHECK (required_votes > 0),
    valid_vote_count INT NOT NULL,
    error_vote_count INT NOT NULL,
    score NUMERIC(6,4) NOT NULL CHECK (score >= 0 AND score <= 1),
    passed BOOLEAN NOT NULL,
    stddev NUMERIC(8,6) NOT NULL,
    score_range NUMERIC(8,6) NOT NULL,
    disagreement NUMERIC(8,6) NOT NULL,
    confidence NUMERIC(8,6) NOT NULL,
    low_confidence BOOLEAN NOT NULL,
    essential_failed BOOLEAN NOT NULL DEFAULT false,
    veto_triggered BOOLEAN NOT NULL DEFAULT false,
    veto_reason TEXT NOT NULL DEFAULT '',
    details JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_eval_score_aggregations_run
    ON eval_score_aggregations(project_id, run_id, created_at DESC);

CREATE TABLE IF NOT EXISTS eval_calibration_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    rubric_version_id UUID NOT NULL REFERENCES rubric_template_versions(id) ON DELETE RESTRICT,
    status TEXT NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'running', 'completed', 'failed')),
    sample_run_ids UUID[] NOT NULL DEFAULT '{}',
    report JSONB NOT NULL DEFAULT '{}',
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

ALTER TABLE annotation_items ADD COLUMN IF NOT EXISTS gold_scores JSONB NOT NULL DEFAULT '{}';
ALTER TABLE annotation_items ADD COLUMN IF NOT EXISTS is_gold BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE annotation_items ADD COLUMN IF NOT EXISTS annotator_id UUID REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE annotation_items ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ;

ALTER TABLE trace_sampling_rules ADD COLUMN IF NOT EXISTS strategy TEXT NOT NULL DEFAULT 'random';
ALTER TABLE trace_sampling_rules ADD COLUMN IF NOT EXISTS daily_budget INT NOT NULL DEFAULT 0;
ALTER TABLE trace_sampling_rules ADD COLUMN IF NOT EXISTS stratification JSONB NOT NULL DEFAULT '{}';

CREATE TABLE IF NOT EXISTS trace_sampling_decisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    rule_id UUID NOT NULL REFERENCES trace_sampling_rules(id) ON DELETE CASCADE,
    run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    stratum TEXT NOT NULL DEFAULT '',
    sampled_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(rule_id, run_id)
);

CREATE INDEX IF NOT EXISTS idx_trace_sampling_decisions_budget
    ON trace_sampling_decisions(rule_id, sampled_at, stratum);

CREATE UNIQUE INDEX IF NOT EXISTS idx_annotation_items_active_run
    ON annotation_items(project_id, run_id)
    WHERE status IN ('pending', 'in_progress');

-- Published versions are immutable; publishing and creating a new version are
-- the only supported mutation path.
CREATE OR REPLACE FUNCTION reject_published_rubric_mutation()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF OLD.status = 'published' THEN
        RAISE EXCEPTION 'published rubric versions are immutable';
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS rubric_version_immutable ON rubric_template_versions;
CREATE TRIGGER rubric_version_immutable
BEFORE UPDATE OR DELETE ON rubric_template_versions
FOR EACH ROW EXECUTE FUNCTION reject_published_rubric_mutation();
