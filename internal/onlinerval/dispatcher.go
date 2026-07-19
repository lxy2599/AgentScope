package onlinerval

import (
	"context"
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
// Uses the real sampling.SamplingRule type for consistency.
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

// ---- Dispatcher ----

// Dispatcher handles online evaluation dispatching for completed runs.
// The dispatch happens asynchronously in a background goroutine to avoid
// blocking the SDK runtime response.
type Dispatcher struct {
	sampling SamplingService
	runs     RunService
	evals    EvalService
}

// NewDispatcher creates a new online evaluation dispatcher.
func NewDispatcher(sampling SamplingService, runs RunService, evals EvalService) *Dispatcher {
	return &Dispatcher{
		sampling: sampling,
		runs:     runs,
		evals:    evals,
	}
}

// DispatchRun triggers online evaluation for a completed/failed run.
// The actual evaluation runs asynchronously.
func (d *Dispatcher) DispatchRun(ctx context.Context, projectID, runID string) error {
	// Get run data
	run, err := d.runs.GetRun(ctx, runID)
	if err != nil {
		return err
	}

	// Build run filters from run data
	runFilters := buildRunFilters(run)

	// Check sampling rules
	sampled, _, err := d.sampling.ShouldSample(ctx, projectID, run.AgentName, runFilters)
	if err != nil {
		return err
	}
	if !sampled {
		return nil
	}

	// Run evaluation asynchronously
	go d.evaluateAsync(run)

	return nil
}

// evaluateAsync performs the actual evaluation in a background goroutine.
func (d *Dispatcher) evaluateAsync(run *Run) {
	ctx := context.Background()

	// Run the evaluation (currently creates a simple throughput score;
	// in production this would use configured evaluators from Phase 2.5)
	if err := d.runEvaluation(ctx, run); err != nil {
		log.Printf("online eval: run %s evaluation failed: %v", run.ID, err)
	}
}

// runEvaluation creates evaluation scores for the given run.
func (d *Dispatcher) runEvaluation(ctx context.Context, run *Run) error {
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
			return err
		}
	}
	return nil
}

// ---- Helpers ----

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
