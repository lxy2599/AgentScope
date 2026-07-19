-- Migration 00003: Online evaluation sampling rules and annotation queue.
--
-- Adds:
--   trace_sampling_rules – project-level rules that define which runs get
--     automatically evaluated after completion
--   annotation_queues – named queues that collect items needing human review
--   annotation_items – individual annotation results linked to runs and eval scores

-- ============================================================
-- Trace sampling rules
-- ============================================================
CREATE TABLE trace_sampling_rules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_id UUID REFERENCES agents(id) ON DELETE SET NULL,
    name TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT true,
    sample_rate REAL NOT NULL CHECK (sample_rate >= 0.0 AND sample_rate <= 1.0),
    filters JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_trace_sampling_rules_project ON trace_sampling_rules(project_id);
CREATE INDEX idx_trace_sampling_rules_enabled ON trace_sampling_rules(project_id, enabled);

-- ============================================================
-- Annotation queues
-- ============================================================
CREATE TABLE annotation_queues (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_annotation_queues_project ON annotation_queues(project_id);

-- ============================================================
-- Annotation items
-- ============================================================
CREATE TABLE annotation_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    queue_id UUID NOT NULL REFERENCES annotation_queues(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    run_id UUID NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    trace_id TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'in_progress', 'completed', 'skipped', 'promoted')),
    priority INT NOT NULL DEFAULT 0,
    scores JSONB NOT NULL DEFAULT '{}',
    corrected_output TEXT NOT NULL DEFAULT '',
    expected_tool_path JSONB,
    comment TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_annotation_items_queue ON annotation_items(queue_id);
CREATE INDEX idx_annotation_items_project ON annotation_items(project_id);
CREATE INDEX idx_annotation_items_status ON annotation_items(status);
CREATE INDEX idx_annotation_items_run ON annotation_items(run_id);
