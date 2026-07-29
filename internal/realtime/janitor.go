package realtime

import (
	"context"
	"fmt"
	"time"
)

// PublishedOutboxCleaner removes bounded batches of already-published events.
// It must never delete a pending record because pending rows are the durable
// recovery source for Redis relay failures.
type PublishedOutboxCleaner interface {
	DeletePublishedBefore(ctx context.Context, cutoff time.Time, limit int) (int64, error)
}

type OutboxJanitorOptions struct {
	Retention time.Duration
	Interval  time.Duration
	BatchSize int
}

type OutboxJanitor struct {
	cleaner PublishedOutboxCleaner
	opts    OutboxJanitorOptions
	now     func() time.Time
}

func NewOutboxJanitor(cleaner PublishedOutboxCleaner, options OutboxJanitorOptions) *OutboxJanitor {
	if options.Retention <= 0 {
		options.Retention = 7 * 24 * time.Hour
	}
	if options.Interval <= 0 {
		options.Interval = time.Hour
	}
	if options.BatchSize <= 0 {
		options.BatchSize = 1000
	}
	return &OutboxJanitor{cleaner: cleaner, opts: options, now: func() time.Time { return time.Now().UTC() }}
}

// RunOnce drains eligible published records in bounded batches.
func (j *OutboxJanitor) RunOnce(ctx context.Context) (int64, error) {
	if j == nil || j.cleaner == nil {
		return 0, fmt.Errorf("realtime outbox cleaner is unavailable")
	}
	cutoff := j.now().Add(-j.opts.Retention)
	var total int64
	for {
		deleted, err := j.cleaner.DeletePublishedBefore(ctx, cutoff, j.opts.BatchSize)
		if err != nil {
			return total, err
		}
		total += deleted
		if deleted < int64(j.opts.BatchSize) {
			return total, nil
		}
		if err := ctx.Err(); err != nil {
			return total, err
		}
	}
}

func (j *OutboxJanitor) Run(ctx context.Context, onError func(error)) {
	if _, err := j.RunOnce(ctx); err != nil && ctx.Err() == nil && onError != nil {
		onError(err)
	}
	ticker := time.NewTicker(j.opts.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := j.RunOnce(ctx); err != nil && ctx.Err() == nil && onError != nil {
				onError(err)
			}
		}
	}
}
