package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ---- Run status constants ----

// RunStatus represents a run's current lifecycle stage.
type RunStatus string

const (
	RunStatusQueued           RunStatus = "queued"
	RunStatusRunning          RunStatus = "running"
	RunStatusWaitingLLM       RunStatus = "waiting_llm"
	RunStatusWaitingTool      RunStatus = "waiting_tool"
	RunStatusWaitingRetrieval RunStatus = "waiting_retrieval"
	RunStatusRetrying         RunStatus = "retrying"
	RunStatusCompleted        RunStatus = "completed"
	RunStatusFailed           RunStatus = "failed"
	RunStatusTimeout          RunStatus = "timeout"
	RunStatusCancelled        RunStatus = "cancelled"
)

// IsValidRunStatus checks if a status string is a valid run status.
func IsValidRunStatus(status string) bool {
	switch RunStatus(status) {
	case RunStatusQueued, RunStatusRunning, RunStatusWaitingLLM, RunStatusWaitingTool,
		RunStatusWaitingRetrieval, RunStatusRetrying, RunStatusCompleted,
		RunStatusFailed, RunStatusTimeout, RunStatusCancelled:
		return true
	default:
		return false
	}
}

// IsTerminalRunStatus returns true for completed/failed/timeout/cancelled.
func IsTerminalRunStatus(status string) bool {
	switch RunStatus(status) {
	case RunStatusCompleted, RunStatusFailed, RunStatusTimeout, RunStatusCancelled:
		return true
	default:
		return false
	}
}

// ActiveRunStatuses contains non-terminal statuses shown in Active Runs.
var ActiveRunStatuses = []string{
	string(RunStatusQueued),
	string(RunStatusRunning),
	string(RunStatusWaitingLLM),
	string(RunStatusWaitingTool),
	string(RunStatusWaitingRetrieval),
	string(RunStatusRetrying),
}

// ---- Domain types for runtime ----

// RuntimeRun is a run in the context of the Active Runtime service.
type RuntimeRun struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Status    string    `json:"status"`
	Input     string    `json:"input,omitempty"`
	Output    string    `json:"output,omitempty"`
	Error     string    `json:"error,omitempty"`
	TraceID   string    `json:"trace_id,omitempty"`
	LatencyMs *int64    `json:"latency_ms,omitempty"`
	AgentID   string    `json:"agent_id,omitempty"`
	AgentName string    `json:"agent_name,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// RunStateEvent represents a state transition event for a run.
type RunStateEvent struct {
	ID         string    `json:"id"`
	RunID      string    `json:"run_id"`
	FromStatus string    `json:"from_status,omitempty"`
	ToStatus   string    `json:"to_status"`
	Message    string    `json:"message,omitempty"`
	Attributes []byte    `json:"attributes,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// RuntimeRunDetail is the full runtime detail including state events.
type RuntimeRunDetail struct {
	RuntimeRun
	AgentInstanceID   string          `json:"agent_instance_id,omitempty"`
	AgentInstanceName string          `json:"agent_instance_name,omitempty"`
	Events            []RunStateEvent `json:"events"`
}

// ActiveRun is a lightweight active run summary for listing.
type ActiveRun struct {
	ID                string     `json:"id"`
	ProjectID         string     `json:"project_id"`
	Status            string     `json:"status"`
	InputSummary      string     `json:"input_summary,omitempty"`
	OutputSummary     string     `json:"output_summary,omitempty"`
	Error             string     `json:"error,omitempty"`
	TraceID           string     `json:"trace_id,omitempty"`
	LatencyMs         *int64     `json:"latency_ms,omitempty"`
	AgentID           string     `json:"agent_id,omitempty"`
	AgentName         string     `json:"agent_name,omitempty"`
	AgentInstanceID   string     `json:"agent_instance_id,omitempty"`
	AgentInstanceName string     `json:"agent_instance_name,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	LastStateEventAt  *time.Time `json:"last_state_event_at,omitempty"`
	LastState         string     `json:"last_state,omitempty"`
}

// ---- Runtime service input types ----

// StartRunInput is the input for starting a new runtime run.
// InstanceID is the external instance identifier (e.g. "inst-1"), not the DB UUID.
type StartRunInput struct {
	ProjectID  string
	AgentID    string
	InstanceID string
	Input      string
	Metadata   map[string]interface{}
	TraceID    string
}

// UpdateRunStateInput is the input for updating a run's state.
type UpdateRunStateInput struct {
	ProjectID  string
	RunID      string
	Status     string
	Message    string
	AgentID    string
	InstanceID string
	Attributes map[string]interface{}
	TraceID    string
}

// FinishRunInput is the input for finishing a run.
type FinishRunInput struct {
	ProjectID  string
	RunID      string
	Output     string
	AgentID    string
	InstanceID string
	TraceID    string
	LatencyMS  int64
}

// FailRunInput is the input for failing a run.
type FailRunInput struct {
	ProjectID  string
	RunID      string
	Error      string
	AgentID    string
	InstanceID string
	Attributes map[string]interface{}
	TraceID    string
	LatencyMS  int64
}

// ---- RuntimeQuerier ----

// RuntimeQuerier defines the database operations the runtime service needs.
type RuntimeQuerier interface {
	CreateRun(ctx context.Context, projectID, source, status, traceID, agentName, agentVersion string, metadata []byte) (Run, error)
	GetRunByID(ctx context.Context, runID string) (Run, error)
	UpdateRunStatus(ctx context.Context, runID, projectID, status, error string) (Run, error)
	StartRuntimeRun(ctx context.Context, projectID, status, traceID, agentName, inputSummary string, metadata []byte) (Run, error)
	CompleteRuntimeRun(ctx context.Context, runID, projectID, outputSummary string, latencyMs int64) (Run, error)
	FailRuntimeRun(ctx context.Context, runID, projectID, errorStr string, latencyMs int64) (Run, error)
	CreateRunStateEvent(ctx context.Context, projectID, runID string, agentID, agentInstanceDBID *string, fromStatus, toStatus, message string, attributes []byte) (RunStateEvent, error)
	ListRunStateEvents(ctx context.Context, runID string) ([]RunStateEvent, error)
	ListActiveRunsByProject(ctx context.Context, projectID string, limit, offset int) ([]ActiveRun, error)
	GetRunRuntimeDetail(ctx context.Context, runID, projectID string) (*RuntimeRunDetail, error)
	SetAgentInstanceActiveRun(ctx context.Context, projectID, agentID, instanceID, runID string) (string, error)
	ClearAgentInstanceActiveRun(ctx context.Context, projectID, agentID, instanceID string) (string, error)
	GetAgentInstanceDBID(ctx context.Context, projectID, agentID, instanceID string) (string, error)
	GetAgentNameByID(ctx context.Context, agentID string) (string, error)
}

// ---- RuntimeService ----

// RuntimeService provides online run lifecycle operations.
type RuntimeService struct {
	q RuntimeQuerier
}

// NewRuntimeService creates a new RuntimeService.
func NewRuntimeService(q RuntimeQuerier) *RuntimeService {
	return &RuntimeService{q: q}
}

// StartRun creates a new run and attaches it to an agent instance.
// Instance validation happens BEFORE run creation to prevent orphan runs.
func (s *RuntimeService) StartRun(ctx context.Context, input StartRunInput) (*RuntimeRun, error) {
	if input.ProjectID == "" {
		return nil, errors.New("projectID is required")
	}

	// Validate agent instance exists BEFORE creating the run
	var agentInstanceDBID string
	if input.InstanceID != "" && input.AgentID != "" {
		dbID, err := s.q.GetAgentInstanceDBID(ctx, input.ProjectID, input.AgentID, input.InstanceID)
		if err != nil {
			return nil, fmt.Errorf("agent instance not found: %w", err)
		}
		agentInstanceDBID = dbID
	}

	// Resolve agent name from registered agent if AgentID is provided
	agentName := "unknown"
	if input.AgentID != "" {
		if name, err := s.q.GetAgentNameByID(ctx, input.AgentID); err == nil && name != "" {
			agentName = name
		}
	}
	metaBytes := marshalAttributes(input.Metadata)

	run, err := s.q.StartRuntimeRun(ctx, input.ProjectID, string(RunStatusRunning), input.TraceID, agentName, input.Input, metaBytes)
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}

	// Attach to agent instance (already validated above)
	if agentInstanceDBID != "" {
		if _, setErr := s.q.SetAgentInstanceActiveRun(ctx, input.ProjectID, input.AgentID, input.InstanceID, run.ID); setErr != nil {
			return nil, fmt.Errorf("set agent instance active run: %w", setErr)
		}
	}

	// Record initial state event with resolved DB UUID
	_, err = s.q.CreateRunStateEvent(ctx, input.ProjectID, run.ID,
		nullableString(input.AgentID), nullableString(agentInstanceDBID),
		"", string(RunStatusRunning), "started", []byte("{}"))
	if err != nil {
		return nil, fmt.Errorf("create state event: %w", err)
	}

	return toRuntimeRun(run, input.AgentID, ""), nil
}

// UpdateRunState updates a run's status and records a state event.
func (s *RuntimeService) UpdateRunState(ctx context.Context, input UpdateRunStateInput) (*RuntimeRun, error) {
	if input.ProjectID == "" || input.RunID == "" {
		return nil, errors.New("projectID and runID are required")
	}
	if !IsValidRunStatus(input.Status) {
		return nil, fmt.Errorf("invalid run status: %s", input.Status)
	}
	if IsTerminalRunStatus(input.Status) {
		return nil, errors.New("use FinishRun/FailRun for terminal states")
	}

	run, err := s.q.GetRunByID(ctx, input.RunID)
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}
	if run.ProjectID != input.ProjectID {
		return nil, errors.New("run not found in project")
	}

	updated, err := s.q.UpdateRunStatus(ctx, input.RunID, input.ProjectID, input.Status, "")
	if err != nil {
		return nil, fmt.Errorf("update run status: %w", err)
	}

	// Resolve instance DB UUID if external instanceID provided
	var instanceDBID string
	if input.InstanceID != "" && input.AgentID != "" {
		dbID, lookupErr := s.q.GetAgentInstanceDBID(ctx, input.ProjectID, input.AgentID, input.InstanceID)
		if lookupErr == nil {
			instanceDBID = dbID
		}
	}

	// Record state event
	attrs := marshalAttributes(input.Attributes)
	_, err = s.q.CreateRunStateEvent(ctx, input.ProjectID, input.RunID,
		nullableString(input.AgentID), nullableString(instanceDBID),
		run.Status, input.Status, input.Message, attrs)
	if err != nil {
		return nil, fmt.Errorf("create state event: %w", err)
	}

	return toRuntimeRun(updated, "", ""), nil
}

// FinishRun completes a run with the completed status.
func (s *RuntimeService) FinishRun(ctx context.Context, input FinishRunInput) (*RuntimeRun, error) {
	if input.ProjectID == "" || input.RunID == "" {
		return nil, errors.New("projectID and runID are required")
	}

	run, err := s.q.GetRunByID(ctx, input.RunID)
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}
	if run.ProjectID != input.ProjectID {
		return nil, errors.New("run not found in project")
	}

	updated, err := s.q.CompleteRuntimeRun(ctx, input.RunID, input.ProjectID, input.Output, input.LatencyMS)
	if err != nil {
		return nil, fmt.Errorf("complete run: %w", err)
	}

	// Clear active run by external instance_id, get DB UUID for event
	var agentInstanceDBID string
	if input.InstanceID != "" && input.AgentID != "" {
		dbID, clearErr := s.q.ClearAgentInstanceActiveRun(ctx, input.ProjectID, input.AgentID, input.InstanceID)
		if clearErr != nil {
			return nil, fmt.Errorf("clear agent instance active run: %w", clearErr)
		}
		agentInstanceDBID = dbID
	}

	// Record state event
	_, err = s.q.CreateRunStateEvent(ctx, input.ProjectID, input.RunID,
		nil, nullableString(agentInstanceDBID),
		run.Status, string(RunStatusCompleted), "completed", []byte("{}"))
	if err != nil {
		return nil, fmt.Errorf("create state event: %w", err)
	}

	return toRuntimeRun(updated, "", ""), nil
}

// FailRun fails a run with an error.
func (s *RuntimeService) FailRun(ctx context.Context, input FailRunInput) (*RuntimeRun, error) {
	if input.ProjectID == "" || input.RunID == "" {
		return nil, errors.New("projectID and runID are required")
	}

	run, err := s.q.GetRunByID(ctx, input.RunID)
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}
	if run.ProjectID != input.ProjectID {
		return nil, errors.New("run not found in project")
	}

	attrs := marshalAttributes(input.Attributes)
	updated, err := s.q.FailRuntimeRun(ctx, input.RunID, input.ProjectID, input.Error, input.LatencyMS)
	if err != nil {
		return nil, fmt.Errorf("fail run: %w", err)
	}

	// Clear active run by external instance_id, get DB UUID for event
	var agentInstanceDBID string
	if input.InstanceID != "" && input.AgentID != "" {
		dbID, clearErr := s.q.ClearAgentInstanceActiveRun(ctx, input.ProjectID, input.AgentID, input.InstanceID)
		if clearErr != nil {
			return nil, fmt.Errorf("clear agent instance active run: %w", clearErr)
		}
		agentInstanceDBID = dbID
	}

	// Record state event
	_, err = s.q.CreateRunStateEvent(ctx, input.ProjectID, input.RunID,
		nil, nullableString(agentInstanceDBID),
		run.Status, string(RunStatusFailed), input.Error, attrs)
	if err != nil {
		return nil, fmt.Errorf("create state event: %w", err)
	}

	return toRuntimeRun(updated, "", ""), nil
}

// ListActiveRuns returns non-terminal runs for a project.
func (s *RuntimeService) ListActiveRuns(ctx context.Context, projectID string, limit, offset int) ([]ActiveRun, error) {
	if projectID == "" {
		return nil, errors.New("projectID is required")
	}
	if limit <= 0 {
		limit = 50
	}
	return s.q.ListActiveRunsByProject(ctx, projectID, limit, offset)
}

// GetRuntimeRunDetail returns a run with its state events.
func (s *RuntimeService) GetRuntimeRunDetail(ctx context.Context, runID, projectID string) (*RuntimeRunDetail, error) {
	if runID == "" || projectID == "" {
		return nil, errors.New("runID and projectID are required")
	}

	detail, err := s.q.GetRunRuntimeDetail(ctx, runID, projectID)
	if err != nil {
		return nil, err
	}

	// Fetch state events
	events, err := s.q.ListRunStateEvents(ctx, runID)
	if err != nil {
		return nil, fmt.Errorf("list state events: %w", err)
	}
	detail.Events = events

	return detail, nil
}

// ---- Helpers ----

func toRuntimeRun(run Run, agentID string, activeInstanceID string) *RuntimeRun {
	return &RuntimeRun{
		ID:        run.ID,
		ProjectID: run.ProjectID,
		Status:    run.Status,
		Input:     run.InputSummary,
		Output:    run.OutputSummary,
		Error:     run.Error,
		TraceID:   run.TraceID,
		LatencyMs: run.LatencyMs,
		AgentID:   agentID,
		AgentName: run.AgentName,
		CreatedAt: run.CreatedAt,
		UpdatedAt: run.UpdatedAt,
	}
}

func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func marshalAttributes(attrs map[string]interface{}) []byte {
	if len(attrs) == 0 {
		return []byte("{}")
	}
	b, err := json.Marshal(attrs)
	if err != nil {
		return []byte("{}")
	}
	return b
}
