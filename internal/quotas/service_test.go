package quotas

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type memoryQuotaRepository struct {
	mu     sync.Mutex
	quota  Quota
	events []Event
}

func (r *memoryQuotaRepository) Get(_ context.Context, projectID string) (Quota, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.quota.ProjectID == "" {
		return Quota{}, ErrQuotaNotFound
	}
	if r.quota.ProjectID != projectID {
		return Quota{}, ErrQuotaNotFound
	}
	return r.quota, nil
}

func (r *memoryQuotaRepository) Upsert(_ context.Context, quota Quota, _ string) (Quota, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	quota.Version = r.quota.Version + 1
	r.quota = quota
	return quota, nil
}

func (r *memoryQuotaRepository) RecordEvent(_ context.Context, event Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
	return nil
}

func (r *memoryQuotaRepository) ListEvents(_ context.Context, _ string, limit int) ([]Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit > len(r.events) {
		limit = len(r.events)
	}
	return append([]Event(nil), r.events[len(r.events)-limit:]...), nil
}

type failingLimiter struct{}

func (failingLimiter) ConsumeTokenBucket(context.Context, string, Dimension, int64, int64, int64, time.Duration) (Decision, error) {
	return Decision{}, errors.New("redis unavailable")
}
func (failingLimiter) ConsumeDailyBudget(context.Context, string, Dimension, int64, int64, int64, time.Time) (Decision, error) {
	return Decision{}, errors.New("redis unavailable")
}
func (failingLimiter) AcquireConcurrency(context.Context, string, Dimension, int64) (Decision, error) {
	return Decision{}, errors.New("redis unavailable")
}
func (failingLimiter) ReleaseConcurrency(context.Context, string, Dimension) error {
	return errors.New("redis unavailable")
}
func (failingLimiter) Current(context.Context, string, Dimension) (int64, error) {
	return 0, errors.New("redis unavailable")
}

type staticBudgetUsage struct {
	tokens int64
	cost   int64
	calls  *int
}

func (s staticBudgetUsage) DailyUsage(context.Context, string, time.Time) (int64, int64, error) {
	if s.calls != nil {
		*s.calls++
	}
	return s.tokens, s.cost, nil
}

func TestServiceUsesBackwardsCompatibleDefaults(t *testing.T) {
	service := NewService(&memoryQuotaRepository{}, nil, nil)
	quota, err := service.GetQuota(context.Background(), "project-1")
	if err != nil {
		t.Fatalf("GetQuota() error = %v", err)
	}
	if quota.ProjectID != "project-1" {
		t.Fatalf("ProjectID = %q, want project-1", quota.ProjectID)
	}
	if quota.IngestRequestsPerMinute <= 0 || quota.SSEConnections <= 0 ||
		quota.TokensPerDay <= 0 || quota.CostMicrosPerDay <= 0 {
		t.Fatalf("defaults must be positive: %+v", quota)
	}
}

func TestServiceIngestRedisFailureFailsOpenAndRecordsEvent(t *testing.T) {
	repo := &memoryQuotaRepository{}
	service := NewService(repo, failingLimiter{}, nil)

	decision := service.CheckIngest(
		context.Background(), "project-1", DimensionIngestRequests, 1,
	)
	if !decision.Allowed || !decision.Degraded {
		t.Fatalf("decision = %+v, want degraded fail-open", decision)
	}
	if len(repo.events) != 1 || repo.events[0].Outcome != EventDegraded {
		t.Fatalf("events = %+v, want one degraded event", repo.events)
	}
}

func TestServiceSSEFailureFailsClosed(t *testing.T) {
	service := NewService(&memoryQuotaRepository{}, failingLimiter{}, nil)

	decision := service.AcquireSSE(context.Background(), "project-1")
	if decision.Allowed || !decision.Degraded {
		t.Fatalf("decision = %+v, want degraded fail-closed", decision)
	}
}

func TestServiceHardCostBudgetFailureFailsClosed(t *testing.T) {
	service := NewService(
		&memoryQuotaRepository{},
		failingLimiter{},
		staticBudgetUsage{cost: 10},
	)

	decision := service.ConsumeDailyBudget(
		context.Background(), "project-1", DimensionCostMicrosDaily, 1,
	)
	if decision.Allowed || !decision.Degraded {
		t.Fatalf("decision = %+v, want degraded fail-closed", decision)
	}
}

func TestServiceAppliesConfiguredRequestSpanAndByteLimits(t *testing.T) {
	limiter, _ := newTestRedisLimiter(t)
	repo := &memoryQuotaRepository{quota: Quota{
		ProjectID:               "project-1",
		IngestRequestsPerMinute: 1,
		IngestSpansPerMinute:    2,
		IngestBytesPerMinute:    3,
		SSEConnections:          1,
		TokensPerDay:            10,
		CostMicrosPerDay:        10,
	}}
	service := NewService(repo, limiter, nil)
	ctx := context.Background()

	for _, tc := range []struct {
		dimension Dimension
		first     int64
		second    int64
	}{
		{DimensionIngestRequests, 1, 1},
		{DimensionIngestSpans, 2, 1},
		{DimensionIngestBytes, 3, 1},
	} {
		if got := service.CheckIngest(ctx, "project-1", tc.dimension, tc.first); !got.Allowed {
			t.Fatalf("%s first decision = %+v", tc.dimension, got)
		}
		if got := service.CheckIngest(ctx, "project-1", tc.dimension, tc.second); got.Allowed {
			t.Fatalf("%s second decision = %+v, want rejected", tc.dimension, got)
		}
	}
}

func TestServiceDailyUsageUsesExternalBaseline(t *testing.T) {
	limiter, _ := newTestRedisLimiter(t)
	repo := &memoryQuotaRepository{quota: Quota{
		ProjectID:               "project-1",
		IngestRequestsPerMinute: 1,
		IngestSpansPerMinute:    1,
		IngestBytesPerMinute:    1,
		SSEConnections:          1,
		TokensPerDay:            100,
		CostMicrosPerDay:        100,
	}}
	service := NewService(repo, limiter, staticBudgetUsage{tokens: 90})

	decision := service.ConsumeDailyBudget(
		context.Background(), "project-1", DimensionTokensDaily, 11,
	)
	if decision.Allowed {
		t.Fatalf("decision = %+v, want rejected against external baseline", decision)
	}
}

func TestServiceCachesDurableDailyUsage(t *testing.T) {
	limiter, _ := newTestRedisLimiter(t)
	calls := 0
	repo := &memoryQuotaRepository{quota: Quota{
		ProjectID:               "project-1",
		IngestRequestsPerMinute: 1,
		IngestSpansPerMinute:    1,
		IngestBytesPerMinute:    1,
		SSEConnections:          1,
		TokensPerDay:            100,
		CostMicrosPerDay:        100,
	}}
	service := NewService(repo, limiter, staticBudgetUsage{tokens: 1, calls: &calls})

	service.ConsumeDailyBudget(context.Background(), "project-1", DimensionTokensDaily, 1)
	service.ConsumeDailyBudget(context.Background(), "project-1", DimensionTokensDaily, 1)

	if calls != 1 {
		t.Fatalf("DailyUsage calls = %d, want 1", calls)
	}
}
