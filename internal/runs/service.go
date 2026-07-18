package runs

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ---- Domain types ----

// Run is the service-level run representation.
type Run struct {
	ID           string    `json:"id"`
	ProjectID    string    `json:"project_id"`
	Source       string    `json:"source"`
	Status       string    `json:"status"`
	TraceID      string    `json:"trace_id"`
	AgentName    string    `json:"agent_name"`
	AgentVersion string    `json:"agent_version,omitempty"`
	InputSummary string    `json:"input_summary,omitempty"`
	OutputSummary string   `json:"output_summary,omitempty"`
	Error        string    `json:"error,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
	LatencyMs    *int64     `json:"latency_ms,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// SpanRecord is a service-level span representation.
type SpanRecord struct {
	RunID        string `json:"run_id"`
	SpanID       string `json:"span_id"`
	TraceID      string `json:"trace_id"`
	ProjectID    string `json:"project_id"`
	ParentSpanID string `json:"parent_span_id,omitempty"`
	Name         string `json:"name"`
	Kind         string `json:"kind,omitempty"`
	StatusCode   string `json:"status_code,omitempty"`
	StatusMsg    string `json:"status_message,omitempty"`
	StartTime    time.Time `json:"start_time"`
	EndTime      *time.Time `json:"end_time,omitempty"`
	DurationMs   *int64    `json:"duration_ms,omitempty"`
	Attributes   string    `json:"attributes,omitempty"`
	Truncated    bool      `json:"truncated"`
}

// SpanEventRecord is a service-level span event representation.
type SpanEventRecord struct {
	ProjectID  string    `json:"project_id"`
	RunID      string    `json:"run_id"`
	TraceID    string    `json:"trace_id"`
	SpanID     string    `json:"span_id"`
	EventName  string    `json:"event_name"`
	EventTime  time.Time `json:"event_time"`
	Attributes string    `json:"attributes,omitempty"`
	Truncated  bool      `json:"truncated"`
}

// RunSummary is a lightweight run representation for listing.
type RunSummary struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Source    string    `json:"source"`
	Status    string    `json:"status"`
	TraceID   string    `json:"trace_id"`
	AgentName string    `json:"agent_name"`
	CreatedAt time.Time `json:"created_at"`
}

// RunDetail combines run with its spans and span events.
type RunDetail struct {
	Run    Run           `json:"run"`
	Spans  []SpanRecord  `json:"spans"`
	Events []SpanEventRecord `json:"events"`
}

// ---- Interfaces ----

// Querier defines the PostgreSQL database operations the runs service needs.
type Querier interface {
	CreateRun(ctx context.Context, projectID, source, status, traceID, agentName, agentVersion string, metadata []byte) (Run, error)
	GetRunByID(ctx context.Context, runID string) (Run, error)
	GetRunByTraceID(ctx context.Context, traceID, projectID string) (Run, error)
	ListRunsByProject(ctx context.Context, projectID string) ([]Run, error)
	UpdateRun(ctx context.Context, runID, agentName, agentVersion, status string, endedAt *time.Time, latencyMs *int64) (Run, error)
	UpdateRunAgentInfo(ctx context.Context, runID, agentName, agentVersion string) (Run, error)
}

// Tracestore defines the ClickHouse trace storage operations the runs service needs.
type Tracestore interface {
	InsertSpans(ctx context.Context, spans []SpanRecord) error
	InsertSpanEvents(ctx context.Context, events []SpanEventRecord) error
	ListSpansByRun(ctx context.Context, runID string) ([]SpanRecord, error)
	ListSpanEventsByRun(ctx context.Context, runID string) ([]SpanEventRecord, error)
}

// ---- Service ----

// Service provides run operations, connecting PostgreSQL run tracking with
// ClickHouse trace storage.
type Service struct {
	q Querier
	ts Tracestore
}

// NewService creates a new runs Service.
func NewService(q Querier, ts Tracestore) *Service {
	return &Service{q: q, ts: ts}
}

// CreateOrGetLiveRunFromTrace creates a new live run or returns the existing one
// for the given trace. The attrs map can contain agentscope.run.id,
// agentscope.run.name, agentscope.agent.name, and agentscope.agent.version.
func (s *Service) CreateOrGetLiveRunFromTrace(ctx context.Context, projectID, traceID string, attrs map[string]string) (Run, error) {
	if projectID == "" || traceID == "" {
		return Run{}, errors.New("projectID and traceID are required")
	}

	// Try to find existing run by trace ID
	existing, err := s.q.GetRunByTraceID(ctx, traceID, projectID)
	if err == nil {
		return existing, nil
	}

	agentName := attrs["agentscope.agent.name"]
	if agentName == "" {
		agentName = "unknown"
	}
	agentVersion := attrs["agentscope.agent.version"]

	run, err := s.q.CreateRun(ctx, projectID, "live", "running", traceID, agentName, agentVersion, nil)
	if err != nil {
		return Run{}, fmt.Errorf("create run: %w", err)
	}

	return run, nil
}

// UpdateRunFromTrace updates an existing run's agent info when new trace data arrives.
// Only updates if the current agent_name is "unknown" and a real name is now available.
// Uses the dedicated UpdateRunAgentInfo query that does NOT touch status/ended_at/latency_ms.
func (s *Service) UpdateRunFromTrace(ctx context.Context, runID, agentName, agentVersion string) (Run, error) {
	if runID == "" {
		return Run{}, errors.New("runID is required")
	}

	run, err := s.q.GetRunByID(ctx, runID)
	if err != nil {
		return Run{}, fmt.Errorf("get run: %w", err)
	}

	// Only update if current agent_name is the default "unknown" and we have a real name
	if agentName == "" || run.AgentName != "unknown" {
		return run, nil
	}

	return s.q.UpdateRunAgentInfo(ctx, runID, agentName, agentVersion)
}

// ListProjectRuns returns a summary of runs for a project.
func (s *Service) ListProjectRuns(ctx context.Context, projectID string) ([]RunSummary, error) {
	if projectID == "" {
		return nil, errors.New("projectID is required")
	}

	runs, err := s.q.ListRunsByProject(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}

	summaries := make([]RunSummary, 0, len(runs))
	for _, r := range runs {
		summaries = append(summaries, RunSummary{
			ID:        r.ID,
			ProjectID: r.ProjectID,
			Source:    r.Source,
			Status:    r.Status,
			TraceID:   r.TraceID,
			AgentName: r.AgentName,
			CreatedAt: r.CreatedAt,
		})
	}
	return summaries, nil
}

// GetRunByTraceID looks up a run by its OTLP trace ID and project.
// Returns an error if no matching run is found.
func (s *Service) GetRunByTraceID(ctx context.Context, traceID, projectID string) (Run, error) {
	if traceID == "" || projectID == "" {
		return Run{}, errors.New("traceID and projectID are required")
	}
	return s.q.GetRunByTraceID(ctx, traceID, projectID)
}

// GetRunDetail returns a run with its spans and span events from ClickHouse.
func (s *Service) GetRunDetail(ctx context.Context, runID string) (RunDetail, error) {
	if runID == "" {
		return RunDetail{}, errors.New("runID is required")
	}

	run, err := s.q.GetRunByID(ctx, runID)
	if err != nil {
		return RunDetail{}, fmt.Errorf("get run: %w", err)
	}

	spans, err := s.ts.ListSpansByRun(ctx, runID)
	if err != nil {
		return RunDetail{}, fmt.Errorf("list spans: %w", err)
	}

	events, err := s.ts.ListSpanEventsByRun(ctx, runID)
	if err != nil {
		return RunDetail{}, fmt.Errorf("list span events: %w", err)
	}

	return RunDetail{
		Run:    run,
		Spans:  spans,
		Events: events,
	}, nil
}
