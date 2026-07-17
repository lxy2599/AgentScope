package adapter

import (
	"context"
	"time"

	"AgentScope/internal/runs"
	"AgentScope/internal/store"
	"AgentScope/internal/tracestore"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// RunQuerier implements runs.Querier using store.Queries.
type RunQuerier struct {
	q *store.Queries
}

// compile-time check
var _ runs.Querier = (*RunQuerier)(nil)

// NewRunQuerier creates a new RunQuerier.
func NewRunQuerier(q *store.Queries) *RunQuerier {
	return &RunQuerier{q: q}
}

func (r *RunQuerier) CreateRun(ctx context.Context, projectID, source, status, traceID, agentName, agentVersion string, metadata []byte) (runs.Run, error) {
	storeRun, err := r.q.CreateRun(ctx, store.CreateRunParams{
		ProjectID:    parsePGUUID(projectID),
		Source:       source,
		Status:       status,
		TraceID:      pgText(traceID),
		AgentName:    agentName,
		AgentVersion: pgText(agentVersion),
		Metadata:     metadata,
	})
	if err != nil {
		return runs.Run{}, err
	}
	return storeRunToDomain(storeRun), nil
}

func (r *RunQuerier) GetRunByID(ctx context.Context, runID string) (runs.Run, error) {
	storeRun, err := r.q.GetRunByID(ctx, parsePGUUID(runID))
	if err != nil {
		return runs.Run{}, err
	}
	return storeRunToDomain(storeRun), nil
}

func (r *RunQuerier) GetRunByTraceID(ctx context.Context, traceID, projectID string) (runs.Run, error) {
	storeRun, err := r.q.GetRunByTraceID(ctx, store.GetRunByTraceIDParams{
		TraceID:   pgText(traceID),
		ProjectID: parsePGUUID(projectID),
	})
	if err != nil {
		return runs.Run{}, err
	}
	return storeRunToDomain(storeRun), nil
}

func (r *RunQuerier) ListRunsByProject(ctx context.Context, projectID string) ([]runs.Run, error) {
	storeRuns, err := r.q.ListRunsByProject(ctx, parsePGUUID(projectID))
	if err != nil {
		return nil, err
	}
	result := make([]runs.Run, 0, len(storeRuns))
	for _, sr := range storeRuns {
		result = append(result, storeRunToDomain(sr))
	}
	return result, nil
}

func (r *RunQuerier) UpdateRun(ctx context.Context, runID, agentName, agentVersion, status string, endedAt *time.Time, latencyMs *int64) (runs.Run, error) {
	var endedAtPg pgtype.Timestamptz
	if endedAt != nil {
		endedAtPg = pgTimestamptz(*endedAt)
	}
	var latencyMsPg pgtype.Int8
	if latencyMs != nil {
		latencyMsPg = pgtype.Int8{Int64: *latencyMs, Valid: true}
	}
	storeRun, err := r.q.UpdateRun(ctx, store.UpdateRunParams{
		ID:           parsePGUUID(runID),
		AgentName:    agentName,
		AgentVersion: pgText(agentVersion),
		Status:       status,
		EndedAt:      endedAtPg,
		LatencyMs:    latencyMsPg,
	})
	if err != nil {
		return runs.Run{}, err
	}
	return storeRunToDomain(storeRun), nil
}

func storeRunToDomain(sr store.Run) runs.Run {
	return runs.Run{
		ID:            uuidToString(sr.ID.Bytes),
		ProjectID:     uuidToString(sr.ProjectID.Bytes),
		Source:        sr.Source,
		Status:        sr.Status,
		TraceID:       strOrZero(sr.TraceID),
		AgentName:     sr.AgentName,
		AgentVersion:  strOrZero(sr.AgentVersion),
		InputSummary:  strOrZero(sr.InputSummary),
		OutputSummary: strOrZero(sr.OutputSummary),
		Error:         strOrZero(sr.Error),
		StartedAt:     pgTimestamptzPtr(sr.StartedAt),
		EndedAt:       pgTimestamptzPtr(sr.EndedAt),
		LatencyMs:     pgInt8Ptr(sr.LatencyMs),
		CreatedAt:     sr.CreatedAt.Time,
		UpdatedAt:     sr.UpdatedAt.Time,
	}
}

// RunTracestore implements runs.Tracestore using tracestore.Store.
type RunTracestore struct {
	ts *tracestore.Store
}

// compile-time check
var _ runs.Tracestore = (*RunTracestore)(nil)

// NewRunTracestore creates a new RunTracestore.
func NewRunTracestore(ts *tracestore.Store) *RunTracestore {
	return &RunTracestore{ts: ts}
}

func (r *RunTracestore) InsertSpans(ctx context.Context, spans []runs.SpanRecord) error {
	ts := make([]tracestore.TraceSpan, 0, len(spans))
	for _, sp := range spans {
		ts = append(ts, runSpanToTraceSpan(sp))
	}
	return r.ts.InsertSpans(ctx, ts)
}

func (r *RunTracestore) InsertSpanEvents(ctx context.Context, events []runs.SpanEventRecord) error {
	ts := make([]tracestore.TraceSpanEvent, 0, len(events))
	for _, ev := range events {
		ts = append(ts, runEventToTraceSpanEvent(ev))
	}
	return r.ts.InsertSpanEvents(ctx, ts)
}

func (r *RunTracestore) ListSpansByRun(ctx context.Context, runID string) ([]runs.SpanRecord, error) {
	id := uuid.MustParse(runID)
	spans, err := r.ts.ListSpansByRun(ctx, id)
	if err != nil {
		return nil, err
	}
	result := make([]runs.SpanRecord, 0, len(spans))
	for _, s := range spans {
		result = append(result, traceSpanToRunSpan(s))
	}
	return result, nil
}

func (r *RunTracestore) ListSpanEventsByRun(ctx context.Context, runID string) ([]runs.SpanEventRecord, error) {
	id := uuid.MustParse(runID)
	events, err := r.ts.ListSpanEventsByRun(ctx, id)
	if err != nil {
		return nil, err
	}
	result := make([]runs.SpanEventRecord, 0, len(events))
	for _, e := range events {
		result = append(result, traceSpanEventToRunEvent(e))
	}
	return result, nil
}

func runSpanToTraceSpan(sp runs.SpanRecord) tracestore.TraceSpan {
	var parentSpanID *string
	if sp.ParentSpanID != "" {
		parentSpanID = &sp.ParentSpanID
	}
	var endTime *time.Time
	if sp.EndTime != nil {
		endTime = sp.EndTime
	}
	var statusMsg *string
	if sp.StatusMsg != "" {
		statusMsg = &sp.StatusMsg
	}
	return tracestore.TraceSpan{
		ProjectID:     goUUID(sp.ProjectID),
		RunID:         goUUID(sp.RunID),
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
	}
}

func runEventToTraceSpanEvent(ev runs.SpanEventRecord) tracestore.TraceSpanEvent {
	return tracestore.TraceSpanEvent{
		ProjectID:  goUUID(ev.ProjectID),
		RunID:      goUUID(ev.RunID),
		TraceID:    ev.TraceID,
		SpanID:     ev.SpanID,
		EventName:  ev.EventName,
		EventTime:  ev.EventTime,
		Attributes: ev.Attributes,
		Truncated:  ev.Truncated,
		IngestedAt: time.Now(),
	}
}

func traceSpanToRunSpan(s tracestore.TraceSpan) runs.SpanRecord {
	var parentSpanID string
	if s.ParentSpanID != nil {
		parentSpanID = *s.ParentSpanID
	}
	var statusMsg string
	if s.StatusMessage != nil {
		statusMsg = *s.StatusMessage
	}
	return runs.SpanRecord{
		ProjectID:    s.ProjectID.String(),
		RunID:        s.RunID.String(),
		SpanID:       s.SpanID,
		TraceID:      s.TraceID,
		ParentSpanID: parentSpanID,
		Name:         s.Name,
		Kind:         s.Kind,
		StatusCode:   s.StatusCode,
		StatusMsg:    statusMsg,
		StartTime:    s.StartTime,
		EndTime:      s.EndTime,
		DurationMs:   s.DurationMs,
		Attributes:   s.Attributes,
		Truncated:    s.Truncated,
	}
}

func traceSpanEventToRunEvent(e tracestore.TraceSpanEvent) runs.SpanEventRecord {
	return runs.SpanEventRecord{
		ProjectID:  e.ProjectID.String(),
		RunID:      e.RunID.String(),
		TraceID:    e.TraceID,
		SpanID:     e.SpanID,
		EventName:  e.EventName,
		EventTime:  e.EventTime,
		Attributes: e.Attributes,
		Truncated:  e.Truncated,
	}
}
