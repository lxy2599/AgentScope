package onlinerval

import (
	"context"
	"errors"
	"sync"
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
	if len(evalSvc.createdScores) != 0 {
		t.Errorf("expected 0 eval scores, got %d", len(evalSvc.createdScores))
	}
}

func TestDispatcher_DispatchRun_Sampled(t *testing.T) {
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

	// Should have created multiple multi-dimensional eval scores
	scores := evalSvc.createdScores
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

	// Verify multi-dimensional scores: should contain efficiency, final_answer, tool_correctness, etc.
	metricKinds := make(map[string]float64)
	for _, s := range scores {
		metricKinds[s.MetricKind] = s.Score
	}
	expectedKinds := []string{"efficiency", "final_answer", "tool_correctness", "safety", "trajectory", "rag_groundedness"}
	for _, kind := range expectedKinds {
		if _, ok := metricKinds[kind]; !ok {
			t.Errorf("missing metric kind %q in scores", kind)
		}
	}

	// All scores should be 1.0 for a completed run
	for _, s := range scores {
		if s.Passed != true {
			t.Errorf("metric %s/%s: passed = false, want true", s.MetricKind, s.MetricName)
		}
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

	if len(evalSvc.createdScores) != 0 {
		t.Errorf("expected 0 eval scores for sample_rate=0, got %d", len(evalSvc.createdScores))
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

	if len(evalSvc.createdScores) == 0 {
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

	if len(evalSvc.createdScores) == 0 {
		t.Error("expected eval scores from async dispatch")
	}
}

func TestDispatcher_FailedRun_MultiDimensionalScores(t *testing.T) {
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

	scores := evalSvc.createdScores
	if len(scores) == 0 {
		t.Fatal("expected eval scores for failed run, got 0")
	}

	// Verify multi-dimensional scores
	metricKinds := make(map[string]bool)
	for _, s := range scores {
		metricKinds[s.MetricKind] = true
	}
	expectedKinds := []string{"efficiency", "final_answer", "tool_correctness", "safety", "trajectory", "rag_groundedness"}
	for _, kind := range expectedKinds {
		if !metricKinds[kind] {
			t.Errorf("missing metric kind %q in scores", kind)
		}
	}

	// efficiency should be 0.0 (failed)
	for _, s := range scores {
		if s.MetricKind == "efficiency" {
			if s.Score != 0.0 {
				t.Errorf("efficiency score = %f, want 0.0", s.Score)
			}
			if s.Passed {
				t.Error("efficiency passed = true, want false")
			}
		}
	}

	// tool_correctness should reflect the tool error
	for _, s := range scores {
		if s.MetricKind == "tool_correctness" {
			if s.Score != 0.0 {
				t.Errorf("tool_correctness score = %f, want 0.0", s.Score)
			}
			if s.Passed {
				t.Error("tool_correctness passed = true, want false")
			}
		}
	}

	// Project isolation still holds
	for _, s := range scores {
		if s.ProjectID != "proj-1" {
			t.Errorf("score has project_id = %q, want %q", s.ProjectID, "proj-1")
		}
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

	for _, score := range evalSvc.createdScores {
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

	if len(evalSvc.createdScores) == 0 {
		t.Fatal("expected eval scores for failed run")
	}

	for _, score := range evalSvc.createdScores {
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

func TestShouldAnnotate_AnnotationCallbackInvoked(t *testing.T) {
	sampling := &fakeSamplingSvc{
		shouldSample: true,
		ruleName:     "always",
	}
	runSvc := &fakeRunSvc{
		run: &Run{ID: "run-cb-1", ProjectID: "proj-1", Status: "failed", TraceID: "trace-cb-1", Error: "error"},
	}
	evalSvc := &fakeEvalSvc{}

	cbCalled := false
	var cbProjectID, cbRunID, cbTraceID string
	disp := NewDispatcher(sampling, runSvc, evalSvc)
	disp.SetAnnotationCallback(func(ctx context.Context, projectID, runID, traceID string, scores []EvalScore) {
		cbCalled = true
		cbProjectID = projectID
		cbRunID = runID
		cbTraceID = traceID
	})

	err := disp.DispatchRun(context.Background(), "proj-1", "run-cb-1")
	if err != nil {
		t.Fatalf("DispatchRun() error = %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if !cbCalled {
		t.Fatal("annotation callback was not called for failed run")
	}
	if cbProjectID != "proj-1" {
		t.Errorf("callback projectID = %q, want %q", cbProjectID, "proj-1")
	}
	if cbRunID != "run-cb-1" {
		t.Errorf("callback runID = %q, want %q", cbRunID, "run-cb-1")
	}
	if cbTraceID != "trace-cb-1" {
		t.Errorf("callback traceID = %q, want %q", cbTraceID, "trace-cb-1")
	}
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

	cbCalled := false
	disp := NewDispatcher(sampling, runSvc, evalSvc)
	disp.SetAnnotationCallback(func(ctx context.Context, projectID, runID, traceID string, scores []EvalScore) {
		cbCalled = true
	})

	err := disp.DispatchRun(context.Background(), "proj-1", "run-cb-2")
	if err != nil {
		t.Fatalf("DispatchRun() error = %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if cbCalled {
		t.Error("annotation callback should NOT be called for completed run with all high scores")
	}
}
