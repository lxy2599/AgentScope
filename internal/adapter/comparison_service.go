package adapter

import (
	"context"
	"encoding/json"

	"AgentScope/internal/experiments"
	"AgentScope/internal/httpapi"
)

// HTTPCompareService implements httpapi.CompareService using experiments.CompareService.
type HTTPCompareService struct {
	svc *experiments.CompareService
}

var _ httpapi.CompareService = (*HTTPCompareService)(nil)

// NewHTTPCompareService creates a new HTTPCompareService.
func NewHTTPCompareService(svc *experiments.CompareService) *HTTPCompareService {
	return &HTTPCompareService{svc: svc}
}

func (h *HTTPCompareService) CreateComparison(ctx context.Context, projectID, baselineExpID, candidateExpID, createdBy, compType, regressionGatesJSON string) (httpapi.ComparisonDetail, error) {
	// Parse regression gates JSON if provided
	var gates *experiments.RegressionGate
	if regressionGatesJSON != "" {
		gates = &experiments.RegressionGate{}
		if err := json.Unmarshal([]byte(regressionGatesJSON), gates); err != nil {
			gates = nil
		}
	}

	detail, err := h.svc.CreateComparison(ctx, projectID, baselineExpID, candidateExpID, createdBy,
		experiments.ComparisonType(compType), gates)
	if err != nil {
		return httpapi.ComparisonDetail{}, err
	}

	return comparisonDetailToHTTP(detail), nil
}

func (h *HTTPCompareService) GetComparison(ctx context.Context, comparisonID string) (httpapi.ComparisonDetail, error) {
	detail, err := h.svc.GetComparison(ctx, comparisonID)
	if err != nil {
		return httpapi.ComparisonDetail{}, err
	}
	return comparisonDetailToHTTP(detail), nil
}

func (h *HTTPCompareService) ListComparisons(ctx context.Context, projectID string) ([]httpapi.Comparison, error) {
	comps, err := h.svc.ListComparisons(ctx, projectID)
	if err != nil {
		return nil, err
	}
	result := make([]httpapi.Comparison, 0, len(comps))
	for _, c := range comps {
		result = append(result, httpapi.Comparison{
			ID:                    c.ID,
			ProjectID:             c.ProjectID,
			BaselineExperimentID:  c.BaselineExperimentID,
			CandidateExperimentID: c.CandidateExperimentID,
			ComparisonType:        string(c.ComparisonType),
			CreatedBy:             c.CreatedBy,
			Status:                c.Status,
			Summary:               c.Summary,
			RegressionGates:       c.RegressionGates,
			CreatedAt:             c.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt:             c.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	return result, nil
}

func comparisonDetailToHTTP(d experiments.ComparisonDetail) httpapi.ComparisonDetail {
	return httpapi.ComparisonDetail{
		Comparison: httpapi.Comparison{
			ID:                    d.ID,
			ProjectID:             d.ProjectID,
			BaselineExperimentID:  d.BaselineExperimentID,
			CandidateExperimentID: d.CandidateExperimentID,
			ComparisonType:        string(d.ComparisonType),
			CreatedBy:             d.CreatedBy,
			Status:                d.Status,
			Summary:               d.Summary,
			RegressionGates:       d.RegressionGates,
			CreatedAt:             d.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt:             d.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		},
		BaselineExperiment: experimentToHTTP(d.BaselineExperiment),
		CandidateExperiment: experimentToHTTP(d.CandidateExperiment),
		ComparisonSummary:  comparisonSummaryToHTTP(d.ComparisonSummary),
		CaseResults:        comparisonCaseResultsToHTTP(d.CaseResults),
	}
}

func experimentToHTTP(e experiments.Experiment) httpapi.Experiment {
	return httpapi.Experiment{
		ID:            e.ID,
		ProjectID:     e.ProjectID,
		DatasetID:     e.DatasetID,
		EvaluatorID:   e.EvaluatorID,
		TargetName:    e.TargetName,
		TargetVersion: e.TargetVersion,
		Status:        e.Status,
		TotalCases:    e.TotalCases,
		PassedCases:   e.PassedCases,
		FailedCases:   e.FailedCases,
		CreatedAt:     e.CreatedAt.Format("2006-01-02T15:04:05Z"),
		// Harness metadata
		ModelProvider:    e.ModelProvider,
		ModelName:        e.ModelName,
		ModelVersion:     e.ModelVersion,
		PromptVersion:    e.PromptVersion,
		HarnessVersion:   e.HarnessVersion,
		DatasetVersion:   e.DatasetVersion,
		ToolsetVersion:   e.ToolsetVersion,
		SkillsetVersion:  e.SkillsetVersion,
		RAGConfigVersion: e.RAGConfigVersion,
		FeatureFlags:     e.FeatureFlags,
	}
}

func comparisonSummaryToHTTP(s experiments.ComparisonSummary) httpapi.ComparisonSummary {
	return httpapi.ComparisonSummary{
		TotalCases:            s.TotalCases,
		PairedCases:           s.PairedCases,
		BaselinePassRate:      s.BaselinePassRate,
		CandidatePassRate:     s.CandidatePassRate,
		BaselineAvgScore:      s.BaselineAvgScore,
		CandidateAvgScore:     s.CandidateAvgScore,
		AvgScoreDelta:         s.AvgScoreDelta,
		ImprovedCases:         s.ImprovedCases,
		RegressedCases:        s.RegressedCases,
		UnchangedCases:        s.UnchangedCases,
		IncomparableCases:     s.IncomparableCases,
		BaselineP95LatencyMs:  s.BaselineP95LatencyMs,
		CandidateP95LatencyMs: s.CandidateP95LatencyMs,
		LatencyDeltaPct:       s.LatencyDeltaPct,
		BaselineAvgCost:       s.BaselineAvgCost,
		CandidateAvgCost:      s.CandidateAvgCost,
		CostDeltaPct:          s.CostDeltaPct,
		BaselineErrorRate:     s.BaselineErrorRate,
		CandidateErrorRate:    s.CandidateErrorRate,
		SafetyRegressed:       s.SafetyRegressed,
		RegressionTriggered:   s.RegressionTriggered,
		RegressionReasons:     s.RegressionReasons,
		MultiDimMetrics:       s.MultiDimMetrics,
	}
}

func comparisonCaseResultsToHTTP(results []experiments.ComparisonCaseResult) []httpapi.ComparisonCaseResult {
	r := make([]httpapi.ComparisonCaseResult, 0, len(results))
	for _, cr := range results {
		r = append(r, httpapi.ComparisonCaseResult{
			ID:                cr.ID,
			ComparisonID:      cr.ComparisonID,
			DatasetCaseID:     cr.DatasetCaseID,
			BaselineRunID:     cr.BaselineRunID,
			CandidateRunID:    cr.CandidateRunID,
			BaselineScore:     cr.BaselineScore,
			CandidateScore:    cr.CandidateScore,
			BaselinePassed:    cr.BaselinePassed,
			CandidatePassed:   cr.CandidatePassed,
			ScoreDiff:         cr.ScoreDiff,
			Status:            cr.Status,
			BaselineLatencyMs: cr.BaselineLatencyMs,
			CandidateLatencyMs: cr.CandidateLatencyMs,
			BaselineCost:      cr.BaselineCost,
			CandidateCost:     cr.CandidateCost,
			CreatedAt:         cr.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	return r
}
