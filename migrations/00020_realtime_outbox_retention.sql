-- Makes bounded retention scans efficient without touching pending events.
CREATE INDEX IF NOT EXISTS idx_realtime_outbox_published_retention
    ON realtime_outbox (published_at, id)
    WHERE published_at IS NOT NULL;
