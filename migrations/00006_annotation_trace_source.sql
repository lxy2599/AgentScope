-- Migration 00006: Add trace-source fields to annotation_items.
--
-- Adds columns needed for the Trace → Annotation → Dataset closed loop:
--   agent_name – copied from runs for traceability
--   input_summary – the run's input
--   output_summary – the run's actual output
--   error – any run error
--   failure_category – for human categorisation
--   privacy_reviewed – flag: has this been privacy-reviewed?
--   reviewer_note – free-text reviewer notes

ALTER TABLE annotation_items ADD COLUMN agent_name TEXT NOT NULL DEFAULT '';
ALTER TABLE annotation_items ADD COLUMN input_summary TEXT NOT NULL DEFAULT '';
ALTER TABLE annotation_items ADD COLUMN output_summary TEXT NOT NULL DEFAULT '';
ALTER TABLE annotation_items ADD COLUMN error TEXT NOT NULL DEFAULT '';
ALTER TABLE annotation_items ADD COLUMN failure_category TEXT NOT NULL DEFAULT '';
ALTER TABLE annotation_items ADD COLUMN privacy_reviewed BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE annotation_items ADD COLUMN reviewer_note TEXT NOT NULL DEFAULT '';
