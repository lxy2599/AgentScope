-- Migration 00001: initial schema for AgentScope MVP.
-- Creates all core tables, constraints, and indexes.
-- Note: trace spans/events are stored in ClickHouse, not PostgreSQL.

-- Enable gen_random_uuid()
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- ============================================================
-- Users
-- ============================================================
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email TEXT NOT NULL UNIQUE,
    password_hash BYTEA NOT NULL,
    display_name TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================
-- Projects
-- ============================================================
CREATE TABLE projects (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE project_members (
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'member',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);

CREATE TABLE project_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL,
    token_prefix TEXT NOT NULL,
    scope TEXT NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ
);

-- ============================================================
-- Runs (unified model: live + eval)
-- ============================================================
CREATE TABLE runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    source TEXT NOT NULL CHECK (source IN ('live', 'eval')),
    status TEXT NOT NULL DEFAULT 'pending',
    trace_id TEXT,
    agent_name TEXT NOT NULL,
    agent_version TEXT,
    input_summary TEXT,
    output_summary TEXT,
    metadata JSONB NOT NULL DEFAULT '{}',
    error TEXT,
    started_at TIMESTAMPTZ,
    ended_at TIMESTAMPTZ,
    latency_ms BIGINT,
    cost NUMERIC(10,6),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================
-- Datasets and cases
-- ============================================================
CREATE TABLE datasets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE dataset_cases (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dataset_id UUID NOT NULL REFERENCES datasets(id) ON DELETE CASCADE,
    input JSONB NOT NULL DEFAULT '{}',
    expected JSONB,
    metadata JSONB NOT NULL DEFAULT '{}',
    sort_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================
-- Evaluators, experiments, and eval jobs
-- ============================================================
CREATE TABLE evaluators (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    type TEXT NOT NULL,
    config JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE experiments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    dataset_id UUID NOT NULL REFERENCES datasets(id),
    evaluator_id UUID REFERENCES evaluators(id),
    target_name TEXT NOT NULL,
    target_version TEXT,
    status TEXT NOT NULL DEFAULT 'pending',
    aggregate_score NUMERIC(10,4),
    total_cases INT NOT NULL DEFAULT 0,
    passed_cases INT NOT NULL DEFAULT 0,
    failed_cases INT NOT NULL DEFAULT 0,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE eval_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    experiment_id UUID NOT NULL REFERENCES experiments(id) ON DELETE CASCADE,
    dataset_case_id UUID NOT NULL REFERENCES dataset_cases(id),
    status TEXT NOT NULL DEFAULT 'queued',
    runner_name TEXT,
    lease_expires_at TIMESTAMPTZ,
    attempts INT NOT NULL DEFAULT 0,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT eval_jobs_running_lease CHECK (status <> 'running' OR lease_expires_at IS NOT NULL)
);

-- ============================================================
-- Eval case results (linked to runs for traceability)
-- ============================================================
CREATE TABLE eval_case_results (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    eval_job_id UUID NOT NULL REFERENCES eval_jobs(id) ON DELETE CASCADE,
    run_id UUID REFERENCES runs(id),
    score NUMERIC(10,4),
    passed BOOLEAN,
    reason TEXT,
    details JSONB NOT NULL DEFAULT '{}',
    actual_output TEXT,
    latency_ms BIGINT,
    cost NUMERIC(10,6),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================
-- Agents (control center)
-- ============================================================
CREATE TABLE agents (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name        VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    metadata    JSONB NOT NULL DEFAULT '{}',
    version     INTEGER NOT NULL DEFAULT 1,
    status      VARCHAR(50) NOT NULL DEFAULT 'active',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(project_id, name)
);

CREATE TABLE agent_versions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    version     INTEGER NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(agent_id, version)
);

CREATE TABLE agent_instances (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    project_id  UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    instance_id VARCHAR(255) NOT NULL,
    instance_name TEXT NOT NULL DEFAULT '',
    status      VARCHAR(50) NOT NULL DEFAULT 'online',
    hostname    VARCHAR(255) NOT NULL DEFAULT '',
    pid         INTEGER NOT NULL DEFAULT 0,
    active_run_id UUID REFERENCES runs(id) ON DELETE SET NULL,
    metadata    JSONB NOT NULL DEFAULT '{}',
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(agent_id, instance_id)
);

CREATE TABLE agent_heartbeats (
    id          BIGSERIAL PRIMARY KEY,
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    instance_id UUID NOT NULL REFERENCES agent_instances(id) ON DELETE CASCADE,
    status      VARCHAR(50) NOT NULL DEFAULT 'online',
    payload     JSONB NOT NULL DEFAULT '{}',
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE agent_capabilities (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    name        VARCHAR(255) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(agent_id, name)
);

-- ============================================================
-- Run state events (Phase 2.2 active runs)
-- ============================================================
CREATE TABLE run_state_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    agent_id UUID REFERENCES agents(id) ON DELETE SET NULL,
    agent_instance_id UUID REFERENCES agent_instances(id) ON DELETE SET NULL,
    from_status TEXT,
    to_status TEXT NOT NULL,
    message TEXT,
    attributes JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================
-- Indexes
-- ============================================================
CREATE INDEX idx_runs_project_id ON runs(project_id);
CREATE INDEX idx_runs_source ON runs(source);
CREATE INDEX idx_runs_status ON runs(status);
CREATE INDEX idx_runs_trace_id ON runs(trace_id);
CREATE INDEX idx_runs_started_at ON runs(started_at DESC);
CREATE INDEX idx_eval_jobs_experiment_id ON eval_jobs(experiment_id);
CREATE INDEX idx_eval_jobs_status ON eval_jobs(status);
CREATE INDEX idx_eval_case_results_eval_job_id ON eval_case_results(eval_job_id);
CREATE INDEX idx_eval_case_results_run_id ON eval_case_results(run_id);
CREATE INDEX idx_agents_project_id ON agents(project_id);
CREATE INDEX idx_agents_status ON agents(status);
CREATE INDEX idx_agent_instances_agent_id ON agent_instances(agent_id);
CREATE INDEX idx_agent_instances_status ON agent_instances(status);
CREATE INDEX idx_agent_heartbeats_agent_id ON agent_heartbeats(agent_id);
CREATE INDEX idx_agent_heartbeats_received_at ON agent_heartbeats(received_at DESC);
CREATE INDEX idx_agent_capabilities_agent_id ON agent_capabilities(agent_id);
CREATE INDEX idx_agent_instances_project_id ON agent_instances(project_id);
CREATE INDEX idx_agent_instances_active_run_id ON agent_instances(active_run_id);
CREATE INDEX idx_run_state_events_project_created ON run_state_events(project_id, created_at DESC);
CREATE INDEX idx_run_state_events_run_created ON run_state_events(run_id, created_at ASC);
CREATE INDEX idx_run_state_events_instance_created ON run_state_events(agent_instance_id, created_at DESC);
CREATE INDEX idx_run_state_events_status_created ON run_state_events(to_status, created_at DESC);
