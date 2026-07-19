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
}

func (f *fakeEvalSvc) CreateEvalScore(ctx context.Context, score EvalScore) error {
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

	// Should have created at least one eval score
	if len(evalSvc.createdScores) == 0 {
		t.Error("expected at least 1 eval score, got 0")
	}

	if len(evalSvc.createdScores) > 0 {
		// Verify the eval score has correct project/run info
		score := evalSvc.createdScores[0]
		if score.ProjectID != "proj-1" {
			t.Errorf("project_id = %q, want %q", score.ProjectID, "proj-1")
		}
		if score.RunID != "run-1" {
			t.Errorf("run_id = %q, want %q", score.RunID, "run-1")
		}
		if score.TraceID != "trace-1" {
			t.Errorf("trace_id = %q, want %q", score.TraceID, "trace-1")
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
