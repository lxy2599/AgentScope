package quotas

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

func TestRedisLimiterRealRedisSharesLimitAcrossInstances(t *testing.T) {
	address := os.Getenv("AGENTSCOPE_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("AGENTSCOPE_TEST_REDIS_ADDR is not set")
	}
	clientA := redis.NewClient(&redis.Options{Addr: address})
	clientB := redis.NewClient(&redis.Options{Addr: address})
	t.Cleanup(func() {
		_ = clientA.Close()
		_ = clientB.Close()
	})
	namespace := "agentscope-test-" + uuid.NewString()
	first := NewRedisLimiter(clientA, namespace)
	second := NewRedisLimiter(clientB, namespace)

	var allowed atomic.Int64
	var wait sync.WaitGroup
	for index := range 50 {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			limiter := first
			if index%2 == 1 {
				limiter = second
			}
			decision, err := limiter.ConsumeTokenBucket(
				context.Background(),
				"project-real",
				DimensionIngestRequests,
				1,
				10,
				10,
				time.Hour,
			)
			if err != nil {
				t.Errorf("consume quota: %v", err)
				return
			}
			if decision.Allowed {
				allowed.Add(1)
			}
		}(index)
	}
	wait.Wait()

	if got := allowed.Load(); got != 10 {
		t.Fatalf("allowed = %d, want 10", got)
	}
}
