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

// Store provides access to ClickHouse trace storage.
type Store struct {
	conn driver.Conn
}

// Open creates a connection to ClickHouse from a DSN and verifies connectivity.
// DSN format: clickhouse://host:port/database?param=value
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
func (s *Store) InsertSpans(ctx context.Context, spans []TraceSpan) error {
	if len(spans) == 0 {
		return nil
	}
	batch, err := s.conn.PrepareBatch(ctx, "INSERT INTO agentscope_traces.trace_spans")
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
	batch, err := s.conn.PrepareBatch(ctx, "INSERT INTO agentscope_traces.trace_span_events")
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
		FROM agentscope_traces.trace_spans
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
		FROM agentscope_traces.trace_span_events
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
