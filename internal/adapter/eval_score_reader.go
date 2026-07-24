package adapter

import (
	"context"

	"AgentScope/internal/evals"
	"AgentScope/internal/experiments"
)

// EvalScoreReaderAdapter adapts *evals.Service to implement experiments.EvalScoreReader.
type EvalScoreReaderAdapter struct {
	svc *evals.Service
}

// NewEvalScoreReaderAdapter creates a new EvalScoreReaderAdapter.
func NewEvalScoreReaderAdapter(svc *evals.Service) *EvalScoreReaderAdapter {
	return &EvalScoreReaderAdapter{svc: svc}
}

// ListEvalScoresByRun implements experiments.EvalScoreReader.
func (a *EvalScoreReaderAdapter) ListEvalScoresByRun(ctx context.Context, projectID, runID string) ([]experiments.EvalScoreResult, error) {
	scores, err := a.svc.ListEvalScoresByRun(ctx, projectID, runID)
	if err != nil {
		return nil, err
	}
	return evalScoreResults(scores), nil
}

// ListEvalScoresByRuns implements experiments.BatchEvalScoreReader.
func (a *EvalScoreReaderAdapter) ListEvalScoresByRuns(ctx context.Context, projectID string, runIDs []string) ([]experiments.EvalScoreResult, error) {
	scores, err := a.svc.ListEvalScoresByRuns(ctx, projectID, runIDs)
	if err != nil {
		return nil, err
	}
	return evalScoreResults(scores), nil
}

func evalScoreResults(scores []evals.EvalScore) []experiments.EvalScoreResult {
	results := make([]experiments.EvalScoreResult, 0, len(scores))
	for _, s := range scores {
		results = append(results, experiments.EvalScoreResult{
			MetricName: s.MetricName,
			MetricKind: s.MetricKind,
			Score:      s.Score,
			Passed:     s.Passed,
			RunID:      s.RunID,
		})
	}
	return results
}
