-- Token lifecycle audit. No plaintext token or complete token hash is stored here.
ALTER TABLE project_tokens
  ADD COLUMN IF NOT EXISTS last_used_endpoint TEXT,
  ADD COLUMN IF NOT EXISTS last_used_agent_id UUID,
  ADD COLUMN IF NOT EXISTS last_used_instance_id UUID;

CREATE TABLE IF NOT EXISTS project_token_audit_events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  token_id UUID NOT NULL REFERENCES project_tokens(id) ON DELETE RESTRICT,
  actor_user_id UUID REFERENCES users(id) ON DELETE SET NULL,
  event_type TEXT NOT NULL,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_project_token_audit_events_token_time
  ON project_token_audit_events(token_id, created_at DESC);
