package onlinerval

import (
	"context"
	"encoding/json"
	"log"
	"strings"

	"AgentScope/internal/sampling"
)

// ---- Domain types ----

// Run represents a completed run for online evaluation.
type Run struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Status    string `json:"status"`
	TraceID   string `json:"trace_id"`
	AgentName string `json:"agent_name"`
	Error     string `json:"error,omitempty"`
}

// EvalScore represents an evaluation score written by the online evaluator.
type EvalScore struct {
	ProjectID  string  `json:"project_id"`
	RunID      string  `json:"run_id"`
	TraceID    string  `json:"trace_id"`
	MetricName string  `json:"metric_name"`
	MetricKind string  `json:"metric_kind"`
	Score      float64 `json:"score"`
	Passed     bool    `json:"passed"`
	Reason     string  `json:"reason"`
}

// ---- Interfaces ----

// SamplingService checks if a run should be sampled for online evaluation.
type SamplingService interface {
	ShouldSample(ctx context.Context, projectID, agentID string, runFilters map[string]any) (bool, *sampling.SamplingRule, error)
}

// RunService provides access to run data.
type RunService interface {
	GetRun(ctx context.Context, runID string) (*Run, error)
}

// EvalService writes evaluation scores.
type EvalService interface {
	CreateEvalScore(ctx context.Context, score EvalScore) error
}

// AnnotationCallback is called when the dispatcher determines a run needs
// a human review (low score or failure). The function should create an
// annotation item in the project's default queue.
type AnnotationCallback func(ctx context.Context, projectID, runID, traceID string, scores []EvalScore)

// ---- Dispatcher ----

// Dispatcher handles online evaluation dispatching for completed runs.
type Dispatcher struct {
	sampling           SamplingService
	runs               RunService
	evals              EvalService
	onLowScore         AnnotationCallback
}

// NewDispatcher creates a new online evaluation dispatcher.
func NewDispatcher(sampling SamplingService, runs RunService, evals EvalService) *Dispatcher {
	return &Dispatcher{
		sampling: sampling,
		runs:     runs,
		evals:    evals,
	}
}

// SetAnnotationCallback sets a callback for creating annotation items when
// a run receives low evaluation scores or fails.
func (d *Dispatcher) SetAnnotationCallback(cb AnnotationCallback) {
	d.onLowScore = cb
}

// DispatchRun triggers online evaluation for a completed/failed run.
func (d *Dispatcher) DispatchRun(ctx context.Context, projectID, runID string) error {
	run, err := d.runs.GetRun(ctx, runID)
	if err != nil {
		return err
	}

	runFilters := buildRunFilters(run)

	sampled, _, err := d.sampling.ShouldSample(ctx, projectID, run.AgentName, runFilters)
	if err != nil {
		return err
	}
	if !sampled {
		return nil
	}

	go d.evaluateAsync(run)
	return nil
}

func (d *Dispatcher) evaluateAsync(run *Run) {
	ctx := context.Background()

	scores, err := d.runEvaluation(ctx, run)
	if err != nil {
		log.Printf("online eval: run %s evaluation failed: %v", run.ID, err)
		return
	}

	// Check if we need to create an annotation item
	if d.onLowScore != nil {
		if shouldAnnotate(run, scores) {
			d.onLowScore(ctx, run.ProjectID, run.ID, run.TraceID, scores)
		}
	}
}

// runEvaluation creates multi-dimensional evaluation scores for the given run.
func (d *Dispatcher) runEvaluation(ctx context.Context, run *Run) ([]EvalScore, error) {
	isCompleted := run.Status == "completed"
	hasError := run.Error != ""

	// Build multi-dimensional scores using Phase 2.5 metric kinds.
	// Metrics that require detailed trace data (trajectory, rag_groundedness)
	// include explicit reasons about what additional data would be needed
	// rather than silently defaulting to the completion status score.
	scores := []EvalScore{
		{
			ProjectID:  run.ProjectID,
			RunID:      run.ID,
			TraceID:    run.TraceID,
			MetricName: "completion_status",
			MetricKind: "efficiency",
			Score:      scoreFromStatus(run.Status),
			Passed:     isCompleted,
			Reason:     statusReason(run.Status, run.Error),
		},
		{
			ProjectID:  run.ProjectID,
			RunID:      run.ID,
			TraceID:    run.TraceID,
			MetricName: "final_answer_quality",
			MetricKind: "final_answer",
			Score:      scoreFromStatus(run.Status),
			Passed:     isCompleted,
			Reason:     finalAnswerReason(run),
		},
		{
			ProjectID:  run.ProjectID,
			RunID:      run.ID,
			TraceID:    run.TraceID,
			MetricName: "tool_correctness",
			MetricKind: "tool_correctness",
			Score:      toolScore(run),
			Passed:     isCompleted && !hasError,
			Reason:     toolReason(run),
		},
		{
			ProjectID:  run.ProjectID,
			RunID:      run.ID,
			TraceID:    run.TraceID,
			MetricName: "safety",
			MetricKind: "safety",
			Score:      safetyScore(run),
			Passed:     !hasError,
			Reason:     safetyReason(run),
		},
		{
			ProjectID:  run.ProjectID,
			RunID:      run.ID,
			TraceID:    run.TraceID,
			MetricName: "trajectory_quality",
			MetricKind: "trajectory",
			Score:      inconclusiveScore(isCompleted),
			Passed:     isCompleted,
			Reason:     "Trajectory evaluation requires span-level trace data analysis from ClickHouse",
		},
		{
			ProjectID:  run.ProjectID,
			RunID:      run.ID,
			TraceID:    run.TraceID,
			MetricName: "rag_groundedness",
			MetricKind: "rag_groundedness",
			Score:      inconclusiveScore(isCompleted),
			Passed:     isCompleted,
			Reason:     "RAG groundedness evaluation requires retrieval call records from trace storage",
		},
	}

	for _, score := range scores {
		if err := d.evals.CreateEvalScore(ctx, score); err != nil {
			return nil, err
		}
	}
	return scores, nil
}

// ---- Multi-dimensional scoring helpers ----

// lowScoreThreshold defines the minimum score for a completed run to
// avoid automatic annotation. Any metric below this threshold triggers annotation.
const lowScoreThreshold = 0.5

func shouldAnnotate(run *Run, scores []EvalScore) bool {
	// Failed, timeout, or cancelled runs always get annotation
	if run.Status != "completed" {
		return true
	}
	// Completed runs with low scores on any metric also get annotation
	for _, s := range scores {
		if s.Score < lowScoreThreshold {
			return true
		}
	}
	return false
}

func buildRunFilters(run *Run) map[string]any {
	filters := map[string]any{
		"status": run.Status,
	}
	if run.Error != "" {
		filters["error"] = run.Error
	}
	return filters
}

func scoreFromStatus(status string) float64 {
	switch status {
	case "completed":
		return 1.0
	case "failed":
		return 0.0
	default:
		return 0.5
	}
}

func statusReason(status, errStr string) string {
	if errStr != "" {
		return "Run " + status + ": " + errStr
	}
	return "Run " + status
}

func finalAnswerReason(run *Run) string {
	if run.Status == "completed" {
		return "Final answer generated; run completed successfully"
	}
	if run.Error != "" {
		return "Final answer not available: " + run.Error
	}
	return "Run did not complete"
}

func toolScore(run *Run) float64 {
	if run.Status == "completed" {
		return 1.0
	}
	if run.Error != "" && strings.Contains(strings.ToLower(run.Error), "tool") {
		return 0.0
	}
	return 0.5
}

func toolReason(run *Run) string {
	if run.Status == "completed" {
		return "No tool errors detected at run level"
	}
	if run.Error != "" && strings.Contains(strings.ToLower(run.Error), "tool") {
		return "Tool error detected: " + run.Error
	}
	if run.Error != "" {
		return "Run failed; tool-level analysis limited"
	}
	return "Run status: " + run.Status
}

func safetyScore(run *Run) float64 {
	if run.Error == "" {
		return 1.0
	}
	return 0.5
}

func safetyReason(run *Run) string {
	if run.Error == "" {
		return "No errors detected during run execution"
	}
	return "Run encountered error: " + run.Error
}

func inconclusiveScore(completed bool) float64 {
	if completed {
		return 0.7 // Assumed acceptable without deep trace analysis
	}
	return 0.3
}

// MarshalScores converts eval scores to JSON string for annotation storage.
func MarshalScores(scores []EvalScore) string {
	b, _ := json.Marshal(scores)
	return string(b)
}
