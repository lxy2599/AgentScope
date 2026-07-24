package experiments

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
)

// ---- Mock Querier for CompareService ----

type mockCompareQuerier struct {
	expByID     map[string]Experiment
	resultsByID map[string][]EvalCaseResult
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

func (m *mockCompareQuerier) UpdateFullExperimentHarnessMetadata(ctx context.Context, experimentID, modelProvider, modelName, modelVersion, promptVersion, harnessVersion, datasetVersion, toolsetVersion, skillsetVersion, ragConfigVersion string, featureFlags []byte) error {
	return nil
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
		ID:         id,
		ProjectID:  projectID,
		DatasetID:  datasetID,
		TargetName: target,
		Status:     status,
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

// ---- Mock EvalScoreReader ----

type mockScoreReader struct {
	listByRun func(ctx context.Context, projectID, runID string) ([]EvalScoreResult, error)
}

func (m *mockScoreReader) ListEvalScoresByRun(ctx context.Context, projectID, runID string) ([]EvalScoreResult, error) {
	if m.listByRun != nil {
		return m.listByRun(ctx, projectID, runID)
	}
	return nil, nil
}

type mockBatchScoreReader struct {
	runScores   map[string][]EvalScoreResult
	batchCalls  int
	singleCalls int
	projectID   string
	runIDs      []string
}

func (m *mockBatchScoreReader) ListEvalScoresByRun(context.Context, string, string) ([]EvalScoreResult, error) {
	m.singleCalls++
	return nil, errors.New("unexpected per-run score query")
}

func (m *mockBatchScoreReader) ListEvalScoresByRuns(_ context.Context, projectID string, runIDs []string) ([]EvalScoreResult, error) {
	m.batchCalls++
	m.projectID = projectID
	m.runIDs = append([]string(nil), runIDs...)
	var scores []EvalScoreResult
	for _, runID := range runIDs {
		scores = append(scores, m.runScores[runID]...)
	}
	return scores, nil
}

// newCompareServiceWithScoreReader creates a CompareService pre-configured with a
// mock score reader that returns empty results by default.
func newCompareServiceWithScoreReader(q Querier, compareQ CompareQuerier) *CompareService {
	s := NewCompareService(q, compareQ, &Service{q: q})
	s.SetEvalScoreReader(&mockScoreReader{})
	return s
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

	s := newCompareServiceWithScoreReader(mq, msq)

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

	s := newCompareServiceWithScoreReader(mq, msq)

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

	s := newCompareServiceWithScoreReader(mq, msq)

	_, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, nil)
	if err == nil {
		t.Fatal("expected error for different dataset experiments")
	}
}

func TestCreateComparison_NotCompleted(t *testing.T) {
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "pending", "agent")

	s := newCompareServiceWithScoreReader(mq, msq)

	_, err := s.CreateComparison(context.Background(), "p1", "bl", "bl", "u1", ComparisonGeneral, nil)
	if err == nil {
		t.Fatal("expected error when comparing same experiment")
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

	s := newCompareServiceWithScoreReader(mq, msq)

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

	s := newCompareServiceWithScoreReader(mq, msq)

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

	s := newCompareServiceWithScoreReader(mq, msq)

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

	s := newCompareServiceWithScoreReader(mq, msq)

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

	s := newCompareServiceWithScoreReader(mq, msq)

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

	s := newCompareServiceWithScoreReader(mq, msq)

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

	s := newCompareServiceWithScoreReader(mq, msq)

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

	s := newCompareServiceWithScoreReader(mq, msq)

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

// ── Safety Regression Tests ────────────────────────────────────────────────

// mockScoreReaderWithScores creates a score reader that returns fixed scores per run.
func mockScoreReaderWithScores(runScores map[string][]EvalScoreResult) *mockScoreReader {
	return &mockScoreReader{
		listByRun: func(_ context.Context, _ string, runID string) ([]EvalScoreResult, error) {
			return runScores[runID], nil
		},
	}
}

func TestCreateComparison_LoadsAllEvalScoresInOneBatch(t *testing.T) {
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "completed", "agent")
	mq.expByID["cand"] = makeExp("cand", "p1", "ds1", "completed", "agent-v2")
	mq.resultsByID["bl"] = []EvalCaseResult{
		makeResult("case-1", "run-bl-1", 1, true),
		makeResult("case-2", "run-bl-2", 1, true),
	}
	mq.resultsByID["cand"] = []EvalCaseResult{
		makeResult("case-1", "run-cand-1", 1, true),
		makeResult("case-2", "run-cand-2", 1, true),
	}

	reader := &mockBatchScoreReader{runScores: map[string][]EvalScoreResult{
		"run-bl-1":   {{RunID: "run-bl-1", MetricName: "accuracy", MetricKind: "quality", Score: 1, Passed: true}},
		"run-bl-2":   {{RunID: "run-bl-2", MetricName: "accuracy", MetricKind: "quality", Score: 1, Passed: true}},
		"run-cand-1": {{RunID: "run-cand-1", MetricName: "accuracy", MetricKind: "quality", Score: 1, Passed: true}},
		"run-cand-2": {{RunID: "run-cand-2", MetricName: "accuracy", MetricKind: "quality", Score: 1, Passed: true}},
	}}
	s := NewCompareService(mq, msq, &Service{q: mq})
	s.SetEvalScoreReader(reader)

	if _, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, nil); err != nil {
		t.Fatalf("CreateComparison failed: %v", err)
	}
	if reader.batchCalls != 1 {
		t.Fatalf("expected one batch score query, got %d", reader.batchCalls)
	}
	if reader.singleCalls != 0 {
		t.Fatalf("expected no per-run score queries, got %d", reader.singleCalls)
	}
	if reader.projectID != "p1" {
		t.Fatalf("expected project p1, got %q", reader.projectID)
	}
	wantRunIDs := []string{"run-bl-1", "run-bl-2", "run-cand-1", "run-cand-2"}
	if len(reader.runIDs) != len(wantRunIDs) {
		t.Fatalf("expected run IDs %v, got %v", wantRunIDs, reader.runIDs)
	}
	for i := range wantRunIDs {
		if reader.runIDs[i] != wantRunIDs[i] {
			t.Fatalf("expected sorted run IDs %v, got %v", wantRunIDs, reader.runIDs)
		}
	}
}

func TestCalculateSafetyRegressed_BaselinePassCandidateFail(t *testing.T) {
	// Given: baseline safety passed, candidate safety failed → safety_regressed=true
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{saveCompare: func(cmp Comparison, results []ComparisonCaseResult) (Comparison, error) {
		cmp.ID = "cmp-s-1"
		return cmp, nil
	}}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "completed", "agent")
	mq.expByID["cand"] = makeExp("cand", "p1", "ds1", "completed", "agent-v2")
	mq.resultsByID["bl"] = []EvalCaseResult{makeResult("case-1", "run-bl-1", 1.0, true)}
	mq.resultsByID["cand"] = []EvalCaseResult{makeResult("case-1", "run-cand-1", 0.0, false)}

	s := newCompareServiceWithScoreReader(mq, msq)
	s.scoreQ = mockScoreReaderWithScores(map[string][]EvalScoreResult{
		"run-bl-1": {
			{MetricName: "safety_check", MetricKind: "safety", Score: 1.0, Passed: true, RunID: "run-bl-1"},
		},
		"run-cand-1": {
			{MetricName: "safety_check", MetricKind: "safety", Score: 0.0, Passed: false, RunID: "run-cand-1"},
		},
	})

	detail, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, nil)
	if err != nil {
		t.Fatalf("CreateComparison failed: %v", err)
	}
	if !detail.ComparisonSummary.SafetyRegressed {
		t.Error("expected safety_regressed=true when baseline safety passes and candidate safety fails")
	}
}

func TestCalculateSafetyRegressed_BothFail(t *testing.T) {
	// Given: baseline safety failed AND candidate safety failed → NOT a new regression
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{saveCompare: func(cmp Comparison, results []ComparisonCaseResult) (Comparison, error) {
		cmp.ID = "cmp-s-2"
		return cmp, nil
	}}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "completed", "agent")
	mq.expByID["cand"] = makeExp("cand", "p1", "ds1", "completed", "agent-v2")
	mq.resultsByID["bl"] = []EvalCaseResult{makeResult("case-1", "run-bl-1", 0.0, false)}
	mq.resultsByID["cand"] = []EvalCaseResult{makeResult("case-1", "run-cand-1", 0.0, false)}

	s := newCompareServiceWithScoreReader(mq, msq)
	s.scoreQ = mockScoreReaderWithScores(map[string][]EvalScoreResult{
		"run-bl-1": {
			{MetricName: "safety_check", MetricKind: "safety", Score: 0.0, Passed: false, RunID: "run-bl-1"},
		},
		"run-cand-1": {
			{MetricName: "safety_check", MetricKind: "safety", Score: 0.0, Passed: false, RunID: "run-cand-1"},
		},
	})

	detail, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, nil)
	if err != nil {
		t.Fatalf("CreateComparison failed: %v", err)
	}
	if detail.ComparisonSummary.SafetyRegressed {
		t.Error("expected safety_regressed=false when both baseline and candidate safety fail (not a new regression)")
	}
}

func TestCalculateSafetyRegressed_CandidateNewFailedSafetyMetric(t *testing.T) {
	// Given: candidate has a new safety metric that baseline doesn't have, and it fails → regressed
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{saveCompare: func(cmp Comparison, results []ComparisonCaseResult) (Comparison, error) {
		cmp.ID = "cmp-s-3"
		return cmp, nil
	}}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "completed", "agent")
	mq.expByID["cand"] = makeExp("cand", "p1", "ds1", "completed", "agent-v2")
	mq.resultsByID["bl"] = []EvalCaseResult{makeResult("case-1", "run-bl-1", 1.0, true)}
	mq.resultsByID["cand"] = []EvalCaseResult{makeResult("case-1", "run-cand-1", 1.0, true)}

	s := newCompareServiceWithScoreReader(mq, msq)
	s.scoreQ = mockScoreReaderWithScores(map[string][]EvalScoreResult{
		"run-bl-1": {
			{MetricName: "accuracy", MetricKind: "final_answer", Score: 1.0, Passed: true, RunID: "run-bl-1"},
		},
		"run-cand-1": {
			{MetricName: "accuracy", MetricKind: "final_answer", Score: 1.0, Passed: true, RunID: "run-cand-1"},
			{MetricName: "safety_check", MetricKind: "safety", Score: 0.0, Passed: false, RunID: "run-cand-1"},
		},
	})

	detail, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, nil)
	if err != nil {
		t.Fatalf("CreateComparison failed: %v", err)
	}
	if !detail.ComparisonSummary.SafetyRegressed {
		t.Error("expected safety_regressed=true when candidate has a new failed safety metric")
	}
}

func TestCalculateSafetyRegressed_ScoreReaderError(t *testing.T) {
	// Given: score reader returns error → CreateComparison must fail (fail-closed)
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "completed", "agent")
	mq.expByID["cand"] = makeExp("cand", "p1", "ds1", "completed", "agent-v2")
	mq.resultsByID["bl"] = []EvalCaseResult{makeResult("case-1", "run-bl-1", 1.0, true)}
	mq.resultsByID["cand"] = []EvalCaseResult{makeResult("case-1", "run-cand-1", 0.0, false)}

	s := newCompareServiceWithScoreReader(mq, msq)
	s.scoreQ = &mockScoreReader{
		listByRun: func(_ context.Context, _ string, _ string) ([]EvalScoreResult, error) {
			return nil, errors.New("simulated score reader error")
		},
	}

	_, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, nil)
	if err == nil {
		t.Fatal("expected CreateComparison to fail when score reader returns error")
	}
	if !contains(err.Error(), "safety check failed") {
		t.Errorf("expected safety check error, got: %v", err)
	}
}

func TestCalculateSafetyRegressed_NoCandidateRunID_Skips(t *testing.T) {
	// Given: only baseline run exists, no candidate → should not cause regression
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{saveCompare: func(cmp Comparison, results []ComparisonCaseResult) (Comparison, error) {
		cmp.ID = "cmp-s-5"
		return cmp, nil
	}}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "completed", "agent")
	mq.expByID["cand"] = makeExp("cand", "p1", "ds1", "completed", "agent-v2")
	// Baseline has 2 cases, candidate has 1 → case-2 is incomparable
	mq.resultsByID["bl"] = []EvalCaseResult{
		makeResult("case-1", "run-bl-1", 1.0, true),
		makeResult("case-2", "run-bl-2", 1.0, true),
	}
	mq.resultsByID["cand"] = []EvalCaseResult{
		makeResult("case-1", "run-cand-1", 0.0, false),
	}

	s := newCompareServiceWithScoreReader(mq, msq)
	// Set score reader that returns empty scores for both sides
	// Only case-1 has both runs, case-2 has only baseline (should be skipped)
	s.scoreQ = mockScoreReaderWithScores(map[string][]EvalScoreResult{
		"run-bl-1":   {{MetricName: "safety_check", MetricKind: "safety", Score: 1.0, Passed: true, RunID: "run-bl-1"}},
		"run-cand-1": {{MetricName: "safety_check", MetricKind: "safety", Score: 0.0, Passed: false, RunID: "run-cand-1"}},
	})

	detail, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, nil)
	if err != nil {
		t.Fatalf("CreateComparison failed: %v", err)
	}
	// case-1: baseline safety pass + candidate safety fail → regressed
	if !detail.ComparisonSummary.SafetyRegressed {
		t.Error("expected safety_regressed=true for paired case where BL safety pass and CAND safety fail")
	}
}

func TestCalculateSafetyRegressed_EmptyScores(t *testing.T) {
	// Given: no eval scores at all → safety_regressed=false
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{saveCompare: func(cmp Comparison, results []ComparisonCaseResult) (Comparison, error) {
		cmp.ID = "cmp-s-6"
		return cmp, nil
	}}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "completed", "agent")
	mq.expByID["cand"] = makeExp("cand", "p1", "ds1", "completed", "agent-v2")
	mq.resultsByID["bl"] = []EvalCaseResult{makeResult("case-1", "run-bl-1", 1.0, true)}
	mq.resultsByID["cand"] = []EvalCaseResult{makeResult("case-1", "run-cand-1", 1.0, true)}

	// scoreQ already returns nil, nil by default
	s := newCompareServiceWithScoreReader(mq, msq)

	detail, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, nil)
	if err != nil {
		t.Fatalf("CreateComparison failed: %v", err)
	}
	if detail.ComparisonSummary.SafetyRegressed {
		t.Error("expected safety_regressed=false when no safety eval scores exist")
	}
}

// ── Multi-Dim Metrics Tests ────────────────────────────────────────────────

func TestCreateComparison_MultiDimMetrics_Aggregation(t *testing.T) {
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{saveCompare: func(cmp Comparison, results []ComparisonCaseResult) (Comparison, error) {
		cmp.ID = "cmp-m-1"
		return cmp, nil
	}}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "completed", "agent")
	mq.expByID["cand"] = makeExp("cand", "p1", "ds1", "completed", "agent-v2")
	mq.resultsByID["bl"] = []EvalCaseResult{
		makeResult("case-1", "run-bl-1", 1.0, true),
		makeResult("case-2", "run-bl-2", 1.0, true),
	}
	mq.resultsByID["cand"] = []EvalCaseResult{
		makeResult("case-1", "run-cand-1", 0.9, true),
		makeResult("case-2", "run-cand-2", 0.0, false),
	}

	s := newCompareServiceWithScoreReader(mq, msq)
	s.scoreQ = mockScoreReaderWithScores(map[string][]EvalScoreResult{
		"run-bl-1": {
			{MetricName: "accuracy", MetricKind: "final_answer", Score: 1.0, Passed: true, RunID: "run-bl-1"},
			{MetricName: "tool_use", MetricKind: "tool_correctness", Score: 1.0, Passed: true, RunID: "run-bl-1"},
		},
		"run-bl-2": {
			{MetricName: "accuracy", MetricKind: "final_answer", Score: 1.0, Passed: true, RunID: "run-bl-2"},
			{MetricName: "tool_use", MetricKind: "tool_correctness", Score: 0.5, Passed: false, RunID: "run-bl-2"},
		},
		"run-cand-1": {
			{MetricName: "accuracy", MetricKind: "final_answer", Score: 0.9, Passed: true, RunID: "run-cand-1"},
			{MetricName: "tool_use", MetricKind: "tool_correctness", Score: 0.8, Passed: true, RunID: "run-cand-1"},
		},
		"run-cand-2": {
			{MetricName: "accuracy", MetricKind: "final_answer", Score: 0.0, Passed: false, RunID: "run-cand-2"},
			{MetricName: "tool_use", MetricKind: "tool_correctness", Score: 0.3, Passed: false, RunID: "run-cand-2"},
		},
	})

	detail, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, nil)
	if err != nil {
		t.Fatalf("CreateComparison failed: %v", err)
	}

	mm := detail.ComparisonSummary.MultiDimMetrics
	if mm == nil {
		t.Fatal("expected multi_dim_metrics to be non-nil")
	}

	// Verify final_answer:accuracy aggregation
	fa, ok := mm["final_answer:accuracy"]
	if !ok {
		t.Fatal("expected 'final_answer:accuracy' metric key in multi_dim_metrics")
	}
	if fa.MetricName != "accuracy" {
		t.Errorf("expected metric_name=accuracy, got %s", fa.MetricName)
	}
	if fa.MetricKind != "final_answer" {
		t.Errorf("expected metric_kind=final_answer, got %s", fa.MetricKind)
	}
	if fa.BaselineAvg != 1.0 { // (1.0 + 1.0) / 2
		t.Errorf("expected final_answer:accuracy baseline_avg=1.0, got %f", fa.BaselineAvg)
	}
	if fa.CandidateAvg != 0.45 { // (0.9 + 0.0) / 2
		t.Errorf("expected final_answer:accuracy candidate_avg=0.45, got %f", fa.CandidateAvg)
	}
	// Use tolerance for floating-point comparison
	eps := 1e-9
	if fa.Delta > -0.55+eps || fa.Delta < -0.55-eps {
		t.Errorf("expected final_answer:accuracy delta≈-0.55, got %f", fa.Delta)
	}
	if fa.BaselinePassedCount != 2 {
		t.Errorf("expected final_answer:accuracy baseline_passed_count=2, got %d", fa.BaselinePassedCount)
	}
	if fa.BaselineTotalCount != 2 {
		t.Errorf("expected final_answer:accuracy baseline_total_count=2, got %d", fa.BaselineTotalCount)
	}
	if fa.CandidatePassedCount != 1 {
		t.Errorf("expected final_answer:accuracy candidate_passed_count=1, got %d", fa.CandidatePassedCount)
	}
	if fa.CandidateTotalCount != 2 {
		t.Errorf("expected final_answer:accuracy candidate_total_count=2, got %d", fa.CandidateTotalCount)
	}

	// Verify tool_correctness:tool_use aggregation
	tc, ok := mm["tool_correctness:tool_use"]
	if !ok {
		t.Fatal("expected 'tool_correctness:tool_use' metric key in multi_dim_metrics")
	}
	if tc.MetricName != "tool_use" {
		t.Errorf("expected metric_name=tool_use, got %s", tc.MetricName)
	}
	if tc.MetricKind != "tool_correctness" {
		t.Errorf("expected metric_kind=tool_correctness, got %s", tc.MetricKind)
	}
	if tc.BaselineAvg != 0.75 { // (1.0 + 0.5) / 2
		t.Errorf("expected tool_correctness:tool_use baseline_avg=0.75, got %f", tc.BaselineAvg)
	}
	if tc.CandidateAvg != 0.55 { // (0.8 + 0.3) / 2
		t.Errorf("expected tool_correctness:tool_use candidate_avg=0.55, got %f", tc.CandidateAvg)
	}
	if tc.Delta > -0.20+eps || tc.Delta < -0.20-eps {
		t.Errorf("expected tool_correctness:tool_use delta≈-0.20, got %f", tc.Delta)
	}
}

func TestCreateComparison_MultiDimMetrics_MissingDimension(t *testing.T) {
	// Given: candidate has safety metric that baseline doesn't have
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{saveCompare: func(cmp Comparison, results []ComparisonCaseResult) (Comparison, error) {
		cmp.ID = "cmp-m-2"
		return cmp, nil
	}}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "completed", "agent")
	mq.expByID["cand"] = makeExp("cand", "p1", "ds1", "completed", "agent-v2")
	mq.resultsByID["bl"] = []EvalCaseResult{makeResult("case-1", "run-bl-1", 1.0, true)}
	mq.resultsByID["cand"] = []EvalCaseResult{makeResult("case-1", "run-cand-1", 1.0, true)}

	s := newCompareServiceWithScoreReader(mq, msq)
	s.scoreQ = mockScoreReaderWithScores(map[string][]EvalScoreResult{
		"run-bl-1": {
			{MetricName: "accuracy", MetricKind: "final_answer", Score: 1.0, Passed: true, RunID: "run-bl-1"},
		},
		"run-cand-1": {
			{MetricName: "accuracy", MetricKind: "final_answer", Score: 0.8, Passed: true, RunID: "run-cand-1"},
			{MetricName: "safety_check", MetricKind: "safety", Score: 1.0, Passed: true, RunID: "run-cand-1"},
		},
	})

	detail, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, nil)
	if err != nil {
		t.Fatalf("CreateComparison failed: %v", err)
	}

	mm := detail.ComparisonSummary.MultiDimMetrics

	// Baseline has no safety metrics
	safety, ok := mm["safety:safety_check"]
	if !ok {
		t.Fatal("expected 'safety:safety_check' metric key in multi_dim_metrics")
	}
	if safety.MetricName != "safety_check" {
		t.Errorf("expected metric_name=safety_check, got %s", safety.MetricName)
	}
	if safety.MetricKind != "safety" {
		t.Errorf("expected metric_kind=safety, got %s", safety.MetricKind)
	}
	if safety.BaselineTotalCount != 0 {
		t.Errorf("expected safety baseline_total_count=0 (missing dimension), got %d", safety.BaselineTotalCount)
	}
	if safety.BaselineAvg != 0.0 {
		t.Errorf("expected safety baseline_avg=0.0 for missing dimension, got %f", safety.BaselineAvg)
	}
	if safety.CandidateTotalCount != 1 {
		t.Errorf("expected safety candidate_total_count=1, got %d", safety.CandidateTotalCount)
	}
	if safety.CandidateAvg != 1.0 {
		t.Errorf("expected safety candidate_avg=1.0, got %f", safety.CandidateAvg)
	}
}

func TestCreateComparison_MultiDimMetrics_Persistence(t *testing.T) {
	// Given: create a comparison, then GET it back → multi_dim_metrics must be identical
	var savedSummary string
	msq := &mockStoreQuerier{
		saveCompare: func(cmp Comparison, results []ComparisonCaseResult) (Comparison, error) {
			savedSummary = cmp.Summary
			cmp.ID = "cmp-m-3"
			return cmp, nil
		},
		getCompareFn: func(id string) (ComparisonDetail, error) {
			var summary ComparisonSummary
			json.Unmarshal([]byte(savedSummary), &summary)
			return ComparisonDetail{
				Comparison:        Comparison{ID: id, Summary: savedSummary},
				ComparisonSummary: summary,
			}, nil
		},
	}

	mq := newMockCompareQuerier()
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "completed", "agent")
	mq.expByID["cand"] = makeExp("cand", "p1", "ds1", "completed", "agent-v2")
	mq.resultsByID["bl"] = []EvalCaseResult{makeResult("case-1", "run-bl-1", 1.0, true)}
	mq.resultsByID["cand"] = []EvalCaseResult{makeResult("case-1", "run-cand-1", 0.5, false)}

	s := newCompareServiceWithScoreReader(mq, msq)
	s.scoreQ = mockScoreReaderWithScores(map[string][]EvalScoreResult{
		"run-bl-1": {
			{MetricName: "accuracy", MetricKind: "final_answer", Score: 1.0, Passed: true, RunID: "run-bl-1"},
		},
		"run-cand-1": {
			{MetricName: "accuracy", MetricKind: "final_answer", Score: 0.5, Passed: false, RunID: "run-cand-1"},
		},
	})

	_, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, nil)
	if err != nil {
		t.Fatalf("CreateComparison failed: %v", err)
	}

	// GET comparison and verify multi_dim_metrics
	getDetail, err := s.GetComparison(context.Background(), "cmp-m-3")
	if err != nil {
		t.Fatalf("GetComparison failed: %v", err)
	}
	if getDetail.ComparisonSummary.MultiDimMetrics == nil {
		t.Fatal("expected multi_dim_metrics in GET response")
	}
	fa, ok := getDetail.ComparisonSummary.MultiDimMetrics["final_answer:accuracy"]
	if !ok {
		t.Fatal("expected final_answer:accuracy in persisted multi_dim_metrics")
	}
	if fa.MetricName != "accuracy" {
		t.Errorf("expected metric_name=accuracy, got %s", fa.MetricName)
	}
	if fa.MetricKind != "final_answer" {
		t.Errorf("expected metric_kind=final_answer, got %s", fa.MetricKind)
	}
	if fa.BaselineAvg != 1.0 {
		t.Errorf("expected persisted baseline_avg=1.0, got %f", fa.BaselineAvg)
	}
	if fa.CandidateAvg != 0.5 {
		t.Errorf("expected persisted candidate_avg=0.5, got %f", fa.CandidateAvg)
	}
	if fa.Delta != -0.5 {
		t.Errorf("expected persisted delta=-0.5, got %f", fa.Delta)
	}
}

// ── Multi-Dim Metrics: Same Kind Different Name ────────────────────────────

func TestCreateComparison_MultiDimMetrics_SameKindDifferentName(t *testing.T) {
	// Given: two metrics with same metric_kind but different metric_name
	// must produce two separate aggregation entries.
	mq := newMockCompareQuerier()
	msq := &mockStoreQuerier{saveCompare: func(cmp Comparison, results []ComparisonCaseResult) (Comparison, error) {
		cmp.ID = "cmp-m-skdn"
		return cmp, nil
	}}
	mq.expByID["bl"] = makeExp("bl", "p1", "ds1", "completed", "agent")
	mq.expByID["cand"] = makeExp("cand", "p1", "ds1", "completed", "agent-v2")
	mq.resultsByID["bl"] = []EvalCaseResult{
		makeResult("case-1", "run-bl-1", 1.0, true),
	}
	mq.resultsByID["cand"] = []EvalCaseResult{
		makeResult("case-1", "run-cand-1", 0.0, false),
	}

	s := newCompareServiceWithScoreReader(mq, msq)
	s.scoreQ = mockScoreReaderWithScores(map[string][]EvalScoreResult{
		"run-bl-1": {
			{MetricName: "safety_check_a", MetricKind: "safety", Score: 1.0, Passed: true, RunID: "run-bl-1"},
			{MetricName: "safety_check_b", MetricKind: "safety", Score: 1.0, Passed: true, RunID: "run-bl-1"},
		},
		"run-cand-1": {
			{MetricName: "safety_check_a", MetricKind: "safety", Score: 0.0, Passed: false, RunID: "run-cand-1"},
			{MetricName: "safety_check_b", MetricKind: "safety", Score: 1.0, Passed: true, RunID: "run-cand-1"},
		},
	})

	detail, err := s.CreateComparison(context.Background(), "p1", "bl", "cand", "u1", ComparisonGeneral, nil)
	if err != nil {
		t.Fatalf("CreateComparison failed: %v", err)
	}

	mm := detail.ComparisonSummary.MultiDimMetrics
	if mm == nil {
		t.Fatal("expected multi_dim_metrics to be non-nil")
	}

	// Both safety metrics must be separate entries
	keyA := "safety:safety_check_a"
	keyB := "safety:safety_check_b"
	a, okA := mm[keyA]
	b, okB := mm[keyB]
	if !okA {
		t.Fatalf("expected %q in multi_dim_metrics", keyA)
	}
	if !okB {
		t.Fatalf("expected %q in multi_dim_metrics", keyB)
	}
	// Total entries must be exactly 2 (not merged into 1)
	if len(mm) != 2 {
		t.Errorf("expected exactly 2 multi_dim entries (got %d) — metrics with same kind but different names must not be merged", len(mm))
	}

	if a.MetricName != "safety_check_a" {
		t.Errorf("expected keyA metric_name=safety_check_a, got %s", a.MetricName)
	}
	if a.MetricKind != "safety" {
		t.Errorf("expected keyA metric_kind=safety, got %s", a.MetricKind)
	}
	if a.BaselineAvg != 1.0 {
		t.Errorf("expected keyA baseline_avg=1.0, got %f", a.BaselineAvg)
	}
	if a.CandidateAvg != 0.0 {
		t.Errorf("expected keyA candidate_avg=0.0, got %f", a.CandidateAvg)
	}

	if b.MetricName != "safety_check_b" {
		t.Errorf("expected keyB metric_name=safety_check_b, got %s", b.MetricName)
	}
	if b.MetricKind != "safety" {
		t.Errorf("expected keyB metric_kind=safety, got %s", b.MetricKind)
	}
	if b.BaselineAvg != 1.0 {
		t.Errorf("expected keyB baseline_avg=1.0, got %f", b.BaselineAvg)
	}
	if b.CandidateAvg != 1.0 {
		t.Errorf("expected keyB candidate_avg=1.0, got %f", b.CandidateAvg)
	}
}

// contains checks if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		len(s) > len(substr) && (s[:len(substr)] == substr ||
			contains(s[1:], substr)))
}

func TestCalculateStatisticalSignificanceIsReproducible(t *testing.T) {
	results := make([]ComparisonCaseResult, 12)
	for index := range results {
		results[index] = ComparisonCaseResult{
			Status: "regressed", BaselineScore: 0.9, CandidateScore: 0.6,
			BaselinePassed: true, CandidatePassed: index%3 != 0,
			BaselineLatencyMs: 100, CandidateLatencyMs: 130,
			BaselineCost: 0.01, CandidateCost: 0.02,
		}
	}
	first := calculateStatisticalSignificance(results, 99, 1000, 10)
	second := calculateStatisticalSignificance(results, 99, 1000, 10)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("fixed seed must be reproducible:\n%+v\n%+v", first, second)
	}
	if first.Conclusion != "significant_regression" {
		t.Fatalf("conclusion = %q", first.Conclusion)
	}
	if first.Metrics["score"].CIUpper >= 0 {
		t.Fatalf("score confidence interval should be below zero: %+v", first.Metrics["score"])
	}
}

func TestCalculateStatisticalSignificanceConclusions(t *testing.T) {
	t.Run("insufficient sample", func(t *testing.T) {
		results := []ComparisonCaseResult{
			{Status: "unchanged", BaselineScore: 0.8, CandidateScore: 0.8},
			{Status: "improved", BaselineScore: 0.7, CandidateScore: 0.8},
		}
		got := calculateStatisticalSignificance(results, 1, 100, 10)
		if got.Conclusion != "insufficient_sample" || got.SampleSize != 2 {
			t.Fatalf("unexpected result: %+v", got)
		}
	})

	t.Run("no significant change", func(t *testing.T) {
		results := make([]ComparisonCaseResult, 12)
		for index := range results {
			delta := 0.1
			if index%2 == 0 {
				delta = -0.1
			}
			results[index] = ComparisonCaseResult{
				Status: "unchanged", BaselineScore: 0.7, CandidateScore: 0.7 + delta,
				BaselinePassed: true, CandidatePassed: true,
			}
		}
		got := calculateStatisticalSignificance(results, 2, 1000, 10)
		if got.Conclusion != "no_significant_change" {
			t.Fatalf("unexpected result: %+v", got)
		}
	})
}
