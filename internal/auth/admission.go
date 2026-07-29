package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"runtime"
	"strings"
	"sync"
	"time"
)

// AdmissionOutcome tells HTTP handlers why an authentication request was denied
// before it reaches a bcrypt operation. The public response deliberately does
// not reveal which internal dimension was exhausted.
type AdmissionOutcome string

const (
	AdmissionAllowed     AdmissionOutcome = "allowed"
	AdmissionRateLimited AdmissionOutcome = "rate_limited"
	AdmissionBusy        AdmissionOutcome = "busy"
)

// RatePolicy is a bounded token-bucket policy. Refill tokens are added every
// Interval; Capacity is also the maximum burst.
type RatePolicy struct {
	Capacity int
	Refill   int
	Interval time.Duration
}

func (p RatePolicy) enabled() bool {
	return p.Capacity > 0 && p.Refill > 0 && p.Interval > 0
}

// AdmissionOptions configures local, process-safe admission controls for the
// expensive login and registration paths. A local controller intentionally
// remains effective when Redis or another shared limiter is unavailable.
type AdmissionOptions struct {
	Enabled              bool
	LoginIP              RatePolicy
	LoginPrincipal       RatePolicy
	RegisterIP           RatePolicy
	RegisterPrincipal    RatePolicy
	BcryptMaxConcurrent  int
	MaxTrackedPrincipals int
}

func DefaultAdmissionOptions() AdmissionOptions {
	return AdmissionOptions{
		Enabled:              true,
		LoginIP:              RatePolicy{Capacity: 10, Refill: 30, Interval: time.Minute},
		LoginPrincipal:       RatePolicy{Capacity: 5, Refill: 8, Interval: time.Minute},
		RegisterIP:           RatePolicy{Capacity: 3, Refill: 5, Interval: 10 * time.Minute},
		RegisterPrincipal:    RatePolicy{Capacity: 2, Refill: 3, Interval: 10 * time.Minute},
		BcryptMaxConcurrent:  max(2, runtime.GOMAXPROCS(0)),
		MaxTrackedPrincipals: 20_000,
	}
}

// AdmissionController protects bcrypt-backed authentication operations with
// layered local buckets and a non-blocking concurrency gate.
type AdmissionController struct {
	enabled     bool
	policies    map[string]RatePolicy
	maxEntries  int
	now         func() time.Time
	mu          sync.Mutex
	buckets     map[string]tokenBucket
	bcryptSlots chan struct{}
}

type tokenBucket struct {
	tokens float64
	at     time.Time
	seenAt time.Time
}

func NewAdmissionController(options AdmissionOptions) *AdmissionController {
	defaults := DefaultAdmissionOptions()
	if options.BcryptMaxConcurrent <= 0 {
		options.BcryptMaxConcurrent = defaults.BcryptMaxConcurrent
	}
	if options.MaxTrackedPrincipals <= 0 {
		options.MaxTrackedPrincipals = defaults.MaxTrackedPrincipals
	}
	return &AdmissionController{
		enabled: options.Enabled,
		policies: map[string]RatePolicy{
			"login:ip":           options.LoginIP,
			"login:principal":    options.LoginPrincipal,
			"register:ip":        options.RegisterIP,
			"register:principal": options.RegisterPrincipal,
		},
		maxEntries:  options.MaxTrackedPrincipals,
		now:         func() time.Time { return time.Now().UTC() },
		buckets:     make(map[string]tokenBucket),
		bcryptSlots: make(chan struct{}, options.BcryptMaxConcurrent),
	}
}

// Admit records one login or registration attempt. On success it returns a
// release function that must be called after the bcrypt-backed operation
// finishes. The controller stores only a hash of the normalized principal.
func (c *AdmissionController) Admit(route, ipPrefix, principal string) (release func(), outcome AdmissionOutcome, retryAfter time.Duration) {
	if c == nil || !c.enabled {
		return func() {}, AdmissionAllowed, 0
	}
	if route != "login" && route != "register" {
		return func() {}, AdmissionAllowed, 0
	}

	now := c.now()
	c.mu.Lock()
	allowed, retryAfter := c.allowLocked(route+":ip:"+bucketIdentity(ipPrefix), c.policies[route+":ip"], now)
	if allowed {
		allowed, retryAfter = c.allowLocked(route+":principal:"+principalHash(principal), c.policies[route+":principal"], now)
	}
	c.mu.Unlock()
	if !allowed {
		return nil, AdmissionRateLimited, retryAfter
	}

	select {
	case c.bcryptSlots <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-c.bcryptSlots }) }, AdmissionAllowed, 0
	default:
		return nil, AdmissionBusy, time.Second
	}
}

func (c *AdmissionController) allowLocked(key string, policy RatePolicy, now time.Time) (bool, time.Duration) {
	if !policy.enabled() {
		return true, 0
	}
	bucket, exists := c.buckets[key]
	if !exists {
		c.evictLocked(now)
		bucket = tokenBucket{tokens: float64(policy.Capacity), at: now}
	}
	elapsed := now.Sub(bucket.at)
	if elapsed > 0 {
		bucket.tokens = minFloat(float64(policy.Capacity), bucket.tokens+elapsed.Seconds()*float64(policy.Refill)/policy.Interval.Seconds())
		bucket.at = now
	}
	bucket.seenAt = now
	if bucket.tokens < 1 {
		c.buckets[key] = bucket
		needed := 1 - bucket.tokens
		seconds := needed * policy.Interval.Seconds() / float64(policy.Refill)
		return false, time.Duration(seconds*float64(time.Second)) + time.Second
	}
	bucket.tokens--
	c.buckets[key] = bucket
	return true, 0
}

func (c *AdmissionController) evictLocked(now time.Time) {
	if len(c.buckets) < c.maxEntries {
		return
	}
	cutoff := now.Add(-time.Hour)
	for key, bucket := range c.buckets {
		if bucket.seenAt.Before(cutoff) {
			delete(c.buckets, key)
			if len(c.buckets) < c.maxEntries {
				return
			}
		}
	}
	// The map is bounded even under an address/email spray. Removing one oldest
	// entry is sufficient because a new key will be inserted immediately after.
	var oldestKey string
	var oldest time.Time
	for key, bucket := range c.buckets {
		if oldestKey == "" || bucket.seenAt.Before(oldest) {
			oldestKey, oldest = key, bucket.seenAt
		}
	}
	if oldestKey != "" {
		delete(c.buckets, oldestKey)
	}
}

func bucketIdentity(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func principalHash(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	sum := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(sum[:])
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
