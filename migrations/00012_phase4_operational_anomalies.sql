-- Phase 4 operational anomaly persistence and transactional notification.

CREATE TABLE IF NOT EXISTS operational_anomalies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    agent_name TEXT NOT NULL,
    agent_version TEXT NOT NULL,
    environment TEXT NOT NULL,
    metric TEXT NOT NULL,
    window_start TIMESTAMPTZ NOT NULL,
    window_end TIMESTAMPTZ NOT NULL,
    observed_value DOUBLE PRECISION NOT NULL,
    baseline_median DOUBLE PRECISION NOT NULL,
    baseline_mad DOUBLE PRECISION NOT NULL,
    deviation_score DOUBLE PRECISION NOT NULL,
    sample_count INTEGER NOT NULL CHECK (sample_count >= 0),
    baseline_sample_count INTEGER NOT NULL CHECK (baseline_sample_count >= 0),
    status TEXT NOT NULL
        CHECK (status IN ('normal', 'anomaly', 'insufficient_baseline')),
    first_detected_at TIMESTAMPTZ NOT NULL,
    last_detected_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (
        project_id, agent_name, agent_version, environment, metric, window_start
    )
);

CREATE INDEX IF NOT EXISTS idx_operational_anomalies_project_recent
    ON operational_anomalies(project_id, window_end DESC);

CREATE OR REPLACE FUNCTION realtime_enqueue_operational_anomaly()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF NEW.status = 'anomaly'
       AND (TG_OP = 'INSERT' OR OLD.status <> 'anomaly') THEN
        INSERT INTO realtime_outbox (
            project_id, event_type, payload, payload_redacted, occurred_at
        ) VALUES (
            NEW.project_id,
            'operational.anomaly',
            jsonb_build_object(
                'anomaly_id', NEW.id,
                'agent_name', NEW.agent_name,
                'agent_version', NEW.agent_version,
                'environment', NEW.environment,
                'metric', NEW.metric,
                'status', NEW.status,
                'observed_value', NEW.observed_value,
                'baseline_median', NEW.baseline_median,
                'deviation_score', NEW.deviation_score,
                'sample_count', NEW.sample_count
            ),
            true,
            NEW.last_detected_at
        );
    END IF;
    RETURN NEW;
END;
$$;

DROP TRIGGER IF EXISTS trg_realtime_operational_anomaly ON operational_anomalies;
CREATE TRIGGER trg_realtime_operational_anomaly
AFTER INSERT OR UPDATE ON operational_anomalies
FOR EACH ROW EXECUTE FUNCTION realtime_enqueue_operational_anomaly();
