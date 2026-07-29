package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

type memoryOutboxReader struct {
	mu      sync.Mutex
	records []OutboxRecord
	err     error
}

func (r *memoryOutboxReader) ListAfter(_ context.Context, projectID string, afterID int64, limit int) ([]OutboxRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	out := make([]OutboxRecord, 0, limit)
	for _, record := range r.records {
		if record.ProjectID == projectID && record.ID > afterID {
			out = append(out, record)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}
func (r *memoryOutboxReader) LatestID(_ context.Context, projectID string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var latest int64
	for _, record := range r.records {
		if record.ProjectID == projectID && record.ID > latest {
			latest = record.ID
		}
	}
	return latest, nil
}
func (r *memoryOutboxReader) OldestID(_ context.Context, projectID string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var oldest int64
	for _, record := range r.records {
		if record.ProjectID == projectID && (oldest == 0 || record.ID < oldest) {
			oldest = record.ID
		}
	}
	return oldest, nil
}

func outboxTestRecord(id int64) OutboxRecord {
	return OutboxRecord{ID: id, SchemaVersion: SchemaVersionV1, ProjectID: "project-1", EventType: EventRunStateChanged, OccurredAt: time.Now().UTC(), Payload: json.RawMessage(`{}`), PayloadRedacted: true}
}

func TestPostgresSubscriberReplaysCanonicalOutboxCursor(t *testing.T) {
	reader := &memoryOutboxReader{records: []OutboxRecord{outboxTestRecord(10), outboxTestRecord(11)}}
	subscriber := NewPostgresSubscriber(reader, PostgresSubscriberOptions{PollInterval: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	items, err := subscriber.Subscribe(ctx, "project-1", "10", SubscriptionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case item := <-items:
		if item.Event == nil || item.Event.ID != 11 {
			t.Fatalf("item = %#v, want event 11", item)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for replay")
	}
}

func TestPostgresSubscriberResetsExpiredAndLegacyCursor(t *testing.T) {
	reader := &memoryOutboxReader{records: []OutboxRecord{outboxTestRecord(20)}}
	subscriber := NewPostgresSubscriber(reader, PostgresSubscriberOptions{PollInterval: time.Millisecond})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	items, err := subscriber.Subscribe(ctx, "project-1", "5", SubscriptionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if item := <-items; !item.Reset || item.ResetReason != ResetCursorExpired {
		t.Fatalf("first item = %#v, want expired reset", item)
	}
	if item := <-items; item.Event == nil || item.Event.ID != 20 {
		t.Fatalf("second item = %#v, want event 20", item)
	}

	legacy, err := subscriber.Subscribe(ctx, "project-1", "1234567890-7", SubscriptionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if item := <-legacy; !item.Reset || item.ResetReason != ResetCursorFormatUnsupported {
		t.Fatalf("legacy cursor item = %#v", item)
	}
}

type failingSubscriber struct{ err error }

func (s failingSubscriber) Subscribe(context.Context, string, string, SubscriptionFilter) (<-chan StreamItem, error) {
	return nil, s.err
}

func TestHybridSubscriberFallsBackWhenRedisSubscriptionCannotStart(t *testing.T) {
	reader := &memoryOutboxReader{records: []OutboxRecord{outboxTestRecord(30), outboxTestRecord(31)}}
	fallback := NewPostgresSubscriber(reader, PostgresSubscriberOptions{PollInterval: time.Millisecond})
	hybrid := NewHybridSubscriber(failingSubscriber{err: errors.New("redis unavailable")}, fallback)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	items, err := hybrid.Subscribe(ctx, "project-1", "30", SubscriptionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case item := <-items:
		if item.Event == nil || item.Event.ID != 31 {
			t.Fatalf("fallback item = %#v", item)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for fallback event")
	}
}

type staticSubscriber struct {
	items <-chan StreamItem
	err   error
}

func (s staticSubscriber) Subscribe(context.Context, string, string, SubscriptionFilter) (<-chan StreamItem, error) {
	return s.items, s.err
}

func TestHybridSubscriberReplaysFromLastDurableIDAfterRedisRuntimeFailure(t *testing.T) {
	reader := &memoryOutboxReader{records: []OutboxRecord{outboxTestRecord(100), outboxTestRecord(101)}}
	fallback := NewPostgresSubscriber(reader, PostgresSubscriberOptions{PollInterval: time.Millisecond})
	primaryItems := make(chan StreamItem, 2)
	primaryEvent, err := outboxTestRecord(100).Event()
	if err != nil {
		t.Fatal(err)
	}
	primaryItems <- StreamItem{Event: &primaryEvent}
	primaryItems <- StreamItem{Err: errors.New("redis stream disconnected")}
	close(primaryItems)

	hybrid := NewHybridSubscriber(staticSubscriber{items: primaryItems}, fallback)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	items, err := hybrid.Subscribe(ctx, "project-1", "", SubscriptionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []int64{100, 101} {
		select {
		case item := <-items:
			if item.Event == nil || item.Event.ID != want {
				t.Fatalf("stream item = %#v, want event %d", item, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for event %d", want)
		}
	}
}
