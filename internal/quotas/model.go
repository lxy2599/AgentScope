package quotas

import (
	"errors"
	"time"
)

// Dimension identifies an independently enforced project quota.
type Dimension string

const (
	DimensionIngestRequests  Dimension = "ingest_requests"
	DimensionIngestSpans     Dimension = "ingest_spans"
	DimensionIngestBytes     Dimension = "ingest_bytes"
	DimensionSSEConnections  Dimension = "sse_connections"
	DimensionTokensDaily     Dimension = "tokens_daily"
	DimensionCostMicrosDaily Dimension = "cost_micros_daily"
)

const (
	EventAllowed  = "allowed"
	EventRejected = "rejected"
	EventDegraded = "degraded"
)

var ErrQuotaNotFound = errors.New("project quota not found")

// Quota is the versioned quota policy for one project.
type Quota struct {
	ProjectID               string    `json:"project_id"`
	IngestRequestsPerMinute int64     `json:"ingest_requests_per_minute"`
	IngestSpansPerMinute    int64     `json:"ingest_spans_per_minute"`
	IngestBytesPerMinute    int64     `json:"ingest_bytes_per_minute"`
	SSEConnections          int64     `json:"sse_connections"`
	TokensPerDay            int64     `json:"tokens_per_day"`
	CostMicrosPerDay        int64     `json:"cost_micros_per_day"`
	Version                 int64     `json:"version"`
	UpdatedAt               time.Time `json:"updated_at,omitempty"`
}

// DefaultQuota preserves prior deployments by setting permissive, finite
// limits instead of rejecting projects without an explicit policy.
func DefaultQuota(projectID string) Quota {
	return Quota{
		ProjectID:               projectID,
		IngestRequestsPerMinute: 6_000,
		IngestSpansPerMinute:    250_000,
		IngestBytesPerMinute:    1 << 30,
		SSEConnections:          100,
		TokensPerDay:            100_000_000,
		CostMicrosPerDay:        100_000_000_000,
		Version:                 0,
	}
}

func (q Quota) Validate() error {
	switch {
	case q.ProjectID == "":
		return errors.New("project ID is required")
	case q.IngestRequestsPerMinute <= 0:
		return errors.New("ingest requests per minute must be positive")
	case q.IngestSpansPerMinute <= 0:
		return errors.New("ingest spans per minute must be positive")
	case q.IngestBytesPerMinute <= 0:
		return errors.New("ingest bytes per minute must be positive")
	case q.SSEConnections <= 0:
		return errors.New("SSE connections must be positive")
	case q.TokensPerDay <= 0:
		return errors.New("tokens per day must be positive")
	case q.CostMicrosPerDay <= 0:
		return errors.New("cost micros per day must be positive")
	default:
		return nil
	}
}

// Decision describes the outcome of one quota operation.
type Decision struct {
	Allowed    bool          `json:"allowed"`
	Dimension  Dimension     `json:"dimension"`
	Limit      int64         `json:"limit"`
	Remaining  int64         `json:"remaining"`
	RetryAfter time.Duration `json:"-"`
	ResetAt    time.Time     `json:"reset_at,omitempty"`
	Degraded   bool          `json:"degraded"`
	Reason     string        `json:"reason,omitempty"`
}

// Event is an auditable rejection or degraded quota decision.
type Event struct {
	ID         string    `json:"id,omitempty"`
	ProjectID  string    `json:"project_id"`
	Dimension  Dimension `json:"dimension"`
	Requested  int64     `json:"requested"`
	Limit      int64     `json:"limit"`
	Remaining  int64     `json:"remaining"`
	Outcome    string    `json:"outcome"`
	FailMode   string    `json:"fail_mode"`
	Reason     string    `json:"reason"`
	OccurredAt time.Time `json:"occurred_at"`
}

// Status combines the configured policy, current Redis usage and recent
// enforcement events for the governance UI.
type Status struct {
	Quota   Quota               `json:"quota"`
	Usage   map[Dimension]int64 `json:"usage"`
	Events  []Event             `json:"events"`
	CanEdit bool                `json:"can_edit"`
}
