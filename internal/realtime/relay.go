package realtime

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Outbox interface {
	Claim(ctx context.Context, limit int) ([]OutboxRecord, error)
	MarkPublished(ctx context.Context, id int64) error
	MarkFailed(ctx context.Context, id int64, message string) error
}

type Publisher interface {
	Publish(ctx context.Context, event Event) error
}

type RelayOptions struct {
	BatchSize    int
	PollInterval time.Duration
}

type RelayResult struct {
	Claimed   int
	Published int
	Failed    int
}

type Relay struct {
	outbox       Outbox
	publisher    Publisher
	batchSize    int
	pollInterval time.Duration
}

func NewRelay(outbox Outbox, publisher Publisher, options RelayOptions) *Relay {
	if options.BatchSize <= 0 {
		options.BatchSize = 100
	}
	if options.PollInterval <= 0 {
		options.PollInterval = 250 * time.Millisecond
	}
	return &Relay{
		outbox:       outbox,
		publisher:    publisher,
		batchSize:    options.BatchSize,
		pollInterval: options.PollInterval,
	}
}

func (r *Relay) RunOnce(ctx context.Context) (RelayResult, error) {
	records, err := r.outbox.Claim(ctx, r.batchSize)
	if err != nil {
		return RelayResult{}, fmt.Errorf("claim realtime outbox: %w", err)
	}
	result := RelayResult{Claimed: len(records)}
	var joined error
	for _, record := range records {
		event, eventErr := record.Event()
		if eventErr == nil {
			eventErr = r.publisher.Publish(ctx, event)
		}
		if eventErr != nil {
			result.Failed++
			joined = errors.Join(joined, eventErr)
			if markErr := r.outbox.MarkFailed(ctx, record.ID, eventErr.Error()); markErr != nil {
				joined = errors.Join(joined, markErr)
			}
			continue
		}
		if markErr := r.outbox.MarkPublished(ctx, record.ID); markErr != nil {
			result.Failed++
			joined = errors.Join(joined, markErr)
			continue
		}
		result.Published++
	}
	return result, joined
}

func (r *Relay) Run(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
			_, _ = r.RunOnce(ctx)
			timer.Reset(r.pollInterval)
		}
	}
}
