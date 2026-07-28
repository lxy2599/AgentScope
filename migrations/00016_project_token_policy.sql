-- Project-level production token policy. PostgreSQL remains the source of truth.
CREATE TABLE IF NOT EXISTS project_token_policies (
  project_id UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
  allow_non_expiring BOOLEAN NOT NULL DEFAULT true,
  max_production_ttl_days INTEGER,
  max_ci_ttl_days INTEGER,
  expiry_warning_days INTEGER[] NOT NULL DEFAULT ARRAY[14, 7, 1],
  unused_warning_days INTEGER NOT NULL DEFAULT 60,
  updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CHECK (max_production_ttl_days IS NULL OR max_production_ttl_days > 0),
  CHECK (max_ci_ttl_days IS NULL OR max_ci_ttl_days > 0),
  CHECK (unused_warning_days > 0)
);
