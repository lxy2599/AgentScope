package onlinerval

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
)

const DefaultJobEvaluatorVersion = "trace-aware-v1"

type WorkerOptions struct {
	WorkerID         string
	Concurrency      int
	PollInterval     time.Duration
	JobTimeout       time.Duration
	StaleClaimAfter  time.Duration
	RecoveryInterval time.Duration
	MaxAttempts      int
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
	if opts.RecoveryInterval <= 0 {
		opts.RecoveryInterval = minDuration(30*time.Second, opts.StaleClaimAfter)
	}
	if opts.MaxAttempts < 1 {
		opts.MaxAttempts = 5
	}
	return &Worker{repo: repo, sampling: sampling, runs: runs, evaluator: evaluator, opts: opts, now: func() time.Time { return time.Now().UTC() }}
}

func (w *Worker) Enqueue(ctx context.Context, projectID, runID, traceID string) (Job, error) {
	return w.repo.Enqueue(ctx, projectID, runID, traceID, DefaultJobEvaluatorVersion)
}

func (w *Worker) Run(ctx context.Context) error {
	// Recovery is intentionally periodic rather than startup-only: a failed
	// persistence call can otherwise leave a Job in running state until the
	// whole process happens to restart.
	w.recoverStaleClaims(ctx)
	pollTicker := time.NewTicker(w.opts.PollInterval)
	recoveryTicker := time.NewTicker(w.opts.RecoveryInterval)
	defer pollTicker.Stop()
	defer recoveryTicker.Stop()
	for {
		if err := w.ProcessAvailable(ctx); err != nil && ctx.Err() == nil {
			log.Printf("online eval worker process available jobs: %v", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-recoveryTicker.C:
			w.recoverStaleClaims(ctx)
		case <-pollTicker.C:
		}
	}
}

func (w *Worker) recoverStaleClaims(ctx context.Context) {
	recovered, err := w.repo.RecoverStaleClaims(ctx, w.now().Add(-w.opts.StaleClaimAfter), w.now())
	if err != nil && ctx.Err() == nil {
		log.Printf("online eval worker recover stale claims: %v", err)
		return
	}
	if recovered > 0 {
		log.Printf("online eval worker recovered %d stale job claim(s)", recovered)
	}
}

// ProcessAvailable is public for deterministic integration tests and one-shot workers.
func (w *Worker) ProcessAvailable(ctx context.Context) error {
	jobs, err := w.repo.Claim(ctx, w.opts.WorkerID, w.opts.Concurrency, w.now())
	if err != nil || len(jobs) == 0 {
		return err
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(jobs))
	for _, job := range jobs {
		job := job
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.process(ctx, job); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	var joined error
	for err := range errs {
		joined = errors.Join(joined, err)
	}
	return joined
}

func (w *Worker) process(parent context.Context, job Job) error {
	ctx, cancel := context.WithTimeout(parent, w.opts.JobTimeout)
	defer cancel()
	run, err := w.runs.GetRun(ctx, job.RunID)
	if err != nil {
		return w.retry(job, fmt.Errorf("load run: %w", err))
	}
	if run.ProjectID != job.ProjectID {
		return w.retry(job, ErrRunProjectMismatch)
	}

	sampled, rule, err := w.sampling.ShouldSample(ctx, job.ProjectID, run.AgentName, buildRunFilters(run))
	if err != nil {
		return w.retry(job, fmt.Errorf("sampling decision: %w", err))
	}
	if !sampled {
		ruleID := ""
		if rule != nil {
			ruleID = rule.ID
		}
		if err := w.repo.MarkSkipped(context.Background(), job.ID, w.opts.WorkerID, job.LeaseToken, ruleID, "not_selected_by_sampling_rule", w.now()); err != nil {
			return fmt.Errorf("mark online evaluation job skipped: %w", err)
		}
		return nil
	}
	ruleID, samplingReason := "", "selected_by_sampling_rule"
	if rule != nil {
		ruleID = rule.ID
		samplingReason += ":" + rule.Name
	}

	scores, source, fallback, err := w.evaluate(ctx, run)
	if err != nil {
		return w.retry(job, err)
	}
	job.SamplingRuleID, job.SamplingReason = ruleID, samplingReason
	completion := Completion{Job: job, Run: run, Scores: scores, EvaluationSource: source, FallbackReason: fallback, CreateAnnotation: shouldAnnotate(run, scores)}
	if _, err := w.repo.Complete(context.Background(), completion, w.opts.WorkerID, job.LeaseToken, w.now()); err != nil {
		return w.retry(job, fmt.Errorf("persist evaluation: %w", err))
	}
	return nil
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

func (w *Worker) retry(job Job, cause error) error {
	attempt := job.AttemptCount
	dead := attempt >= job.MaxAttempts || attempt >= w.opts.MaxAttempts
	delay := time.Second * time.Duration(1<<min(attempt-1, 6))
	if delay < time.Second {
		delay = time.Second
	}
	if err := w.repo.Retry(context.Background(), job.ID, w.opts.WorkerID, job.LeaseToken, sanitizeJobError(cause), w.now().Add(delay), dead, w.now()); err != nil {
		return fmt.Errorf("record online evaluation retry for job %s: %w", job.ID, err)
	}
	return nil
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func minDuration(a, b time.Duration) time.Duration {
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
