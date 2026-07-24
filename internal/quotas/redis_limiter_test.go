package quotas

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestRedisLimiter(t *testing.T) (*RedisLimiter, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRedisLimiter(client, "test"), server
}

func TestRedisLimiterTokenBucketDoesNotOversubscribeConcurrently(t *testing.T) {
	limiter, _ := newTestRedisLimiter(t)
	ctx := context.Background()

	var allowed atomic.Int64
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			decision, err := limiter.ConsumeTokenBucket(
				ctx, "project-1", DimensionIngestRequests, 1, 10, 10, time.Minute,
			)
			if err != nil {
				t.Errorf("ConsumeTokenBucket() error = %v", err)
				return
			}
			if decision.Allowed {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := allowed.Load(); got != 10 {
		t.Fatalf("allowed = %d, want 10", got)
	}
}

func TestRedisLimiterInstancesShareTokenBucket(t *testing.T) {
	server := miniredis.RunT(t)
	clientA := redis.NewClient(&redis.Options{Addr: server.Addr()})
	clientB := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = clientA.Close()
		_ = clientB.Close()
	})
	first := NewRedisLimiter(clientA, "shared")
	second := NewRedisLimiter(clientB, "shared")
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		limiter := first
		if i%2 == 1 {
			limiter = second
		}
		decision, err := limiter.ConsumeTokenBucket(
			ctx, "project-1", DimensionIngestSpans, 1, 2, 2, time.Hour,
		)
		if err != nil {
			t.Fatalf("consume %d: %v", i, err)
		}
		if decision.Allowed != (i < 2) {
			t.Fatalf("consume %d allowed = %v, want %v", i, decision.Allowed, i < 2)
		}
	}
}

func TestRedisLimiterConcurrencyAcquireRelease(t *testing.T) {
	limiter, _ := newTestRedisLimiter(t)
	ctx := context.Background()

	first, err := limiter.AcquireConcurrency(ctx, "project-1", DimensionSSEConnections, 2)
	if err != nil || !first.Allowed {
		t.Fatalf("first acquire = %+v, err = %v", first, err)
	}
	second, err := limiter.AcquireConcurrency(ctx, "project-1", DimensionSSEConnections, 2)
	if err != nil || !second.Allowed {
		t.Fatalf("second acquire = %+v, err = %v", second, err)
	}
	third, err := limiter.AcquireConcurrency(ctx, "project-1", DimensionSSEConnections, 2)
	if err != nil {
		t.Fatalf("third acquire error = %v", err)
	}
	if third.Allowed {
		t.Fatal("third acquire allowed, want rejected")
	}

	if err := limiter.ReleaseConcurrency(ctx, "project-1", DimensionSSEConnections); err != nil {
		t.Fatalf("release: %v", err)
	}
	afterRelease, err := limiter.AcquireConcurrency(ctx, "project-1", DimensionSSEConnections, 2)
	if err != nil || !afterRelease.Allowed {
		t.Fatalf("acquire after release = %+v, err = %v", afterRelease, err)
	}
}

func TestRedisLimiterDailyBudgetIncludesBaseline(t *testing.T) {
	limiter, _ := newTestRedisLimiter(t)
	ctx := context.Background()

	first, err := limiter.ConsumeDailyBudget(
		ctx, "project-1", DimensionTokensDaily, 20, 100, 70, time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("first budget consume: %v", err)
	}
	if !first.Allowed || first.Remaining != 10 {
		t.Fatalf("first = %+v, want allowed with 10 remaining", first)
	}

	second, err := limiter.ConsumeDailyBudget(
		ctx, "project-1", DimensionTokensDaily, 11, 100, 70, time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("second budget consume: %v", err)
	}
	if second.Allowed {
		t.Fatalf("second = %+v, want rejected", second)
	}
}
