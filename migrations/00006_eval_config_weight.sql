-- Migration 00006: Add weight, pass_threshold, enabled fields to eval_metric_definitions.
-- Also add aggregate_score tracking and inline evaluator config support for experiments.

-- ============================================================
-- 1. Extend eval_metric_definitions with weight, threshold, enabled
-- ============================================================
ALTER TABLE eval_metric_definitions
    ADD COLUMN IF NOT EXISTS weight NUMERIC(6,4) NOT NULL DEFAULT 1.0,
    ADD COLUMN IF NOT EXISTS pass_threshold NUMERIC(6,4) NOT NULL DEFAULT 0.7,
    ADD COLUMN IF NOT EXISTS enabled BOOLEAN NOT NULL DEFAULT true;

-- Validate weight (must be >= 0)
ALTER TABLE eval_metric_definitions
    ADD CONSTRAINT eval_metric_definitions_weight_check
    CHECK (weight >= 0.0);

-- Validate pass_threshold (must be in [0, 1])
ALTER TABLE eval_metric_definitions
    ADD CONSTRAINT eval_metric_definitions_pass_threshold_check
    CHECK (pass_threshold >= 0.0 AND pass_threshold <= 1.0);

-- ============================================================
-- 2. Add evaluator_config JSONB to experiments for inline evaluator configs
-- ============================================================
ALTER TABLE experiments
    ADD COLUMN IF NOT EXISTS evaluator_config JSONB;

-- evaluator_config is NOT NULL DEFAULT '{}', only when evaluator_id is NULL
-- This allows an experiment to carry its evaluator config directly without
-- requiring a separate evaluators row.

-- ============================================================
-- 3. Add quality_pass_cases / quality_failed_cases to experiments
-- ============================================================
ALTER TABLE experiments
    ADD COLUMN IF NOT EXISTS quality_pass_cases INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS quality_failed_cases INT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS pass_threshold NUMERIC(6,4) NOT NULL DEFAULT 0.7;
