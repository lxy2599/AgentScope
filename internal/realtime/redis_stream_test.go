package realtime

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestRedisStreamPublishIsIdempotent(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	stream := NewRedisStream(client, RedisStreamOptions{
		MaxLen:    100,
		DedupeTTL: time.Hour,
	})
	event := Event{
		ID:              101,
		SchemaVersion:   SchemaVersionV1,
		ProjectID:       "project-1",
		RunID:           "run-1",
		Type:            EventRunStarted,
		OccurredAt:      time.Now().UTC(),
		Payload:         json.RawMessage(`{"status":"running"}`),
		PayloadRedacted: true,
	}

	if err := stream.Publish(context.Background(), event); err != nil {
		t.Fatalf("first Publish() error = %v", err)
	}
	if err := stream.Publish(context.Background(), event); err != nil {
		t.Fatalf("second Publish() error = %v", err)
	}

	messages, err := client.XRange(context.Background(), stream.StreamKey(event.ProjectID), "-", "+").Result()
	if err != nil {
		t.Fatalf("XRange() error = %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("stream message count = %d, want 1", len(messages))
	}
	if messages[0].ID != "101-0" {
		t.Fatalf("message ID = %q, want 101-0", messages[0].ID)
	}
}

func TestRedisStreamReadAfterCursor(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	stream := NewRedisStream(client, RedisStreamOptions{MaxLen: 100, DedupeTTL: time.Hour})
	ctx := context.Background()

	for id, eventType := range map[int64]string{
		10: EventRunStarted,
		11: EventRunStateChanged,
		12: EventRunCompleted,
	} {
		err := stream.Publish(ctx, Event{
			ID:              id,
			SchemaVersion:   SchemaVersionV1,
			ProjectID:       "project-1",
			RunID:           "run-1",
			Type:            eventType,
			OccurredAt:      time.Now().UTC(),
			Payload:         json.RawMessage(`{}`),
			PayloadRedacted: true,
		})
		if err != nil {
			t.Fatalf("Publish(%d) error = %v", id, err)
		}
	}

	events, err := stream.Read(ctx, "project-1", "10", 10, 0)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("Read() count = %d, want 2", len(events))
	}
	if events[0].ID != 11 || events[1].ID != 12 {
		t.Fatalf("Read() IDs = %d,%d, want 11,12", events[0].ID, events[1].ID)
	}
}

func TestRedisStreamRejectsInvalidEvent(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	stream := NewRedisStream(client, RedisStreamOptions{})

	err := stream.Publish(context.Background(), Event{ID: 1})
	if err == nil {
		t.Fatal("Publish() error = nil, want validation error")
	}
}
