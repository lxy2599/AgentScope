package evals

import (
	"context"
	"errors"
	"testing"
)

// fakeQuerier implements Querier for testing.
type fakeQuerier struct {
	defs    map[string]MetricDefinition
	scores  map[string][]EvalScore
	agg     map[string][]MetricKindAggregation
	counter int
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{
		defs:   make(map[string]MetricDefinition),
		scores: make(map[string][]EvalScore),
		agg:    make(map[string][]MetricKindAggregation),
	}
}

func (f *fakeQuerier) CreateMetricDefinition(ctx context.Context, projectID, name, kind, config string) (MetricDefinition, error) {
	f.counter++
	id := "def-" + itoa(f.counter)
	d := MetricDefinition{
		ID:        id,
		ProjectID: projectID,
		Name:      name,
		Kind:      kind,
		Config:    config,
	}
	f.defs[id] = d
	return d, nil
}

func (f *fakeQuerier) ListMetricDefinitions(ctx context.Context, projectID string) ([]MetricDefinition, error) {
	var result []MetricDefinition
	for _, d := range f.defs {
		if d.ProjectID == projectID {
			result = append(result, d)
		}
	}
	if result == nil {
		return []MetricDefinition{}, nil
	}
	return result, nil
}

func (f *fakeQuerier) GetMetricDefinition(ctx context.Context, definitionID string) (MetricDefinition, error) {
	d, ok := f.defs[definitionID]
	if !ok {
		return MetricDefinition{}, errors.New("not found")
	}
	return d, nil
}

func (f *fakeQuerier) CreateEvalScore(ctx context.Context, score EvalScore) (EvalScore, error) {
	f.counter++
	id := "score-" + itoa(f.counter)
	score.ID = id
	f.scores[score.RunID] = append(f.scores[score.RunID], score)
	return score, nil
}

func (f *fakeQuerier) ListEvalScoresByRun(ctx context.Context, runID string) ([]EvalScore, error) {
	return f.scores[runID], nil
}

func (f *fakeQuerier) ListEvalScoresByProject(ctx context.Context, projectID string) ([]EvalScore, error) {
	var result []EvalScore
	for _, scores := range f.scores {
		for _, s := range scores {
			if s.ProjectID == projectID {
				result = append(result, s)
			}
		}
	}
	return result, nil
}

func (f *fakeQuerier) AggregateEvalScoresByExperiment(ctx context.Context, experimentID string) ([]MetricKindAggregation, error) {
	return f.agg[experimentID], nil
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}

func TestCreateMetricDefinition(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		d, err := svc.CreateMetricDefinition(ctx, "proj-1", "final-answer", "final_answer", `{"type":"exact"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if d.Name != "final-answer" {
			t.Errorf("name = %q", d.Name)
		}
		if d.Kind != "final_answer" {
			t.Errorf("kind = %q", d.Kind)
		}
		if d.ProjectID != "proj-1" {
			t.Errorf("project_id = %q", d.ProjectID)
		}
	})

	t.Run("invalid_kind", func(t *testing.T) {
		_, err := svc.CreateMetricDefinition(ctx, "proj-1", "invalid", "unknown_kind", "{}")
		if err == nil {
			t.Fatal("expected error for unsupported kind")
		}
	})

	t.Run("missing_project_id", func(t *testing.T) {
		_, err := svc.CreateMetricDefinition(ctx, "", "test", "final_answer", "{}")
		if err == nil {
			t.Fatal("expected error for empty projectID")
		}
	})

	t.Run("missing_name", func(t *testing.T) {
		_, err := svc.CreateMetricDefinition(ctx, "proj-1", "", "final_answer", "{}")
		if err == nil {
			t.Fatal("expected error for empty name")
		}
	})
}

func TestListMetricDefinitions(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)
	ctx := context.Background()

	// Create definitions in two projects
	svc.CreateMetricDefinition(ctx, "proj-a", "m1", "final_answer", "{}")
	svc.CreateMetricDefinition(ctx, "proj-a", "m2", "tool_correctness", "{}")
	svc.CreateMetricDefinition(ctx, "proj-b", "m3", "safety", "{}")

	t.Run("list_project_a", func(t *testing.T) {
		defs, err := svc.ListMetricDefinitions(ctx, "proj-a")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(defs) != 2 {
			t.Errorf("got %d definitions, want 2", len(defs))
		}
	})

	t.Run("list_project_b", func(t *testing.T) {
		defs, err := svc.ListMetricDefinitions(ctx, "proj-b")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(defs) != 1 {
			t.Errorf("got %d definitions, want 1", len(defs))
		}
	})

	t.Run("empty_result", func(t *testing.T) {
		defs, err := svc.ListMetricDefinitions(ctx, "proj-empty")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(defs) != 0 {
			t.Errorf("expected empty list, got %d", len(defs))
		}
	})

	t.Run("missing_project_id", func(t *testing.T) {
		_, err := svc.ListMetricDefinitions(ctx, "")
		if err == nil {
			t.Fatal("expected error for empty projectID")
		}
	})
}

func TestCreateEvalScore(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		s, err := svc.CreateEvalScore(ctx, EvalScore{
			ProjectID:  "proj-1",
			RunID:      "run-1",
			TraceID:    "trace-abc",
			MetricName: "final-answer-quality",
			MetricKind: "final_answer",
			Score:      0.95,
			Passed:     true,
			Reason:     "Exact match",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if s.ID == "" {
			t.Fatal("score ID not set")
		}
		if s.Score != 0.95 {
			t.Errorf("score = %f, want 0.95", s.Score)
		}
		if !s.Passed {
			t.Error("expected passed to be true")
		}
	})

	t.Run("score_out_of_range", func(t *testing.T) {
		_, err := svc.CreateEvalScore(ctx, EvalScore{
			ProjectID:  "proj-1",
			RunID:      "run-1",
			MetricName: "test",
			MetricKind: "final_answer",
			Score:      1.5,
		})
		if err == nil {
			t.Fatal("expected error for score > 1.0")
		}
	})

	t.Run("negative_score", func(t *testing.T) {
		_, err := svc.CreateEvalScore(ctx, EvalScore{
			ProjectID:  "proj-1",
			RunID:      "run-1",
			MetricName: "test",
			MetricKind: "final_answer",
			Score:      -0.1,
		})
		if err == nil {
			t.Fatal("expected error for negative score")
		}
	})

	t.Run("invalid_kind", func(t *testing.T) {
		_, err := svc.CreateEvalScore(ctx, EvalScore{
			ProjectID:  "proj-1",
			RunID:      "run-1",
			MetricName: "test",
			MetricKind: "invalid_kind",
			Score:      0.5,
		})
		if err == nil {
			t.Fatal("expected error for invalid kind")
		}
	})

	t.Run("missing_project_id", func(t *testing.T) {
		_, err := svc.CreateEvalScore(ctx, EvalScore{
			RunID:      "run-1",
			MetricName: "test",
			MetricKind: "final_answer",
			Score:      0.5,
		})
		if err == nil {
			t.Fatal("expected error for empty projectID")
		}
	})

	t.Run("missing_run_id", func(t *testing.T) {
		_, err := svc.CreateEvalScore(ctx, EvalScore{
			ProjectID:  "proj-1",
			MetricName: "test",
			MetricKind: "final_answer",
			Score:      0.5,
		})
		if err == nil {
			t.Fatal("expected error for empty runID")
		}
	})
}

func TestListEvalScoresByRun(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)
	ctx := context.Background()

	// Create scores for two runs
	svc.CreateEvalScore(ctx, EvalScore{
		ProjectID: "proj-1", RunID: "run-1",
		MetricName: "final-answer", MetricKind: "final_answer",
		Score: 0.9, Passed: true, Reason: "good",
	})
	svc.CreateEvalScore(ctx, EvalScore{
		ProjectID: "proj-1", RunID: "run-1",
		MetricName: "tool", MetricKind: "tool_correctness",
		Score: 0.5, Passed: false, Reason: "bad",
	})
	svc.CreateEvalScore(ctx, EvalScore{
		ProjectID: "proj-1", RunID: "run-2",
		MetricName: "safety", MetricKind: "safety",
		Score: 1.0, Passed: true, Reason: "ok",
	})

	t.Run("list_run_1", func(t *testing.T) {
		scores, err := svc.ListEvalScoresByRun(ctx, "run-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(scores) != 2 {
			t.Errorf("got %d scores, want 2", len(scores))
		}
	})

	t.Run("list_run_2", func(t *testing.T) {
		scores, err := svc.ListEvalScoresByRun(ctx, "run-2")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(scores) != 1 {
			t.Errorf("got %d scores, want 1", len(scores))
		}
	})

	t.Run("empty_result", func(t *testing.T) {
		scores, err := svc.ListEvalScoresByRun(ctx, "run-nonexistent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(scores) != 0 {
			t.Errorf("expected 0 scores, got %d", len(scores))
		}
	})

	t.Run("missing_run_id", func(t *testing.T) {
		_, err := svc.ListEvalScoresByRun(ctx, "")
		if err == nil {
			t.Fatal("expected error for empty runID")
		}
	})
}

func TestAggregateEvalScoresByExperiment(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)
	ctx := context.Background()

	t.Run("empty_result", func(t *testing.T) {
		agg, err := svc.AggregateEvalScoresByExperiment(ctx, "exp-empty")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(agg) != 0 {
			t.Errorf("expected 0 aggregations, got %d", len(agg))
		}
	})

	t.Run("missing_experiment_id", func(t *testing.T) {
		_, err := svc.AggregateEvalScoresByExperiment(ctx, "")
		if err == nil {
			t.Fatal("expected error for empty experimentID")
		}
	})
}
