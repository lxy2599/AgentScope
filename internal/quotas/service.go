package quotas

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Repository interface {
	Get(ctx context.Context, projectID string) (Quota, error)
	Upsert(ctx context.Context, quota Quota, updatedBy string) (Quota, error)
	RecordEvent(ctx context.Context, event Event) error
	ListEvents(ctx context.Context, projectID string, limit int) ([]Event, error)
}

// BudgetUsageReader provides the durable ClickHouse token and cost baseline.
type BudgetUsageReader interface {
	DailyUsage(ctx context.Context, projectID string, since time.Time) (tokens, costMicros int64, err error)
}

type Service struct {
	repository     Repository
	limiter        Limiter
	usage          BudgetUsageReader
	limiterTimeout time.Duration
	now            func() time.Time
	budgetMu       sync.Mutex
	budgetCache    map[string]budgetCacheEntry
	budgetTTL      time.Duration
}

type ServiceOptions struct {
	LimiterTimeout time.Duration
}

type budgetCacheEntry struct {
	tokens     int64
	costMicros int64
	expiresAt  time.Time
}

func NewService(repository Repository, limiter Limiter, usage BudgetUsageReader) *Service {
	return NewServiceWithOptions(repository, limiter, usage, ServiceOptions{})
}

func NewServiceWithOptions(
	repository Repository,
	limiter Limiter,
	usage BudgetUsageReader,
	options ServiceOptions,
) *Service {
	if options.LimiterTimeout <= 0 {
		options.LimiterTimeout = 250 * time.Millisecond
	}
	return &Service{
		repository:     repository,
		limiter:        limiter,
		usage:          usage,
		limiterTimeout: options.LimiterTimeout,
		now:            time.Now,
		budgetCache:    make(map[string]budgetCacheEntry),
		budgetTTL:      15 * time.Second,
	}
}

func (s *Service) GetQuota(ctx context.Context, projectID string) (Quota, error) {
	if projectID == "" {
		return Quota{}, errors.New("project ID is required")
	}
	if s.repository == nil {
		return DefaultQuota(projectID), nil
	}
	quota, err := s.repository.Get(ctx, projectID)
	if errors.Is(err, ErrQuotaNotFound) {
		return DefaultQuota(projectID), nil
	}
	if err != nil {
		return Quota{}, err
	}
	return quota, nil
}

func (s *Service) UpdateQuota(
	ctx context.Context,
	projectID, updatedBy string,
	quota Quota,
) (Quota, error) {
	if s.repository == nil {
		return Quota{}, errors.New("quota repository is unavailable")
	}
	quota.ProjectID = projectID
	if err := quota.Validate(); err != nil {
		return Quota{}, err
	}
	return s.repository.Upsert(ctx, quota, updatedBy)
}

func (s *Service) CheckIngest(
	ctx context.Context,
	projectID string,
	dimension Dimension,
	amount int64,
) Decision {
	quota, err := s.GetQuota(ctx, projectID)
	if err != nil {
		return s.failure(ctx, projectID, dimension, amount, 0, true, err)
	}
	var limit int64
	switch dimension {
	case DimensionIngestRequests:
		limit = quota.IngestRequestsPerMinute
	case DimensionIngestSpans:
		limit = quota.IngestSpansPerMinute
	case DimensionIngestBytes:
		limit = quota.IngestBytesPerMinute
	default:
		return s.failure(ctx, projectID, dimension, amount, 0, true, errors.New("unsupported ingest quota dimension"))
	}
	if s.limiter == nil {
		return Decision{Allowed: true, Dimension: dimension, Limit: limit, Remaining: limit, Degraded: true, Reason: "distributed quota disabled"}
	}
	limiterCtx, cancel := s.limiterContext(ctx)
	defer cancel()
	decision, err := s.limiter.ConsumeTokenBucket(
		limiterCtx, projectID, dimension, amount, limit, limit, time.Minute,
	)
	if err != nil {
		return s.failure(ctx, projectID, dimension, amount, limit, true, err)
	}
	s.recordIfNotAllowed(ctx, projectID, amount, decision, "fail_open")
	return decision
}

func (s *Service) AcquireSSE(ctx context.Context, projectID string) Decision {
	quota, err := s.GetQuota(ctx, projectID)
	if err != nil {
		return s.failure(ctx, projectID, DimensionSSEConnections, 1, 0, false, err)
	}
	if s.limiter == nil {
		return s.failure(ctx, projectID, DimensionSSEConnections, 1, quota.SSEConnections, false, errors.New("distributed quota disabled"))
	}
	limiterCtx, cancel := s.limiterContext(ctx)
	defer cancel()
	decision, err := s.limiter.AcquireConcurrency(
		limiterCtx, projectID, DimensionSSEConnections, quota.SSEConnections,
	)
	if err != nil {
		return s.failure(ctx, projectID, DimensionSSEConnections, 1, quota.SSEConnections, false, err)
	}
	s.recordIfNotAllowed(ctx, projectID, 1, decision, "fail_closed")
	return decision
}

func (s *Service) ReleaseSSE(ctx context.Context, projectID string) {
	if s.limiter == nil {
		return
	}
	limiterCtx, cancel := s.limiterContext(ctx)
	defer cancel()
	if err := s.limiter.ReleaseConcurrency(limiterCtx, projectID, DimensionSSEConnections); err != nil {
		s.record(ctx, Event{
			ProjectID: projectID,
			Dimension: DimensionSSEConnections,
			Requested: -1,
			Outcome:   EventDegraded,
			FailMode:  "fail_closed",
			Reason:    err.Error(),
		})
	}
}

func (s *Service) ConsumeDailyBudget(
	ctx context.Context,
	projectID string,
	dimension Dimension,
	amount int64,
) Decision {
	quota, err := s.GetQuota(ctx, projectID)
	if err != nil {
		return s.failure(ctx, projectID, dimension, amount, 0, false, err)
	}
	limit := quota.TokensPerDay
	if dimension == DimensionCostMicrosDaily {
		limit = quota.CostMicrosPerDay
	} else if dimension != DimensionTokensDaily {
		return s.failure(ctx, projectID, dimension, amount, 0, false, errors.New("unsupported hard budget dimension"))
	}
	if s.limiter == nil {
		return s.failure(ctx, projectID, dimension, amount, limit, false, errors.New("distributed quota disabled"))
	}

	now := s.now()
	var baseline int64
	if s.usage != nil {
		tokens, costMicros, usageErr := s.dailyUsage(ctx, projectID, now)
		if usageErr != nil {
			return s.failure(ctx, projectID, dimension, amount, limit, false, fmt.Errorf("read durable budget usage: %w", usageErr))
		}
		baseline = tokens
		if dimension == DimensionCostMicrosDaily {
			baseline = costMicros
		}
	}

	limiterCtx, cancel := s.limiterContext(ctx)
	defer cancel()
	decision, err := s.limiter.ConsumeDailyBudget(
		limiterCtx, projectID, dimension, amount, limit, baseline, nextUTCMidnight(now),
	)
	if err != nil {
		return s.failure(ctx, projectID, dimension, amount, limit, false, err)
	}
	s.recordIfNotAllowed(ctx, projectID, amount, decision, "fail_closed")
	return decision
}

func (s *Service) dailyUsage(
	ctx context.Context,
	projectID string,
	now time.Time,
) (int64, int64, error) {
	key := projectID + ":" + now.UTC().Format("2006-01-02")
	s.budgetMu.Lock()
	cached, ok := s.budgetCache[key]
	s.budgetMu.Unlock()
	if ok && now.Before(cached.expiresAt) {
		return cached.tokens, cached.costMicros, nil
	}
	tokens, costMicros, err := s.usage.DailyUsage(ctx, projectID, startOfUTCDay(now))
	if err != nil {
		return 0, 0, err
	}
	s.budgetMu.Lock()
	s.budgetCache[key] = budgetCacheEntry{
		tokens:     tokens,
		costMicros: costMicros,
		expiresAt:  now.Add(s.budgetTTL),
	}
	s.budgetMu.Unlock()
	return tokens, costMicros, nil
}

func (s *Service) Status(ctx context.Context, projectID string) (Status, error) {
	quota, err := s.GetQuota(ctx, projectID)
	if err != nil {
		return Status{}, err
	}
	status := Status{
		Quota: quota,
		Usage: make(map[Dimension]int64),
	}
	for _, dimension := range []Dimension{
		DimensionIngestRequests,
		DimensionIngestSpans,
		DimensionIngestBytes,
		DimensionSSEConnections,
		DimensionTokensDaily,
		DimensionCostMicrosDaily,
	} {
		if s.limiter != nil {
			limiterCtx, cancel := s.limiterContext(ctx)
			current, currentErr := s.limiter.Current(limiterCtx, projectID, dimension)
			cancel()
			if currentErr == nil {
				status.Usage[dimension] = current
			}
		}
	}
	if s.repository != nil {
		status.Events, err = s.repository.ListEvents(ctx, projectID, 50)
		if err != nil {
			return Status{}, err
		}
	}
	if status.Events == nil {
		status.Events = []Event{}
	}
	return status, nil
}

func (s *Service) limiterContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, s.limiterTimeout)
}

func (s *Service) failure(
	ctx context.Context,
	projectID string,
	dimension Dimension,
	amount, limit int64,
	failOpen bool,
	err error,
) Decision {
	reason := "quota_enforcement_unavailable"
	if failOpen {
		reason = "quota_backend_unavailable"
	}
	decision := Decision{
		Allowed:   failOpen,
		Dimension: dimension,
		Limit:     limit,
		Remaining: 0,
		Degraded:  true,
		Reason:    reason,
	}
	failMode := "fail_closed"
	if failOpen {
		failMode = "fail_open"
	}
	s.record(ctx, Event{
		ProjectID: projectID,
		Dimension: dimension,
		Requested: amount,
		Limit:     limit,
		Remaining: 0,
		Outcome:   EventDegraded,
		FailMode:  failMode,
		Reason:    reason,
	})
	return decision
}

func (s *Service) recordIfNotAllowed(
	ctx context.Context,
	projectID string,
	amount int64,
	decision Decision,
	failMode string,
) {
	if decision.Allowed {
		return
	}
	s.record(ctx, Event{
		ProjectID: projectID,
		Dimension: decision.Dimension,
		Requested: amount,
		Limit:     decision.Limit,
		Remaining: decision.Remaining,
		Outcome:   EventRejected,
		FailMode:  failMode,
		Reason:    "quota exceeded",
	})
}

func (s *Service) record(ctx context.Context, event Event) {
	if s.repository == nil {
		return
	}
	event.OccurredAt = s.now()
	_ = s.repository.RecordEvent(ctx, event)
}

func startOfUTCDay(now time.Time) time.Time {
	utc := now.UTC()
	return time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
}
