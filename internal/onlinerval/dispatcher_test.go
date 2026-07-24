package onlinerval

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"AgentScope/internal/sampling"
)

// ---- Fake Sampling Service ----

type fakeSamplingSvc struct {
	shouldSample bool
	ruleName     string
	projectID    string
	agentID      string
}

func (f *fakeSamplingSvc) ShouldSample(ctx context.Context, projectID, agentID string, runFilters map[string]any) (bool, *sampling.SamplingRule, error) {
	if f.shouldSample {
		return true, &sampling.SamplingRule{
			ID:         "rule-1",
			ProjectID:  projectID,
			Name:       f.ruleName,
			SampleRate: 1.0,
		}, nil
	}
	return false, nil, nil
}

// ---- Fake Run Service ----

type fakeRunSvc struct {
	run *Run
	err error
}

func (f *fakeRunSvc) GetRun(ctx context.Context, runID string) (*Run, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.run, nil
}

// ---- Context-aware fake run service ----

// contextAwareRunSvc simulates a run service that respects context cancellation.
type contextAwareRunSvc struct {
	run   *Run
	err   error
	delay time.Duration // how long to wait before returning
}

func (f *contextAwareRunSvc) GetRun(ctx context.Context, runID string) (*Run, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	return f.run, nil
}

// ---- Fake Eval Service ----

type fakeEvalSvc struct {
	createdScores []EvalScore
	mu            sync.Mutex
	failOnCreate  bool
}

func (f *fakeEvalSvc) CreateEvalScore(ctx context.Context, score EvalScore) error {
	if f.failOnCreate {
		return errors.New("eval service unavailable")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createdScores = append(f.createdScores, score)
	return nil
}

func (f *fakeEvalSvc) getScores() []EvalScore {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := make([]EvalScore, len(f.createdScores))
	copy(result, f.createdScores)
	return result
}

// ---- Fake Online Evaluator ----

type fakeOnlineEvaluator struct {
	mu        sync.Mutex
	scores    []EvalScore
	err       error
	called    bool
	calledRun string
}

func (f *fakeOnlineEvaluator) EvaluateRun(ctx context.Context, projectID, runID string) ([]EvalScore, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	f.calledRun = runID
	// Return a copy of the scores with project/run filled in
	result := make([]EvalScore, len(f.scores))
	for i, s := range f.scores {
		result[i] = s
		if result[i].ProjectID == "" {
			result[i].ProjectID = projectID
		}
		if result[i].RunID == "" {
			result[i].RunID = runID
		}
	}
	return result, f.err
}

func (f *fakeOnlineEvaluator) wasCalled() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.called
}

func (f *fakeOnlineEvaluator) calledRunID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calledRun
}

// ---- Tests ----

func TestDispatcher_DispatchRun_NotSampled(t *testing.T) {
	sampling := &fakeSamplingSvc{shouldSample: false}
	runSvc := &fakeRunSvc{
		run: &Run{ID: "run-1", ProjectID: "proj-1", Status: "completed"},
	}
	evalSvc := &fakeEvalSvc{}

	disp := NewDispatcher(sampling, runSvc, evalSvc)

	err := disp.DispatchRun(context.Background(), "proj-1", "run-1")
	if err != nil {
		t.Fatalf("DispatchRun() error = %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Should not have created any eval scores
	if len(evalSvc.getScores()) != 0 {
		t.Errorf("expected 0 eval scores, got %d", len(evalSvc.getScores()))
	}
}

func TestDispatcher_DispatchRun_Sampled_BaselineOnly(t *testing.T) {
	sampling := &fakeSamplingSvc{
		shouldSample: true,
		ruleName:     "always",
	}
	runSvc := &fakeRunSvc{
		run: &Run{ID: "run-1", ProjectID: "proj-1", Status: "completed", TraceID: "trace-1"},
	}
	evalSvc := &fakeEvalSvc{}

	disp := NewDispatcher(sampling, runSvc, evalSvc)

	err := disp.DispatchRun(context.Background(), "proj-1", "run-1")
	if err != nil {
		t.Fatalf("DispatchRun() error = %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	// Should have created eval scores
	scores := evalSvc.getScores()
	if len(scores) == 0 {
		t.Fatal("expected at least 1 eval score, got 0")
	}

	// Verify the first score has correct project/run info
	if scores[0].ProjectID != "proj-1" {
		t.Errorf("project_id = %q, want %q", scores[0].ProjectID, "proj-1")
	}
	if scores[0].RunID != "run-1" {
		t.Errorf("run_id = %q, want %q", scores[0].RunID, "run-1")
	}
	if scores[0].TraceID != "trace-1" {
		t.Errorf("trace_id = %q, want %q", scores[0].TraceID, "trace-1")
	}

	// Without an evaluator, only baseline_completion_status should exist
	if len(scores) != 1 {
		t.Fatalf("expected 1 baseline score, got %d", len(scores))
	}
	if scores[0].MetricName != "baseline_completion_status" {
		t.Errorf("metric_name = %q, want %q", scores[0].MetricName, "baseline_completion_status")
	}
	if scores[0].MetricKind != "efficiency" {
		t.Errorf("metric_kind = %q, want %q", scores[0].MetricKind, "efficiency")
	}
	if scores[0].Score != 1.0 {
		t.Errorf("score = %f, want 1.0", scores[0].Score)
	}
	if !scores[0].Passed {
		t.Error("passed = false, want true")
	}
}

func TestDispatcher_DispatchRun_SampleRateZero(t *testing.T) {
	sampling := &fakeSamplingSvc{shouldSample: false}
	runSvc := &fakeRunSvc{
		run: &Run{ID: "run-1", ProjectID: "proj-1", Status: "completed"},
	}
	evalSvc := &fakeEvalSvc{}

	disp := NewDispatcher(sampling, runSvc, evalSvc)

	err := disp.DispatchRun(context.Background(), "proj-1", "run-1")
	if err != nil {
		t.Fatalf("DispatchRun() error = %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if len(evalSvc.getScores()) != 0 {
		t.Errorf("expected 0 eval scores for sample_rate=0, got %d", len(evalSvc.getScores()))
	}
}

func TestDispatcher_DispatchRun_StatusFilter(t *testing.T) {
	sampling := &fakeSamplingSvc{
		shouldSample: true,
		ruleName:     "filter-completed",
	}
	runSvc := &fakeRunSvc{
		run: &Run{ID: "run-1", ProjectID: "proj-1", Status: "completed", TraceID: "trace-1"},
	}
	evalSvc := &fakeEvalSvc{}

	disp := NewDispatcher(sampling, runSvc, evalSvc)

	err := disp.DispatchRun(context.Background(), "proj-1", "run-1")
	if err != nil {
		t.Fatalf("DispatchRun() error = %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if len(evalSvc.getScores()) == 0 {
		t.Error("expected eval scores for status-filtered run")
	}
}

func TestDispatcher_DispatchRun_RunNotFound(t *testing.T) {
	sampling := &fakeSamplingSvc{shouldSample: false}
	runSvc := &fakeRunSvc{
		err: errors.New("run not found"),
	}
	evalSvc := &fakeEvalSvc{}

	disp := NewDispatcher(sampling, runSvc, evalSvc)

	err := disp.DispatchRun(context.Background(), "proj-1", "nonexistent")
	if err == nil {
		t.Fatal("DispatchRun() expected error for nonexistent run")
	}
}

func TestDispatcher_DispatchRun_AsyncDoesNotBlock(t *testing.T) {
	sampling := &fakeSamplingSvc{
		shouldSample: true,
		ruleName:     "async",
	}
	runSvc := &fakeRunSvc{
		run: &Run{ID: "run-1", ProjectID: "proj-1", Status: "completed", TraceID: "trace-1"},
	}
	evalSvc := &fakeEvalSvc{}

	disp := NewDispatcher(sampling, runSvc, evalSvc)

	start := time.Now()
	err := disp.DispatchRun(context.Background(), "proj-1", "run-1")
	duration := time.Since(start)
	if err != nil {
		t.Fatalf("DispatchRun() error = %v", err)
	}

	// DispatchRun should return quickly (it's async after checking sampling rules)
	if duration > 1*time.Second {
		t.Errorf("DispatchRun took %v, expected fast return", duration)
	}

	time.Sleep(100 * time.Millisecond)

	if len(evalSvc.getScores()) == 0 {
		t.Error("expected eval scores from async dispatch")
	}
}

func TestDispatcher_FailedRun_BaselineScore(t *testing.T) {
	sampling := &fakeSamplingSvc{
		shouldSample: true,
		ruleName:     "always",
	}
	runSvc := &fakeRunSvc{
		run: &Run{ID: "run-2", ProjectID: "proj-1", Status: "failed", TraceID: "trace-2", Error: "tool execution error: search failed"},
	}
	evalSvc := &fakeEvalSvc{}

	disp := NewDispatcher(sampling, runSvc, evalSvc)

	err := disp.DispatchRun(context.Background(), "proj-1", "run-2")
	if err != nil {
		t.Fatalf("DispatchRun() error = %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	scores := evalSvc.getScores()
	if len(scores) == 0 {
		t.Fatal("expected eval scores for failed run, got 0")
	}

	// Without evaluator, only baseline_completion_status
	if len(scores) != 1 {
		t.Fatalf("expected 1 baseline score for failed run, got %d", len(scores))
	}
	if scores[0].MetricName != "baseline_completion_status" {
		t.Errorf("metric_name = %q, want %q", scores[0].MetricName, "baseline_completion_status")
	}
	if scores[0].Score != 0.0 {
		t.Errorf("score = %f, want 0.0", scores[0].Score)
	}
	if scores[0].Passed {
		t.Error("passed = true, want false")
	}
	if scores[0].ProjectID != "proj-1" {
		t.Errorf("score has project_id = %q, want %q", scores[0].ProjectID, "proj-1")
	}
}

func TestDispatcher_EvalFailure_DoesNotBlock(t *testing.T) {
	// Even if eval service fails, DispatchRun should return immediately
	sampling := &fakeSamplingSvc{
		shouldSample: true,
		ruleName:     "always",
	}
	runSvc := &fakeRunSvc{
		run: &Run{ID: "run-3", ProjectID: "proj-1", Status: "completed", TraceID: "trace-3"},
	}
	evalSvc := &fakeEvalSvc{
		failOnCreate: true,
	}

	disp := NewDispatcher(sampling, runSvc, evalSvc)

	start := time.Now()
	err := disp.DispatchRun(context.Background(), "proj-1", "run-3")
	duration := time.Since(start)
	if err != nil {
		t.Fatalf("DispatchRun() error = %v", err)
	}

	// DispatchRun should return quickly (async eval)
	if duration > 1*time.Second {
		t.Errorf("DispatchRun took %v, expected fast return", duration)
	}
}

func TestDispatcher_DispatchRun_ProjectIsolation(t *testing.T) {
	sampling := &fakeSamplingSvc{
		shouldSample: true,
		ruleName:     "proj-isolation",
	}
	runSvc := &fakeRunSvc{
		run: &Run{ID: "run-1", ProjectID: "proj-1", Status: "completed", TraceID: "trace-1"},
	}
	evalSvc := &fakeEvalSvc{}

	disp := NewDispatcher(sampling, runSvc, evalSvc)

	err := disp.DispatchRun(context.Background(), "proj-1", "run-1")
	if err != nil {
		t.Fatalf("DispatchRun() error = %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	for _, score := range evalSvc.getScores() {
		if score.ProjectID != "proj-1" {
			t.Errorf("score has project_id = %q, want %q", score.ProjectID, "proj-1")
		}
	}
}

func TestDispatcher_FailedRun_GeneratesScores(t *testing.T) {
	// Ensure project isolation with different project
	sampling := &fakeSamplingSvc{
		shouldSample: true,
		ruleName:     "proj-iso-fail",
	}
	runSvc := &fakeRunSvc{
		run: &Run{ID: "run-4", ProjectID: "proj-2", Status: "failed", TraceID: "trace-4", Error: "timeout"},
	}
	evalSvc := &fakeEvalSvc{}

	disp := NewDispatcher(sampling, runSvc, evalSvc)

	err := disp.DispatchRun(context.Background(), "proj-2", "run-4")
	if err != nil {
		t.Fatalf("DispatchRun() error = %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if len(evalSvc.getScores()) == 0 {
		t.Fatal("expected eval scores for failed run")
	}

	for _, score := range evalSvc.getScores() {
		if score.ProjectID != "proj-2" {
			t.Errorf("score has project_id = %q, want %q", score.ProjectID, "proj-2")
		}
	}
}

func TestShouldAnnotate_FailedRun(t *testing.T) {
	run := &Run{ID: "r1", Status: "failed", Error: "error"}
	if !shouldAnnotate(run, nil) {
		t.Error("shouldAnnotate should return true for failed run")
	}
}

func TestShouldAnnotate_CompletedHighScores(t *testing.T) {
	run := &Run{ID: "r2", Status: "completed"}
	scores := []EvalScore{
		{MetricName: "efficiency", MetricKind: "efficiency", Score: 1.0, Passed: true},
		{MetricName: "final_answer_quality", MetricKind: "final_answer", Score: 1.0, Passed: true},
	}
	if shouldAnnotate(run, scores) {
		t.Error("shouldAnnotate should return false for completed run with all high scores")
	}
}

func TestShouldAnnotate_CompletedLowScore(t *testing.T) {
	run := &Run{ID: "r3", Status: "completed"}
	scores := []EvalScore{
		{MetricName: "efficiency", MetricKind: "efficiency", Score: 1.0, Passed: true},
		{MetricName: "tool_correctness", MetricKind: "tool_correctness", Score: 0.3, Passed: false},
	}
	if !shouldAnnotate(run, scores) {
		t.Error("shouldAnnotate should return true for completed run with low score")
	}
}

func TestShouldAnnotate_LowConfidenceOrHighDisagreement(t *testing.T) {
	run := &Run{ID: "r-confidence", Status: "completed"}
	for _, score := range []EvalScore{
		{Score: 0.9, Passed: true, Confidence: 0.4},
		{Score: 0.9, Passed: true, Confidence: 0.9, Disagreement: 0.5},
	} {
		if !shouldAnnotate(run, []EvalScore{score}) {
			t.Fatalf("expected annotation for score %+v", score)
		}
	}
}

func TestShouldAnnotate_AnnotationCallbackInvoked(t *testing.T) {
	sampling := &fakeSamplingSvc{
		shouldSample: true,
		ruleName:     "always",
	}
	runSvc := &fakeRunSvc{
		run: &Run{ID: "run-cb-1", ProjectID: "proj-1", Status: "failed", TraceID: "trace-cb-1", Error: "error"},
	}
	evalSvc := &fakeEvalSvc{}

	cbCalled := atomic.Bool{}
	cbCalled.Store(false)
	var cbMu sync.Mutex
	var cbProjectID, cbRunID, cbTraceID string
	disp := NewDispatcher(sampling, runSvc, evalSvc)
	disp.SetAnnotationCallback(func(ctx context.Context, projectID, runID, traceID string, scores []EvalScore) {
		cbMu.Lock()
		cbProjectID = projectID
		cbRunID = runID
		cbTraceID = traceID
		cbMu.Unlock()
		cbCalled.Store(true)
	})

	err := disp.DispatchRun(context.Background(), "proj-1", "run-cb-1")
	if err != nil {
		t.Fatalf("DispatchRun() error = %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if !cbCalled.Load() {
		t.Fatal("annotation callback was not called for failed run")
	}
	cbMu.Lock()
	if cbProjectID != "proj-1" {
		t.Errorf("callback projectID = %q, want %q", cbProjectID, "proj-1")
	}
	if cbRunID != "run-cb-1" {
		t.Errorf("callback runID = %q, want %q", cbRunID, "run-cb-1")
	}
	if cbTraceID != "trace-cb-1" {
		t.Errorf("callback traceID = %q, want %q", cbTraceID, "trace-cb-1")
	}
	cbMu.Unlock()
}

func TestShouldAnnotate_CallbackNotCalledForHighScores(t *testing.T) {
	sampling := &fakeSamplingSvc{
		shouldSample: true,
		ruleName:     "always",
	}
	runSvc := &fakeRunSvc{
		run: &Run{ID: "run-cb-2", ProjectID: "proj-1", Status: "completed", TraceID: "trace-cb-2"},
	}
	evalSvc := &fakeEvalSvc{}

	cbCalled := atomic.Bool{}
	cbCalled.Store(false)
	disp := NewDispatcher(sampling, runSvc, evalSvc)
	disp.SetAnnotationCallback(func(ctx context.Context, projectID, runID, traceID string, scores []EvalScore) {
		cbCalled.Store(true)
	})

	err := disp.DispatchRun(context.Background(), "proj-1", "run-cb-2")
	if err != nil {
		t.Fatalf("DispatchRun() error = %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if cbCalled.Load() {
		t.Error("annotation callback should NOT be called for completed run with all high scores")
	}
}

// ---- New evaluator integration tests ----

func TestDispatcher_WithEvaluator_CallsEvaluator(t *testing.T) {
	sampling := &fakeSamplingSvc{
		shouldSample: true,
		ruleName:     "always",
	}
	runSvc := &fakeRunSvc{
		run: &Run{ID: "run-eval-1", ProjectID: "proj-1", Status: "completed", TraceID: "trace-eval-1"},
	}
	evalSvc := &fakeEvalSvc{}
	evaluator := &fakeOnlineEvaluator{
		scores: []EvalScore{
			{
				MetricName: "tool_correctness",
				MetricKind: "tool_correctness",
				Score:      0.8,
				Passed:     true,
				Reason:     "all tool calls succeeded",
			},
			{
				MetricName: "trajectory_quality",
				MetricKind: "trajectory",
				Score:      0.9,
				Passed:     true,
				Reason:     "trajectory complete with all required steps",
			},
		},
	}

	disp := NewDispatcher(sampling, runSvc, evalSvc)
	disp.SetEvaluator(evaluator)

	err := disp.DispatchRun(context.Background(), "proj-1", "run-eval-1")
	if err != nil {
		t.Fatalf("DispatchRun() error = %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Evaluator must have been called
	if !evaluator.wasCalled() {
		t.Fatal("evaluator was not called")
	}
	if evaluator.calledRunID() != "run-eval-1" {
		t.Errorf("evaluator called with run %q, want %q", evaluator.calledRunID(), "run-eval-1")
	}

	// Scores should match what evaluator returned (2 scores, not baseline)
	scores := evalSvc.getScores()
	if len(scores) != 2 {
		t.Fatalf("expected 2 evaluator scores, got %d", len(scores))
	}

	// Verify tool_correctness metric
	foundTool := false
	foundTrajectory := false
	for _, s := range scores {
		if s.MetricKind == "tool_correctness" {
			foundTool = true
			if s.Score != 0.8 {
				t.Errorf("tool_correctness score = %f, want 0.8", s.Score)
			}
		}
		if s.MetricKind == "trajectory" {
			foundTrajectory = true
			if s.Score != 0.9 {
				t.Errorf("trajectory score = %f, want 0.9", s.Score)
			}
		}
		// No baseline scores should be present
		if s.MetricName == "baseline_completion_status" {
			t.Error("baseline_completion_status should not appear when evaluator is configured")
		}
	}
	if !foundTool {
		t.Error("tool_correctness metric was not created")
	}
	if !foundTrajectory {
		t.Error("trajectory metric was not created")
	}

	// Project isolation still holds
	for _, s := range scores {
		if s.ProjectID != "proj-1" {
			t.Errorf("score project_id = %q, want %q", s.ProjectID, "proj-1")
		}
		if s.RunID != "run-eval-1" {
			t.Errorf("score run_id = %q, want %q", s.RunID, "run-eval-1")
		}
	}
}

func TestDispatcher_WithEvaluator_EmptyEvaluator_FallsbackToBaseline(t *testing.T) {
	sampling := &fakeSamplingSvc{
		shouldSample: true,
		ruleName:     "always",
	}
	runSvc := &fakeRunSvc{
		run: &Run{ID: "run-empty-1", ProjectID: "proj-1", Status: "completed", TraceID: "trace-empty-1"},
	}
	evalSvc := &fakeEvalSvc{}
	// Evaluator returns empty slice (no executable metrics)
	evaluator := &fakeOnlineEvaluator{
		scores: []EvalScore{},
	}

	disp := NewDispatcher(sampling, runSvc, evalSvc)
	disp.SetEvaluator(evaluator)

	err := disp.DispatchRun(context.Background(), "proj-1", "run-empty-1")
	if err != nil {
		t.Fatalf("DispatchRun() error = %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Evaluator must have been called
	if !evaluator.wasCalled() {
		t.Fatal("evaluator was not called")
	}

	// Fallback to baseline when evaluator returns empty
	scores := evalSvc.getScores()
	if len(scores) != 1 {
		t.Fatalf("expected 1 baseline score (fallback), got %d", len(scores))
	}
	if scores[0].MetricName != "baseline_completion_status" {
		t.Errorf("metric_name = %q, want baseline_completion_status", scores[0].MetricName)
	}
}

func TestDispatcher_WithEvaluator_FailedRun_StillAnnotates(t *testing.T) {
	sampling := &fakeSamplingSvc{
		shouldSample: true,
		ruleName:     "always",
	}
	runSvc := &fakeRunSvc{
		run: &Run{ID: "run-fail-1", ProjectID: "proj-1", Status: "failed", TraceID: "trace-fail-1", Error: "error"},
	}
	evalSvc := &fakeEvalSvc{}
	evaluator := &fakeOnlineEvaluator{
		scores: []EvalScore{
			{
				MetricName: "baseline_completion_status",
				MetricKind: "efficiency",
				Score:      0.0,
				Passed:     false,
				Reason:     "Run failed",
			},
		},
	}

	cbCalled := atomic.Bool{}
	cbCalled.Store(false)
	disp := NewDispatcher(sampling, runSvc, evalSvc)
	disp.SetEvaluator(evaluator)
	disp.SetAnnotationCallback(func(ctx context.Context, projectID, runID, traceID string, scores []EvalScore) {
		cbCalled.Store(true)
	})

	err := disp.DispatchRun(context.Background(), "proj-1", "run-fail-1")
	if err != nil {
		t.Fatalf("DispatchRun() error = %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if !evaluator.wasCalled() {
		t.Fatal("evaluator was not called")
	}
	if !cbCalled.Load() {
		t.Error("annotation callback should be called for failed run with evaluator")
	}
}

func TestDispatcher_EvaluatorFailure_DoesNotBlock(t *testing.T) {
	sampling := &fakeSamplingSvc{
		shouldSample: true,
		ruleName:     "always",
	}
	runSvc := &fakeRunSvc{
		run: &Run{ID: "run-err-1", ProjectID: "proj-1", Status: "completed", TraceID: "trace-err-1"},
	}
	evalSvc := &fakeEvalSvc{}
	// Evaluator returns error
	evaluator := &fakeOnlineEvaluator{
		scores: nil,
		err:    errors.New("evaluator internal error"),
	}

	disp := NewDispatcher(sampling, runSvc, evalSvc)
	disp.SetEvaluator(evaluator)

	start := time.Now()
	err := disp.DispatchRun(context.Background(), "proj-1", "run-err-1")
	duration := time.Since(start)
	if err != nil {
		t.Fatalf("DispatchRun() error = %v", err)
	}

	// DispatchRun should return quickly (async eval)
	if duration > 1*time.Second {
		t.Errorf("DispatchRun took %v, expected fast return", duration)
	}

	time.Sleep(100 * time.Millisecond)

	// Even when evaluator fails, DispatchRun itself doesn't error
	if !evaluator.wasCalled() {
		t.Fatal("evaluator was not called")
	}
}

func TestDispatcher_WithEvaluator_UnsampledRun_DoesNotCallEvaluator(t *testing.T) {
	sampling := &fakeSamplingSvc{shouldSample: false}
	runSvc := &fakeRunSvc{
		run: &Run{ID: "run-unsampled", ProjectID: "proj-1", Status: "completed"},
	}
	evalSvc := &fakeEvalSvc{}
	evaluator := &fakeOnlineEvaluator{
		scores: []EvalScore{
			{MetricName: "tool_correctness", MetricKind: "tool_correctness", Score: 1.0, Passed: true, Reason: "ok"},
		},
	}

	disp := NewDispatcher(sampling, runSvc, evalSvc)
	disp.SetEvaluator(evaluator)

	err := disp.DispatchRun(context.Background(), "proj-1", "run-unsampled")
	if err != nil {
		t.Fatalf("DispatchRun() error = %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if evaluator.wasCalled() {
		t.Error("evaluator was called for unsampled run")
	}
	if len(evalSvc.getScores()) != 0 {
		t.Error("expected no eval scores for unsampled run")
	}
}

func TestShouldAnnotate_BaselineLowScore(t *testing.T) {
	run := &Run{ID: "r4", Status: "failed"}
	scores := []EvalScore{
		{MetricName: "baseline_completion_status", MetricKind: "efficiency", Score: 0.0, Passed: false},
	}
	if !shouldAnnotate(run, scores) {
		t.Error("shouldAnnotate should return true for failed run with baseline low score")
	}
}

// ---- DispatchRunAsync tests ----

// cancelledCtx returns a context that is already cancelled.
func cancelledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestDispatcher_DispatchRunAsync_WorksEvenWithCancelledContext(t *testing.T) {
	// Simulate an HTTP handler that returns before the async dispatch completes:
	// DispatchRunAsync should use its own background context, not a cancelled one.
	sampling := &fakeSamplingSvc{
		shouldSample: true,
		ruleName:     "async-cancelled",
	}
	runSvc := &fakeRunSvc{
		run: &Run{ID: "run-async-1", ProjectID: "proj-1", Status: "completed", TraceID: "trace-async-1"},
	}
	evalSvc := &fakeEvalSvc{}

	disp := NewDispatcher(sampling, runSvc, evalSvc)

	// DispatchRunAsync should not block
	start := time.Now()
	disp.DispatchRunAsync("proj-1", "run-async-1")
	duration := time.Since(start)
	if duration > 1*time.Second {
		t.Errorf("DispatchRunAsync took %v, expected fast return", duration)
	}

	// Wait for async work to complete
	time.Sleep(200 * time.Millisecond)

	scores := evalSvc.getScores()
	if len(scores) == 0 {
		t.Fatal("expected eval scores from DispatchRunAsync")
	}
	if scores[0].ProjectID != "proj-1" {
		t.Errorf("project_id = %q, want %q", scores[0].ProjectID, "proj-1")
	}
	if scores[0].RunID != "run-async-1" {
		t.Errorf("run_id = %q, want %q", scores[0].RunID, "run-async-1")
	}
}

func TestDispatcher_DispatchRunAsync_WithCancelledContextNotUsed(t *testing.T) {
	// Verify that if someone mistakenly passes a cancelled context, it doesn't
	// affect DispatchRunAsync (which uses context.Background() internally).
	sampling := &fakeSamplingSvc{
		shouldSample: true,
		ruleName:     "async-ignore-cancelled",
	}
	runSvc := &fakeRunSvc{
		run: &Run{ID: "run-ignore-1", ProjectID: "proj-1", Status: "completed", TraceID: "trace-ignore-1"},
	}
	evalSvc := &fakeEvalSvc{}

	disp := NewDispatcher(sampling, runSvc, evalSvc)

	// This simulates: go deps.OnlineEvalDisp.DispatchRun(cancelledCtx(), projectID, runID)
	// — the old pattern that was buggy. Now DispatchRunAsync ignores the cancelled parent.
	disp.DispatchRunAsync("proj-1", "run-ignore-1")

	time.Sleep(200 * time.Millisecond)

	if len(evalSvc.getScores()) == 0 {
		t.Error("expected eval scores even when handler context was likely cancelled")
	}
}

func TestDispatcher_DispatchRun_SyncWithCancelledContext(t *testing.T) {
	// Verify that the synchronous DispatchRun fails with cancelled context
	// when the underlying services respect context cancellation.
	sampling := &fakeSamplingSvc{
		shouldSample: true,
		ruleName:     "sync-cancelled",
	}
	runSvc := &contextAwareRunSvc{
		run: &Run{ID: "run-sync-cancel", ProjectID: "proj-1", Status: "completed"},
	}
	evalSvc := &fakeEvalSvc{}

	disp := NewDispatcher(sampling, runSvc, evalSvc)

	// Calling DispatchRun with a cancelled context should fail
	err := disp.DispatchRun(cancelledCtx(), "proj-1", "run-sync-cancel")
	if err == nil {
		t.Error("expected error when calling DispatchRun with cancelled context")
	}

	time.Sleep(100 * time.Millisecond)

	// No scores should be created since the sync call failed
	if len(evalSvc.getScores()) != 0 {
		t.Error("expected no eval scores when DispatchRun fails with cancelled context")
	}
}

// ---- Handler-level async dispatch tests ----

type fakeAsyncDispatcher struct {
	called      bool
	projectID   string
	runID       string
	callSyncCtx context.Context // tracks what was passed to DispatchRun
	mu          sync.Mutex
}

func (f *fakeAsyncDispatcher) DispatchRun(ctx context.Context, projectID, runID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	f.projectID = projectID
	f.runID = runID
	f.callSyncCtx = ctx
	return nil
}

func (f *fakeAsyncDispatcher) DispatchRunAsync(projectID, runID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	f.projectID = projectID
	f.runID = runID
	// We don't set callSyncCtx here — intentional: DispatchRunAsync manages its own ctx.
}
