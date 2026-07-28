-- Project Token security baseline: owner/admin governance, revocation and metadata.
ALTER TABLE project_tokens
  ADD COLUMN IF NOT EXISTS created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS revoked_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS revoked_by UUID REFERENCES users(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS revocation_reason TEXT,
  ADD COLUMN IF NOT EXISTS rotates_token_id UUID REFERENCES project_tokens(id) ON DELETE SET NULL,
  ADD COLUMN IF NOT EXISTS environment TEXT NOT NULL DEFAULT 'development',
  ADD COLUMN IF NOT EXISTS token_type TEXT NOT NULL DEFAULT 'custom';

CREATE INDEX IF NOT EXISTS idx_project_tokens_project_created
  ON project_tokens(project_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_project_tokens_active_expiry
  ON project_tokens(project_id, expires_at)
  WHERE revoked_at IS NULL;
