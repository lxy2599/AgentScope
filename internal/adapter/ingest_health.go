package adapter

import (
	"context"
	"time"

	"AgentScope/internal/ingest"
	"AgentScope/internal/tracestore"
)

// IngestHealthRecorder adapts the ClickHouse tracestore to ingest.HealthRecorder.
type IngestHealthRecorder struct {
	store *tracestore.Store
}

var _ ingest.HealthRecorder = (*IngestHealthRecorder)(nil)
var _ ingest.HealthBatchSink = (*IngestHealthRecorder)(nil)

func NewIngestHealthRecorder(store *tracestore.Store) *IngestHealthRecorder {
	return &IngestHealthRecorder{store: store}
}

func (r *IngestHealthRecorder) RecordHealthEvent(ctx context.Context, event ingest.HealthEvent) error {
	return r.RecordHealthEvents(ctx, []ingest.HealthEvent{event})
}

func (r *IngestHealthRecorder) RecordHealthEvents(ctx context.Context, events []ingest.HealthEvent) error {
	records := make([]tracestore.IngestHealthEvent, 0, len(events))
	for _, event := range events {
		records = append(records, tracestore.IngestHealthEvent{
			ProjectID:          goUUID(event.ProjectID),
			AgentName:          event.AgentName,
			Outcome:            string(event.Outcome),
			HTTPStatus:         int32(event.HTTPStatus),
			SpanCount:          uint64(event.SpanCount),
			EventCount:         uint64(event.EventCount),
			SemanticCount:      uint64(event.SemanticCount),
			MissingRunID:       event.MissingRunID,
			MissingTraceID:     event.MissingTraceID,
			ErrorCode:          event.ErrorCode,
			DurationMs:         event.Duration.Milliseconds(),
			SDKQueueDepth:      event.SDKQueueDepth,
			SDKQueueCapacity:   event.SDKQueueCapacity,
			SDKSpansDropped:    event.SDKSpansDropped,
			SDKExportRetries:   event.SDKExportRetries,
			SDKExportLatencyMs: event.SDKExportLatencyMs,
			EventTime:          event.OccurredAt.UTC(),
			IngestedAt:         time.Now().UTC(),
		})
	}
	return r.store.InsertIngestHealthEvents(ctx, records)
}
