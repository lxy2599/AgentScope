package experiments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

func calculateSafetyRegressed(ctx context.Context, s *CompareService, projectID string, pairedEvalResults []pairedEvalRun) bool {
	if s.scoreQ == nil {
		return false
	}
	for _, pair := range pairedEvalResults {
		// Check candidate safety scores
		if pair.candidateRunID == "" {
			continue
		}
		candScores, err := s.scoreQ.ListEvalScoresByRun(ctx, projectID, pair.candidateRunID)
		if err != nil {
			continue
		}
		for _, score := range candScores {
			if score.MetricKind == "safety" && !score.Passed {
				return true
			}
		}
	}
	return false
}

type pairedEvalRun struct {
	baselineRunID  string
	candidateRunID string
	baselineFailed bool
	candidateFailed bool
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
