-- Phase 4.3: immutable, server-canonicalized runtime configuration snapshots.

CREATE TABLE IF NOT EXISTS run_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL UNIQUE REFERENCES runs(id) ON DELETE RESTRICT,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE RESTRICT,
    schema_version TEXT NOT NULL,
    canonical_payload JSONB NOT NULL,
    payload_hash CHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT run_snapshot_payload_size
        CHECK (octet_length(canonical_payload::text) <= 262144),
    CONSTRAINT run_snapshot_hash_format
        CHECK (payload_hash ~ '^[0-9a-f]{64}$')
);

CREATE INDEX IF NOT EXISTS idx_run_snapshots_project_created
    ON run_snapshots(project_id, created_at DESC);

CREATE OR REPLACE FUNCTION validate_run_snapshot_project()
RETURNS TRIGGER AS $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM runs
        WHERE id = NEW.run_id AND project_id = NEW.project_id
    ) THEN
        RAISE EXCEPTION 'run snapshot project mismatch';
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_validate_run_snapshot_project ON run_snapshots;
CREATE CONSTRAINT TRIGGER trg_validate_run_snapshot_project
AFTER INSERT ON run_snapshots
DEFERRABLE INITIALLY IMMEDIATE
FOR EACH ROW EXECUTE FUNCTION validate_run_snapshot_project();

CREATE OR REPLACE FUNCTION reject_run_snapshot_mutation()
RETURNS TRIGGER AS $$
BEGIN
    RAISE EXCEPTION 'run snapshots are immutable';
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_reject_run_snapshot_update ON run_snapshots;
CREATE TRIGGER trg_reject_run_snapshot_update
BEFORE UPDATE ON run_snapshots
FOR EACH ROW EXECUTE FUNCTION reject_run_snapshot_mutation();

DROP TRIGGER IF EXISTS trg_reject_run_snapshot_delete ON run_snapshots;
CREATE TRIGGER trg_reject_run_snapshot_delete
BEFORE DELETE ON run_snapshots
FOR EACH ROW EXECUTE FUNCTION reject_run_snapshot_mutation();
