package realtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeOutbox struct {
	records   []OutboxRecord
	published []int64
	failed    map[int64]string
}

func (f *fakeOutbox) Claim(_ context.Context, limit int) ([]OutboxRecord, error) {
	if limit < len(f.records) {
		return f.records[:limit], nil
	}
	return f.records, nil
}

func (f *fakeOutbox) MarkPublished(_ context.Context, id int64) error {
	f.published = append(f.published, id)
	return nil
}

func (f *fakeOutbox) MarkFailed(_ context.Context, id int64, message string) error {
	if f.failed == nil {
		f.failed = make(map[int64]string)
	}
	f.failed[id] = message
	return nil
}

type fakePublisher struct {
	published []int64
	failID    int64
}

func (f *fakePublisher) Publish(_ context.Context, event Event) error {
	if event.ID == f.failID {
		return errors.New("redis unavailable")
	}
	f.published = append(f.published, event.ID)
	return nil
}

func TestRelayRunOnceMarksPerRecordOutcome(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	outbox := &fakeOutbox{records: []OutboxRecord{
		{ID: 1, SchemaVersion: SchemaVersionV1, ProjectID: "p", EventType: EventRunStarted, Payload: []byte(`{}`), OccurredAt: now, PayloadRedacted: true},
		{ID: 2, SchemaVersion: SchemaVersionV1, ProjectID: "p", EventType: EventRunStateChanged, Payload: []byte(`{}`), OccurredAt: now, PayloadRedacted: true},
		{ID: 3, SchemaVersion: SchemaVersionV1, ProjectID: "p", EventType: EventRunCompleted, Payload: []byte(`{}`), OccurredAt: now, PayloadRedacted: true},
	}}
	publisher := &fakePublisher{failID: 2}
	relay := NewRelay(outbox, publisher, RelayOptions{BatchSize: 10})

	result, err := relay.RunOnce(context.Background())
	if err == nil {
		t.Fatal("RunOnce() error = nil, want aggregate publish error")
	}
	if result.Claimed != 3 || result.Published != 2 || result.Failed != 1 {
		t.Fatalf("RunOnce() result = %#v, want claimed=3 published=2 failed=1", result)
	}
	if len(outbox.published) != 2 || outbox.published[0] != 1 || outbox.published[1] != 3 {
		t.Fatalf("marked published = %v, want [1 3]", outbox.published)
	}
	if outbox.failed[2] == "" {
		t.Fatal("record 2 was not marked failed")
	}
}

func TestRelayUsesConfiguredBatchSize(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	outbox := &fakeOutbox{records: []OutboxRecord{
		{ID: 1, SchemaVersion: SchemaVersionV1, ProjectID: "p", EventType: EventRunStarted, Payload: []byte(`{}`), OccurredAt: now, PayloadRedacted: true},
		{ID: 2, SchemaVersion: SchemaVersionV1, ProjectID: "p", EventType: EventRunStarted, Payload: []byte(`{}`), OccurredAt: now, PayloadRedacted: true},
	}}
	relay := NewRelay(outbox, &fakePublisher{}, RelayOptions{BatchSize: 1})

	result, err := relay.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result.Claimed != 1 {
		t.Fatalf("Claimed = %d, want 1", result.Claimed)
	}
}
