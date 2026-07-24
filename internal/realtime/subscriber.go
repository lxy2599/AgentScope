package realtime

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

const ResetCursorExpired = "cursor_expired"

type SubscriptionFilter struct {
	RunID   string
	AgentID string
	Types   map[string]struct{}
}

func (f SubscriptionFilter) Matches(event Event) bool {
	if f.RunID != "" && event.RunID != f.RunID {
		return false
	}
	if f.AgentID != "" && event.AgentID != f.AgentID {
		return false
	}
	if len(f.Types) > 0 {
		if _, ok := f.Types[event.Type]; !ok {
			return false
		}
	}
	return true
}

type StreamItem struct {
	Event       *Event
	Reset       bool
	ResetReason string
	Err         error
}

type Subscriber interface {
	Subscribe(
		ctx context.Context,
		projectID, afterID string,
		filter SubscriptionFilter,
	) (<-chan StreamItem, error)
}

type SubscriberOptions struct {
	ReadCount int64
	Block     time.Duration
}

type RedisSubscriber struct {
	stream    *RedisStream
	readCount int64
	block     time.Duration
}

func NewRedisSubscriber(stream *RedisStream, options SubscriberOptions) *RedisSubscriber {
	if options.ReadCount <= 0 {
		options.ReadCount = 100
	}
	if options.Block <= 0 {
		options.Block = 15 * time.Second
	}
	return &RedisSubscriber{
		stream:    stream,
		readCount: options.ReadCount,
		block:     options.Block,
	}
}

func (s *RedisSubscriber) Subscribe(
	ctx context.Context,
	projectID, afterID string,
	filter SubscriptionFilter,
) (<-chan StreamItem, error) {
	if err := s.stream.Ping(ctx); err != nil {
		return nil, err
	}
	oldestID, err := s.stream.OldestID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	cursor := ""
	if strings.TrimSpace(afterID) == "" {
		cursor, err = s.stream.LatestID(ctx, projectID)
		if err != nil {
			return nil, err
		}
	} else {
		cursor = normalizeStreamID(afterID)
	}
	expired := cursorExpired(cursor, oldestID)
	if expired {
		cursor = "0-0"
	}

	items := make(chan StreamItem, s.readCount)
	go func() {
		defer close(items)
		if expired && !sendStreamItem(ctx, items, StreamItem{
			Reset:       true,
			ResetReason: ResetCursorExpired,
		}) {
			return
		}
		for {
			var events []Event
			var readErr error
			events, readErr = s.stream.Read(ctx, projectID, cursor, s.readCount, 0)
			if readErr != nil {
				if !errors.Is(readErr, context.Canceled) {
					sendStreamItem(ctx, items, StreamItem{Err: readErr})
				}
				return
			}
			if len(events) == 0 {
				events, readErr = s.stream.Read(ctx, projectID, cursor, s.readCount, s.block)
				if readErr != nil {
					if errors.Is(readErr, context.Canceled) || errors.Is(readErr, context.DeadlineExceeded) {
						return
					}
					sendStreamItem(ctx, items, StreamItem{Err: readErr})
					return
				}
			}
			for i := range events {
				cursor = events[i].StreamID()
				if !filter.Matches(events[i]) {
					continue
				}
				event := events[i]
				if !sendStreamItem(ctx, items, StreamItem{Event: &event}) {
					return
				}
			}
			select {
			case <-ctx.Done():
				return
			default:
			}
		}
	}()
	return items, nil
}

func sendStreamItem(ctx context.Context, target chan<- StreamItem, item StreamItem) bool {
	select {
	case target <- item:
		return true
	case <-ctx.Done():
		return false
	}
}

func cursorExpired(cursor, oldest string) bool {
	if oldest == "" || cursor == "" || cursor == "0-0" || cursor == "$" {
		return false
	}
	cursorValue, cursorErr := strconv.ParseInt(strings.SplitN(cursor, "-", 2)[0], 10, 64)
	oldestValue, oldestErr := strconv.ParseInt(strings.SplitN(oldest, "-", 2)[0], 10, 64)
	return cursorErr == nil && oldestErr == nil && cursorValue < oldestValue
}
