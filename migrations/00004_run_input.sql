-- Migration 00004: Add full input/output columns to runs table.
--
-- The runs table previously only stored input_summary and output_summary
-- (truncated/summary versions). This migration adds full input and output
-- TEXT columns so that operations like promote-to-dataset have access to
-- the complete input data, and the online evaluation can reference the full
-- output for richer scoring.

ALTER TABLE runs ADD COLUMN IF NOT EXISTS input TEXT NOT NULL DEFAULT '';
ALTER TABLE runs ADD COLUMN IF NOT EXISTS output TEXT NOT NULL DEFAULT '';
