package evals

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ---- Domain types ----

// MetricDefinition represents a registered metric definition for a project.
type MetricDefinition struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Config    string    `json:"config,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// EvalScore represents a single multi-dimensional evaluation score.
type EvalScore struct {
	ID             string    `json:"id"`
	ProjectID      string    `json:"project_id"`
	RunID          string    `json:"run_id"`
	TraceID        string    `json:"trace_id"`
	SpanID         string    `json:"span_id,omitempty"`
	MetricName     string    `json:"metric_name"`
	MetricKind     string    `json:"metric_kind"`
	Score          float64   `json:"score"`
	Passed         bool      `json:"passed"`
	Reason         string    `json:"reason"`
	Details        string    `json:"details,omitempty"`
	EvaluatorRunID string    `json:"evaluator_run_id,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// MetricKindAggregation represents aggregated scores by metric kind.
type MetricKindAggregation struct {
	MetricKind  string  `json:"metric_kind"`
	TotalCount  int64   `json:"total_count"`
	PassedCount int64   `json:"passed_count"`
	AvgScore    float64 `json:"avg_score"`
}

// Supported metric kinds
var SupportedMetricKinds = map[string]bool{
	"final_answer":         true,
	"trajectory":           true,
	"tool_correctness":     true,
	"argument_correctness": true,
	"rag_groundedness":     true,
	"safety":               true,
	"efficiency":           true,
		"helpfulness":          true,
}

// ---- Querier interface ----

// Querier defines the database operations the evals service needs.
type Querier interface {
	CreateMetricDefinition(ctx context.Context, projectID, name, kind, config string) (MetricDefinition, error)
	ListMetricDefinitions(ctx context.Context, projectID string) ([]MetricDefinition, error)
	GetMetricDefinition(ctx context.Context, definitionID string) (MetricDefinition, error)

	CreateEvalScore(ctx context.Context, score EvalScore) (EvalScore, error)
	ListEvalScoresByRun(ctx context.Context, projectID, runID string) ([]EvalScore, error)
	ListEvalScoresByProject(ctx context.Context, projectID string) ([]EvalScore, error)
	AggregateEvalScoresByExperiment(ctx context.Context, projectID, experimentID string) ([]MetricKindAggregation, error)
}

// ---- Service ----

// Service provides eval score operations.
type Service struct {
	q Querier
}

// NewService creates a new evals Service.
func NewService(q Querier) *Service {
	return &Service{q: q}
}

// ---- Metric Definition methods ----

// CreateMetricDefinition creates a new metric definition with project isolation.
func (s *Service) CreateMetricDefinition(ctx context.Context, projectID, name, kind, config string) (MetricDefinition, error) {
	if projectID == "" {
		return MetricDefinition{}, errors.New("projectID is required")
	}
	if name == "" {
		return MetricDefinition{}, errors.New("name is required")
	}
	if kind == "" {
		return MetricDefinition{}, errors.New("kind is required")
	}
	if !SupportedMetricKinds[kind] {
		return MetricDefinition{}, fmt.Errorf("unsupported metric kind %q", kind)
	}
	return s.q.CreateMetricDefinition(ctx, projectID, name, kind, config)
}

// ListMetricDefinitions returns all metric definitions for a project.
func (s *Service) ListMetricDefinitions(ctx context.Context, projectID string) ([]MetricDefinition, error) {
	if projectID == "" {
		return nil, errors.New("projectID is required")
	}
	return s.q.ListMetricDefinitions(ctx, projectID)
}

// GetMetricDefinition returns a single metric definition by ID.
func (s *Service) GetMetricDefinition(ctx context.Context, definitionID string) (MetricDefinition, error) {
	if definitionID == "" {
		return MetricDefinition{}, errors.New("definitionID is required")
	}
	return s.q.GetMetricDefinition(ctx, definitionID)
}

// ---- Eval Score methods ----

// CreateEvalScore creates a score with validation of project and run.
func (s *Service) CreateEvalScore(ctx context.Context, score EvalScore) (EvalScore, error) {
	if score.ProjectID == "" {
		return EvalScore{}, errors.New("projectID is required")
	}
	if score.RunID == "" {
		return EvalScore{}, errors.New("runID is required")
	}
	if score.MetricName == "" {
		return EvalScore{}, errors.New("metricName is required")
	}
	if score.MetricKind == "" {
		return EvalScore{}, errors.New("metricKind is required")
	}
	if !SupportedMetricKinds[score.MetricKind] {
		return EvalScore{}, fmt.Errorf("unsupported metric kind %q", score.MetricKind)
	}
	if score.Score < 0.0 || score.Score > 1.0 {
		return EvalScore{}, fmt.Errorf("score %f out of range [0.0, 1.0]", score.Score)
	}
	return s.q.CreateEvalScore(ctx, score)
}

// ListEvalScoresByRun returns all eval scores for a run.
func (s *Service) ListEvalScoresByRun(ctx context.Context, projectID, runID string) ([]EvalScore, error) {
	if projectID == "" {
		return nil, errors.New("projectID is required")
	}
	if runID == "" {
		return nil, errors.New("runID is required")
	}
	return s.q.ListEvalScoresByRun(ctx, projectID, runID)
}

// ListEvalScoresByProject returns all eval scores for a project.
func (s *Service) ListEvalScoresByProject(ctx context.Context, projectID string) ([]EvalScore, error) {
	if projectID == "" {
		return nil, errors.New("projectID is required")
	}
	return s.q.ListEvalScoresByProject(ctx, projectID)
}

// AggregateEvalScoresByExperiment aggregates scores by metric kind for a given experiment.
func (s *Service) AggregateEvalScoresByExperiment(ctx context.Context, projectID, experimentID string) ([]MetricKindAggregation, error) {
	if projectID == "" {
		return nil, errors.New("projectID is required")
	}
	if experimentID == "" {
		return nil, errors.New("experimentID is required")
	}
	return s.q.AggregateEvalScoresByExperiment(ctx, projectID, experimentID)
}
