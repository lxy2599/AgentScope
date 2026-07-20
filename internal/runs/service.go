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
	ID            string     `json:"id"`
	ProjectID     string     `json:"project_id"`
	Source        string     `json:"source"`
	Status        string     `json:"status"`
	TraceID       string     `json:"trace_id"`
	AgentName     string     `json:"agent_name"`
	AgentVersion  string     `json:"agent_version,omitempty"`
	InputSummary  string     `json:"input_summary,omitempty"`
	OutputSummary string     `json:"output_summary,omitempty"`
	Input         string     `json:"input,omitempty"`
	Output        string     `json:"output,omitempty"`
	Error         string     `json:"error,omitempty"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	EndedAt       *time.Time `json:"ended_at,omitempty"`
	LatencyMs     *int64     `json:"latency_ms,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// SpanRecord is a service-level span representation.
type SpanRecord struct {
	RunID        string     `json:"run_id"`
	SpanID       string     `json:"span_id"`
	TraceID      string     `json:"trace_id"`
	ProjectID    string     `json:"project_id"`
	ParentSpanID string     `json:"parent_span_id,omitempty"`
	Name         string     `json:"name"`
	Kind         string     `json:"kind,omitempty"`
	StatusCode   string     `json:"status_code,omitempty"`
	StatusMsg    string     `json:"status_message,omitempty"`
	StartTime    time.Time  `json:"start_time"`
	EndTime      *time.Time `json:"end_time,omitempty"`
	DurationMs   *int64     `json:"duration_ms,omitempty"`
	Attributes   string     `json:"attributes,omitempty"`
	Truncated    bool       `json:"truncated"`
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

// LLMCallRecord is a structured LLM call for API responses.
type LLMCallRecord struct {
	ProjectID       string    `json:"project_id"`
	RunID           string    `json:"run_id"`
	TraceID         string    `json:"trace_id"`
	SpanID          string    `json:"span_id"`
	AgentID         string    `json:"agent_id,omitempty"`
	AgentInstanceID string    `json:"agent_instance_id,omitempty"`
	Provider        string    `json:"provider"`
	Model           string    `json:"model"`
	InputTokens     *int64    `json:"input_tokens,omitempty"`
	OutputTokens    *int64    `json:"output_tokens,omitempty"`
	TotalTokens     *int64    `json:"total_tokens,omitempty"`
	Cost            *float64  `json:"cost,omitempty"`
	InputSummary    string    `json:"input_summary,omitempty"`
	OutputSummary   string    `json:"output_summary,omitempty"`
	ContentCaptured bool      `json:"content_captured"`
	Status          string    `json:"status"`
	Error           string    `json:"error,omitempty"`
	StartTime       time.Time `json:"start_time"`
	DurationMs      *int64    `json:"duration_ms,omitempty"`
}

// ToolCallRecord is a structured tool call for API responses.
type ToolCallRecord struct {
	ProjectID        string    `json:"project_id"`
	RunID            string    `json:"run_id"`
	TraceID          string    `json:"trace_id"`
	SpanID           string    `json:"span_id"`
	AgentID          string    `json:"agent_id,omitempty"`
	AgentInstanceID  string    `json:"agent_instance_id,omitempty"`
	ToolName         string    `json:"tool_name"`
	ToolType         string    `json:"tool_type,omitempty"`
	ArgumentsSummary string    `json:"arguments_summary,omitempty"`
	ResultSummary    string    `json:"result_summary,omitempty"`
	Success          bool      `json:"success"`
	Error            string    `json:"error,omitempty"`
	RetryCount       int64     `json:"retry_count"`
	SideEffectRisk   string    `json:"side_effect_risk,omitempty"`
	StartTime        time.Time `json:"start_time"`
	DurationMs       *int64    `json:"duration_ms,omitempty"`
}

// RetrievalCallRecord is a structured retrieval call for API responses.
type RetrievalCallRecord struct {
	ProjectID       string    `json:"project_id"`
	RunID           string    `json:"run_id"`
	TraceID         string    `json:"trace_id"`
	SpanID          string    `json:"span_id"`
	AgentID         string    `json:"agent_id,omitempty"`
	AgentInstanceID string    `json:"agent_instance_id,omitempty"`
	KnowledgeBase   string    `json:"knowledge_base"`
	QueryText       string    `json:"query_text"`
	TopK            *int64    `json:"top_k,omitempty"`
	ReturnedCount   *int64    `json:"returned_count,omitempty"`
	DocumentIDs     []string  `json:"document_ids,omitempty"`
	Scores          []float64 `json:"scores,omitempty"`
	StartTime       time.Time `json:"start_time"`
	DurationMs      *int64    `json:"duration_ms,omitempty"`
}

// SkillCallRecord is a structured skill call for API responses.
type SkillCallRecord struct {
	ProjectID       string    `json:"project_id"`
	RunID           string    `json:"run_id"`
	TraceID         string    `json:"trace_id"`
	SpanID          string    `json:"span_id"`
	AgentID         string    `json:"agent_id,omitempty"`
	AgentInstanceID string    `json:"agent_instance_id,omitempty"`
	SkillName       string    `json:"skill_name"`
	SkillVersion    string    `json:"skill_version,omitempty"`
	InputSummary    string    `json:"input_summary,omitempty"`
	OutputSummary   string    `json:"output_summary,omitempty"`
	Success         bool      `json:"success"`
	Error           string    `json:"error,omitempty"`
	StartTime       time.Time `json:"start_time"`
	DurationMs      *int64    `json:"duration_ms,omitempty"`
}

// RunDetail combines run with its spans, events, and structured call records.
type RunDetail struct {
	Run            Run                   `json:"run"`
	Spans          []SpanRecord          `json:"spans"`
	Events         []SpanEventRecord     `json:"events"`
	LLMCalls       []LLMCallRecord       `json:"llm_calls,omitempty"`
	ToolCalls      []ToolCallRecord      `json:"tool_calls,omitempty"`
	RetrievalCalls []RetrievalCallRecord `json:"retrieval_calls,omitempty"`
	SkillCalls     []SkillCallRecord     `json:"skill_calls,omitempty"`
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
	UpdateRunTraceID(ctx context.Context, runID, traceID string) (Run, error)
	FailRunnerRun(ctx context.Context, runID, projectID, errorStr string) (Run, error)
}

// Tracestore defines the ClickHouse trace storage operations the runs service needs.
type Tracestore interface {
	InsertSpans(ctx context.Context, spans []SpanRecord) error
	InsertSpanEvents(ctx context.Context, events []SpanEventRecord) error
	ListSpansByRun(ctx context.Context, runID string) ([]SpanRecord, error)
	ListSpanEventsByRun(ctx context.Context, runID string) ([]SpanEventRecord, error)
	ListLLMCallsByRun(ctx context.Context, runID string) ([]LLMCallRecord, error)
	ListToolCallsByRun(ctx context.Context, runID string) ([]ToolCallRecord, error)
	ListRetrievalCallsByRun(ctx context.Context, runID string) ([]RetrievalCallRecord, error)
	ListSkillCallsByRun(ctx context.Context, runID string) ([]SkillCallRecord, error)
}

// ---- Service ----

// Service provides run operations, connecting PostgreSQL run tracking with
// ClickHouse trace storage.
type Service struct {
	q  Querier
	ts Tracestore
}

// NewService creates a new runs Service.
func NewService(q Querier, ts Tracestore) *Service {
	return &Service{q: q, ts: ts}
}

// CreateOrGetLiveRunFromTrace creates a new live run or returns the existing one
// for the given trace. The attrs map can contain agentscope.run.id,
// agentscope.run.name, agentscope.agent.name, and agentscope.agent.version.
//
// Resolution order:
//  1. If attrs contains agentscope.run.id, look up that run by its PostgreSQL ID.
//     This handles the case where the SDK runtime has already created a run before
//     OTLP spans arrive — the ingestion handler finds and reuses the existing run
//     instead of creating a duplicate.
//  2. If a run already exists with the same trace_id + project_id, return it.
//  3. Otherwise, create a new run.
func (s *Service) CreateOrGetLiveRunFromTrace(ctx context.Context, projectID, traceID string, attrs map[string]string) (Run, error) {
	if projectID == "" || traceID == "" {
		return Run{}, errors.New("projectID and traceID are required")
	}

	// 1. If agentscope.run.id is specified, use that existing run directly.
	// This prevents duplicate runs when the SDK runtime already created one.
	if runID := attrs["agentscope.run.id"]; runID != "" {
		existing, err := s.q.GetRunByID(ctx, runID)
		if err == nil && existing.ProjectID == projectID {
			// Update the run's trace_id to the actual OTLP trace ID
			// so users can navigate from run to trace and back.
			// If the update fails, propagate the error — do NOT silently
			// return the run with a stale/wrong trace_id.
			if existing.TraceID != traceID {
				updated, updErr := s.q.UpdateRunTraceID(ctx, runID, traceID)
				if updErr != nil {
					return Run{}, fmt.Errorf("update trace_id for run %s: %w", runID, updErr)
				}
				return updated, nil
			}
			return existing, nil
		}
	}

	// 2. Try to find existing run by trace ID
	existing, err := s.q.GetRunByTraceID(ctx, traceID, projectID)
	if err == nil {
		return existing, nil
	}

	agentName := attrs["agentscope.agent.name"]
	if agentName == "" {
		agentName = "unknown"
	}
	agentVersion := attrs["agentscope.agent.version"]

	run, err := s.q.CreateRun(ctx, projectID, "live", "running", traceID, agentName, agentVersion, []byte("{}"))
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

// GetRun returns a run by ID without loading ClickHouse trace data.
func (s *Service) GetRun(ctx context.Context, runID string) (Run, error) {
	if runID == "" {
		return Run{}, errors.New("runID is required")
	}
	return s.q.GetRunByID(ctx, runID)
}

// GetRunDetail returns a run with its spans, events, and structured call records.
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

	llmCalls, err := s.ts.ListLLMCallsByRun(ctx, runID)
	if err != nil {
		return RunDetail{}, fmt.Errorf("list llm calls: %w", err)
	}

	toolCalls, err := s.ts.ListToolCallsByRun(ctx, runID)
	if err != nil {
		return RunDetail{}, fmt.Errorf("list tool calls: %w", err)
	}

	retrievalCalls, err := s.ts.ListRetrievalCallsByRun(ctx, runID)
	if err != nil {
		return RunDetail{}, fmt.Errorf("list retrieval calls: %w", err)
	}

	skillCalls, err := s.ts.ListSkillCallsByRun(ctx, runID)
	if err != nil {
		return RunDetail{}, fmt.Errorf("list skill calls: %w", err)
	}

	return RunDetail{
		Run:            run,
		Spans:          spans,
		Events:         events,
		LLMCalls:       llmCalls,
		ToolCalls:      toolCalls,
		RetrievalCalls: retrievalCalls,
		SkillCalls:     skillCalls,
	}, nil
}

// FailRunByRunner fails a run by its PostgreSQL ID, verifying it belongs to the project.
// Used by the runner API (runner:write scope) to close runs when target execution fails.
// Uses the dedicated FailRunnerRun query that only touches status/error/ended_at/updated_at,
// preserving agent_name, agent_version, trace_id, input, and output.
func (s *Service) FailRunByRunner(ctx context.Context, runID, projectID, errorStr string) error {
	if runID == "" || projectID == "" {
		return errors.New("runID and projectID are required")
	}
	run, err := s.q.GetRunByID(ctx, runID)
	if err != nil {
		return fmt.Errorf("get run: %w", err)
	}
	if run.ProjectID != projectID {
		return errors.New("run not found in project")
	}
	if run.Status == "failed" || run.Status == "completed" {
		return nil // idempotent: already terminal
	}
	updated, err := s.q.FailRunnerRun(ctx, runID, projectID, errorStr)
	if err != nil {
		return fmt.Errorf("fail run: %w", err)
	}
	if updated.Error != errorStr {
		return fmt.Errorf("fail run: error column mismatch (got %q, want %q)", updated.Error, errorStr)
	}
	return nil
}

