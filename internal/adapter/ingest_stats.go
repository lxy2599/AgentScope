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
	health, err := a.store.QueryIngestHealthSummary(ctx, pid, minutes)
	if err != nil {
		return nil, err
	}

	tokenCost, err := a.store.QueryTokenCostSummary(ctx, pid, minutes)
	if err != nil {
		return nil, err
	}

	return &httpapi.IngestStatsDTO{
		ProjectID:  projectID,
		Minutes:    minutes,
		Spans:      stats.SpanCount,
		Events:     stats.EventCount,
		Errors:     stats.ErrorCount,
		LLMs:       stats.LLMCount,
		Tools:      stats.ToolCount,
		Retrievals: stats.RetrievalCnt,
		Skills:     stats.SkillCount,
		Tokens:     tokenCost.TotalTokens,
		Cost:       tokenCost.TotalCostUSD,
		Accepted:   health.AcceptedCount, Rejected: health.RejectedCount,
		StorageFailed: health.StorageFailedCount, MissingRunID: health.MissingRunIDCount,
		MissingTraceID: health.MissingTraceIDCount, LastSuccessAt: health.LastSuccessAt,
		LastFailureAt: health.LastFailureAt, LastErrorCode: health.LastErrorCode,
		Status: ingestHealthStatus(health.AcceptedCount, health.RejectedCount, health.StorageFailedCount),
	}, nil
}

func (a *IngestStatsAdapter) ListHealthEvents(ctx context.Context, projectID string, minutes, limit int) ([]httpapi.IngestHealthEventDTO, error) {
	pid := goUUID(projectID)
	if pid == uuid.Nil {
		return []httpapi.IngestHealthEventDTO{}, nil
	}
	items, err := a.store.ListIngestHealthEvents(ctx, pid, minutes, limit)
	if err != nil {
		return nil, err
	}
	result := make([]httpapi.IngestHealthEventDTO, 0, len(items))
	for _, item := range items {
		result = append(result, httpapi.IngestHealthEventDTO{AgentName: item.AgentName, Outcome: item.Outcome, HTTPStatus: item.HTTPStatus, SpanCount: item.SpanCount, ErrorCode: item.ErrorCode, EventTime: item.EventTime})
	}
	return result, nil
}

func ingestHealthStatus(accepted, rejected, storageFailed uint64) string {
	if storageFailed > 0 {
		return "storage_error"
	}
	if rejected > 0 {
		return "degraded"
	}
	if accepted == 0 {
		return "no_traffic"
	}
	return "healthy"
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
