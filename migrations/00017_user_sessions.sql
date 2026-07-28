-- Revocable server-authoritative browser sessions. Cookie secrets are never persisted.
CREATE TABLE IF NOT EXISTS user_sessions (
  id UUID PRIMARY KEY,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  secret_hash BYTEA NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_used_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ,
  revoked_reason TEXT,
  user_agent_summary TEXT,
  ip_prefix TEXT
);

CREATE INDEX IF NOT EXISTS idx_user_sessions_active_user
  ON user_sessions(user_id, expires_at DESC)
  WHERE revoked_at IS NULL;
