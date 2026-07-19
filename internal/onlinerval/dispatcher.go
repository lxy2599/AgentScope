package onlinerval

import (
	"context"
	"encoding/json"
	"log"

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
		if shouldAnnotate(run) {
			d.onLowScore(ctx, run.ProjectID, run.ID, run.TraceID, scores)
		}
	}
}

// runEvaluation creates evaluation scores for the given run.
func (d *Dispatcher) runEvaluation(ctx context.Context, run *Run) ([]EvalScore, error) {
	scores := []EvalScore{
		{
			ProjectID:  run.ProjectID,
			RunID:      run.ID,
			TraceID:    run.TraceID,
			MetricName: "completion_status",
			MetricKind: "efficiency",
			Score:      scoreFromStatus(run.Status),
			Passed:     run.Status == "completed",
			Reason:     statusReason(run.Status, run.Error),
		},
	}

	for _, score := range scores {
		if err := d.evals.CreateEvalScore(ctx, score); err != nil {
			return nil, err
		}
	}
	return scores, nil
}

// ---- Helpers ----

func shouldAnnotate(run *Run) bool {
	if run.Status != "completed" {
		return true // failed, timeout, cancelled runs always get annotation
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

// MarshalScores converts eval scores to JSON string for annotation storage.
func MarshalScores(scores []EvalScore) string {
	b, _ := json.Marshal(scores)
	return string(b)
}
