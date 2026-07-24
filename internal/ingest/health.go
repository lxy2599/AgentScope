package ingest

import (
	"context"
	"time"
)

// HealthOutcome describes the terminal result of one authenticated ingest request.
type HealthOutcome string

const (
	HealthAccepted       HealthOutcome = "accepted"
	HealthRateLimited    HealthOutcome = "rate_limited"
	HealthRejectedBody   HealthOutcome = "rejected_body"
	HealthRejectedDecode HealthOutcome = "rejected_decode"
	HealthStorageFailed  HealthOutcome = "storage_failed"
)

// HealthEvent contains operational metadata only. It must never contain trace content.
type HealthEvent struct {
	ProjectID      string
	AgentName      string
	Outcome        HealthOutcome
	HTTPStatus     int
	SpanCount      int
	EventCount     int
	SemanticCount  int
	MissingRunID   bool
	MissingTraceID bool
	ErrorCode      string
	Duration       time.Duration
	SDKQueueDepth      *int64
	SDKQueueCapacity   *int64
	SDKSpansDropped    *int64
	SDKExportRetries   *int64
	SDKExportLatencyMs *float64
	OccurredAt     time.Time
}

// HealthRecorder persists best-effort ingest health observations.
// Recording failures must not change the ingest request result.
type HealthRecorder interface {
	RecordHealthEvent(ctx context.Context, event HealthEvent) error
}
