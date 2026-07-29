package realtime

import (
	"context"
	"strconv"
	"strings"
)

// HybridSubscriber uses Redis for a fresh low-latency subscription and falls
// back to PostgreSQL for reconnects and runtime Redis failures. A numeric SSE
// cursor is always interpreted as a durable outbox ID, so fallback replay does
// not depend on a Redis Stream cursor.
type HybridSubscriber struct {
	primary  Subscriber
	fallback Subscriber
}

func NewHybridSubscriber(primary, fallback Subscriber) *HybridSubscriber {
	return &HybridSubscriber{primary: primary, fallback: fallback}
}

func (s *HybridSubscriber) Subscribe(ctx context.Context, projectID, afterID string, filter SubscriptionFilter) (<-chan StreamItem, error) {
	if s == nil || s.fallback == nil {
		return nil, ErrSubscriberUnavailable
	}
	// A reconnect owns a durable numeric Last-Event-ID. Replay it directly from
	// PostgreSQL instead of trying to translate it into an ephemeral Redis ID.
	if strings.TrimSpace(afterID) != "" || s.primary == nil {
		return s.fallback.Subscribe(ctx, projectID, afterID, filter)
	}
	primaryItems, err := s.primary.Subscribe(ctx, projectID, "", filter)
	if err != nil {
		return s.fallback.Subscribe(ctx, projectID, afterID, filter)
	}
	items := make(chan StreamItem, 100)
	go s.forwardWithFallback(ctx, items, primaryItems, projectID, filter)
	return items, nil
}

func (s *HybridSubscriber) forwardWithFallback(ctx context.Context, target chan<- StreamItem, primary <-chan StreamItem, projectID string, filter SubscriptionFilter) {
	defer close(target)
	lastID := ""
	current := primary
	usingFallback := false
	for {
		select {
		case <-ctx.Done():
			return
		case item, open := <-current:
			if !open || item.Err != nil {
				if usingFallback {
					if item.Err != nil {
						sendStreamItem(ctx, target, item)
					}
					return
				}
				fallbackItems, err := s.fallback.Subscribe(ctx, projectID, lastID, filter)
				if err != nil {
					sendStreamItem(ctx, target, StreamItem{Err: err})
					return
				}
				current = fallbackItems
				usingFallback = true
				continue
			}
			if item.Event != nil {
				lastID = strconv.FormatInt(item.Event.ID, 10)
			}
			if !sendStreamItem(ctx, target, item) {
				return
			}
		}
	}
}
