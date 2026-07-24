package tracestore

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ---- Aggregation Query Types ----

// ProjectIngestStats represents project-level ingest statistics for a time window.
type ProjectIngestStats struct {
	SpanCount    uint64  `ch:"span_count"`
	EventCount   uint64  `ch:"event_count"`
	ErrorCount   uint64  `ch:"error_count"`
	LLMCount     uint64  `ch:"llm_count"`
	ToolCount    uint64  `ch:"tool_count"`
	RetrievalCnt uint64  `ch:"retrieval_count"`
	SkillCount   uint64  `ch:"skill_count"`
	TotalTokens  uint64  `ch:"total_tokens"`
	TotalCost    float64 `ch:"total_cost"`
}

// AgentErrorStats represents agent-level error statistics for a time window.
type AgentErrorStats struct {
	AgentName  string  `ch:"agent_name"`
	ErrorCount uint64  `ch:"error_count"`
	RunCount   uint64  `ch:"run_count"`
	P95Latency float64 `ch:"p95_latency"`
}

// TokenCostSummary represents token and cost totals from LLM calls.
type TokenCostSummary struct {
	TotalTokens  uint64  `ch:"total_tokens"`
	TotalCostUSD float64 `ch:"total_cost"`
	CallCount    uint64  `ch:"call_count"`
}

// IngestHealthSummary aggregates accepted and failed ingest requests.
type IngestHealthSummary struct {
	AcceptedCount       uint64     `ch:"accepted_count"`
	RejectedCount       uint64     `ch:"rejected_count"`
	StorageFailedCount  uint64     `ch:"storage_failed_count"`
	MissingRunIDCount   uint64     `ch:"missing_run_id_count"`
	MissingTraceIDCount uint64     `ch:"missing_trace_id_count"`
	LastSuccessAt       *time.Time `ch:"last_success_at"`
	LastFailureAt       *time.Time `ch:"last_failure_at"`
	LastErrorCode       string     `ch:"last_error_code"`
	SDKQueueDepth       *int64     `ch:"sdk_queue_depth"`
	SDKQueueCapacity    *int64     `ch:"sdk_queue_capacity"`
	SDKSpansDropped     *int64     `ch:"sdk_spans_dropped"`
	SDKExportRetries    *int64     `ch:"sdk_export_retries"`
	SDKExportLatencyMs  *float64   `ch:"sdk_export_latency_ms"`
}

// IngestHealthEventSummary is a safe, content-free event for the UI.
type IngestHealthEventSummary struct {
	AgentName  string    `ch:"agent_name"`
	Outcome    string    `ch:"outcome"`
	HTTPStatus int32     `ch:"http_status"`
	SpanCount  uint64    `ch:"span_count"`
	ErrorCode  string    `ch:"error_code"`
	EventTime  time.Time `ch:"event_time"`
}

// ---- QueryProjectIngestStats ----

// QueryProjectIngestStats returns ingest statistics for the given project
// over the last N minutes by querying trace tables directly.
func (s *Store) QueryProjectIngestStats(ctx context.Context, projectID uuid.UUID, minutes int) (*ProjectIngestStats, error) {
	stats := &ProjectIngestStats{}
	cutoff := time.Now().Add(-time.Duration(minutes) * time.Minute)

	// Span count and error count from trace_spans
	err := s.conn.QueryRow(ctx,
		`SELECT
			count() AS span_count,
			countIf(status_code = 'ERROR') AS error_count
		FROM trace_spans
		WHERE project_id = $1 AND start_time >= $2`,
		projectID, cutoff,
	).Scan(&stats.SpanCount, &stats.ErrorCount)
	if err != nil {
		return nil, fmt.Errorf("query span stats: %w", err)
	}

	// Event count
	err = s.conn.QueryRow(ctx,
		`SELECT count() AS event_count
		FROM trace_span_events
		WHERE project_id = $1 AND event_time >= $2`,
		projectID, cutoff,
	).Scan(&stats.EventCount)
	if err != nil {
		return nil, fmt.Errorf("query event count: %w", err)
	}

	// LLM count
	err = s.conn.QueryRow(ctx,
		`SELECT count() AS llm_count
		FROM trace_llm_calls
		WHERE project_id = $1 AND start_time >= $2`,
		projectID, cutoff,
	).Scan(&stats.LLMCount)
	if err != nil {
		return nil, fmt.Errorf("query llm count: %w", err)
	}

	// Tool count
	err = s.conn.QueryRow(ctx,
		`SELECT count() AS tool_count
		FROM trace_tool_calls
		WHERE project_id = $1 AND start_time >= $2`,
		projectID, cutoff,
	).Scan(&stats.ToolCount)
	if err != nil {
		return nil, fmt.Errorf("query tool count: %w", err)
	}

	// Retrieval count
	err = s.conn.QueryRow(ctx,
		`SELECT count() AS retrieval_count
		FROM trace_retrieval_calls
		WHERE project_id = $1 AND start_time >= $2`,
		projectID, cutoff,
	).Scan(&stats.RetrievalCnt)
	if err != nil {
		return nil, fmt.Errorf("query retrieval count: %w", err)
	}

	// Skill count
	err = s.conn.QueryRow(ctx,
		`SELECT count() AS skill_count
		FROM trace_skill_calls
		WHERE project_id = $1 AND start_time >= $2`,
		projectID, cutoff,
	).Scan(&stats.SkillCount)
	if err != nil {
		return nil, fmt.Errorf("query skill count: %w", err)
	}

	return stats, nil
}

// QueryTokenCostSummary returns total tokens and cost from LLM calls for
// the given project over the last N minutes.
func (s *Store) QueryTokenCostSummary(ctx context.Context, projectID uuid.UUID, minutes int) (*TokenCostSummary, error) {
	summary := &TokenCostSummary{}
	cutoff := time.Now().Add(-time.Duration(minutes) * time.Minute)

	err := s.conn.QueryRow(ctx,
		`SELECT
			count() AS call_count,
			toUInt64(ifNull(sum(total_tokens), 0)) AS total_tokens,
			toFloat64(ifNull(sum(cost), 0)) AS total_cost
		FROM trace_llm_calls
		WHERE project_id = $1 AND start_time >= $2`,
		projectID, cutoff,
	).Scan(&summary.CallCount, &summary.TotalTokens, &summary.TotalCostUSD)
	if err != nil {
		return nil, fmt.Errorf("query token cost: %w", err)
	}
	return summary, nil
}

// QueryTokenCostSince returns the durable token and cost totals since a fixed
// timestamp. Cost is converted to integer micro-dollars for exact quota checks.
func (s *Store) QueryTokenCostSince(
	ctx context.Context,
	projectID uuid.UUID,
	since time.Time,
) (tokens, costMicros int64, err error) {
	var totalTokens uint64
	var totalCost float64
	err = s.conn.QueryRow(ctx,
		`SELECT
			toUInt64(ifNull(sum(total_tokens), 0)),
			toFloat64(ifNull(sum(cost), 0))
		FROM trace_llm_calls
		WHERE project_id = $1 AND start_time >= $2`,
		projectID,
		since,
	).Scan(&totalTokens, &totalCost)
	if err != nil {
		return 0, 0, fmt.Errorf("query daily token cost: %w", err)
	}
	return int64(totalTokens), int64(totalCost * 1_000_000), nil
}

// QueryAgentErrorStats returns per-agent error statistics for the given
// project over the last N minutes.
func (s *Store) QueryAgentErrorStats(ctx context.Context, projectID uuid.UUID, minutes int) ([]AgentErrorStats, error) {
	cutoff := time.Now().Add(-time.Duration(minutes) * time.Minute)

	// Try trace_llm_calls first for agent_name info embedded in attributes.
	// If the attributes column contains "agentscope.agent.name", extract it.
	// For now, use trace_spans which is more universal.
	rows, err := s.conn.Query(ctx,
		`SELECT
			extractAll(attributes, '"agentscope\\.agent\\.name":"([^"]+)"')[1] AS agent_name,
			countIf(status_code = 'ERROR') AS error_count,
			uniqExact(run_id) AS run_count,
			quantile(0.95)(duration_ms) AS p95_latency
		FROM trace_spans
		WHERE project_id = $1 AND start_time >= $2 AND duration_ms IS NOT NULL
		GROUP BY agent_name
		ORDER BY error_count DESC`,
		projectID, cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("query agent error stats: %w", err)
	}
	defer rows.Close()

	var stats []AgentErrorStats
	for rows.Next() {
		var s AgentErrorStats
		if err := rows.ScanStruct(&s); err != nil {
			return nil, fmt.Errorf("scan agent error stat: %w", err)
		}
		stats = append(stats, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return stats, nil
}

// QueryIngestHealthSummary reads health events directly so very recent failures
// are visible before asynchronous rollups catch up.
func (s *Store) QueryIngestHealthSummary(ctx context.Context, projectID uuid.UUID, minutes int) (*IngestHealthSummary, error) {
	result := &IngestHealthSummary{}
	cutoff := time.Now().Add(-time.Duration(minutes) * time.Minute)
	err := s.conn.QueryRow(ctx, `SELECT
		countIf(outcome = 'accepted'),
		countIf(outcome != 'accepted'),
		countIf(outcome = 'storage_failed'),
		countIf(missing_run_id),
		countIf(missing_trace_id),
		maxIf(event_time, outcome = 'accepted'),
		maxIf(event_time, outcome != 'accepted'),
		argMaxIf(error_code, event_time, outcome != 'accepted'),
		argMax(sdk_queue_depth, event_time),
		argMax(sdk_queue_capacity, event_time),
		argMax(sdk_spans_dropped, event_time),
		argMax(sdk_export_retries, event_time),
		argMax(sdk_export_latency_ms, event_time)
	FROM trace_ingest_health_events
	WHERE project_id = $1 AND event_time >= $2`, projectID, cutoff).
		Scan(&result.AcceptedCount, &result.RejectedCount, &result.StorageFailedCount,
			&result.MissingRunIDCount, &result.MissingTraceIDCount, &result.LastSuccessAt,
			&result.LastFailureAt, &result.LastErrorCode, &result.SDKQueueDepth,
			&result.SDKQueueCapacity, &result.SDKSpansDropped, &result.SDKExportRetries,
			&result.SDKExportLatencyMs)
	if err != nil {
		return nil, fmt.Errorf("query ingest health summary: %w", err)
	}
	return result, nil
}

// ListIngestHealthEvents returns recent health evidence scoped to one project.
func (s *Store) ListIngestHealthEvents(ctx context.Context, projectID uuid.UUID, minutes, limit int) ([]IngestHealthEventSummary, error) {
	cutoff := time.Now().Add(-time.Duration(minutes) * time.Minute)
	rows, err := s.conn.Query(ctx, `SELECT agent_name, outcome, http_status, span_count, error_code, event_time
	FROM trace_ingest_health_events WHERE project_id = $1 AND event_time >= $2
	ORDER BY event_time DESC LIMIT $3`, projectID, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("query ingest health events: %w", err)
	}
	defer rows.Close()
	var result []IngestHealthEventSummary
	for rows.Next() {
		var item IngestHealthEventSummary
		if err := rows.ScanStruct(&item); err != nil {
			return nil, fmt.Errorf("scan ingest health event: %w", err)
		}
		result = append(result, item)
	}
	return result, rows.Err()
}
