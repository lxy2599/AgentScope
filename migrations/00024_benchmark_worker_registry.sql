-- Durable, short-lived benchmark worker registrations.
--
-- Workers refresh their project-scoped liveness lease through the control
-- plane. Registrations survive an AgentScope process restart, while every
-- read filters by expires_at so a worker that stops heartbeating cannot remain
-- available beyond its configured TTL.
CREATE TABLE benchmark_workers (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    worker_name TEXT NOT NULL,
    execution_kinds JSONB NOT NULL,
    capacity INT NOT NULL,
    version TEXT NOT NULL DEFAULT '',
    last_seen_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (project_id, worker_name),
    CONSTRAINT benchmark_workers_name_nonempty
        CHECK (length(btrim(worker_name)) > 0),
    CONSTRAINT benchmark_workers_execution_kinds_array
        CHECK (jsonb_typeof(execution_kinds) = 'array'),
    CONSTRAINT benchmark_workers_execution_kinds_nonempty
        CHECK (jsonb_array_length(execution_kinds) > 0),
    CONSTRAINT benchmark_workers_capacity_positive
        CHECK (capacity > 0),
    CONSTRAINT benchmark_workers_expiry_after_last_seen
        CHECK (expires_at > last_seen_at)
);

CREATE INDEX idx_benchmark_workers_project_expiry
    ON benchmark_workers(project_id, expires_at);
