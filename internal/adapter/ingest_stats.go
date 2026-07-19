package adapter

import (
	"context"

	"AgentScope/internal/httpapi"
	"AgentScope/internal/tracestore"
	"github.com/google/uuid"
)

// IngestStatsAdapter implements httpapi.IngestStatsQuerier using a real
// tracestore.Store for ClickHouse aggregation queries.
type IngestStatsAdapter struct {
	store *tracestore.Store
}

// compile-time check
var _ httpapi.IngestStatsQuerier = (*IngestStatsAdapter)(nil)

// NewIngestStatsAdapter creates a new IngestStatsAdapter.
func NewIngestStatsAdapter(store *tracestore.Store) *IngestStatsAdapter {
	return &IngestStatsAdapter{store: store}
}

// QueryProjectStats implements httpapi.IngestStatsQuerier.
func (a *IngestStatsAdapter) QueryProjectStats(ctx context.Context, projectID string, minutes int) (*httpapi.IngestStatsDTO, error) {
	pid := goUUID(projectID)
	if pid == uuid.Nil {
		return &httpapi.IngestStatsDTO{ProjectID: projectID, Minutes: minutes}, nil
	}

	stats, err := a.store.QueryProjectIngestStats(ctx, pid, minutes)
	if err != nil {
		return nil, err
	}

	tokenCost, err := a.store.QueryTokenCostSummary(ctx, pid, minutes)
	if err != nil {
		return nil, err
	}

	return &httpapi.IngestStatsDTO{
		ProjectID:   projectID,
		Minutes:     minutes,
		Spans:       stats.SpanCount,
		Events:      stats.EventCount,
		Errors:      stats.ErrorCount,
		LLMs:        stats.LLMCount,
		Tools:       stats.ToolCount,
		Retrievals:  stats.RetrievalCnt,
		Skills:      stats.SkillCount,
		Tokens:      tokenCost.TotalTokens,
		Cost:        tokenCost.TotalCostUSD,
	}, nil
}

// QueryAgentStats implements httpapi.IngestStatsQuerier.
func (a *IngestStatsAdapter) QueryAgentStats(ctx context.Context, projectID string, minutes int) ([]httpapi.AgentStatsDTO, error) {
	pid := goUUID(projectID)
	if pid == uuid.Nil {
		return nil, nil
	}

	results, err := a.store.QueryAgentErrorStats(ctx, pid, minutes)
	if err != nil {
		return nil, err
	}

	dtos := make([]httpapi.AgentStatsDTO, 0, len(results))
	for _, r := range results {
		dtos = append(dtos, httpapi.AgentStatsDTO{
			AgentName:  r.AgentName,
			ErrorCount: r.ErrorCount,
			RunCount:   r.RunCount,
			P95Latency: r.P95Latency,
		})
	}
	return dtos, nil
}
