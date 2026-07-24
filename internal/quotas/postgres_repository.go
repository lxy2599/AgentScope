package quotas

import (
	"context"
	"errors"

	"AgentScope/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// PostgresRepository persists quota policy and enforcement audit events.
type PostgresRepository struct {
	queries *store.Queries
}

func NewPostgresRepository(queries *store.Queries) *PostgresRepository {
	return &PostgresRepository{queries: queries}
}

func (r *PostgresRepository) Get(ctx context.Context, projectID string) (Quota, error) {
	row, err := r.queries.GetProjectQuota(ctx, quotaUUID(projectID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Quota{}, ErrQuotaNotFound
	}
	if err != nil {
		return Quota{}, err
	}
	return quotaFromStore(row), nil
}

func (r *PostgresRepository) Upsert(
	ctx context.Context,
	quota Quota,
	updatedBy string,
) (Quota, error) {
	row, err := r.queries.UpsertProjectQuota(ctx, store.UpsertProjectQuotaParams{
		ProjectID:               quotaUUID(quota.ProjectID),
		IngestRequestsPerMinute: quota.IngestRequestsPerMinute,
		IngestSpansPerMinute:    quota.IngestSpansPerMinute,
		IngestBytesPerMinute:    quota.IngestBytesPerMinute,
		SseConnections:          quota.SSEConnections,
		TokensPerDay:            quota.TokensPerDay,
		CostMicrosPerDay:        quota.CostMicrosPerDay,
		UpdatedBy:               quotaUUID(updatedBy),
	})
	if err != nil {
		return Quota{}, err
	}
	return quotaFromStore(row), nil
}

func (r *PostgresRepository) RecordEvent(ctx context.Context, event Event) error {
	_, err := r.queries.CreateProjectQuotaEvent(ctx, store.CreateProjectQuotaEventParams{
		ProjectID:  quotaUUID(event.ProjectID),
		Dimension:  string(event.Dimension),
		Requested:  event.Requested,
		QuotaLimit: event.Limit,
		Remaining:  event.Remaining,
		Outcome:    event.Outcome,
		FailMode:   event.FailMode,
		Reason:     event.Reason,
		OccurredAt: pgtype.Timestamptz{Time: event.OccurredAt, Valid: true},
	})
	return err
}

func (r *PostgresRepository) ListEvents(
	ctx context.Context,
	projectID string,
	limit int,
) ([]Event, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.queries.ListProjectQuotaEvents(ctx, store.ListProjectQuotaEventsParams{
		ProjectID: quotaUUID(projectID),
		Limit:     int32(limit),
	})
	if err != nil {
		return nil, err
	}
	events := make([]Event, 0, len(rows))
	for _, row := range rows {
		events = append(events, Event{
			ID:         quotaUUIDString(row.ID),
			ProjectID:  quotaUUIDString(row.ProjectID),
			Dimension:  Dimension(row.Dimension),
			Requested:  row.Requested,
			Limit:      row.QuotaLimit,
			Remaining:  row.Remaining,
			Outcome:    row.Outcome,
			FailMode:   row.FailMode,
			Reason:     row.Reason,
			OccurredAt: row.OccurredAt.Time,
		})
	}
	return events, nil
}

func quotaFromStore(row store.ProjectQuota) Quota {
	return Quota{
		ProjectID:               quotaUUIDString(row.ProjectID),
		IngestRequestsPerMinute: row.IngestRequestsPerMinute,
		IngestSpansPerMinute:    row.IngestSpansPerMinute,
		IngestBytesPerMinute:    row.IngestBytesPerMinute,
		SSEConnections:          row.SseConnections,
		TokensPerDay:            row.TokensPerDay,
		CostMicrosPerDay:        row.CostMicrosPerDay,
		Version:                 row.Version,
		UpdatedAt:               row.UpdatedAt.Time,
	}
}

func quotaUUID(value string) pgtype.UUID {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return pgtype.UUID{}
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}
}

func quotaUUIDString(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	return uuid.UUID(value.Bytes).String()
}
