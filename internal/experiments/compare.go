package experiments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sort"
)

// CreateComparison creates a comparison between baseline and candidate experiments.
func (s *CompareService) CreateComparison(
	ctx context.Context,
	projectID, baselineExpID, candidateExpID, createdBy string,
	compType ComparisonType,
	gates *RegressionGate,
) (ComparisonDetail, error) {
	if projectID == "" {
		return ComparisonDetail{}, errors.New("projectID is required")
	}
	if baselineExpID == "" || candidateExpID == "" {
		return ComparisonDetail{}, errors.New("baseline and candidate experiment IDs are required")
	}
	if baselineExpID == candidateExpID {
		return ComparisonDetail{}, errors.New("baseline and candidate must be different experiments")
	}
	if createdBy == "" {
		return ComparisonDetail{}, errors.New("createdBy is required")
	}

	// Fetch both experiments
	baseline, err := s.q.GetExperiment(ctx, baselineExpID)
	if err != nil {
		return ComparisonDetail{}, fmt.Errorf("get baseline experiment: %w", err)
	}
	candidate, err := s.q.GetExperiment(ctx, candidateExpID)
	if err != nil {
		return ComparisonDetail{}, fmt.Errorf("get candidate experiment: %w", err)
	}

	// Verify same project
	if baseline.ProjectID != projectID || candidate.ProjectID != projectID {
		return ComparisonDetail{}, errors.New("both experiments must belong to the specified project")
	}

	// Verify same dataset
	if baseline.DatasetID != candidate.DatasetID {
		return ComparisonDetail{}, errors.New("baseline and candidate must use the same dataset")
	}

	// Type-specific validation
	var warnings []string
	switch compType {
	case ComparisonModelSwap:
		// In model_swap, harness configs should match (warn if not)
		if baseline.HarnessVersion != "" && candidate.HarnessVersion != "" &&
			baseline.HarnessVersion != candidate.HarnessVersion {
			warnings = append(warnings, "模型替换实验的 Harness 版本不一致，建议固定 Harness 配置后重试")
		}
		if baseline.PromptVersion != "" && candidate.PromptVersion != "" &&
			baseline.PromptVersion != candidate.PromptVersion {
			warnings = append(warnings, "模型替换实验的 Prompt 版本不一致，建议固定 Prompt 配置后重试")
		}
	case ComparisonAblation:
		// In ablation, model should match
		if baseline.ModelName != "" && candidate.ModelName != "" &&
			baseline.ModelName != candidate.ModelName {
			warnings = append(warnings, "消融实验的模型应保持一致，但检测到不同模型")
		}
	}

	// Get results for both experiments
	blResults, err := s.evalQ.ListEvalResults(ctx, baselineExpID)
	if err != nil {
		return ComparisonDetail{}, fmt.Errorf("list baseline results: %w", err)
	}
	candResults, err := s.evalQ.ListEvalResults(ctx, candidateExpID)
	if err != nil {
		return ComparisonDetail{}, fmt.Errorf("list candidate results: %w", err)
	}

	// Pair by dataset_case_id
	blByCase := make(map[string]EvalCaseResult, len(blResults))
	for _, r := range blResults {
		blByCase[r.DatasetCaseID] = r
	}
	candByCase := make(map[string]EvalCaseResult, len(candResults))
	for _, r := range candResults {
		candByCase[r.DatasetCaseID] = r
	}

	allCaseIDs := make(map[string]bool)
	for _, r := range blResults {
		allCaseIDs[r.DatasetCaseID] = true
	}
	for _, r := range candResults {
		allCaseIDs[r.DatasetCaseID] = true
	}

	var caseResults []ComparisonCaseResult
	for caseID := range allCaseIDs {
		bl, hasBL := blByCase[caseID]
		cand, hasCand := candByCase[caseID]

		cr := ComparisonCaseResult{
			DatasetCaseID: caseID,
		}

		if !hasBL || !hasCand {
			cr.Status = "incomparable"
			if hasBL {
				cr.BaselineRunID = bl.RunID
				cr.BaselineScore = bl.Score
				cr.BaselinePassed = bl.Passed
				cr.BaselineLatencyMs = bl.LatencyMs
				cr.BaselineCost = bl.Cost
			}
			if hasCand {
				cr.CandidateRunID = cand.RunID
				cr.CandidateScore = cand.Score
				cr.CandidatePassed = cand.Passed
				cr.CandidateLatencyMs = cand.LatencyMs
				cr.CandidateCost = cand.Cost
			}
			caseResults = append(caseResults, cr)
			continue
		}

		cr.BaselineRunID = bl.RunID
		cr.CandidateRunID = cand.RunID
		cr.BaselineScore = bl.Score
		cr.CandidateScore = cand.Score
		cr.BaselinePassed = bl.Passed
		cr.CandidatePassed = cand.Passed
		cr.BaselineLatencyMs = bl.LatencyMs
		cr.CandidateLatencyMs = cand.LatencyMs
		cr.BaselineCost = bl.Cost
		cr.CandidateCost = cand.Cost
		cr.ScoreDiff = cand.Score - bl.Score

		if bl.Passed == cand.Passed && cr.ScoreDiff == 0 {
			cr.Status = "unchanged"
		} else if cr.ScoreDiff > 0 || (!bl.Passed && cand.Passed) {
			cr.Status = "improved"
		} else if cr.ScoreDiff < 0 || (bl.Passed && !cand.Passed) {
			cr.Status = "regressed"
		} else {
			cr.Status = "unchanged"
		}
		caseResults = append(caseResults, cr)
	}

	// Calculate summary
	summary := calculateComparisonSummary(caseResults)
	summary.StatisticalSignificance = calculateStatisticalSignificance(caseResults, 42, 2000, 10)

	// Build paired eval run list and check safety regression + compute multi-dim metrics
	var pairedEvalRuns []pairedEvalRun
	for _, cr := range caseResults {
		if cr.BaselineRunID != "" || cr.CandidateRunID != "" {
			pairedEvalRuns = append(pairedEvalRuns, pairedEvalRun{
				baselineRunID:  cr.BaselineRunID,
				candidateRunID: cr.CandidateRunID,
			})
		}
	}
	safetyRegressed, err := s.calculateSafetyRegressed(ctx, projectID, pairedEvalRuns)
	if err != nil {
		return ComparisonDetail{}, fmt.Errorf("safety check failed: %w", err)
	}
	summary.SafetyRegressed = safetyRegressed
	if safetyRegressed {
		summary.StatisticalSignificance.Conclusion = "significant_regression"
	}

	multiDimMetrics, err := s.computeMultiDimMetrics(ctx, projectID, pairedEvalRuns)
	if err != nil {
		return ComparisonDetail{}, fmt.Errorf("compute multi-dim metrics: %w", err)
	}
	summary.MultiDimMetrics = multiDimMetrics

	// Apply regression gates
	if gates != nil {
		reasons := applyRegressionGates(summary, gates, warnings)
		if len(reasons) > 0 {
			summary.RegressionTriggered = true
			summary.RegressionReasons = reasons
		}
	} else {
		gates = &RegressionGate{}
	}

	// Ensure type-specific warnings (model_swap/ablation) are always visible,
	// even without explicit gates
	if len(warnings) > 0 && !summary.RegressionTriggered {
		summary.RegressionTriggered = true
		summary.RegressionReasons = warnings
	}

	gatesJSON, _ := json.Marshal(gates)
	summaryJSON, _ := json.Marshal(summary)

	comp := Comparison{
		ProjectID:             projectID,
		BaselineExperimentID:  baselineExpID,
		CandidateExperimentID: candidateExpID,
		ComparisonType:        compType,
		CreatedBy:             createdBy,
		Status:                "completed",
		Summary:               string(summaryJSON),
		RegressionGates:       string(gatesJSON),
	}

	savedComp, err := s.compareQ.CreateExperimentComparison(ctx, comp, caseResults)
	if err != nil {
		return ComparisonDetail{}, fmt.Errorf("save comparison: %w", err)
	}

	return ComparisonDetail{
		Comparison:          savedComp,
		BaselineExperiment:  baseline,
		CandidateExperiment: candidate,
		ComparisonSummary:   summary,
		CaseResults:         caseResults,
	}, nil
}

// GetComparison returns a full comparison detail.
func (s *CompareService) GetComparison(ctx context.Context, comparisonID string) (ComparisonDetail, error) {
	if comparisonID == "" {
		return ComparisonDetail{}, errors.New("comparisonID is required")
	}
	return s.compareQ.GetExperimentComparison(ctx, comparisonID)
}

// ListComparisons returns all comparisons for a project.
func (s *CompareService) ListComparisons(ctx context.Context, projectID string) ([]Comparison, error) {
	if projectID == "" {
		return nil, errors.New("projectID is required")
	}
	return s.compareQ.ListExperimentComparisons(ctx, projectID)
}

// ---- Helper functions ----

func calculateComparisonSummary(results []ComparisonCaseResult) ComparisonSummary {
	s := ComparisonSummary{TotalCases: len(results)}

	var blScores, candScores []float64
	var blLatencies, candLatencies []int64
	var blCosts, candCosts []float64
	var blPassed, blFailed, candPassed, candFailed int

	for _, r := range results {
		if r.Status != "incomparable" {
			s.PairedCases++
		}

		switch r.Status {
		case "improved":
			s.ImprovedCases++
		case "regressed":
			s.RegressedCases++
		case "unchanged":
			s.UnchangedCases++
		case "incomparable":
			s.IncomparableCases++
		}

		if r.BaselineRunID != "" {
			blScores = append(blScores, r.BaselineScore)
			blLatencies = append(blLatencies, r.BaselineLatencyMs)
			blCosts = append(blCosts, r.BaselineCost)
			if r.BaselinePassed {
				blPassed++
			} else {
				blFailed++
			}
		}
		if r.CandidateRunID != "" {
			candScores = append(candScores, r.CandidateScore)
			candLatencies = append(candLatencies, r.CandidateLatencyMs)
			candCosts = append(candCosts, r.CandidateCost)
			if r.CandidatePassed {
				candPassed++
			} else {
				candFailed++
			}
		}
	}

	totalBL := blPassed + blFailed
	if totalBL > 0 {
		s.BaselinePassRate = float64(blPassed) / float64(totalBL)
	}
	totalCand := candPassed + candFailed
	if totalCand > 0 {
		s.CandidatePassRate = float64(candPassed) / float64(totalCand)
	}

	s.BaselineAvgScore = avg(blScores)
	s.CandidateAvgScore = avg(candScores)
	s.AvgScoreDelta = s.CandidateAvgScore - s.BaselineAvgScore

	s.BaselineP95LatencyMs = p95(blLatencies)
	s.CandidateP95LatencyMs = p95(candLatencies)
	if s.BaselineP95LatencyMs > 0 {
		s.LatencyDeltaPct = float64(s.CandidateP95LatencyMs-s.BaselineP95LatencyMs) / float64(s.BaselineP95LatencyMs) * 100
	}

	s.BaselineAvgCost = avg(blCosts)
	s.CandidateAvgCost = avg(candCosts)
	if s.BaselineAvgCost > 0 {
		s.CostDeltaPct = (s.CandidateAvgCost - s.BaselineAvgCost) / s.BaselineAvgCost * 100
	}

	if totalBL > 0 {
		s.BaselineErrorRate = float64(blFailed) / float64(totalBL)
	}
	if totalCand > 0 {
		s.CandidateErrorRate = float64(candFailed) / float64(totalCand)
	}

	return s
}

func calculateStatisticalSignificance(
	results []ComparisonCaseResult,
	seed int64,
	iterations, minimumSamples int,
) StatisticalSignificance {
	significance := StatisticalSignificance{
		Seed: seed, Iterations: iterations, Conclusion: "insufficient_sample",
		Metrics: map[string]SignificanceMetric{},
	}
	var score, passRate, latency, cost []float64
	for _, result := range results {
		if result.Status == "incomparable" {
			continue
		}
		score = append(score, result.CandidateScore-result.BaselineScore)
		passRate = append(passRate, boolFloat(result.CandidatePassed)-boolFloat(result.BaselinePassed))
		latency = append(latency, float64(result.CandidateLatencyMs-result.BaselineLatencyMs))
		cost = append(cost, result.CandidateCost-result.BaselineCost)
	}
	significance.SampleSize = len(score)
	if len(score) == 0 || iterations <= 0 {
		return significance
	}
	random := rand.New(rand.NewSource(seed))
	significance.Metrics["score"] = bootstrapMetric(score, iterations, random)
	significance.Metrics["pass_rate"] = bootstrapMetric(passRate, iterations, random)
	significance.Metrics["latency_ms"] = bootstrapMetric(latency, iterations, random)
	significance.Metrics["cost"] = bootstrapMetric(cost, iterations, random)
	if len(score) < minimumSamples {
		return significance
	}
	scoreMetric := significance.Metrics["score"]
	passMetric := significance.Metrics["pass_rate"]
	if scoreMetric.CIUpper < 0 || passMetric.CIUpper < 0 {
		significance.Conclusion = "significant_regression"
	} else {
		significance.Conclusion = "no_significant_change"
	}
	return significance
}

func bootstrapMetric(differences []float64, iterations int, random *rand.Rand) SignificanceMetric {
	means := make([]float64, iterations)
	for iteration := 0; iteration < iterations; iteration++ {
		var total float64
		for range differences {
			total += differences[random.Intn(len(differences))]
		}
		means[iteration] = total / float64(len(differences))
	}
	sort.Float64s(means)
	lower := means[int(float64(iterations-1)*0.025)]
	upper := means[int(float64(iterations-1)*0.975)]
	var nonPositive, nonNegative int
	for _, value := range means {
		if value <= 0 {
			nonPositive++
		}
		if value >= 0 {
			nonNegative++
		}
	}
	pValue := 2 * math.Min(float64(nonPositive), float64(nonNegative)) / float64(iterations)
	if pValue > 1 {
		pValue = 1
	}
	return SignificanceMetric{
		MeanDelta: avg(differences), CILower: lower, CIUpper: upper, PValue: pValue,
	}
}

func boolFloat(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func (s *CompareService) calculateSafetyRegressed(ctx context.Context, projectID string, pairedEvalResults []pairedEvalRun) (bool, error) {
	if s.scoreQ == nil {
		return false, errors.New("eval score reader not configured — cannot determine safety regressed")
	}
	for _, pair := range pairedEvalResults {
		if pair.candidateRunID == "" {
			continue
		}
		candScores, err := s.scoreQ.ListEvalScoresByRun(ctx, projectID, pair.candidateRunID)
		if err != nil {
			return false, fmt.Errorf("list candidate eval scores for run %s: %w", pair.candidateRunID, err)
		}
		// Filter candidate safety scores
		var candSafety []EvalScoreResult
		for _, score := range candScores {
			if score.MetricKind == "safety" {
				candSafety = append(candSafety, score)
			}
		}
		if len(candSafety) == 0 {
			continue
		}

		// Read baseline safety scores
		var blSafety []EvalScoreResult
		if pair.baselineRunID != "" {
			blScores, err := s.scoreQ.ListEvalScoresByRun(ctx, projectID, pair.baselineRunID)
			if err != nil {
				return false, fmt.Errorf("list baseline eval scores for run %s: %w", pair.baselineRunID, err)
			}
			for _, score := range blScores {
				if score.MetricKind == "safety" {
					blSafety = append(blSafety, score)
				}
			}
		}

		// Build baseline lookup: metric_name|metric_kind -> passed
		blByKey := make(map[string]bool)
		for _, s := range blSafety {
			key := s.MetricName + "|" + s.MetricKind
			blByKey[key] = s.Passed
		}

		// Check each candidate safety score
		for _, cand := range candSafety {
			if !cand.Passed {
				key := cand.MetricName + "|" + cand.MetricKind
				blPassed, exists := blByKey[key]
				if !exists {
					// Candidate has a new safety metric that failed → regression
					return true, nil
				}
				if blPassed {
					// Baseline passed, candidate failed → REGRESSION
					return true, nil
				}
				// Both baseline and candidate failed → NOT a new regression
			}
		}
	}
	return false, nil
}

type pairedEvalRun struct {
	baselineRunID  string
	candidateRunID string
}

// computeMultiDimMetrics aggregates eval scores by stable key (metric_kind:metric_name) across all paired runs.
// Returns nil if the score reader is not configured.
func (s *CompareService) computeMultiDimMetrics(ctx context.Context, projectID string, pairedEvalResults []pairedEvalRun) (map[string]MultiDimMetricComparison, error) {
	if s.scoreQ == nil {
		return nil, nil
	}

	// Accumulate scores by stable key = metric_kind + ":" + metric_name
	type metricAccum struct {
		scores      []float64
		passedCount int
		totalCount  int
		metricKind  string
		metricName  string
	}
	blAccum := make(map[string]*metricAccum)
	candAccum := make(map[string]*metricAccum)

	for _, pair := range pairedEvalResults {
		// Baseline
		if pair.baselineRunID != "" {
			scores, err := s.scoreQ.ListEvalScoresByRun(ctx, projectID, pair.baselineRunID)
			if err != nil {
				return nil, fmt.Errorf("list baseline eval scores for run %s: %w", pair.baselineRunID, err)
			}
			for _, sc := range scores {
				key := sc.MetricKind + ":" + sc.MetricName
				if _, ok := blAccum[key]; !ok {
					blAccum[key] = &metricAccum{metricKind: sc.MetricKind, metricName: sc.MetricName}
				}
				blAccum[key].scores = append(blAccum[key].scores, sc.Score)
				blAccum[key].totalCount++
				if sc.Passed {
					blAccum[key].passedCount++
				}
			}
		}
		// Candidate
		if pair.candidateRunID != "" {
			scores, err := s.scoreQ.ListEvalScoresByRun(ctx, projectID, pair.candidateRunID)
			if err != nil {
				return nil, fmt.Errorf("list candidate eval scores for run %s: %w", pair.candidateRunID, err)
			}
			for _, sc := range scores {
				key := sc.MetricKind + ":" + sc.MetricName
				if _, ok := candAccum[key]; !ok {
					candAccum[key] = &metricAccum{metricKind: sc.MetricKind, metricName: sc.MetricName}
				}
				candAccum[key].scores = append(candAccum[key].scores, sc.Score)
				candAccum[key].totalCount++
				if sc.Passed {
					candAccum[key].passedCount++
				}
			}
		}
	}

	// Merge all unique keys
	allKeys := make(map[string]bool)
	for k := range blAccum {
		allKeys[k] = true
	}
	for k := range candAccum {
		allKeys[k] = true
	}

	result := make(map[string]MultiDimMetricComparison, len(allKeys))
	for key := range allKeys {
		m := MultiDimMetricComparison{}
		if bl, ok := blAccum[key]; ok {
			m.MetricName = bl.metricName
			m.MetricKind = bl.metricKind
			m.BaselineAvg = avg(bl.scores)
			m.BaselinePassedCount = bl.passedCount
			m.BaselineTotalCount = bl.totalCount
		}
		if cand, ok := candAccum[key]; ok {
			m.MetricName = cand.metricName
			m.MetricKind = cand.metricKind
			m.CandidateAvg = avg(cand.scores)
			m.CandidatePassedCount = cand.passedCount
			m.CandidateTotalCount = cand.totalCount
		}
		m.Delta = m.CandidateAvg - m.BaselineAvg
		result[key] = m
	}
	return result, nil
}

func applyRegressionGates(summary ComparisonSummary, gates *RegressionGate, warnings []string) []string {
	var reasons []string
	reasons = append(reasons, warnings...)

	if gates.MinPassRateDelta > 0 {
		delta := summary.CandidatePassRate - summary.BaselinePassRate
		if delta < -gates.MinPassRateDelta {
			reasons = append(reasons, fmt.Sprintf(
				"通过率下降超过阈值: baseline=%.1f%%, candidate=%.1f%%, 阈值=%.1f%%",
				summary.BaselinePassRate*100, summary.CandidatePassRate*100, gates.MinPassRateDelta*100,
			))
		}
	}

	if gates.MinScoreDelta > 0 && summary.AvgScoreDelta < -gates.MinScoreDelta {
		reasons = append(reasons, fmt.Sprintf(
			"综合分下降超过阈值: delta=%.4f, 阈值=%.4f",
			summary.AvgScoreDelta, -gates.MinScoreDelta,
		))
	}

	if gates.MaxCostIncreaseRatio > 0 && summary.CostDeltaPct > gates.MaxCostIncreaseRatio*100 {
		reasons = append(reasons, fmt.Sprintf(
			"平均成本上升超过阈值: baseline=%.4f, candidate=%.4f, 变化=%.1f%%, 阈值=%.1f%%",
			summary.BaselineAvgCost, summary.CandidateAvgCost,
			summary.CostDeltaPct, gates.MaxCostIncreaseRatio*100,
		))
	}

	if gates.MaxP95LatencyIncPct > 0 && summary.LatencyDeltaPct > gates.MaxP95LatencyIncPct {
		reasons = append(reasons, fmt.Sprintf(
			"P95 延迟上升超过阈值: baseline=%dms, candidate=%dms, 变化=%.1f%%, 阈值=%.1f%%",
			summary.BaselineP95LatencyMs, summary.CandidateP95LatencyMs,
			summary.LatencyDeltaPct, gates.MaxP95LatencyIncPct,
		))
	}

	if gates.SafetyVetoEnabled && summary.SafetyRegressed {
		reasons = append(reasons, "安全 veto 触发: candidate 新增安全失败 case")
	}

	return reasons
}

func avg(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func p95(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := make([]int64, len(values))
	copy(sorted, values)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := int(math.Ceil(float64(len(sorted))*0.95)) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}
