package ingest

import (
	"context"
)

// TracestoreWriter is the subset of tracestore operations needed for trace writing.
// This interface exists to decouple the ingestion handler from the concrete
// ClickHouse store, making it possible to swap in a Kafka/NATS writer later.
type TracestoreWriter interface {
	InsertSpans(ctx context.Context, spans []SpanRecord) error
	InsertSpanEvents(ctx context.Context, events []SpanEventRecord) error
	InsertLLMCalls(ctx context.Context, calls []LLMCallRecord) error
	InsertToolCalls(ctx context.Context, calls []ToolCallRecord) error
	InsertRetrievalCalls(ctx context.Context, calls []RetrievalCallRecord) error
	InsertSkillCalls(ctx context.Context, calls []SkillCallRecord) error
	InsertThoughts(ctx context.Context, thoughts []ThoughtRecord) error
	InsertFinalAnswers(ctx context.Context, answers []FinalAnswerRecord) error
}

// TraceBatchWriter writes a complete batch of trace data (spans + events + all
// structured call types) as a single logical unit. Implementations may write
// directly to ClickHouse, enqueue into a message broker, or batch for async flush.
//
// This is the primary abstraction for Phase 2.7 ingest pipeline evolution:
// new backends (Kafka, NATS, S3) implement this interface and are swapped in
// via Dependencies without touching the handler.
type TraceBatchWriter interface {
	// WriteBatch persists a complete batch of trace data. Implementations are
	// responsible for transactional semantics where possible (e.g. all-or-nothing
	// for a single request's spans).
	WriteBatch(ctx context.Context, batch *TraceBatch) error
	Close() error
}

// TraceBatch holds all records extracted from a single ingest request.
type TraceBatch struct {
	Spans        []SpanRecord
	Events       []SpanEventRecord
	LLMCalls     []LLMCallRecord
	ToolCalls    []ToolCallRecord
	Retrievals   []RetrievalCallRecord
	Skills       []SkillCallRecord
	Thoughts     []ThoughtRecord
	FinalAnswers []FinalAnswerRecord
}

// NewTraceBatch creates a TraceBatch from decoded span/event records and
// structured call records.
func NewTraceBatch(
	spans []SpanRecord,
	events []SpanEventRecord,
	llmCalls []LLMCallRecord,
	toolCalls []ToolCallRecord,
	retrievals []RetrievalCallRecord,
	skills []SkillCallRecord,
	thoughts []ThoughtRecord,
	finalAnswers []FinalAnswerRecord,
) *TraceBatch {
	return &TraceBatch{
		Spans:        spans,
		Events:       events,
		LLMCalls:     llmCalls,
		ToolCalls:    toolCalls,
		Retrievals:   retrievals,
		Skills:       skills,
		Thoughts:     thoughts,
		FinalAnswers: finalAnswers,
	}
}

// ClickHouseTraceWriter writes trace batches directly to ClickHouse via a
// TracestoreWriter. This is the default production writer.
type ClickHouseTraceWriter struct {
	store TracestoreWriter
}

// NewClickHouseTraceWriter creates a ClickHouseTraceWriter.
func NewClickHouseTraceWriter(store TracestoreWriter) *ClickHouseTraceWriter {
	return &ClickHouseTraceWriter{store: store}
}

// WriteBatch writes all records in the batch to ClickHouse.
func (w *ClickHouseTraceWriter) WriteBatch(ctx context.Context, batch *TraceBatch) error {
	if err := w.store.InsertSpans(ctx, batch.Spans); err != nil {
		return err
	}
	if err := w.store.InsertSpanEvents(ctx, batch.Events); err != nil {
		return err
	}
	if err := w.store.InsertLLMCalls(ctx, batch.LLMCalls); err != nil {
		return err
	}
	if err := w.store.InsertToolCalls(ctx, batch.ToolCalls); err != nil {
		return err
	}
	if err := w.store.InsertRetrievalCalls(ctx, batch.Retrievals); err != nil {
		return err
	}
	if err := w.store.InsertSkillCalls(ctx, batch.Skills); err != nil {
		return err
	}
	if err := w.store.InsertThoughts(ctx, batch.Thoughts); err != nil {
		return err
	}
	if err := w.store.InsertFinalAnswers(ctx, batch.FinalAnswers); err != nil {
		return err
	}
	return nil
}

// Close is a no-op for ClickHouseTraceWriter; connection lifecycle is managed
// by the Store.
func (w *ClickHouseTraceWriter) Close() error { return nil }
