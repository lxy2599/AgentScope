-- name: EnqueueRealtimeEvent :one
INSERT INTO realtime_outbox (
    schema_version, project_id, run_id, agent_id, agent_instance_id,
    event_type, payload, payload_redacted, occurred_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9
)
RETURNING *;

-- name: ClaimRealtimeOutbox :many
WITH candidates AS (
    SELECT id
    FROM realtime_outbox
    WHERE published_at IS NULL
      AND available_at <= now()
      AND (claimed_at IS NULL OR claimed_at < now() - interval '30 seconds')
    ORDER BY id
    FOR UPDATE SKIP LOCKED
    LIMIT $1
)
UPDATE realtime_outbox AS o
SET claimed_at = now()
FROM candidates
WHERE o.id = candidates.id
RETURNING o.*;

-- name: MarkRealtimeOutboxPublished :exec
UPDATE realtime_outbox
SET published_at = now(),
    claimed_at = NULL,
    last_error = ''
WHERE id = $1;

-- name: MarkRealtimeOutboxFailed :exec
UPDATE realtime_outbox
SET attempts = attempts + 1,
    claimed_at = NULL,
    available_at = now() + LEAST(interval '60 seconds', interval '1 second' * power(2, LEAST(attempts, 6))),
    last_error = left($2, 1024)
WHERE id = $1;

-- name: ListRealtimeOutboxAfter :many
SELECT *
FROM realtime_outbox
WHERE project_id = $1
  AND id > $2
ORDER BY id
LIMIT $3;

-- name: DeletePublishedRealtimeOutboxBefore :execrows
DELETE FROM realtime_outbox
WHERE published_at IS NOT NULL
  AND published_at < $1;
