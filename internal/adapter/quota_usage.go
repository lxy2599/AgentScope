package adapter

import (
	"context"
	"time"

	"AgentScope/internal/quotas"
	"AgentScope/internal/tracestore"
	"github.com/google/uuid"
)

type QuotaBudgetUsage struct {
	store *tracestore.Store
}

var _ quotas.BudgetUsageReader = (*QuotaBudgetUsage)(nil)

func NewQuotaBudgetUsage(store *tracestore.Store) *QuotaBudgetUsage {
	return &QuotaBudgetUsage{store: store}
}

func (q *QuotaBudgetUsage) DailyUsage(
	ctx context.Context,
	projectID string,
	since time.Time,
) (int64, int64, error) {
	id, err := uuid.Parse(projectID)
	if err != nil {
		return 0, 0, err
	}
	return q.store.QueryTokenCostSince(ctx, id, since)
}
