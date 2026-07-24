-- Phase 4 realtime event outbox.

CREATE TABLE IF NOT EXISTS realtime_outbox (
    id BIGSERIAL PRIMARY KEY,
    schema_version TEXT NOT NULL DEFAULT 'realtime-event-v1',
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    run_id UUID REFERENCES runs(id) ON DELETE CASCADE,
    agent_id UUID REFERENCES agents(id) ON DELETE CASCADE,
    agent_instance_id UUID REFERENCES agent_instances(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}',
    payload_redacted BOOLEAN NOT NULL DEFAULT true,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at TIMESTAMPTZ,
    published_at TIMESTAMPTZ,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    CONSTRAINT realtime_outbox_payload_size
        CHECK (octet_length(payload::text) <= 16384)
);

CREATE INDEX IF NOT EXISTS idx_realtime_outbox_pending
    ON realtime_outbox (available_at, id)
    WHERE published_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_realtime_outbox_project_id
    ON realtime_outbox (project_id, id);
CREATE INDEX IF NOT EXISTS idx_realtime_outbox_run_id
    ON realtime_outbox (run_id, id)
    WHERE run_id IS NOT NULL;

CREATE OR REPLACE FUNCTION realtime_enqueue_run_state_event()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    realtime_type TEXT;
BEGIN
    realtime_type := CASE
        WHEN NEW.from_status IS NULL OR NEW.from_status = '' THEN 'run.started'
        WHEN NEW.to_status = 'completed' THEN 'run.completed'
        WHEN NEW.to_status IN ('failed', 'timeout', 'cancelled') THEN 'run.failed'
        ELSE 'run.state.changed'
    END;

    INSERT INTO realtime_outbox (
        project_id, run_id, agent_id, agent_instance_id, event_type,
        payload, payload_redacted, occurred_at
    ) VALUES (
        NEW.project_id, NEW.run_id, NEW.agent_id, NEW.agent_instance_id,
        realtime_type,
        jsonb_build_object(
            'from_status', COALESCE(NEW.from_status, ''),
            'to_status', NEW.to_status,
            'message', COALESCE(NEW.message, '')
        ),
        true,
        NEW.created_at
    );
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_realtime_run_state_event ON run_state_events;
CREATE TRIGGER trg_realtime_run_state_event
AFTER INSERT ON run_state_events
FOR EACH ROW EXECUTE FUNCTION realtime_enqueue_run_state_event();

CREATE OR REPLACE FUNCTION realtime_enqueue_agent_registered()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO realtime_outbox (
        project_id, agent_id, event_type, payload, payload_redacted, occurred_at
    ) VALUES (
        NEW.project_id,
        NEW.id,
        'agent.registered',
        jsonb_build_object('name', NEW.name, 'status', NEW.status),
        true,
        NEW.created_at
    );
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_realtime_agent_registered ON agents;
CREATE TRIGGER trg_realtime_agent_registered
AFTER INSERT ON agents
FOR EACH ROW EXECUTE FUNCTION realtime_enqueue_agent_registered();

CREATE OR REPLACE FUNCTION realtime_enqueue_agent_heartbeat()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    heartbeat_project_id UUID;
BEGIN
    SELECT project_id
    INTO heartbeat_project_id
    FROM agent_instances
    WHERE id = NEW.instance_id;

    INSERT INTO realtime_outbox (
        project_id, agent_id, agent_instance_id, event_type,
        payload, payload_redacted, occurred_at
    ) VALUES (
        heartbeat_project_id,
        NEW.agent_id,
        NEW.instance_id,
        'agent.heartbeat',
        jsonb_build_object('status', NEW.status),
        true,
        NEW.received_at
    );
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_realtime_agent_heartbeat ON agent_heartbeats;
CREATE TRIGGER trg_realtime_agent_heartbeat
AFTER INSERT ON agent_heartbeats
FOR EACH ROW EXECUTE FUNCTION realtime_enqueue_agent_heartbeat();
