package tracestore

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/google/uuid"
)

// TraceSpan represents an OpenTelemetry span stored in ClickHouse.
type TraceSpan struct {
	ProjectID     uuid.UUID  `ch:"project_id"`
	RunID         uuid.UUID  `ch:"run_id"`
	TraceID       string     `ch:"trace_id"`
	SpanID        string     `ch:"span_id"`
	ParentSpanID  *string    `ch:"parent_span_id"`
	Name          string     `ch:"name"`
	Kind          string     `ch:"kind"`
	StatusCode    string     `ch:"status_code"`
	StatusMessage *string    `ch:"status_message"`
	StartTime     time.Time  `ch:"start_time"`
	EndTime       *time.Time `ch:"end_time"`
	DurationMs    *int64     `ch:"duration_ms"`
	Attributes    string     `ch:"attributes"`
	Truncated     bool       `ch:"truncated"`
	IngestedAt    time.Time  `ch:"ingested_at"`
}

// TraceSpanEvent represents an OpenTelemetry span event stored in ClickHouse.
type TraceSpanEvent struct {
	ProjectID  uuid.UUID `ch:"project_id"`
	RunID      uuid.UUID `ch:"run_id"`
	TraceID    string    `ch:"trace_id"`
	SpanID     string    `ch:"span_id"`
	EventName  string    `ch:"event_name"`
	EventTime  time.Time `ch:"event_time"`
	Attributes string    `ch:"attributes"`
	Truncated  bool      `ch:"truncated"`
	IngestedAt time.Time `ch:"ingested_at"`
}

// TraceLLMCall represents a structured LLM call extracted from span attributes.
type TraceLLMCall struct {
	ProjectID        uuid.UUID `ch:"project_id"`
	RunID            uuid.UUID `ch:"run_id"`
	TraceID          string    `ch:"trace_id"`
	SpanID           string    `ch:"span_id"`
	AgentID          *string   `ch:"agent_id"`
	AgentInstanceID  *string   `ch:"agent_instance_id"`
	Provider         string    `ch:"provider"`
	Model            string    `ch:"model"`
	InputTokens      *int64    `ch:"input_tokens"`
	OutputTokens     *int64    `ch:"output_tokens"`
	TotalTokens      *int64    `ch:"total_tokens"`
	CacheReadTokens  *int64    `ch:"cache_read_tokens"`
	CacheWriteTokens *int64    `ch:"cache_write_tokens"`
	TTFTMs           *int64    `ch:"ttft_ms"`
	Cost             *float64  `ch:"cost"`
	EstimatedCost    *float64  `ch:"estimated_cost"`
	ActualCost       *float64  `ch:"actual_cost"`
	CostSource       string    `ch:"cost_source"`
	InputSummary     string    `ch:"input_summary"`
	OutputSummary    string    `ch:"output_summary"`
	ContentCaptured  bool      `ch:"content_captured"`
	Status           string    `ch:"status"`
	Error            *string   `ch:"error"`
	StartTime        time.Time `ch:"start_time"`
	DurationMs       *int64    `ch:"duration_ms"`
	IngestedAt       time.Time `ch:"ingested_at"`
}

// TraceToolCall represents a structured tool call extracted from span attributes.
type TraceToolCall struct {
	ProjectID        uuid.UUID `ch:"project_id"`
	RunID            uuid.UUID `ch:"run_id"`
	TraceID          string    `ch:"trace_id"`
	SpanID           string    `ch:"span_id"`
	AgentID          *string   `ch:"agent_id"`
	AgentInstanceID  *string   `ch:"agent_instance_id"`
	ToolName         string    `ch:"tool_name"`
	ToolType         string    `ch:"tool_type"`
	ArgumentsSummary string    `ch:"arguments_summary"`
	ResultSummary    string    `ch:"result_summary"`
	Success          bool      `ch:"success"`
	Error            *string   `ch:"error"`
	RetryCount       int64     `ch:"retry_count"`
	SideEffectRisk   *string   `ch:"side_effect_risk"`
	StartTime        time.Time `ch:"start_time"`
	DurationMs       *int64    `ch:"duration_ms"`
	IngestedAt       time.Time `ch:"ingested_at"`
}

// TraceRetrievalCall represents a structured knowledge base retrieval call.
type TraceRetrievalCall struct {
	ProjectID       uuid.UUID `ch:"project_id"`
	RunID           uuid.UUID `ch:"run_id"`
	TraceID         string    `ch:"trace_id"`
	SpanID          string    `ch:"span_id"`
	AgentID         *string   `ch:"agent_id"`
	AgentInstanceID *string   `ch:"agent_instance_id"`
	KnowledgeBase   string    `ch:"knowledge_base"`
	QueryText       string    `ch:"query_text"`
	TopK            *int64    `ch:"top_k"`
	ReturnedCount   *int64    `ch:"returned_count"`
	DocumentIDs     []string  `ch:"document_ids"`
	Scores          []float64 `ch:"scores"`
	StartTime       time.Time `ch:"start_time"`
	DurationMs      *int64    `ch:"duration_ms"`
	IngestedAt      time.Time `ch:"ingested_at"`
}

// TraceSkillCall represents a structured skill invocation extracted from span attributes.
type TraceSkillCall struct {
	ProjectID       uuid.UUID `ch:"project_id"`
	RunID           uuid.UUID `ch:"run_id"`
	TraceID         string    `ch:"trace_id"`
	SpanID          string    `ch:"span_id"`
	AgentID         *string   `ch:"agent_id"`
	AgentInstanceID *string   `ch:"agent_instance_id"`
	SkillName       string    `ch:"skill_name"`
	SkillVersion    string    `ch:"skill_version"`
	InputSummary    string    `ch:"input_summary"`
	OutputSummary   string    `ch:"output_summary"`
	Success         bool      `ch:"success"`
	Error           *string   `ch:"error"`
	StartTime       time.Time `ch:"start_time"`
	DurationMs      *int64    `ch:"duration_ms"`
	IngestedAt      time.Time `ch:"ingested_at"`
}

// TraceThought represents a structured agent thought stored in ClickHouse.
type TraceThought struct {
	ProjectID       uuid.UUID `ch:"project_id"`
	RunID           uuid.UUID `ch:"run_id"`
	TraceID         string    `ch:"trace_id"`
	SpanID          string    `ch:"span_id"`
	AgentID         string    `ch:"agent_id"`
	AgentInstanceID string    `ch:"agent_instance_id"`
	Stage           string    `ch:"stage"`
	Summary         string    `ch:"summary"`
	Content         string    `ch:"content"`
	Confidence      *float64  `ch:"confidence"`
	ContentCaptured bool      `ch:"content_captured"`
	StartTime       time.Time `ch:"start_time"`
	DurationMs      *int64    `ch:"duration_ms"`
	IngestedAt      time.Time `ch:"ingested_at"`
}

// TraceFinalAnswer represents a structured agent final answer stored in ClickHouse.
type TraceFinalAnswer struct {
	ProjectID        uuid.UUID `ch:"project_id"`
	RunID            uuid.UUID `ch:"run_id"`
	TraceID          string    `ch:"trace_id"`
	SpanID           string    `ch:"span_id"`
	AnswerSummary    string    `ch:"answer_summary"`
	AnswerOutput     string    `ch:"answer_output"`
	StructuredOutput string    `ch:"structured_output"`
	StartTime        time.Time `ch:"start_time"`
	DurationMs       *int64    `ch:"duration_ms"`
	IngestedAt       time.Time `ch:"ingested_at"`
}

// IngestHealthEvent is content-free operational evidence for one ingest request.
type IngestHealthEvent struct {
	ProjectID          uuid.UUID `ch:"project_id"`
	AgentName          string    `ch:"agent_name"`
	Outcome            string    `ch:"outcome"`
	HTTPStatus         int32     `ch:"http_status"`
	SpanCount          uint64    `ch:"span_count"`
	EventCount         uint64    `ch:"event_count"`
	SemanticCount      uint64    `ch:"semantic_count"`
	MissingRunID       bool      `ch:"missing_run_id"`
	MissingTraceID     bool      `ch:"missing_trace_id"`
	ErrorCode          string    `ch:"error_code"`
	DurationMs         int64     `ch:"duration_ms"`
	SDKQueueDepth      *int64    `ch:"sdk_queue_depth"`
	SDKQueueCapacity   *int64    `ch:"sdk_queue_capacity"`
	SDKSpansDropped    *int64    `ch:"sdk_spans_dropped"`
	SDKExportRetries   *int64    `ch:"sdk_export_retries"`
	SDKExportLatencyMs *float64  `ch:"sdk_export_latency_ms"`
	EventTime          time.Time `ch:"event_time"`
	IngestedAt         time.Time `ch:"ingested_at"`
}

// Store provides access to ClickHouse trace storage.
type Store struct {
	conn driver.Conn
}

// Open creates a connection to ClickHouse from a DSN and verifies connectivity.
// DSN format: clickhouse://host:port/database?param=value
// All subsequent SQL queries operate on the database specified in the DSN.
func Open(ctx context.Context, dsn string) (*Store, error) {
	opts, err := parseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse clickhouse DSN: %w", err)
	}
	conn, err := clickhouse.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open clickhouse: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping clickhouse: %w", err)
	}
	return &Store{conn: conn}, nil
}

// parseDSN parses a ClickHouse DSN string into Options.
// clickhouse://user:password@host:9000/database?param=value
func parseDSN(dsn string) (*clickhouse.Options, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, err
	}
	password, _ := u.User.Password()
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		port = "9000"
	}
	addr := host + ":" + port
	db := strings.TrimPrefix(u.Path, "/")
	if db == "" {
		db = "default"
	}

	opts := &clickhouse.Options{
		Addr: []string{addr},
		Auth: clickhouse.Auth{
			Username: u.User.Username(),
			Password: password,
			Database: db,
		},
		Settings: clickhouse.Settings{
			"insert_quorum": 1,
		},
	}
	return opts, nil
}

// InsertSpans inserts a batch of trace spans into ClickHouse.
// Uses unqualified table names — the target database is determined by the
// connection DSN passed to Open.
func (s *Store) InsertSpans(ctx context.Context, spans []TraceSpan) error {
	if len(spans) == 0 {
		return nil
	}
	batch, err := s.conn.PrepareBatch(ctx, "INSERT INTO trace_spans")
	if err != nil {
		return fmt.Errorf("prepare batch for spans: %w", err)
	}
	for _, span := range spans {
		if err := batch.Append(
			span.ProjectID,
			span.RunID,
			span.TraceID,
			span.SpanID,
			span.ParentSpanID,
			span.Name,
			span.Kind,
			span.StatusCode,
			span.StatusMessage,
			span.StartTime,
			span.EndTime,
			span.DurationMs,
			span.Attributes,
			span.Truncated,
			span.IngestedAt,
		); err != nil {
			return fmt.Errorf("append span to batch: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send span batch: %w", err)
	}
	return nil
}

// InsertSpanEvents inserts a batch of trace span events into ClickHouse.
func (s *Store) InsertSpanEvents(ctx context.Context, events []TraceSpanEvent) error {
	if len(events) == 0 {
		return nil
	}
	batch, err := s.conn.PrepareBatch(ctx, "INSERT INTO trace_span_events")
	if err != nil {
		return fmt.Errorf("prepare batch for span events: %w", err)
	}
	for _, ev := range events {
		if err := batch.Append(
			ev.ProjectID,
			ev.RunID,
			ev.TraceID,
			ev.SpanID,
			ev.EventName,
			ev.EventTime,
			ev.Attributes,
			ev.Truncated,
			ev.IngestedAt,
		); err != nil {
			return fmt.Errorf("append span event to batch: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send span event batch: %w", err)
	}
	return nil
}

// ListSpansByRun retrieves all spans for a given run, ordered by start_time.
func (s *Store) ListSpansByRun(ctx context.Context, runID uuid.UUID) ([]TraceSpan, error) {
	rows, err := s.conn.Query(ctx,
		`SELECT
			project_id, run_id, trace_id, span_id, parent_span_id,
			name, kind, status_code, status_message,
			start_time, end_time, duration_ms,
			attributes, truncated, ingested_at
		FROM trace_spans
		WHERE run_id = $1
		ORDER BY start_time ASC`,
		runID)
	if err != nil {
		return nil, fmt.Errorf("query spans by run: %w", err)
	}
	defer rows.Close()

	var spans []TraceSpan
	for rows.Next() {
		var s TraceSpan
		if err := rows.ScanStruct(&s); err != nil {
			return nil, fmt.Errorf("scan span: %w", err)
		}
		spans = append(spans, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("spans rows iteration: %w", err)
	}
	return spans, nil
}

// ListSpanEventsByRun retrieves all span events for a given run, ordered by event_time.
func (s *Store) ListSpanEventsByRun(ctx context.Context, runID uuid.UUID) ([]TraceSpanEvent, error) {
	rows, err := s.conn.Query(ctx,
		`SELECT
			project_id, run_id, trace_id, span_id,
			event_name, event_time,
			attributes, truncated, ingested_at
		FROM trace_span_events
		WHERE run_id = $1
		ORDER BY event_time ASC`,
		runID)
	if err != nil {
		return nil, fmt.Errorf("query span events by run: %w", err)
	}
	defer rows.Close()

	var events []TraceSpanEvent
	for rows.Next() {
		var e TraceSpanEvent
		if err := rows.ScanStruct(&e); err != nil {
			return nil, fmt.Errorf("scan span event: %w", err)
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("span events rows iteration: %w", err)
	}
	return events, nil
}

// Close closes the ClickHouse connection.
func (s *Store) Close() error {
	return s.conn.Close()
}

// InsertIngestHealthEvents persists content-free ingest health observations.
func (s *Store) InsertIngestHealthEvents(ctx context.Context, events []IngestHealthEvent) error {
	if len(events) == 0 {
		return nil
	}
	batch, err := s.conn.PrepareBatch(ctx, `INSERT INTO trace_ingest_health_events (
		project_id, agent_name, outcome, http_status, span_count, event_count,
		semantic_count, missing_run_id, missing_trace_id, error_code, duration_ms,
		sdk_queue_depth, sdk_queue_capacity, sdk_spans_dropped,
		sdk_export_retries, sdk_export_latency_ms, event_time, ingested_at
	)`)
	if err != nil {
		return fmt.Errorf("prepare ingest health batch: %w", err)
	}
	for _, event := range events {
		if err := batch.AppendStruct(&event); err != nil {
			return fmt.Errorf("append ingest health event: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send ingest health batch: %w", err)
	}
	return nil
}

// ---- LLM Calls ----

// InsertLLMCalls inserts a batch of structured LLM call records into ClickHouse.
func (s *Store) InsertLLMCalls(ctx context.Context, calls []TraceLLMCall) error {
	if len(calls) == 0 {
		return nil
	}
	batch, err := s.conn.PrepareBatch(ctx, `INSERT INTO trace_llm_calls (
		project_id, run_id, trace_id, span_id, agent_id, agent_instance_id,
		provider, model, input_tokens, output_tokens, total_tokens,
		cache_read_tokens, cache_write_tokens, ttft_ms, cost, estimated_cost,
		actual_cost, cost_source, input_summary, output_summary, content_captured,
		status, error, start_time, duration_ms, ingested_at
	)`)
	if err != nil {
		return fmt.Errorf("prepare batch for llm calls: %w", err)
	}
	for _, c := range calls {
		if err := batch.Append(
			c.ProjectID, c.RunID, c.TraceID, c.SpanID,
			c.AgentID, c.AgentInstanceID,
			c.Provider, c.Model,
			c.InputTokens, c.OutputTokens, c.TotalTokens,
			c.CacheReadTokens, c.CacheWriteTokens, c.TTFTMs,
			c.Cost, c.EstimatedCost, c.ActualCost, c.CostSource,
			c.InputSummary, c.OutputSummary, c.ContentCaptured,
			c.Status, c.Error,
			c.StartTime, c.DurationMs, c.IngestedAt,
		); err != nil {
			return fmt.Errorf("append llm call to batch: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send llm call batch: %w", err)
	}
	return nil
}

// ListLLMCallsByRun retrieves LLM calls for a given run, ordered by start_time.
func (s *Store) ListLLMCallsByRun(ctx context.Context, runID uuid.UUID) ([]TraceLLMCall, error) {
	rows, err := s.conn.Query(ctx,
		`SELECT
			project_id, run_id, trace_id, span_id,
			agent_id, agent_instance_id,
			provider, model,
			input_tokens, output_tokens, total_tokens,
			cache_read_tokens, cache_write_tokens, ttft_ms,
			cost, estimated_cost, actual_cost, cost_source,
			input_summary, output_summary, content_captured,
			status, error,
			start_time, duration_ms, ingested_at
		FROM trace_llm_calls
		WHERE run_id = $1
		ORDER BY start_time ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("query llm calls by run: %w", err)
	}
	defer rows.Close()

	var calls []TraceLLMCall
	for rows.Next() {
		var c TraceLLMCall
		if err := rows.ScanStruct(&c); err != nil {
			return nil, fmt.Errorf("scan llm call: %w", err)
		}
		calls = append(calls, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("llm calls rows iteration: %w", err)
	}
	return calls, nil
}

// ---- Tool Calls ----

// InsertToolCalls inserts a batch of structured tool call records into ClickHouse.
func (s *Store) InsertToolCalls(ctx context.Context, calls []TraceToolCall) error {
	if len(calls) == 0 {
		return nil
	}
	batch, err := s.conn.PrepareBatch(ctx, "INSERT INTO trace_tool_calls")
	if err != nil {
		return fmt.Errorf("prepare batch for tool calls: %w", err)
	}
	for _, c := range calls {
		if err := batch.Append(
			c.ProjectID, c.RunID, c.TraceID, c.SpanID,
			c.AgentID, c.AgentInstanceID,
			c.ToolName, c.ToolType,
			c.ArgumentsSummary, c.ResultSummary,
			c.Success, c.Error, c.RetryCount, c.SideEffectRisk,
			c.StartTime, c.DurationMs, c.IngestedAt,
		); err != nil {
			return fmt.Errorf("append tool call to batch: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send tool call batch: %w", err)
	}
	return nil
}

// ListToolCallsByRun retrieves tool calls for a given run, ordered by start_time.
func (s *Store) ListToolCallsByRun(ctx context.Context, runID uuid.UUID) ([]TraceToolCall, error) {
	rows, err := s.conn.Query(ctx,
		`SELECT
			project_id, run_id, trace_id, span_id,
			agent_id, agent_instance_id,
			tool_name, tool_type,
			arguments_summary, result_summary,
			success, error, retry_count, side_effect_risk,
			start_time, duration_ms, ingested_at
		FROM trace_tool_calls
		WHERE run_id = $1
		ORDER BY start_time ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("query tool calls by run: %w", err)
	}
	defer rows.Close()

	var calls []TraceToolCall
	for rows.Next() {
		var c TraceToolCall
		if err := rows.ScanStruct(&c); err != nil {
			return nil, fmt.Errorf("scan tool call: %w", err)
		}
		calls = append(calls, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("tool calls rows iteration: %w", err)
	}
	return calls, nil
}

// ---- Retrieval Calls ----

// InsertRetrievalCalls inserts a batch of structured retrieval call records into ClickHouse.
func (s *Store) InsertRetrievalCalls(ctx context.Context, calls []TraceRetrievalCall) error {
	if len(calls) == 0 {
		return nil
	}
	batch, err := s.conn.PrepareBatch(ctx, "INSERT INTO trace_retrieval_calls")
	if err != nil {
		return fmt.Errorf("prepare batch for retrieval calls: %w", err)
	}
	for _, c := range calls {
		if err := batch.Append(
			c.ProjectID, c.RunID, c.TraceID, c.SpanID,
			c.AgentID, c.AgentInstanceID,
			c.KnowledgeBase, c.QueryText,
			c.TopK, c.ReturnedCount,
			c.DocumentIDs, c.Scores,
			c.StartTime, c.DurationMs, c.IngestedAt,
		); err != nil {
			return fmt.Errorf("append retrieval call to batch: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send retrieval call batch: %w", err)
	}
	return nil
}

// ListRetrievalCallsByRun retrieves retrieval calls for a given run, ordered by start_time.
func (s *Store) ListRetrievalCallsByRun(ctx context.Context, runID uuid.UUID) ([]TraceRetrievalCall, error) {
	rows, err := s.conn.Query(ctx,
		`SELECT
			project_id, run_id, trace_id, span_id,
			agent_id, agent_instance_id,
			knowledge_base, query_text,
			top_k, returned_count,
			document_ids, scores,
			start_time, duration_ms, ingested_at
		FROM trace_retrieval_calls
		WHERE run_id = $1
		ORDER BY start_time ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("query retrieval calls by run: %w", err)
	}
	defer rows.Close()

	var calls []TraceRetrievalCall
	for rows.Next() {
		var c TraceRetrievalCall
		if err := rows.ScanStruct(&c); err != nil {
			return nil, fmt.Errorf("scan retrieval call: %w", err)
		}
		calls = append(calls, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("retrieval calls rows iteration: %w", err)
	}
	return calls, nil
}

// ---- Skill Calls ----

// InsertSkillCalls inserts a batch of structured skill call records into ClickHouse.
func (s *Store) InsertSkillCalls(ctx context.Context, calls []TraceSkillCall) error {
	if len(calls) == 0 {
		return nil
	}
	batch, err := s.conn.PrepareBatch(ctx, "INSERT INTO trace_skill_calls")
	if err != nil {
		return fmt.Errorf("prepare batch for skill calls: %w", err)
	}
	for _, c := range calls {
		if err := batch.Append(
			c.ProjectID, c.RunID, c.TraceID, c.SpanID,
			c.AgentID, c.AgentInstanceID,
			c.SkillName, c.SkillVersion,
			c.InputSummary, c.OutputSummary,
			c.Success, c.Error,
			c.StartTime, c.DurationMs, c.IngestedAt,
		); err != nil {
			return fmt.Errorf("append skill call to batch: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send skill call batch: %w", err)
	}
	return nil
}

// ListSkillCallsByRun retrieves skill calls for a given run, ordered by start_time.
func (s *Store) ListSkillCallsByRun(ctx context.Context, runID uuid.UUID) ([]TraceSkillCall, error) {
	rows, err := s.conn.Query(ctx,
		`SELECT
			project_id, run_id, trace_id, span_id,
			agent_id, agent_instance_id,
			skill_name, skill_version,
			input_summary, output_summary,
			success, error,
			start_time, duration_ms, ingested_at
		FROM trace_skill_calls
		WHERE run_id = $1
		ORDER BY start_time ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("query skill calls by run: %w", err)
	}
	defer rows.Close()

	var calls []TraceSkillCall
	for rows.Next() {
		var c TraceSkillCall
		if err := rows.ScanStruct(&c); err != nil {
			return nil, fmt.Errorf("scan skill call: %w", err)
		}
		calls = append(calls, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("skill calls rows iteration: %w", err)
	}
	return calls, nil
}

// ---- Thought Records ----

// InsertThoughts inserts a batch of structured thought records into ClickHouse.
func (s *Store) InsertThoughts(ctx context.Context, thoughts []TraceThought) error {
	if len(thoughts) == 0 {
		return nil
	}
	batch, err := s.conn.PrepareBatch(ctx, "INSERT INTO trace_thoughts")
	if err != nil {
		return fmt.Errorf("prepare batch for thoughts: %w", err)
	}
	for _, th := range thoughts {
		if err := batch.Append(
			th.ProjectID, th.RunID, th.TraceID, th.SpanID,
			th.AgentID, th.AgentInstanceID,
			th.Stage, th.Summary, th.Content, th.Confidence, th.ContentCaptured,
			th.StartTime, th.DurationMs, th.IngestedAt,
		); err != nil {
			return fmt.Errorf("append thought to batch: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send thought batch: %w", err)
	}
	return nil
}

// ListThoughtsByRun retrieves thought records for a given run, ordered by start_time.
func (s *Store) ListThoughtsByRun(ctx context.Context, runID uuid.UUID) ([]TraceThought, error) {
	rows, err := s.conn.Query(ctx,
		`SELECT
			project_id, run_id, trace_id, span_id,
			agent_id, agent_instance_id,
			stage, summary, content, confidence, content_captured,
			start_time, duration_ms, ingested_at
		FROM trace_thoughts
		WHERE run_id = $1
		ORDER BY start_time ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("query thoughts by run: %w", err)
	}
	defer rows.Close()

	var thoughts []TraceThought
	for rows.Next() {
		var th TraceThought
		if err := rows.ScanStruct(&th); err != nil {
			return nil, fmt.Errorf("scan thought: %w", err)
		}
		thoughts = append(thoughts, th)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("thought rows iteration: %w", err)
	}
	return thoughts, nil
}

// ---- Final Answer Records ----

// InsertFinalAnswers inserts a batch of structured final answer records into ClickHouse.
func (s *Store) InsertFinalAnswers(ctx context.Context, answers []TraceFinalAnswer) error {
	if len(answers) == 0 {
		return nil
	}
	batch, err := s.conn.PrepareBatch(ctx, "INSERT INTO trace_final_answers")
	if err != nil {
		return fmt.Errorf("prepare batch for final answers: %w", err)
	}
	for _, fa := range answers {
		if err := batch.Append(
			fa.ProjectID, fa.RunID, fa.TraceID, fa.SpanID,
			fa.AnswerSummary, fa.AnswerOutput, fa.StructuredOutput,
			fa.StartTime, fa.DurationMs, fa.IngestedAt,
		); err != nil {
			return fmt.Errorf("append final answer to batch: %w", err)
		}
	}
	if err := batch.Send(); err != nil {
		return fmt.Errorf("send final answer batch: %w", err)
	}
	return nil
}

// ListFinalAnswersByRun retrieves final answer records for a given run, ordered by start_time.
func (s *Store) ListFinalAnswersByRun(ctx context.Context, runID uuid.UUID) ([]TraceFinalAnswer, error) {
	rows, err := s.conn.Query(ctx,
		`SELECT
			project_id, run_id, trace_id, span_id,
			answer_summary, answer_output, structured_output,
			start_time, duration_ms, ingested_at
		FROM trace_final_answers
		WHERE run_id = $1
		ORDER BY start_time ASC`, runID)
	if err != nil {
		return nil, fmt.Errorf("query final answers by run: %w", err)
	}
	defer rows.Close()

	var answers []TraceFinalAnswer
	for rows.Next() {
		var fa TraceFinalAnswer
		if err := rows.ScanStruct(&fa); err != nil {
			return nil, fmt.Errorf("scan final answer: %w", err)
		}
		answers = append(answers, fa)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("final answers rows iteration: %w", err)
	}
	return answers, nil
}
