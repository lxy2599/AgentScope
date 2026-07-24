package realtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestSubscriptionFilterMatches(t *testing.T) {
	t.Parallel()

	filter := SubscriptionFilter{
		RunID:   "run-1",
		AgentID: "agent-1",
		Types: map[string]struct{}{
			EventRunStateChanged: {},
		},
	}
	base := Event{RunID: "run-1", AgentID: "agent-1", Type: EventRunStateChanged}
	if !filter.Matches(base) {
		t.Fatal("matching event was rejected")
	}
	for name, event := range map[string]Event{
		"run":   {RunID: "run-2", AgentID: "agent-1", Type: EventRunStateChanged},
		"agent": {RunID: "run-1", AgentID: "agent-2", Type: EventRunStateChanged},
		"type":  {RunID: "run-1", AgentID: "agent-1", Type: EventRunCompleted},
	} {
		t.Run(name, func(t *testing.T) {
			if filter.Matches(event) {
				t.Fatalf("non-matching %s event was accepted", name)
			}
		})
	}
}

func TestRedisSubscriberReportsExpiredCursorThenContinues(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	stream := NewRedisStream(client, RedisStreamOptions{MaxLen: 100})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, id := range []int64{20, 21} {
		err := stream.Publish(ctx, Event{
			ID:              id,
			SchemaVersion:   SchemaVersionV1,
			ProjectID:       "project-1",
			RunID:           "run-1",
			Type:            EventRunStateChanged,
			OccurredAt:      time.Now().UTC(),
			Payload:         json.RawMessage(`{}`),
			PayloadRedacted: true,
		})
		if err != nil {
			t.Fatalf("Publish(%d) error = %v", id, err)
		}
	}

	subscriber := NewRedisSubscriber(stream, SubscriberOptions{
		ReadCount: 10,
		Block:     10 * time.Millisecond,
	})
	items, err := subscriber.Subscribe(ctx, "project-1", "5", SubscriptionFilter{RunID: "run-1"})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}

	first := <-items
	if !first.Reset || first.ResetReason != ResetCursorExpired {
		t.Fatalf("first item = %#v, want cursor-expired reset", first)
	}
	second := <-items
	if second.Event == nil || second.Event.ID != 20 {
		t.Fatalf("second item = %#v, want event 20", second)
	}
	cancel()
}

func TestRedisSubscriberRejectsProjectMismatch(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	stream := NewRedisStream(client, RedisStreamOptions{})
	ctx := context.Background()
	if err := stream.Publish(ctx, Event{
		ID:              1,
		SchemaVersion:   SchemaVersionV1,
		ProjectID:       "project-2",
		Type:            EventRunStarted,
		OccurredAt:      time.Now().UTC(),
		Payload:         json.RawMessage(`{}`),
		PayloadRedacted: true,
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	subscriber := NewRedisSubscriber(stream, SubscriberOptions{})
	items, err := subscriber.Subscribe(ctx, "project-1", "0", SubscriptionFilter{})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	select {
	case item := <-items:
		if item.Event != nil {
			t.Fatalf("received cross-project event: %#v", item.Event)
		}
	case <-time.After(20 * time.Millisecond):
	}
}

func TestRedisSubscriberWithoutCursorOnlyReceivesNewEvents(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	stream := NewRedisStream(client, RedisStreamOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	publishTestEvent := func(id int64) {
		t.Helper()
		if err := stream.Publish(ctx, Event{
			ID:              id,
			SchemaVersion:   SchemaVersionV1,
			ProjectID:       "project-1",
			Type:            EventRunStateChanged,
			OccurredAt:      time.Now().UTC(),
			Payload:         json.RawMessage(`{}`),
			PayloadRedacted: true,
		}); err != nil {
			t.Fatalf("Publish(%d) error = %v", id, err)
		}
	}

	publishTestEvent(1)
	subscriber := NewRedisSubscriber(stream, SubscriberOptions{
		ReadCount: 10,
		Block:     100 * time.Millisecond,
	})
	items, err := subscriber.Subscribe(ctx, "project-1", "", SubscriptionFilter{})
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	publishTestEvent(2)

	select {
	case item := <-items:
		if item.Event == nil || item.Event.ID != 2 {
			t.Fatalf("item = %#v, want only new event 2", item)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for new event")
	}
}
