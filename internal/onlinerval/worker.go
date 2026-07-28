package onlinerval

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

const defaultJobEvaluatorVersion = "trace-aware-v1"

type WorkerOptions struct {
	WorkerID        string
	Concurrency     int
	PollInterval    time.Duration
	JobTimeout      time.Duration
	StaleClaimAfter time.Duration
	MaxAttempts     int
}

// Worker evaluates durable Jobs and is safe to run in multiple processes:
// repository Claim uses row locking, while all completion side effects are
// transactionally keyed by job ID.
type Worker struct {
	repo      JobRepository
	sampling  SamplingService
	runs      RunService
	evaluator OnlineEvaluator
	opts      WorkerOptions
	now       func() time.Time
}

func NewWorker(repo JobRepository, sampling SamplingService, runs RunService, evaluator OnlineEvaluator, opts WorkerOptions) *Worker {
	if opts.WorkerID == "" {
		opts.WorkerID = "online-eval-worker"
	}
	if opts.Concurrency < 1 {
		opts.Concurrency = 1
	}
	if opts.PollInterval <= 0 {
		opts.PollInterval = 500 * time.Millisecond
	}
	if opts.JobTimeout <= 0 {
		opts.JobTimeout = 30 * time.Second
	}
	if opts.StaleClaimAfter <= 0 {
		opts.StaleClaimAfter = 2 * opts.JobTimeout
	}
	if opts.MaxAttempts < 1 {
		opts.MaxAttempts = 5
	}
	return &Worker{repo: repo, sampling: sampling, runs: runs, evaluator: evaluator, opts: opts, now: func() time.Time { return time.Now().UTC() }}
}

func (w *Worker) Enqueue(ctx context.Context, projectID, runID, traceID string) (Job, error) {
	return w.repo.Enqueue(ctx, projectID, runID, traceID, defaultJobEvaluatorVersion)
}

func (w *Worker) Run(ctx context.Context) error {
	if _, err := w.repo.RecoverStaleClaims(ctx, w.now().Add(-w.opts.StaleClaimAfter), w.now()); err != nil && ctx.Err() == nil {
		return fmt.Errorf("recover stale online evaluation jobs: %w", err)
	}
	ticker := time.NewTicker(w.opts.PollInterval)
	defer ticker.Stop()
	for {
		if err := w.ProcessAvailable(ctx); err != nil && ctx.Err() == nil {
			log.Printf("online eval worker: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// ProcessAvailable is public for deterministic integration tests and one-shot workers.
func (w *Worker) ProcessAvailable(ctx context.Context) error {
	jobs, err := w.repo.Claim(ctx, w.opts.WorkerID, w.opts.Concurrency, w.now())
	if err != nil || len(jobs) == 0 {
		return err
	}
	var wg sync.WaitGroup
	for _, job := range jobs {
		job := job
		wg.Add(1)
		go func() { defer wg.Done(); w.process(ctx, job) }()
	}
	wg.Wait()
	return nil
}

func (w *Worker) process(parent context.Context, job Job) {
	ctx, cancel := context.WithTimeout(parent, w.opts.JobTimeout)
	defer cancel()
	run, err := w.runs.GetRun(ctx, job.RunID)
	if err != nil {
		w.retry(job, fmt.Errorf("load run: %w", err))
		return
	}
	if run.ProjectID != job.ProjectID {
		_ = w.repo.Retry(context.Background(), job.ID, w.opts.WorkerID, ErrRunProjectMismatch.Error(), w.now(), true, w.now())
		return
	}

	sampled, rule, err := w.sampling.ShouldSample(ctx, job.ProjectID, run.AgentName, buildRunFilters(run))
	if err != nil {
		w.retry(job, fmt.Errorf("sampling decision: %w", err))
		return
	}
	if !sampled {
		ruleID := ""
		if rule != nil {
			ruleID = rule.ID
		}
		_ = w.repo.MarkSkipped(context.Background(), job.ID, ruleID, "not_selected_by_sampling_rule", w.now())
		return
	}
	ruleID, samplingReason := "", "selected_by_sampling_rule"
	if rule != nil {
		ruleID = rule.ID
		samplingReason += ":" + rule.Name
	}

	scores, source, fallback, err := w.evaluate(ctx, run)
	if err != nil {
		w.retry(job, err)
		return
	}
	job.SamplingRuleID, job.SamplingReason = ruleID, samplingReason
	completion := Completion{Job: job, Run: run, Scores: scores, EvaluationSource: source, FallbackReason: fallback, CreateAnnotation: shouldAnnotate(run, scores)}
	if _, err := w.repo.Complete(context.Background(), completion, w.now()); err != nil {
		w.retry(job, fmt.Errorf("persist evaluation: %w", err))
		return
	}
}

func (w *Worker) evaluate(ctx context.Context, run *Run) ([]EvalScore, string, string, error) {
	if w.evaluator != nil {
		scores, err := w.evaluator.EvaluateRun(ctx, run.ProjectID, run.ID)
		if err == nil && len(scores) > 0 {
			return scores, "trace_heuristic", "", nil
		}
		if err != nil {
			return []EvalScore{baselineScore(run)}, "baseline_fallback", "trace_evaluator_error", nil
		}
		return []EvalScore{baselineScore(run)}, "baseline_fallback", "trace_evaluator_empty", nil
	}
	return []EvalScore{baselineScore(run)}, "baseline_fallback", "evaluator_not_configured", nil
}

func baselineScore(run *Run) EvalScore {
	return EvalScore{ProjectID: run.ProjectID, RunID: run.ID, TraceID: run.TraceID, MetricName: "baseline_completion_status", MetricKind: "efficiency", Score: scoreFromStatus(run.Status), Passed: run.Status == "completed", Reason: "Baseline completion-status fallback; trace evaluator result unavailable"}
}

func (w *Worker) retry(job Job, err error) {
	attempt := job.AttemptCount
	dead := attempt >= job.MaxAttempts || attempt >= w.opts.MaxAttempts
	delay := time.Second * time.Duration(1<<min(attempt-1, 6))
	if delay < time.Second {
		delay = time.Second
	}
	_ = w.repo.Retry(context.Background(), job.ID, w.opts.WorkerID, sanitizeJobError(err), w.now().Add(delay), dead, w.now())
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func sanitizeJobError(err error) string {
	if err == nil {
		return ""
	}
	value := err.Error()
	if len(value) > 500 {
		return value[:500]
	}
	return value
}
