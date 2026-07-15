package adapter

import (
	"context"
	"time"

	"AgentScope/internal/ingest"
	"AgentScope/internal/tracestore"
)

// HTTPTracestore implements httpapi.TracestoreInterface using tracestore.Store.
// It adds ProjectID, RunID (parsed to uuid.UUID) and IngestedAt timestamp.
type HTTPTracestore struct {
	ts *tracestore.Store
}

// compile-time check
var _ interface {
	InsertSpans(ctx context.Context, spans []ingest.SpanRecord) error
	InsertSpanEvents(ctx context.Context, events []ingest.SpanEventRecord) error
} = (*HTTPTracestore)(nil)

// NewHTTPTracestore creates a new HTTPTracestore.
func NewHTTPTracestore(ts *tracestore.Store) *HTTPTracestore {
	return &HTTPTracestore{ts: ts}
}

func (h *HTTPTracestore) InsertSpans(ctx context.Context, spans []ingest.SpanRecord) error {
	ts := make([]tracestore.TraceSpan, 0, len(spans))
	for _, sp := range spans {
		projectID := goUUID(sp.ProjectID)
		runID := goUUID(sp.RunID)

		var parentSpanID *string
		if sp.ParentSpanID != "" {
			parentSpanID = &sp.ParentSpanID
		}
		var endTime *time.Time
		if sp.EndTime != nil {
			endTime = sp.EndTime
		}
		var statusMsg *string
		if sp.StatusMessage != "" {
			statusMsg = &sp.StatusMessage
		}

		ts = append(ts, tracestore.TraceSpan{
			ProjectID:     projectID,
			RunID:         runID,
			TraceID:       sp.TraceID,
			SpanID:        sp.SpanID,
			ParentSpanID:  parentSpanID,
			Name:          sp.Name,
			Kind:          sp.Kind,
			StatusCode:    sp.StatusCode,
			StatusMessage: statusMsg,
			StartTime:     sp.StartTime,
			EndTime:       endTime,
			DurationMs:    sp.DurationMs,
			Attributes:    sp.Attributes,
			Truncated:     sp.Truncated,
			IngestedAt:    time.Now(),
		})
	}
	return h.ts.InsertSpans(ctx, ts)
}

func (h *HTTPTracestore) InsertSpanEvents(ctx context.Context, events []ingest.SpanEventRecord) error {
	ts := make([]tracestore.TraceSpanEvent, 0, len(events))
	for _, ev := range events {
		ts = append(ts, tracestore.TraceSpanEvent{
			ProjectID:  goUUID(ev.ProjectID),
			RunID:      goUUID(ev.RunID),
			TraceID:    ev.TraceID,
			SpanID:     ev.SpanID,
			EventName:  ev.EventName,
			EventTime:  ev.EventTime,
			Attributes: ev.Attributes,
			Truncated:  ev.Truncated,
			IngestedAt: time.Now(),
		})
	}
	return h.ts.InsertSpanEvents(ctx, ts)
}
