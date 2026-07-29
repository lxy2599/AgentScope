package adapter

import (
	"context"
	"time"

	"AgentScope/internal/realtime"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// RealtimeOutbox adapts sqlc queries to the realtime relay contract.
type RealtimeOutbox struct {
	q *store.Queries
}

func NewRealtimeOutbox(q *store.Queries) *RealtimeOutbox {
	return &RealtimeOutbox{q: q}
}

func (o *RealtimeOutbox) Claim(ctx context.Context, limit int) ([]realtime.OutboxRecord, error) {
	rows, err := o.q.ClaimRealtimeOutbox(ctx, int32(limit))
	if err != nil {
		return nil, err
	}
	records := make([]realtime.OutboxRecord, 0, len(rows))
	for _, row := range rows {
		records = append(records, outboxRecord(row))
	}
	return records, nil
}

func (o *RealtimeOutbox) MarkPublished(ctx context.Context, id int64) error {
	return o.q.MarkRealtimeOutboxPublished(ctx, id)
}

func (o *RealtimeOutbox) MarkFailed(ctx context.Context, id int64, message string) error {
	return o.q.MarkRealtimeOutboxFailed(ctx, store.MarkRealtimeOutboxFailedParams{
		ID:   id,
		Left: message,
	})
}

func (o *RealtimeOutbox) Enqueue(ctx context.Context, event realtime.Event) (realtime.OutboxRecord, error) {
	row, err := o.q.EnqueueRealtimeEvent(ctx, store.EnqueueRealtimeEventParams{
		SchemaVersion:   event.SchemaVersion,
		ProjectID:       parsePGUUID(event.ProjectID),
		RunID:           parsePGUUID(event.RunID),
		AgentID:         parsePGUUID(event.AgentID),
		AgentInstanceID: parsePGUUID(event.AgentInstanceID),
		EventType:       event.Type,
		Payload:         event.Payload,
		PayloadRedacted: event.PayloadRedacted,
		OccurredAt:      pgTimestamptz(event.OccurredAt),
	})
	if err != nil {
		return realtime.OutboxRecord{}, err
	}
	return outboxRecord(row), nil
}

func outboxRecord(row store.RealtimeOutbox) realtime.OutboxRecord {
	return realtime.OutboxRecord{
		ID:              row.ID,
		SchemaVersion:   row.SchemaVersion,
		ProjectID:       nullableUUIDString(row.ProjectID),
		RunID:           nullableUUIDString(row.RunID),
		AgentID:         nullableUUIDString(row.AgentID),
		AgentInstanceID: nullableUUIDString(row.AgentInstanceID),
		EventType:       row.EventType,
		Payload:         row.Payload,
		OccurredAt:      timestamptzOrZero(row.OccurredAt),
		PayloadRedacted: row.PayloadRedacted,
	}
}

func nullableUUIDString(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	return UUIDToString(value.Bytes)
}

func timestamptzOrZero(value pgtype.Timestamptz) time.Time {
	if !value.Valid {
		return time.Time{}
	}
	return value.Time
}

// ListAfter reads the durable outbox by its canonical event ID. It intentionally
// includes committed-but-not-yet-published rows so a PostgreSQL fallback can
// provide at-least-once delivery while Redis is unavailable.
func (o *RealtimeOutbox) ListAfter(ctx context.Context, projectID string, afterID int64, limit int) ([]realtime.OutboxRecord, error) {
	if limit < 1 {
		limit = 100
	}
	rows, err := o.q.ListRealtimeOutboxAfter(ctx, store.ListRealtimeOutboxAfterParams{
		ProjectID: parsePGUUID(projectID),
		ID:        afterID,
		Limit:     int32(limit),
	})
	if err != nil {
		return nil, err
	}
	out := make([]realtime.OutboxRecord, 0, len(rows))
	for _, row := range rows {
		out = append(out, outboxRecord(row))
	}
	return out, nil
}

func (o *RealtimeOutbox) LatestID(ctx context.Context, projectID string) (int64, error) {
	return o.q.GetLatestRealtimeOutboxID(ctx, parsePGUUID(projectID))
}

func (o *RealtimeOutbox) OldestID(ctx context.Context, projectID string) (int64, error) {
	return o.q.GetOldestRealtimeOutboxID(ctx, parsePGUUID(projectID))
}

// DeletePublishedBefore removes one bounded batch of published records. Pending
// records are deliberately never eligible for retention cleanup.
func (o *RealtimeOutbox) DeletePublishedBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error) {
	if limit < 1 {
		limit = 1000
	}
	return o.q.DeletePublishedRealtimeOutboxBefore(ctx, store.DeletePublishedRealtimeOutboxBeforeParams{
		PublishedAt: pgTimestamptz(cutoff),
		Limit:       int32(limit),
	})
}
