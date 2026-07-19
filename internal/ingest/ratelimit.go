package ingest

import (
	"fmt"
	"sync"
	"time"
)

// ErrRateLimited is returned when a project has exceeded its ingest quota.
var ErrRateLimited = fmt.Errorf("rate limited: too many ingest requests")

// TokenBucket implements a simple token bucket rate limiter.
type TokenBucket struct {
	mu         sync.Mutex
	capacity   int
	tokens     int
	refill     int
	interval   time.Duration
	lastRefill time.Time
}

// NewTokenBucket creates a new token bucket with the given capacity, refill rate, and refill interval.
func NewTokenBucket(capacity, refill int, interval time.Duration) *TokenBucket {
	return &TokenBucket{
		capacity:   capacity,
		tokens:     capacity,
		refill:     refill,
		interval:   interval,
		lastRefill: time.Now(),
	}
}

// Allow tries to consume one token from the bucket.
func (tb *TokenBucket) Allow() bool {
	return tb.AllowN(1)
}

// AllowN tries to consume n tokens from the bucket.
func (tb *TokenBucket) AllowN(n int) bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	// Refill based on elapsed time
	now := time.Now()
	elapsed := now.Sub(tb.lastRefill)
	if elapsed >= tb.interval {
		intervals := int(elapsed / tb.interval)
		tb.tokens += intervals * tb.refill
		if tb.tokens > tb.capacity {
			tb.tokens = tb.capacity
		}
		tb.lastRefill = now
	}

	if tb.tokens < n {
		return false
	}
	tb.tokens -= n
	return true
}

// ProjectRateLimiter manages per-project rate limiters.
type ProjectRateLimiter struct {
	mu       sync.RWMutex
	buckets  map[string]*TokenBucket
	capacity int
	refill   int
	interval time.Duration
}

// NewProjectRateLimiter creates a new per-project rate limiter.
func NewProjectRateLimiter(capacity, refill int, interval time.Duration) *ProjectRateLimiter {
	return &ProjectRateLimiter{
		buckets:  make(map[string]*TokenBucket),
		capacity: capacity,
		refill:   refill,
		interval: interval,
	}
}

// Allow checks if a project identified by projectID is allowed to consume n tokens.
func (prl *ProjectRateLimiter) Allow(projectID string, n int) bool {
	prl.mu.RLock()
	bucket, exists := prl.buckets[projectID]
	prl.mu.RUnlock()

	if !exists {
		prl.mu.Lock()
		// Double-check after acquiring write lock
		bucket, exists = prl.buckets[projectID]
		if !exists {
			bucket = NewTokenBucket(prl.capacity, prl.refill, prl.interval)
			prl.buckets[projectID] = bucket
		}
		prl.mu.Unlock()
	}

	return bucket.AllowN(n)
}
