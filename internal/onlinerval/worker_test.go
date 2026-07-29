package onlinerval

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryJobRepo struct {
	mu          sync.Mutex
	jobs        map[string]Job
	completions []Completion
	skipped     []string
	retried     []string
	retryErr    error
	recoveries  int
}

func newMemoryJobRepo(job Job) *memoryJobRepo {
	return &memoryJobRepo{jobs: map[string]Job{job.ID: job}}
}
func (r *memoryJobRepo) Enqueue(context.Context, string, string, string, string) (Job, error) {
	return Job{}, errors.New("not implemented")
}
func (r *memoryJobRepo) Claim(_ context.Context, worker string, limit int, now time.Time) ([]Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []Job{}
	for id, j := range r.jobs {
		if len(out) >= limit || (j.Status != JobQueued && j.Status != JobRetryWait) || j.AvailableAt.After(now) {
			continue
		}
		j.Status = JobRunning
		j.ClaimedBy = worker
		j.AttemptCount++
		j.ClaimedAt = &now
		r.jobs[id] = j
		out = append(out, j)
	}
	return out, nil
}
func (r *memoryJobRepo) MarkSkipped(_ context.Context, id, _, _, rule, reason string, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	j := r.jobs[id]
	j.Status = JobSkipped
	j.SamplingRuleID = rule
	j.SamplingReason = reason
	j.FinishedAt = &at
	r.jobs[id] = j
	r.skipped = append(r.skipped, id)
	return nil
}
func (r *memoryJobRepo) Complete(_ context.Context, c Completion, _, _ string, at time.Time) (Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j := r.jobs[c.Job.ID]
	j.Status = JobSucceeded
	j.EvaluationSource = c.EvaluationSource
	j.FallbackReason = c.FallbackReason
	j.SamplingRuleID = c.Job.SamplingRuleID
	j.SamplingReason = c.Job.SamplingReason
	j.FinishedAt = &at
	if c.CreateAnnotation {
		j.AnnotationItemID = "annotation-" + j.ID
	}
	r.jobs[j.ID] = j
	r.completions = append(r.completions, c)
	return j, nil
}
func (r *memoryJobRepo) Retry(_ context.Context, id, _, _, why string, available time.Time, dead bool, at time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.retryErr != nil {
		return r.retryErr
	}
	j := r.jobs[id]
	if dead {
		j.Status = JobDeadLetter
		j.FinishedAt = &at
	} else {
		j.Status = JobRetryWait
		j.AvailableAt = available
	}
	j.LastError = why
	r.jobs[id] = j
	r.retried = append(r.retried, id)
	return nil
}
func (r *memoryJobRepo) RecoverStaleClaims(context.Context, time.Time, time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recoveries++
	return 0, nil
}
func (r *memoryJobRepo) recoveryCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.recoveries
}
func (r *memoryJobRepo) Get(_ context.Context, _, id string) (Job, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	j, ok := r.jobs[id]
	if !ok {
		return Job{}, ErrJobNotFound
	}
	return j, nil
}
func (r *memoryJobRepo) List(context.Context, string, string, int) ([]Job, error) { return nil, nil }
func (r *memoryJobRepo) RetryManually(context.Context, string, string, time.Time) (Job, error) {
	return Job{}, nil
}

func TestWorkerPersistsTraceEvaluationAndAnnotationAtomically(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	job := Job{ID: "job-1", ProjectID: "proj-1", RunID: "run-1", Status: JobQueued, AvailableAt: now, MaxAttempts: 5}
	repo := newMemoryJobRepo(job)
	evaluator := &fakeOnlineEvaluator{scores: []EvalScore{{MetricName: "trajectory_quality", MetricKind: "trajectory", Score: 0.2, Passed: false, Reason: "tool failed"}}}
	worker := NewWorker(repo, &fakeSamplingSvc{shouldSample: true, ruleName: "failure-sample"}, &fakeRunSvc{run: &Run{ID: "run-1", ProjectID: "proj-1", TraceID: "trace-1", Status: "completed", AgentName: "agent"}}, evaluator, WorkerOptions{WorkerID: "test", Concurrency: 1})
	worker.now = func() time.Time { return now }
	if err := worker.ProcessAvailable(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, _ := repo.Get(context.Background(), "proj-1", "job-1")
	if result.Status != JobSucceeded || result.EvaluationSource != "trace_heuristic" || result.AnnotationItemID == "" {
		t.Fatalf("job not completed with trace result + annotation: %#v", result)
	}
	if len(repo.completions) != 1 || len(repo.completions[0].Scores) != 1 {
		t.Fatalf("completion = %#v", repo.completions)
	}
}
func TestWorkerMarksUnsampledJobSkippedAndUsesExplicitBaselineFallback(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	job := Job{ID: "skip", ProjectID: "proj", RunID: "run", Status: JobQueued, AvailableAt: now, MaxAttempts: 5}
	repo := newMemoryJobRepo(job)
	worker := NewWorker(repo, &fakeSamplingSvc{shouldSample: false}, &fakeRunSvc{run: &Run{ID: "run", ProjectID: "proj", Status: "failed"}}, nil, WorkerOptions{WorkerID: "test"})
	worker.now = func() time.Time { return now }
	if err := worker.ProcessAvailable(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, _ := repo.Get(context.Background(), "proj", "skip")
	if result.Status != JobSkipped {
		t.Fatalf("status=%s want skipped", result.Status)
	}
	// A separately sampled run must identify the fallback rather than pretending it was trace-aware.
	job2 := Job{ID: "fallback", ProjectID: "proj", RunID: "run", Status: JobQueued, AvailableAt: now, MaxAttempts: 5}
	repo2 := newMemoryJobRepo(job2)
	worker2 := NewWorker(repo2, &fakeSamplingSvc{shouldSample: true}, &fakeRunSvc{run: &Run{ID: "run", ProjectID: "proj", Status: "failed"}}, nil, WorkerOptions{WorkerID: "test"})
	worker2.now = func() time.Time { return now }
	_ = worker2.ProcessAvailable(context.Background())
	fallback, _ := repo2.Get(context.Background(), "proj", "fallback")
	if fallback.EvaluationSource != "baseline_fallback" || fallback.FallbackReason != "evaluator_not_configured" {
		t.Fatalf("fallback metadata=%#v", fallback)
	}
}
func TestWorkerSchedulesRetryAndDeadLettersAfterAttemptLimit(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	job := Job{ID: "retry", ProjectID: "proj", RunID: "run", Status: JobQueued, AvailableAt: now, MaxAttempts: 1}
	repo := newMemoryJobRepo(job)
	worker := NewWorker(repo, &fakeSamplingSvc{shouldSample: true}, &fakeRunSvc{err: errors.New("database unavailable")}, nil, WorkerOptions{WorkerID: "test", MaxAttempts: 1})
	worker.now = func() time.Time { return now }
	_ = worker.ProcessAvailable(context.Background())
	result, _ := repo.Get(context.Background(), "proj", "retry")
	if result.Status != JobDeadLetter || result.LastError == "" {
		t.Fatalf("retry result=%#v", result)
	}
}

func TestWorkerSurfacesRetryPersistenceFailure(t *testing.T) {
	now := time.Date(2026, 7, 28, 10, 0, 0, 0, time.UTC)
	job := Job{ID: "retry-write-failure", ProjectID: "proj", RunID: "run", Status: JobQueued, AvailableAt: now, MaxAttempts: 5}
	repo := newMemoryJobRepo(job)
	repo.retryErr = errors.New("postgres unavailable")
	worker := NewWorker(repo, &fakeSamplingSvc{shouldSample: true}, &fakeRunSvc{err: errors.New("load failure")}, nil, WorkerOptions{WorkerID: "test"})
	worker.now = func() time.Time { return now }
	if err := worker.ProcessAvailable(context.Background()); err == nil || !strings.Contains(err.Error(), "record online evaluation retry") {
		t.Fatalf("ProcessAvailable error = %v, want surfaced retry persistence failure", err)
	}
}

func TestWorkerRecoversStaleClaimsPeriodically(t *testing.T) {
	repo := newMemoryJobRepo(Job{})
	worker := NewWorker(repo, &fakeSamplingSvc{}, &fakeRunSvc{}, nil, WorkerOptions{
		WorkerID: "test", PollInterval: time.Hour, JobTimeout: time.Second,
		StaleClaimAfter: time.Second, RecoveryInterval: time.Millisecond,
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { _ = worker.Run(ctx); close(done) }()
	deadline := time.After(200 * time.Millisecond)
	for repo.recoveryCount() < 2 {
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatalf("recovery count = %d, want at least 2", repo.recoveryCount())
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-done
}
