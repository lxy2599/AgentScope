package adapter

import (
	"context"

	"AgentScope/internal/evals"
)

// compile-time check
var _ EvalHTTPService = (*HTTPEvalService)(nil)

// EvalHTTPService defines the eval operations needed by HTTP handlers.
type EvalHTTPService interface {
	CreateMetricDefinition(ctx context.Context, projectID, name, kind, config string) (evals.MetricDefinition, error)
	UpdateMetricDefinition(ctx context.Context, projectID, definitionID, name, kind, config string) (evals.MetricDefinition, error)
	DeleteMetricDefinition(ctx context.Context, projectID, definitionID string) error
	ListMetricDefinitions(ctx context.Context, projectID string) ([]evals.MetricDefinition, error)
	CreateEvalScore(ctx context.Context, score evals.EvalScore) (evals.EvalScore, error)
	ListEvalScoresByRun(ctx context.Context, projectID, runID string) ([]evals.EvalScore, error)
	ListEvalScoresByRuns(ctx context.Context, projectID string, runIDs []string) ([]evals.EvalScore, error)
	AggregateEvalScoresByExperiment(ctx context.Context, projectID, experimentID string) ([]evals.MetricKindAggregation, error)
}

// HTTPEvalService wraps an evals.Service to implement EvalHTTPService.
type HTTPEvalService struct {
	svc *evals.Service
}

// NewHTTPEvalService creates a new HTTPEvalService.
func NewHTTPEvalService(svc *evals.Service) *HTTPEvalService {
	return &HTTPEvalService{svc: svc}
}

func (h *HTTPEvalService) CreateMetricDefinition(ctx context.Context, projectID, name, kind, config string) (evals.MetricDefinition, error) {
	return h.svc.CreateMetricDefinition(ctx, projectID, name, kind, config)
}

func (h *HTTPEvalService) ListMetricDefinitions(ctx context.Context, projectID string) ([]evals.MetricDefinition, error) {
	return h.svc.ListMetricDefinitions(ctx, projectID)
}

func (h *HTTPEvalService) CreateEvalScore(ctx context.Context, score evals.EvalScore) (evals.EvalScore, error) {
	return h.svc.CreateEvalScore(ctx, score)
}

func (h *HTTPEvalService) ListEvalScoresByRun(ctx context.Context, projectID, runID string) ([]evals.EvalScore, error) {
	return h.svc.ListEvalScoresByRun(ctx, projectID, runID)
}

func (h *HTTPEvalService) ListEvalScoresByRuns(ctx context.Context, projectID string, runIDs []string) ([]evals.EvalScore, error) {
	return h.svc.ListEvalScoresByRuns(ctx, projectID, runIDs)
}

func (h *HTTPEvalService) AggregateEvalScoresByExperiment(ctx context.Context, projectID, experimentID string) ([]evals.MetricKindAggregation, error) {
	return h.svc.AggregateEvalScoresByExperiment(ctx, projectID, experimentID)
}

func (h *HTTPEvalService) UpdateMetricDefinition(ctx context.Context, projectID, definitionID, name, kind, config string) (evals.MetricDefinition, error) {
	return h.svc.UpdateMetricDefinition(ctx, projectID, definitionID, name, kind, config)
}

func (h *HTTPEvalService) DeleteMetricDefinition(ctx context.Context, projectID, definitionID string) error {
	return h.svc.DeleteMetricDefinition(ctx, projectID, definitionID)
}

// ---- Compile-time interface checks for nil references in app.go ----
// These are used to verify the compile-time check pattern without needing
// a value assignment before the var block.
