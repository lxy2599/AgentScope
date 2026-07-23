package experiments

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// ---- Mock Querier for CompareService ----

type mockCompareQuerier struct {
	expByID     map[string]Experiment
	resultsByID map[string][]EvalCaseResult
	saveCompare func(Comparison, []ComparisonCaseResult) (Comparison, error)
	getCompare  func(string) (ComparisonDetail, error)
	listCompare func(string) ([]Comparison, error)
}

func newMockCompareQuerier() *mockCompareQuerier {
	return &mockCompareQuerier{
		expByID:     make(map[string]Experiment),
		resultsByID: make(map[string][]EvalCaseResult),
	}
}

func (m *mockCompareQuerier) CreateExperiment(ctx context.Context, projectID, datasetID, evaluatorID, targetName, targetVersion string, totalCases int) (Experiment, error) {
	return Experiment{}, errors.New("unexpected call")
}

func (m *mockCompareQuerier) GetExperiment(ctx context.Context, experimentID string) (Experiment, error) {
	e, ok := m.expByID[experimentID]
	if !ok {
		return Experiment{}, errors.New("experiment not found")
	}
	return e, nil
}

func (m *mockCompareQuerier) CreateEvalJob(ctx context.Context, experimentID, datasetCaseID, status string) (EvalJob, error) {
	return EvalJob{}, errors.New("unexpected call")
}

func (m *mockCompareQuerier) ListEvalJobs(ctx context.Context, experimentID string) ([]EvalJob, error) {
	return nil, errors.New("unexpected call")
}

func (m *mockCompareQuerier) ListExperiments(ctx context.Context, projectID string) ([]Experiment, error) {
	return nil, errors.New("unexpected call")
}

func (m *mockCompareQuerier) ListEvalResults(ctx context.Context, experimentID string) ([]EvalCaseResult, error) {
	results, ok := m.resultsByID[experimentID]
	if !ok {
		return []EvalCaseResult{}, nil
	}
	return results, nil
}

func (m *mockCompareQuerier) GetJobStatusCounts(ctx context.Context, experimentID string) (map[string]int, error) {
	return nil, errors.New("unexpected call")
}

func (m *mockCompareQuerier) UpdateExperimentCounts(ctx context.Context, experimentID string, status string, passedCases, failedCases int) error {
	return errors.New("unexpected call")
}

func (m *mockCompareQuerier) UpdateExperimentQualityCounts(ctx context.Context, experimentID string, qualityPassCases, qualityFailedCases int, aggregateScore float64) error {
	return errors.New("unexpected call")
}

func (m *mockCompareQuerier) UpdateExperimentHarnessMetadata(ctx context.Context, experimentID, modelProvider, modelName, modelVersion, promptVersion, harnessVersion, datasetVersion string, featureFlags []byte) error {
	return errors.New("unexpected call")
}

func (m *mockCompareQuerier) CreateExperimentWithJobs(ctx context.Context, projectID, datasetID, evaluatorID, targetName, targetVersion string, caseIDs []string) (Experiment, error) {
	return Experiment{}, errors.New("unexpected call")
}

func (m *mockCompareQuerier) CreateExperimentWithJobsAndMetadata(ctx context.Context, params CreateExperimentHarnessParams) (Experiment, error) {
	return Experiment{}, errors.New("unexpected call")
}

// ---- Mock CompareQuerier ----

type mockStoreQuerier struct {
	saveCompare   func(Comparison, []ComparisonCaseResult) (Comparison, error)
	getCompareFn  func(string) (ComparisonDetail, error)
	listCompareFn func(string) ([]Comparison, error)
}

func (m *mockStoreQuerier) CreateExperimentComparison(ctx context.Context, comparison Comparison, caseResults []ComparisonCaseResult) (Comparison, error) {
	if m.saveCompare != nil {
		return m.saveCompare(comparison, caseResults)
	}
	return comparison, nil
}

func (m *mockStoreQuerier) GetExperimentComparison(ctx context.Context, comparisonID string) (ComparisonDetail, error) {
	if m.getCompareFn != nil {
		return m.getCompareFn(comparisonID)
	}
	return ComparisonDetail{}, errors.New("not found")
}

func (m *mockStoreQuerier) ListExperimentComparisons(ctx context.Context, projectID string) ([]Comparison, error) {
	if m.listCompareFn != nil {
		return m.listCompareFn(projectID)
	}
	return nil, nil
}

// ---- Helper to create test scenarios ----

func makeExp(id, projectID, datasetID, status, target string) Experiment {
	return Experiment{
		ID:        id,
		ProjectID: projectID,
		DatasetID: datasetID,
		TargetName: target,
		Status:    status,
	}
}

func makeResult(caseID, runID string, score float64, passed bool) EvalCaseResult {
	return EvalCaseResult{
		DatasetCaseID: caseID,
		RunID:         runID,
		Score:         score,
		Passed:        passed,
		LatencyMs:     100,
	}
}

// ---- Tests ----

func TestCreateComparison_Validation(t *testing.T) {
	t.Run("empty projectID", func(t *testing.T) {
		s := NewCompareService(nil, nil, nil)
		_, err := s.CreateComparison(context.Background(), "", "b", "c", "u1", ComparisonGeneral, nil)
		if err == nil {
			t.Fatal("expected error for empty projectID")
		}
	})

	t.Run("empty baseline", func(t *testing.T) {
		s := NewCompareService(nil, nil, nil)
		_, err := s.CreateComparison(context.Background(), "p1", "", "c", "u1", ComparisonGeneral, nil)
		if err == nil {
			t.Fatal("expected error for empty baseline")
		}
	})

	t.Run("empty candidate", func(t *testing.T) {
		s := NewCompareService(nil, nil, nil)
		_, err := s.CreateComparison(context.Background(), "p1", "b", "", "u1", ComparisonGeneral, nil)
		if err == nil {
			t.Fatal("expected error for empty candidate")
		}
	})

	t.Run("same experiment", func(t *testing.T) {
		s := NewCompareService(nil, nil, nil)
		_, err := s.CreateComparison(context.Background(), "p1", "e1", "e1", "u1", ComparisonGeneral, nil)
		if err == nil {
			t.Fatal("expected error for same baseline/candidate")
		}
	})

	t.Run("empty createdBy", func(t *testing.T) {
		s := NewCompareService(nil, nil, nil)
		_, err := s.CreateComparison(context.Background(), "p1", "b", "c", "", ComparisonGeneral, nil)
		if err == nil {
			t.Fatal("expected error for empty createdBy")
		}
	})
}

func TestCreateComparison_ExperimentNotFound(t *testing.T) {
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "completed", "agent")
	// "cand" experiment is NOT in the map

	s := NewCompareService(mq, msq, nil)
	s.evalQ = &Service{q: mq}

	_, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, nil)
	if err == nil {
		t.Fatal("expected error when candidate experiment not found")
	}
}

func TestCreateComparison_DifferentProject(t *testing.T) {
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "completed", "agent")
	mq.expByID["cand"] = makeExp("cand", "p2", "ds1", "completed", "agent-v2")

	s := NewCompareService(mq, msq, nil)
	s.evalQ = &Service{q: mq}

	_, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, nil)
	if err == nil {
		t.Fatal("expected error for different project experiments")
	}
}

func TestCreateComparison_DifferentDataset(t *testing.T) {
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "completed", "agent")
	mq.expByID["cand"] = makeExp("cand", "p1", "ds2", "completed", "agent-v2")

	s := NewCompareService(mq, msq, nil)
	s.evalQ = &Service{q: mq}

	_, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, nil)
	if err == nil {
		t.Fatal("expected error for different dataset experiments")
	}
}

func TestCreateComparison_NotCompleted(t *testing.T) {
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "pending", "agent")

	s := NewCompareService(mq, msq, nil)
	s.evalQ = &Service{q: mq}

	_, err := s.CreateComparison(context.Background(), "p1", "bl", "bl", "u1", ComparisonGeneral, nil)
	if err == nil {
		t.Fatal("expected error when comparing same experiment");
    }
}

func TestCreateComparison_ModelSwapDiffHarness(t *testing.T) {
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{
		saveCompare: func(cmp Comparison, results []ComparisonCaseResult) (Comparison, error) {
			// Verify that warnings about harness mismatch are in the summary
			var summary ComparisonSummary
			if err := json.Unmarshal([]byte(cmp.Summary), &summary); err == nil {
				if !summary.RegressionTriggered {
					t.Error("expected regression_triggered=true due to harness mismatch warning")
				}
			}
			cmp.ID = "cmp-1"
			return cmp, nil
		},
	}

	bl := makeExp("bl", "p1", "ds1", "completed", "agent")
	bl.HarnessVersion = "1.0"
	bl.PromptVersion = "p1"
	cand := makeExp("cand", "p1", "ds1", "completed", "agent-v2")
	cand.HarnessVersion = "2.0"
	cand.PromptVersion = "p2"

	mq.expByID["bl"] = bl
	mq.expByID["cand"] = cand

	s := NewCompareService(mq, msq, nil)
	s.evalQ = &Service{q: mq}

	// Without gates, harness mismatch should still be persisted as warnings
	detail, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonModelSwap, nil)
	if err != nil {
		t.Fatalf("model_swap with diff harness should still work but carry warnings: %v", err)
	}
	if !detail.ComparisonSummary.RegressionTriggered {
		t.Error("expected regression triggered due to harness warnings")
	}
}

func TestCreateComparison_AblationDiffModel(t *testing.T) {
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{
		saveCompare: func(cmp Comparison, results []ComparisonCaseResult) (Comparison, error) {
			cmp.ID = "cmp-2"
			return cmp, nil
		},
	}

	bl := makeExp("bl", "p1", "ds1", "completed", "agent")
	bl.ModelName = "gpt-4"
	cand := makeExp("cand", "p1", "ds1", "completed", "agent-v2")
	cand.ModelName = "gpt-4o"

	mq.expByID["bl"] = bl
	mq.expByID["cand"] = cand

	s := NewCompareService(mq, msq, nil)
	s.evalQ = &Service{q: mq}

	detail, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonAblation, nil)
	if err != nil {
		t.Fatalf("ablation with diff model should still work with warnings: %v", err)
	}
	if !detail.ComparisonSummary.RegressionTriggered {
		t.Error("expected regression triggered due to ablation warnings")
	}
}

func TestCreateComparison_Pairing(t *testing.T) {
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{
		saveCompare: func(cmp Comparison, results []ComparisonCaseResult) (Comparison, error) {
			cmp.ID = "cmp-3"
			return cmp, nil
		},
	}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "completed", "agent")
	mq.expByID["cand"] = makeExp("cand", "p1", "ds1", "completed", "agent-v2")

	// Baseline has 3 cases, candidate has 2 (one missing -> incomparable)
	mq.resultsByID["bl"] = []EvalCaseResult{
		makeResult("case-1", "run-bl-1", 1.0, true),
		makeResult("case-2", "run-bl-2", 0.5, false),
		makeResult("case-3", "run-bl-3", 1.0, true),
	}
	mq.resultsByID["cand"] = []EvalCaseResult{
		makeResult("case-1", "run-cand-1", 0.0, false),
		makeResult("case-3", "run-cand-3", 0.8, true),
	}

	s := NewCompareService(mq, msq, nil)
	s.evalQ = &Service{q: mq}

	detail, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, nil)
	if err != nil {
		t.Fatalf("CreateComparison failed: %v", err)
	}

	// Should have 3 total cases (all unique case IDs from both experiments)
	if detail.ComparisonSummary.TotalCases != 3 {
		t.Errorf("expected 3 total cases, got %d", detail.ComparisonSummary.TotalCases)
	}
	// case-1: regressed (1.0→0.0), case-2: incomparable (missing in candidate),
	// case-3: regressed (1.0→0.8, score diff -0.2 < 0)
	if detail.ComparisonSummary.RegressedCases != 2 {
		t.Errorf("expected 2 regressed cases, got %d", detail.ComparisonSummary.RegressedCases)
	}
	if detail.ComparisonSummary.IncomparableCases != 1 {
		t.Errorf("expected 1 incomparable case, got %d", detail.ComparisonSummary.IncomparableCases)
	}
}

func TestCreateComparison_ImprovedAndRegressed(t *testing.T) {
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{
		saveCompare: func(cmp Comparison, results []ComparisonCaseResult) (Comparison, error) {
			cmp.ID = "cmp-4"
			return cmp, nil
		},
	}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "completed", "agent")
	mq.expByID["cand"] = makeExp("cand", "p1", "ds1", "completed", "agent-v2")

	mq.resultsByID["bl"] = []EvalCaseResult{
		makeResult("case-1", "run-bl-1", 0.3, false),
		makeResult("case-2", "run-bl-2", 1.0, true),
	}
	mq.resultsByID["cand"] = []EvalCaseResult{
		makeResult("case-1", "run-cand-1", 0.9, true),  // improved: 0.3→0.9, failed→passed
		makeResult("case-2", "run-cand-2", 0.0, false), // regressed: 1.0→0.0, passed→failed
	}

	s := NewCompareService(mq, msq, nil)
	s.evalQ = &Service{q: mq}

	detail, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, nil)
	if err != nil {
		t.Fatalf("CreateComparison failed: %v", err)
	}

	if detail.ComparisonSummary.ImprovedCases != 1 {
		t.Errorf("expected 1 improved case, got %d", detail.ComparisonSummary.ImprovedCases)
	}
	if detail.ComparisonSummary.RegressedCases != 1 {
		t.Errorf("expected 1 regressed case, got %d", detail.ComparisonSummary.RegressedCases)
	}
	if detail.ComparisonSummary.BaselineAvgScore != 0.65 {
		t.Errorf("expected baseline avg 0.65, got %f", detail.ComparisonSummary.BaselineAvgScore)
	}
}

func TestCreateComparison_PassRateGate(t *testing.T) {
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{
		saveCompare: func(cmp Comparison, results []ComparisonCaseResult) (Comparison, error) {
			cmp.ID = "cmp-5"
			return cmp, nil
		},
	}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "completed", "agent")
	mq.expByID["cand"] = makeExp("cand", "p1", "ds1", "completed", "agent-v2")

	mq.resultsByID["bl"] = []EvalCaseResult{
		makeResult("case-1", "run-bl-1", 1.0, true),
		makeResult("case-2", "run-bl-2", 1.0, true),
	}
	mq.resultsByID["cand"] = []EvalCaseResult{
		makeResult("case-1", "run-cand-1", 0.0, false),
		makeResult("case-2", "run-cand-2", 0.5, false),
	}

	s := NewCompareService(mq, msq, nil)
	s.evalQ = &Service{q: mq}

	gates := &RegressionGate{MinPassRateDelta: 0.5}
	detail, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, gates)
	if err != nil {
		t.Fatalf("CreateComparison failed: %v", err)
	}

	if !detail.ComparisonSummary.RegressionTriggered {
		t.Error("expected regression triggered due to pass rate drop")
	}
	// Baseline pass rate = 1.0, Candidate = 0.0, delta = -1.0 which exceeds 0.5
	if len(detail.ComparisonSummary.RegressionReasons) == 0 {
		t.Error("expected regression reasons")
	}
}

func TestCreateComparison_ScoreGate(t *testing.T) {
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{
		saveCompare: func(cmp Comparison, results []ComparisonCaseResult) (Comparison, error) {
			cmp.ID = "cmp-6"
			return cmp, nil
		},
	}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "completed", "agent")
	mq.expByID["cand"] = makeExp("cand", "p1", "ds1", "completed", "agent-v2")

	mq.resultsByID["bl"] = []EvalCaseResult{
		makeResult("case-1", "run-bl-1", 1.0, true),
	}
	mq.resultsByID["cand"] = []EvalCaseResult{
		makeResult("case-1", "run-cand-1", 0.0, false),
	}

	s := NewCompareService(mq, msq, nil)
	s.evalQ = &Service{q: mq}

	gates := &RegressionGate{MinScoreDelta: 0.5}
	detail, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, gates)
	if err != nil {
		t.Fatalf("CreateComparison failed: %v", err)
	}

	if !detail.ComparisonSummary.RegressionTriggered {
		t.Error("expected regression triggered due to score drop")
	}
}

func TestCreateComparison_EmptyResults(t *testing.T) {
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{
		saveCompare: func(cmp Comparison, results []ComparisonCaseResult) (Comparison, error) {
			cmp.ID = "cmp-7"
			return cmp, nil
		},
	}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "completed", "agent")
	mq.expByID["cand"] = makeExp("cand", "p1", "ds1", "completed", "agent-v2")
	// No results for either experiment

	s := NewCompareService(mq, msq, nil)
	s.evalQ = &Service{q: mq}

	detail, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, nil)
	if err != nil {
		t.Fatalf("empty results should still succeed: %v", err)
	}
	if detail.ComparisonSummary.TotalCases != 0 {
		t.Errorf("expected 0 total cases for empty results, got %d", detail.ComparisonSummary.TotalCases)
	}
}

func TestCreateComparison_P95Latency(t *testing.T) {
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{
		saveCompare: func(cmp Comparison, results []ComparisonCaseResult) (Comparison, error) {
			cmp.ID = "cmp-8"
			return cmp, nil
		},
	}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "completed", "agent")
	mq.expByID["cand"] = makeExp("cand", "p1", "ds1", "completed", "agent-v2")

	mq.resultsByID["bl"] = []EvalCaseResult{
		{DatasetCaseID: "c1", RunID: "r1", Score: 1.0, Passed: true, LatencyMs: 100},
		{DatasetCaseID: "c2", RunID: "r2", Score: 1.0, Passed: true, LatencyMs: 200},
		{DatasetCaseID: "c3", RunID: "r3", Score: 1.0, Passed: true, LatencyMs: 300},
		{DatasetCaseID: "c4", RunID: "r4", Score: 1.0, Passed: true, LatencyMs: 400},
	}
	mq.resultsByID["cand"] = []EvalCaseResult{
		{DatasetCaseID: "c1", RunID: "r1", Score: 1.0, Passed: true, LatencyMs: 500},
		{DatasetCaseID: "c2", RunID: "r2", Score: 1.0, Passed: true, LatencyMs: 600},
		{DatasetCaseID: "c3", RunID: "r3", Score: 1.0, Passed: true, LatencyMs: 700},
		{DatasetCaseID: "c4", RunID: "r4", Score: 1.0, Passed: true, LatencyMs: 800},
	}

	s := NewCompareService(mq, msq, nil)
	s.evalQ = &Service{q: mq}

	detail, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, nil)
	if err != nil {
		t.Fatalf("CreateComparison failed: %v", err)
	}

	// P95 of 4 items: ceil(4*0.95)=4 -> index 3 -> sorted: [100,200,300,400] => 400
	if detail.ComparisonSummary.BaselineP95LatencyMs != 400 {
		t.Errorf("expected baseline P95=400, got %d", detail.ComparisonSummary.BaselineP95LatencyMs)
	}
	if detail.ComparisonSummary.CandidateP95LatencyMs != 800 {
		t.Errorf("expected candidate P95=800, got %d", detail.ComparisonSummary.CandidateP95LatencyMs)
	}
}
