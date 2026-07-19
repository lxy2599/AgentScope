-- Migration 00005: Repair dev schema for columns added retroactively to 00001.
--
-- This migration adds columns/tables that were introduced in later edits of
-- migrations/00001_init.sql but never shipped as a standalone migration.
-- It is designed to be idempotent (safe to re-run).
--
-- Related: 00002, 00003, 00004 are already idempotent with IF NOT EXISTS.

-- ============================================================
-- agent_instances: columns added retroactively to 00001_init.sql
-- ============================================================

ALTER TABLE agent_instances ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES projects(id) ON DELETE CASCADE;

ALTER TABLE agent_instances ADD COLUMN IF NOT EXISTS instance_name TEXT NOT NULL DEFAULT '';

ALTER TABLE agent_instances ADD COLUMN IF NOT EXISTS active_run_id UUID REFERENCES runs(id) ON DELETE SET NULL;

ALTER TABLE agent_instances ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- Backfill project_id for rows that were created before this column existed
UPDATE agent_instances ai
SET project_id = a.project_id
FROM agents a
WHERE ai.agent_id = a.id AND ai.project_id IS NULL;

-- ============================================================
-- agents: last_heartbeat_at needed by UpdateAgentStatus query
-- ============================================================

ALTER TABLE agents ADD COLUMN IF NOT EXISTS last_heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT now();
