package realtime

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const ResetCursorFormatUnsupported = "cursor_format_unsupported"

// OutboxReader reads the PostgreSQL-authoritative realtime event sequence.
type OutboxReader interface {
	ListAfter(ctx context.Context, projectID string, afterID int64, limit int) ([]OutboxRecord, error)
	LatestID(ctx context.Context, projectID string) (int64, error)
	OldestID(ctx context.Context, projectID string) (int64, error)
}

type PostgresSubscriberOptions struct {
	ReadCount    int
	PollInterval time.Duration
}

// PostgresSubscriber provides bounded, at-least-once replay from the durable
// outbox. Its public cursor is the monotonic realtime_outbox.id, not a Redis
// Stream ID, so it remains valid across transport failover.
type PostgresSubscriber struct {
	reader       OutboxReader
	readCount    int
	pollInterval time.Duration
}

func NewPostgresSubscriber(reader OutboxReader, options PostgresSubscriberOptions) *PostgresSubscriber {
	if options.ReadCount <= 0 {
		options.ReadCount = 100
	}
	if options.PollInterval <= 0 {
		options.PollInterval = time.Second
	}
	return &PostgresSubscriber{reader: reader, readCount: options.ReadCount, pollInterval: options.PollInterval}
}

func (s *PostgresSubscriber) Subscribe(ctx context.Context, projectID, afterID string, filter SubscriptionFilter) (<-chan StreamItem, error) {
	if s == nil || s.reader == nil {
		return nil, fmt.Errorf("postgres realtime reader is unavailable")
	}
	cursor, resetReason, err := s.initialCursor(ctx, projectID, afterID)
	if err != nil {
		return nil, err
	}
	items := make(chan StreamItem, s.readCount)
	go func() {
		defer close(items)
		if resetReason != "" && !sendStreamItem(ctx, items, StreamItem{Reset: true, ResetReason: resetReason}) {
			return
		}
		ticker := time.NewTicker(s.pollInterval)
		defer ticker.Stop()
		for {
			rows, err := s.reader.ListAfter(ctx, projectID, cursor, s.readCount)
			if err != nil {
				if ctx.Err() == nil {
					sendStreamItem(ctx, items, StreamItem{Err: err})
				}
				return
			}
			if len(rows) == 0 {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					continue
				}
			}
			for _, row := range rows {
				cursor = row.ID
				event, eventErr := row.Event()
				if eventErr != nil {
					sendStreamItem(ctx, items, StreamItem{Err: eventErr})
					return
				}
				if !filter.Matches(event) {
					continue
				}
				if !sendStreamItem(ctx, items, StreamItem{Event: &event}) {
					return
				}
			}
		}
	}()
	return items, nil
}

func (s *PostgresSubscriber) initialCursor(ctx context.Context, projectID, raw string) (int64, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		latest, err := s.reader.LatestID(ctx, projectID)
		return latest, "", err
	}
	cursor, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || cursor < 0 {
		latest, latestErr := s.reader.LatestID(ctx, projectID)
		if latestErr != nil {
			return 0, "", latestErr
		}
		return latest, ResetCursorFormatUnsupported, nil
	}
	oldest, err := s.reader.OldestID(ctx, projectID)
	if err != nil {
		return 0, "", err
	}
	if oldest > 0 && cursor < oldest {
		return oldest - 1, ResetCursorExpired, nil
	}
	return cursor, "", nil
}
