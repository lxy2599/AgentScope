package onlinerval

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"AgentScope/internal/sampling"
)

// Default online evaluation timeout for async dispatch.
const defaultAsyncTimeout = 30 * time.Second

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

// OnlineEvaluator evaluates a run using multi-dimensional scoring.
// It should return scores for all applicable metric kinds. If a metric kind
// cannot be evaluated (e.g., missing trace data), it should still return a
// score with an explicit reason explaining why.
type OnlineEvaluator interface {
	EvaluateRun(ctx context.Context, projectID, runID string) ([]EvalScore, error)
}

// AnnotationCallback is called when the dispatcher determines a run needs
// a human review (low score or failure). The function should create an
// annotation item in the project's default queue.
type AnnotationCallback func(ctx context.Context, projectID, runID, traceID string, scores []EvalScore)

// ---- Dispatcher ----

// Dispatcher handles online evaluation dispatching for completed runs.
type Dispatcher struct {
	sampling      SamplingService
	runs          RunService
	evals         EvalService
	evaluator     OnlineEvaluator
	onLowScore    AnnotationCallback
}

// NewDispatcher creates a new online evaluation dispatcher.
func NewDispatcher(sampling SamplingService, runs RunService, evals EvalService) *Dispatcher {
	return &Dispatcher{
		sampling: sampling,
		runs:     runs,
		evals:    evals,
	}
}

// SetEvaluator sets the online evaluator that produces multi-dimensional scores.
func (d *Dispatcher) SetEvaluator(eval OnlineEvaluator) {
	d.evaluator = eval
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

// DispatchRunAsync dispatches online evaluation asynchronously using a
// background context with a default timeout. It is safe to call from a
// goroutine in an HTTP handler without depending on the request context.
func (d *Dispatcher) DispatchRunAsync(projectID, runID string) {
	ctx, cancel := context.WithTimeout(context.Background(), defaultAsyncTimeout)
	defer cancel()

	if err := d.DispatchRun(ctx, projectID, runID); err != nil {
		log.Printf("online eval: async dispatch failed for run %s: %v", runID, err)
	}
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

// runEvaluation produces evaluation scores for the run.
// If an OnlineEvaluator is configured, it is called first. If it returns
// non-empty scores, those are used. Otherwise, a baseline fallback score
// (baseline_completion_status) is used.
func (d *Dispatcher) runEvaluation(ctx context.Context, run *Run) ([]EvalScore, error) {
	// Try the configured evaluator first
	if d.evaluator != nil {
		scores, err := d.evaluator.EvaluateRun(ctx, run.ProjectID, run.ID)
		if err != nil {
			// Evaluator error: log and fall back to baseline
			log.Printf("online eval: evaluator failed for run %s: %v, falling back to baseline", run.ID, err)
		} else if len(scores) > 0 {
			// Evaluator returned scores - persist and return them
			for _, score := range scores {
				if err := d.evals.CreateEvalScore(ctx, score); err != nil {
					return nil, err
				}
			}
			return scores, nil
		}
		// Empty results: fall through to baseline
	}

	// Baseline fallback: only completion status score
	baselineScore := d.baselineScore(run)
	if err := d.evals.CreateEvalScore(ctx, baselineScore); err != nil {
		return nil, err
	}
	return []EvalScore{baselineScore}, nil
}

// baselineScore creates a single baseline completion status score.
// This is used when no OnlineEvaluator is configured or the evaluator
// cannot produce scores. It only reflects whether the run completed.
func (d *Dispatcher) baselineScore(run *Run) EvalScore {
	isCompleted := run.Status == "completed"

	return EvalScore{
		ProjectID:  run.ProjectID,
		RunID:      run.ID,
		TraceID:    run.TraceID,
		MetricName: "baseline_completion_status",
		MetricKind: "efficiency",
		Score:      scoreFromStatus(run.Status),
		Passed:     isCompleted,
		Reason:     "No online evaluator configured; baseline status score only",
	}
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

// MarshalScores converts eval scores to JSON string for annotation storage.
func MarshalScores(scores []EvalScore) string {
	b, _ := json.Marshal(scores)
	return string(b)
}
