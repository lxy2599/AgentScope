package quotas

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const tokenBucketLua = `
local values = redis.call("HMGET", KEYS[1], "tokens", "last")
local tokens = tonumber(values[1])
local last = tonumber(values[2])
local capacity = tonumber(ARGV[1])
local refill = tonumber(ARGV[2])
local interval = tonumber(ARGV[3])
local now = tonumber(ARGV[4])
local amount = tonumber(ARGV[5])
if tokens == nil then tokens = capacity end
if last == nil then last = now end
if now > last then
  tokens = math.min(capacity, tokens + ((now - last) / interval) * refill)
  last = now
end
local allowed = 0
local retry = 0
if tokens >= amount then
  tokens = tokens - amount
  allowed = 1
else
  retry = math.ceil(((amount - tokens) / refill) * interval)
end
redis.call("HSET", KEYS[1], "tokens", tokens, "last", last, "capacity", capacity)
redis.call("PEXPIRE", KEYS[1], math.max(interval * 2, 1000))
return {allowed, math.floor(tokens), retry}
`

const dailyBudgetLua = `
local current = tonumber(redis.call("GET", KEYS[1]) or "0")
local baseline = tonumber(ARGV[1])
local amount = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])
if baseline > current then current = baseline end
if current + amount > limit then
  return {0, math.max(0, limit - current)}
end
current = current + amount
redis.call("SET", KEYS[1], current, "PX", math.max(ttl, 1000))
return {1, math.max(0, limit - current)}
`

const acquireConcurrencyLua = `
local current = tonumber(redis.call("GET", KEYS[1]) or "0")
local limit = tonumber(ARGV[1])
if current + 1 > limit then
  return {0, math.max(0, limit - current)}
end
current = redis.call("INCR", KEYS[1])
redis.call("EXPIRE", KEYS[1], 86400)
return {1, math.max(0, limit - current)}
`

const releaseConcurrencyLua = `
local current = tonumber(redis.call("GET", KEYS[1]) or "0")
if current <= 1 then
  redis.call("DEL", KEYS[1])
  return 0
end
return redis.call("DECR", KEYS[1])
`

// Limiter is the distributed primitive required by Service.
type Limiter interface {
	ConsumeTokenBucket(ctx context.Context, projectID string, dimension Dimension, amount, capacity, refill int64, interval time.Duration) (Decision, error)
	ConsumeDailyBudget(ctx context.Context, projectID string, dimension Dimension, amount, limit, baseline int64, resetAt time.Time) (Decision, error)
	AcquireConcurrency(ctx context.Context, projectID string, dimension Dimension, limit int64) (Decision, error)
	ReleaseConcurrency(ctx context.Context, projectID string, dimension Dimension) error
	Current(ctx context.Context, projectID string, dimension Dimension) (int64, error)
}

// RedisLimiter executes all quota mutations atomically in Redis.
type RedisLimiter struct {
	client      redis.UniversalClient
	namespace   string
	tokenBucket *redis.Script
	dailyBudget *redis.Script
	acquire     *redis.Script
	release     *redis.Script
	now         func() time.Time
}

func NewRedisLimiter(client redis.UniversalClient, namespace string) *RedisLimiter {
	if namespace == "" {
		namespace = "agentscope"
	}
	return &RedisLimiter{
		client:      client,
		namespace:   namespace,
		tokenBucket: redis.NewScript(tokenBucketLua),
		dailyBudget: redis.NewScript(dailyBudgetLua),
		acquire:     redis.NewScript(acquireConcurrencyLua),
		release:     redis.NewScript(releaseConcurrencyLua),
		now:         time.Now,
	}
}

func (r *RedisLimiter) ConsumeTokenBucket(
	ctx context.Context,
	projectID string,
	dimension Dimension,
	amount, capacity, refill int64,
	interval time.Duration,
) (Decision, error) {
	if amount <= 0 || capacity <= 0 || refill <= 0 || interval <= 0 {
		return Decision{}, fmt.Errorf("invalid token bucket arguments")
	}
	key := r.tokenBucketKey(projectID, dimension, interval)
	result, err := r.tokenBucket.Run(
		ctx,
		r.client,
		[]string{key},
		capacity,
		refill,
		interval.Milliseconds(),
		r.now().UnixMilli(),
		amount,
	).Result()
	if err != nil {
		return Decision{}, err
	}
	values, err := scriptIntegers(result, 3)
	if err != nil {
		return Decision{}, err
	}
	return Decision{
		Allowed:    values[0] == 1,
		Dimension:  dimension,
		Limit:      capacity,
		Remaining:  values[1],
		RetryAfter: time.Duration(values[2]) * time.Millisecond,
	}, nil
}

func (r *RedisLimiter) ConsumeDailyBudget(
	ctx context.Context,
	projectID string,
	dimension Dimension,
	amount, limit, baseline int64,
	resetAt time.Time,
) (Decision, error) {
	now := r.now()
	if amount < 0 || limit <= 0 {
		return Decision{}, fmt.Errorf("invalid daily budget arguments")
	}
	if !resetAt.After(now) {
		resetAt = nextUTCMidnight(now)
	}
	key := r.dailyKey(projectID, dimension, now)
	result, err := r.dailyBudget.Run(
		ctx,
		r.client,
		[]string{key},
		baseline,
		amount,
		limit,
		resetAt.Sub(now).Milliseconds(),
	).Result()
	if err != nil {
		return Decision{}, err
	}
	values, err := scriptIntegers(result, 2)
	if err != nil {
		return Decision{}, err
	}
	return Decision{
		Allowed:    values[0] == 1,
		Dimension:  dimension,
		Limit:      limit,
		Remaining:  values[1],
		RetryAfter: resetAt.Sub(now),
		ResetAt:    resetAt,
	}, nil
}

func (r *RedisLimiter) AcquireConcurrency(
	ctx context.Context,
	projectID string,
	dimension Dimension,
	limit int64,
) (Decision, error) {
	result, err := r.acquire.Run(
		ctx,
		r.client,
		[]string{r.concurrencyKey(projectID, dimension)},
		limit,
	).Result()
	if err != nil {
		return Decision{}, err
	}
	values, err := scriptIntegers(result, 2)
	if err != nil {
		return Decision{}, err
	}
	return Decision{
		Allowed:   values[0] == 1,
		Dimension: dimension,
		Limit:     limit,
		Remaining: values[1],
	}, nil
}

func (r *RedisLimiter) ReleaseConcurrency(
	ctx context.Context,
	projectID string,
	dimension Dimension,
) error {
	return r.release.Run(
		ctx,
		r.client,
		[]string{r.concurrencyKey(projectID, dimension)},
	).Err()
}

func (r *RedisLimiter) Current(
	ctx context.Context,
	projectID string,
	dimension Dimension,
) (int64, error) {
	switch dimension {
	case DimensionSSEConnections:
		value, err := r.client.Get(ctx, r.concurrencyKey(projectID, dimension)).Int64()
		if err == redis.Nil {
			return 0, nil
		}
		return value, err
	case DimensionTokensDaily, DimensionCostMicrosDaily:
		value, err := r.client.Get(ctx, r.dailyKey(projectID, dimension, r.now())).Int64()
		if err == redis.Nil {
			return 0, nil
		}
		return value, err
	default:
		values, err := r.client.HMGet(
			ctx,
			r.tokenBucketKey(projectID, dimension, time.Minute),
			"tokens",
			"capacity",
		).Result()
		if err != nil || len(values) < 2 || values[0] == nil || values[1] == nil {
			return 0, err
		}
		tokens, err := strconv.ParseFloat(fmt.Sprint(values[0]), 64)
		if err != nil {
			return 0, err
		}
		capacity, err := strconv.ParseFloat(fmt.Sprint(values[1]), 64)
		if err != nil {
			return 0, err
		}
		return int64(math.Ceil(capacity - tokens)), nil
	}
}

func (r *RedisLimiter) tokenBucketKey(projectID string, dimension Dimension, interval time.Duration) string {
	return fmt.Sprintf("%s:quota:bucket:%s:%s:%d", r.namespace, projectID, dimension, interval.Milliseconds())
}

func (r *RedisLimiter) concurrencyKey(projectID string, dimension Dimension) string {
	return fmt.Sprintf("%s:quota:gauge:%s:%s", r.namespace, projectID, dimension)
}

func (r *RedisLimiter) dailyKey(projectID string, dimension Dimension, now time.Time) string {
	return fmt.Sprintf(
		"%s:quota:daily:%s:%s:%s",
		r.namespace,
		projectID,
		dimension,
		now.UTC().Format("2006-01-02"),
	)
}

func scriptIntegers(result any, expected int) ([]int64, error) {
	values, ok := result.([]interface{})
	if !ok || len(values) != expected {
		return nil, fmt.Errorf("unexpected Redis script result: %T %v", result, result)
	}
	parsed := make([]int64, len(values))
	for i, value := range values {
		switch v := value.(type) {
		case int64:
			parsed[i] = v
		case string:
			number, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse Redis result %q: %w", v, err)
			}
			parsed[i] = number
		default:
			return nil, fmt.Errorf("unexpected Redis script value: %T", value)
		}
	}
	return parsed, nil
}

func nextUTCMidnight(now time.Time) time.Time {
	utc := now.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day()+1, 0, 0, 0, 0, time.UTC)
}
