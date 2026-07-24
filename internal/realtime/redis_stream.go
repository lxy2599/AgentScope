package realtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const publishScript = `
local event_id = ARGV[1]
if redis.call("HEXISTS", KEYS[2], event_id) == 1 then
  return redis.call("HGET", KEYS[2], event_id)
end
local stream_id = redis.call(
  "XADD", KEYS[1], "MAXLEN", "~", ARGV[2], "*", "data", ARGV[3]
)
redis.call("HSET", KEYS[2], event_id, stream_id)
redis.call("EXPIRE", KEYS[2], ARGV[4])
return stream_id
`

type RedisStreamOptions struct {
	Prefix    string
	MaxLen    int64
	DedupeTTL time.Duration
}

// RedisStream publishes and reads project-isolated realtime streams.
type RedisStream struct {
	client    redis.UniversalClient
	prefix    string
	maxLen    int64
	dedupeTTL time.Duration
	script    *redis.Script
}

func NewRedisStream(client redis.UniversalClient, options RedisStreamOptions) *RedisStream {
	if options.Prefix == "" {
		options.Prefix = "agentscope:events"
	}
	if options.MaxLen <= 0 {
		options.MaxLen = 10000
	}
	if options.DedupeTTL <= 0 {
		options.DedupeTTL = 7 * 24 * time.Hour
	}
	return &RedisStream{
		client:    client,
		prefix:    options.Prefix,
		maxLen:    options.MaxLen,
		dedupeTTL: options.DedupeTTL,
		script:    redis.NewScript(publishScript),
	}
}

func (s *RedisStream) StreamKey(projectID string) string {
	return s.prefix + ":" + projectID
}

func (s *RedisStream) dedupeKey(projectID string) string {
	return s.StreamKey(projectID) + ":published"
}

func (s *RedisStream) Publish(ctx context.Context, event Event) error {
	if err := event.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal realtime event: %w", err)
	}
	ttlSeconds := int64(s.dedupeTTL / time.Second)
	if ttlSeconds < 1 {
		ttlSeconds = 1
	}
	_, err = s.script.Run(
		ctx,
		s.client,
		[]string{s.StreamKey(event.ProjectID), s.dedupeKey(event.ProjectID)},
		strconv.FormatInt(event.ID, 10),
		strconv.FormatInt(s.maxLen, 10),
		string(data),
		strconv.FormatInt(ttlSeconds, 10),
	).Result()
	if err != nil {
		return fmt.Errorf("publish realtime event %d: %w", event.ID, err)
	}
	return nil
}

// Read returns entries strictly newer than afterID. A zero block duration performs
// a non-blocking range query; a positive duration uses XREAD.
func (s *RedisStream) Read(ctx context.Context, projectID, afterID string, count int64, block time.Duration) ([]Event, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project id is required")
	}
	if count <= 0 {
		count = 100
	}
	cursor := normalizeStreamID(afterID)
	var messages []redis.XMessage
	if block > 0 {
		streams, err := s.client.XRead(ctx, &redis.XReadArgs{
			Streams: []string{s.StreamKey(projectID), cursor},
			Count:   count,
			Block:   block,
		}).Result()
		if err != nil && err != redis.Nil {
			return nil, fmt.Errorf("read realtime stream: %w", err)
		}
		if len(streams) > 0 {
			messages = streams[0].Messages
		}
	} else {
		result, err := s.client.XRangeN(ctx, s.StreamKey(projectID), "("+cursor, "+", count).Result()
		if err != nil && err != redis.Nil {
			return nil, fmt.Errorf("range realtime stream: %w", err)
		}
		messages = result
	}

	events := make([]Event, 0, len(messages))
	for _, message := range messages {
		raw, ok := message.Values["data"]
		if !ok {
			return nil, fmt.Errorf("stream message %s has no data field", message.ID)
		}
		var event Event
		if err := json.Unmarshal([]byte(fmt.Sprint(raw)), &event); err != nil {
			return nil, fmt.Errorf("decode stream message %s: %w", message.ID, err)
		}
		event.StreamCursor = message.ID
		if event.ProjectID != projectID {
			return nil, fmt.Errorf("stream message %s project mismatch", message.ID)
		}
		if err := event.Validate(); err != nil {
			return nil, fmt.Errorf("invalid stream message %s: %w", message.ID, err)
		}
		events = append(events, event)
	}
	return events, nil
}

func (s *RedisStream) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *RedisStream) OldestID(ctx context.Context, projectID string) (string, error) {
	messages, err := s.client.XRangeN(ctx, s.StreamKey(projectID), "-", "+", 1).Result()
	if err != nil && err != redis.Nil {
		return "", fmt.Errorf("read oldest realtime event: %w", err)
	}
	if len(messages) == 0 {
		return "", nil
	}
	return messages[0].ID, nil
}

func (s *RedisStream) LatestID(ctx context.Context, projectID string) (string, error) {
	messages, err := s.client.XRevRangeN(ctx, s.StreamKey(projectID), "+", "-", 1).Result()
	if err != nil && err != redis.Nil {
		return "", fmt.Errorf("read latest realtime event: %w", err)
	}
	if len(messages) == 0 {
		return "0-0", nil
	}
	return messages[0].ID, nil
}

func (s *RedisStream) Close() error {
	return s.client.Close()
}

func normalizeStreamID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "0-0"
	}
	if id == "$" {
		return "$"
	}
	if !strings.Contains(id, "-") {
		return id + "-0"
	}
	return id
}
