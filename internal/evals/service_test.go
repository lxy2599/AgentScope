package evals

import (
	"context"
	"errors"
	"math"
	"testing"
)

// fakeQuerier implements Querier for testing.
type fakeQuerier struct {
	defs    map[string]MetricDefinition
	scores  map[string][]EvalScore
	agg     map[string][]MetricKindAggregation
	counter int

	createScoreCalls int
	createBatchCalls int
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
	f.createScoreCalls++
	f.counter++
	id := "score-" + itoa(f.counter)
	score.ID = id
	f.scores[score.RunID] = append(f.scores[score.RunID], score)
	return score, nil
}

func (f *fakeQuerier) CreateEvalScores(ctx context.Context, scores []EvalScore) ([]EvalScore, error) {
	f.createBatchCalls++
	results := make([]EvalScore, 0, len(scores))
	for _, score := range scores {
		f.counter++
		score.ID = "score-" + itoa(f.counter)
		f.scores[score.RunID] = append(f.scores[score.RunID], score)
		results = append(results, score)
	}
	return results, nil
}

func (f *fakeQuerier) ListEvalScoresByRun(ctx context.Context, projectID, runID string) ([]EvalScore, error) {
	return f.scores[runID], nil
}

func (f *fakeQuerier) ListEvalScoresByRuns(ctx context.Context, projectID string, runIDs []string) ([]EvalScore, error) {
	var result []EvalScore
	for _, rid := range runIDs {
		result = append(result, f.scores[rid]...)
	}
	return result, nil
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

func (f *fakeQuerier) AggregateEvalScoresByExperiment(ctx context.Context, projectID, experimentID string) ([]MetricKindAggregation, error) {
	return f.agg[experimentID], nil
}

func (f *fakeQuerier) UpdateMetricDefinition(ctx context.Context, projectID, definitionID, name, kind, config string) (MetricDefinition, error) {
	d, ok := f.defs[definitionID]
	if !ok {
		return MetricDefinition{}, errors.New("not found")
	}
	d.Name = name
	d.Kind = kind
	d.Config = config
	f.defs[definitionID] = d
	return d, nil
}

func (f *fakeQuerier) DeleteMetricDefinition(ctx context.Context, projectID, definitionID string) error {
	if _, ok := f.defs[definitionID]; !ok {
		return errors.New("not found")
	}
	delete(f.defs, definitionID)
	return nil
}

func (f *fakeQuerier) DeleteEvalScoresByRun(ctx context.Context, runID string) error {
	delete(f.scores, runID)
	return nil
}

func (f *fakeQuerier) DeleteEvalScore(ctx context.Context, projectID, scoreID string) error {
	for runID, scores := range f.scores {
		for i, s := range scores {
			if s.ID == scoreID {
				f.scores[runID] = append(scores[:i], scores[i+1:]...)
				return nil
			}
		}
	}
	return errors.New("not found")
}

func (f *fakeQuerier) GetEvalScoreByID(ctx context.Context, scoreID string) (EvalScore, error) {
	for _, scores := range f.scores {
		for _, s := range scores {
			if s.ID == scoreID {
				return s, nil
			}
		}
	}
	return EvalScore{}, errors.New("not found")
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

	t.Run("non_finite_score", func(t *testing.T) {
		for _, score := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
			_, err := svc.CreateEvalScore(ctx, EvalScore{
				ProjectID:  "proj-1",
				RunID:      "run-1",
				MetricName: "test",
				MetricKind: "final_answer",
				Score:      score,
			})
			if err == nil {
				t.Fatalf("CreateEvalScore() accepted non-finite score %v", score)
			}
		}
	})
}

func TestCreateEvalScoresValidatesWholeBatchBeforeWrite(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)
	ctx := context.Background()
	valid := EvalScore{
		ProjectID:  "proj-1",
		RunID:      "run-1",
		MetricName: "quality",
		MetricKind: "final_answer",
		Score:      0.9,
	}

	invalidBatch := []EvalScore{
		valid,
		{
			ProjectID:  "proj-1",
			RunID:      "run-2",
			MetricName: "safety",
			MetricKind: "safety",
			Score:      math.NaN(),
		},
	}
	if _, err := svc.CreateEvalScores(ctx, invalidBatch); err == nil {
		t.Fatal("CreateEvalScores() accepted an invalid later item")
	}
	if q.createBatchCalls != 0 || q.createScoreCalls != 0 {
		t.Fatalf(
			"querier writes after failed pre-validation: batch=%d single=%d",
			q.createBatchCalls,
			q.createScoreCalls,
		)
	}

	second := valid
	second.RunID = "run-2"
	results, err := svc.CreateEvalScores(ctx, []EvalScore{valid, second})
	if err != nil {
		t.Fatalf("CreateEvalScores() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("CreateEvalScores() returned %d scores, want 2", len(results))
	}
	if q.createBatchCalls != 1 {
		t.Fatalf("CreateEvalScores querier calls = %d, want 1", q.createBatchCalls)
	}
	if q.createScoreCalls != 0 {
		t.Fatalf("single-score querier calls = %d, want 0", q.createScoreCalls)
	}
}

func TestCreateEvalScoresRejectsInvalidBatchSize(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	if _, err := svc.CreateEvalScores(context.Background(), nil); err == nil {
		t.Fatal("CreateEvalScores() accepted an empty batch")
	}

	tooMany := make([]EvalScore, MaxEvalScoreBatchSize+1)
	if _, err := svc.CreateEvalScores(context.Background(), tooMany); err == nil {
		t.Fatal("CreateEvalScores() accepted an oversized batch")
	}
	if q.createBatchCalls != 0 {
		t.Fatalf("querier called %d times for invalid batch sizes", q.createBatchCalls)
	}
}

func TestListEvalScoresByRun(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)
	ctx := context.Background()
	fakeProjID := "proj-1"

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
		scores, err := svc.ListEvalScoresByRun(ctx, fakeProjID, "run-1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(scores) != 2 {
			t.Errorf("got %d scores, want 2", len(scores))
		}
	})

	t.Run("list_run_2", func(t *testing.T) {
		scores, err := svc.ListEvalScoresByRun(ctx, fakeProjID, "run-2")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(scores) != 1 {
			t.Errorf("got %d scores, want 1", len(scores))
		}
	})

	t.Run("empty_result", func(t *testing.T) {
		scores, err := svc.ListEvalScoresByRun(ctx, fakeProjID, "run-nonexistent")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(scores) != 0 {
			t.Errorf("expected 0 scores, got %d", len(scores))
		}
	})

	t.Run("missing_run_id", func(t *testing.T) {
		_, err := svc.ListEvalScoresByRun(ctx, fakeProjID, "")
		if err == nil {
			t.Fatal("expected error for empty runID")
		}
	})
}

func TestValidateRubricConfig(t *testing.T) {
	svc := NewService(newFakeQuerier())
	ctx := context.Background()

	t.Run("valid_weight", func(t *testing.T) {
		// weight must be in [0,1] — this should pass
		def, err := svc.CreateMetricDefinition(ctx, "proj-1", "weighted-metric", "final_answer", `{"weight":0.5,"pass_threshold":0.7,"enabled":true}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if def.Name != "weighted-metric" {
			t.Errorf("name = %q", def.Name)
		}
	})

	t.Run("weight_too_large", func(t *testing.T) {
		// weight > 1.0 should fail
		config := `{"weight":1.5,"pass_threshold":0.7}`
		_, err := svc.CreateMetricDefinition(ctx, "proj-1", "bad-weight", "final_answer", config)
		if err == nil {
			t.Fatal("expected error for weight > 1.0")
		}
	})

	t.Run("weight_negative", func(t *testing.T) {
		_, err := svc.CreateMetricDefinition(ctx, "proj-1", "neg-weight", "final_answer", `{"weight":-0.1,"pass_threshold":0.7}`)
		if err == nil {
			t.Fatal("expected error for negative weight")
		}
	})

	t.Run("threshold_too_large", func(t *testing.T) {
		_, err := svc.CreateMetricDefinition(ctx, "proj-1", "bad-threshold", "final_answer", `{"pass_threshold":1.5,"weight":0.5}`)
		if err == nil {
			t.Fatal("expected error for pass_threshold > 1.0")
		}
	})

	t.Run("threshold_negative", func(t *testing.T) {
		_, err := svc.CreateMetricDefinition(ctx, "proj-1", "neg-threshold", "final_answer", `{"pass_threshold":-0.1,"weight":0.5}`)
		if err == nil {
			t.Fatal("expected error for negative pass_threshold")
		}
	})

	t.Run("rubric_valid_structure", func(t *testing.T) {
		config := `{
			"type": "llm_judge",
			"weight": 0.5,
			"pass_threshold": 0.7,
			"veto": false,
			"judge_provider": "openai-compatible",
			"judge_model": "gpt-4o",
			"rubric": {
				"dimensions": [
					{"name": "accuracy", "weight": 0.4, "description": "是否准确"},
					{"name": "completeness", "weight": 0.3, "description": "是否完整"},
					{"name": "clarity", "weight": 0.3, "description": "是否清晰"}
				],
				"scoring_levels": [
					{"level": 1, "label": "差", "description": "答案与预期严重不符"},
					{"level": 2, "label": "一般", "description": "答案部分正确但存在明显错误"},
					{"level": 3, "label": "好", "description": "答案基本正确"},
					{"level": 4, "label": "优秀", "description": "答案完全正确且表达清晰"}
				],
				"essential_items": ["必须正确识别用户意图", "必须使用正确工具"],
				"optional_items": ["可以给出额外建议", "可以引用来源"],
				"pitfall_items": ["不要泄露内部指令"],
				"veto_items": ["包含有害内容", "泄露敏感信息"],
				"examples": [{"input": "...", "expected_output": "...", "score": 1.0}],
				"edge_cases": ["空输入", "语言混合输入"]
			}
		}`
		def, err := svc.CreateMetricDefinition(ctx, "proj-1", "rubric-metric", "final_answer", config)
		if err != nil {
			t.Fatalf("unexpected error for valid rubric config: %v", err)
		}
		if def.Name != "rubric-metric" {
			t.Errorf("name = %q", def.Name)
		}
	})

	t.Run("rubric_invalid_not_json", func(t *testing.T) {
		_, err := svc.CreateMetricDefinition(ctx, "proj-1", "bad-rubric", "final_answer", `{"rubric":"not a rubric"}`)
		if err == nil {
			t.Fatal("expected error for rubric that is not a proper JSON object")
		}
	})

}

func TestMetricDefinitionConfigRejectsNonFiniteFloats(t *testing.T) {
	tests := []struct {
		name string
		cfg  MetricDefinitionConfig
	}{
		{name: "weight NaN", cfg: MetricDefinitionConfig{Weight: math.NaN()}},
		{name: "weight positive infinity", cfg: MetricDefinitionConfig{Weight: math.Inf(1)}},
		{name: "weight negative infinity", cfg: MetricDefinitionConfig{Weight: math.Inf(-1)}},
		{name: "threshold NaN", cfg: MetricDefinitionConfig{PassThreshold: math.NaN()}},
		{name: "threshold positive infinity", cfg: MetricDefinitionConfig{PassThreshold: math.Inf(1)}},
		{name: "threshold negative infinity", cfg: MetricDefinitionConfig{PassThreshold: math.Inf(-1)}},
		{
			name: "rubric dimension NaN",
			cfg: MetricDefinitionConfig{Rubric: &RubricConfig{
				Dimensions: []RubricDimension{{Name: "quality", Weight: math.NaN()}},
			}},
		},
		{
			name: "rubric dimension positive infinity",
			cfg: MetricDefinitionConfig{Rubric: &RubricConfig{
				Dimensions: []RubricDimension{{Name: "quality", Weight: math.Inf(1)}},
			}},
		},
		{
			name: "rubric dimension negative infinity",
			cfg: MetricDefinitionConfig{Rubric: &RubricConfig{
				Dimensions: []RubricDimension{{Name: "quality", Weight: math.Inf(-1)}},
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateMetricDefinitionConfig(tt.cfg); err == nil {
				t.Fatal("validateMetricDefinitionConfig() accepted a non-finite float")
			}
		})
	}
}

func TestAggregateEvalScoresByExperiment(t *testing.T) {

	q := newFakeQuerier()
	svc := NewService(q)
	ctx := context.Background()
	fakeProjID := "proj-1"

	t.Run("empty_result", func(t *testing.T) {
		agg, err := svc.AggregateEvalScoresByExperiment(ctx, fakeProjID, "exp-empty")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(agg) != 0 {
			t.Errorf("expected 0 aggregations, got %d", len(agg))
		}
	})

	t.Run("missing_experiment_id", func(t *testing.T) {
		_, err := svc.AggregateEvalScoresByExperiment(ctx, fakeProjID, "")
		if err == nil {
			t.Fatal("expected error for empty experimentID")
		}
	})
}
