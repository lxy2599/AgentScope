-- Durable, project-scoped benchmark catalog snapshots.
--
-- A catalog revision payload is immutable once accepted. The separate head row
-- selects the revision currently exposed by the read APIs, so a later sync can
-- advance a project without rewriting an already-pinned revision.

CREATE TABLE benchmark_catalog_revisions (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    revision TEXT NOT NULL,
    payload JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, revision),
    CONSTRAINT benchmark_catalog_revisions_revision_nonempty
        CHECK (length(btrim(revision)) > 0),
    CONSTRAINT benchmark_catalog_revisions_payload_object
        CHECK (jsonb_typeof(payload) = 'object')
);

CREATE TABLE benchmark_catalog_heads (
    project_id UUID PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    revision TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT benchmark_catalog_heads_revision_fk
        FOREIGN KEY (project_id, revision)
        REFERENCES benchmark_catalog_revisions(project_id, revision)
        ON DELETE RESTRICT
);

CREATE INDEX idx_benchmark_catalog_revisions_project_created_at
    ON benchmark_catalog_revisions(project_id, created_at DESC);
