package adapter

import (
	"context"

	"AgentScope/internal/evals"
	"AgentScope/internal/onlinerval"
	"AgentScope/internal/runs"
)

// OnlineRunService adapts runs.Service to implement onlinerval.RunService.
type OnlineRunService struct {
	svc *runs.Service
}

var _ onlinerval.RunService = (*OnlineRunService)(nil)

// NewOnlineRunService creates a new OnlineRunService adapter.
func NewOnlineRunService(svc *runs.Service) *OnlineRunService {
	return &OnlineRunService{svc: svc}
}

func (s *OnlineRunService) GetRun(ctx context.Context, runID string) (*onlinerval.Run, error) {
	run, err := s.svc.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	return &onlinerval.Run{
		ID:        run.ID,
		ProjectID: run.ProjectID,
		Status:    run.Status,
		TraceID:   run.TraceID,
		AgentName: run.AgentName,
		Error:     run.Error,
	}, nil
}

// OnlineEvalService adapts evals.Service to implement onlinerval.EvalService.
type OnlineEvalService struct {
	svc *evals.Service
}

var _ onlinerval.EvalService = (*OnlineEvalService)(nil)

// NewOnlineEvalService creates a new OnlineEvalService adapter.
func NewOnlineEvalService(svc *evals.Service) *OnlineEvalService {
	return &OnlineEvalService{svc: svc}
}

func (s *OnlineEvalService) CreateEvalScore(ctx context.Context, score onlinerval.EvalScore) error {
	_, err := s.svc.CreateEvalScore(ctx, evals.EvalScore{
		ProjectID:  score.ProjectID,
		RunID:      score.RunID,
		TraceID:    score.TraceID,
		MetricName: score.MetricName,
		MetricKind: score.MetricKind,
		Score:      score.Score,
		Passed:     score.Passed,
		Reason:     score.Reason,
	})
	return err
}

// OnlineRunDetailAdapter adapts runs.Service to implement onlinerval.RunDetailProvider.
type OnlineRunDetailAdapter struct {
	svc *runs.Service
}

var _ onlinerval.RunDetailProvider = (*OnlineRunDetailAdapter)(nil)

// NewOnlineRunDetailAdapter creates a new OnlineRunDetailAdapter.
func NewOnlineRunDetailAdapter(svc *runs.Service) *OnlineRunDetailAdapter {
	return &OnlineRunDetailAdapter{svc: svc}
}

// GetRunDetail returns run detail with full trace data for the evaluator.
func (a *OnlineRunDetailAdapter) GetRunDetail(ctx context.Context, runID string) (*runs.RunDetail, error) {
	detail, err := a.svc.GetRunDetail(ctx, runID)
	if err != nil {
		return nil, err
	}
	return &detail, nil
}
