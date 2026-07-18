package adapter

import (
	"context"
	"fmt"
	"sort"

	"AgentScope/internal/evals"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// EvalQuerier implements evals.Querier using store.Queries.
type EvalQuerier struct {
	q *store.Queries
}

// compile-time check
var _ evals.Querier = (*EvalQuerier)(nil)

// NewEvalQuerier creates a new EvalQuerier.
func NewEvalQuerier(q *store.Queries) *EvalQuerier {
	return &EvalQuerier{q: q}
}

// CreateMetricDefinition creates a metric definition.
func (e *EvalQuerier) CreateMetricDefinition(ctx context.Context, projectID, name, kind, config string) (evals.MetricDefinition, error) {
	storeDef, err := e.q.CreateMetricDefinition(ctx, store.CreateMetricDefinitionParams{
		ProjectID: parsePGUUID(projectID),
		Name:      name,
		Kind:      kind,
		Config:    []byte(config),
	})
	if err != nil {
		return evals.MetricDefinition{}, fmt.Errorf("create metric definition: %w", err)
	}
	return metricDefToDomain(storeDef), nil
}

// ListMetricDefinitions returns all metric definitions for a project.
func (e *EvalQuerier) ListMetricDefinitions(ctx context.Context, projectID string) ([]evals.MetricDefinition, error) {
	storeDefs, err := e.q.ListMetricDefinitionsByProject(ctx, parsePGUUID(projectID))
	if err != nil {
		return nil, fmt.Errorf("list metric definitions: %w", err)
	}
	result := make([]evals.MetricDefinition, 0, len(storeDefs))
	for _, d := range storeDefs {
		result = append(result, metricDefToDomain(d))
	}
	return result, nil
}

// GetMetricDefinition returns a single metric definition by ID.
func (e *EvalQuerier) GetMetricDefinition(ctx context.Context, definitionID string) (evals.MetricDefinition, error) {
	storeDef, err := e.q.GetMetricDefinitionByID(ctx, parsePGUUID(definitionID))
	if err != nil {
		return evals.MetricDefinition{}, fmt.Errorf("get metric definition: %w", err)
	}
	return metricDefToDomain(storeDef), nil
}

// CreateEvalScore creates an eval score.
func (e *EvalQuerier) CreateEvalScore(ctx context.Context, score evals.EvalScore) (evals.EvalScore, error) {
	var evaluatorRunID pgtype.UUID
	if score.EvaluatorRunID != "" {
		evaluatorRunID = parsePGUUID(score.EvaluatorRunID)
	}

	var numericScore pgtype.Numeric
	if err := numericScore.Scan(fmt.Sprintf("%f", score.Score)); err != nil {
		return evals.EvalScore{}, fmt.Errorf("convert score to numeric: %w", err)
	}

	storeScore, err := e.q.CreateEvalScore(ctx, store.CreateEvalScoreParams{
		ProjectID:      parsePGUUID(score.ProjectID),
		RunID:          parsePGUUID(score.RunID),
		TraceID:        score.TraceID,
		SpanID:         score.SpanID,
		MetricName:     score.MetricName,
		MetricKind:     score.MetricKind,
		Score:          numericScore,
		Passed:         score.Passed,
		Reason:         score.Reason,
		Details:        []byte(score.Details),
		EvaluatorRunID: evaluatorRunID,
	})
	if err != nil {
		return evals.EvalScore{}, fmt.Errorf("create eval score: %w", err)
	}
	return storeEvalScoreToDomain(storeScore), nil
}

// ListEvalScoresByRun returns all eval scores for a run.
func (e *EvalQuerier) ListEvalScoresByRun(ctx context.Context, runID string) ([]evals.EvalScore, error) {
	storeScores, err := e.q.ListEvalScoresByRun(ctx, parsePGUUID(runID))
	if err != nil {
		return nil, fmt.Errorf("list eval scores by run: %w", err)
	}
	return storeEvalScoresToDomain(storeScores), nil
}

// ListEvalScoresByProject returns all eval scores for a project.
func (e *EvalQuerier) ListEvalScoresByProject(ctx context.Context, projectID string) ([]evals.EvalScore, error) {
	storeScores, err := e.q.ListEvalScoresByProject(ctx, parsePGUUID(projectID))
	if err != nil {
		return nil, fmt.Errorf("list eval scores by project: %w", err)
	}
	return storeEvalScoresToDomain(storeScores), nil
}

// AggregateEvalScoresByExperiment aggregates scores by metric kind for an experiment.
func (e *EvalQuerier) AggregateEvalScoresByExperiment(ctx context.Context, experimentID string) ([]evals.MetricKindAggregation, error) {
	rows, err := e.q.AggregateEvalScoresByExperiment(ctx, parsePGUUID(experimentID))
	if err != nil {
		return nil, fmt.Errorf("aggregate eval scores by experiment: %w", err)
	}
	result := make([]evals.MetricKindAggregation, 0, len(rows))
	for _, row := range rows {
		avgScore := 0.0
		if row.AvgScore.Valid {
			fv, _ := row.AvgScore.Float64Value()
			avgScore = fv.Float64
		}
		result = append(result, evals.MetricKindAggregation{
			MetricKind:  row.MetricKind,
			TotalCount:  row.TotalCount,
			PassedCount: row.PassedCount,
			AvgScore:    avgScore,
		})
	}
	// Sort by metric kind for consistent output
	sort.Slice(result, func(i, j int) bool {
		return result[i].MetricKind < result[j].MetricKind
	})
	return result, nil
}

// ---- Conversion helpers ----

func metricDefToDomain(d store.EvalMetricDefinition) evals.MetricDefinition {
	return evals.MetricDefinition{
		ID:        UUIDToString(d.ID.Bytes),
		ProjectID: UUIDToString(d.ProjectID.Bytes),
		Name:      d.Name,
		Kind:      d.Kind,
		Config:    string(d.Config),
		CreatedAt: d.CreatedAt.Time,
	}
}

func storeEvalScoreToDomain(s store.EvalScore) evals.EvalScore {
	evalRunID := ""
	if s.EvaluatorRunID.Valid {
		evalRunID = UUIDToString(s.EvaluatorRunID.Bytes)
	}
	score := 0.0
	if s.Score.Valid {
		fv, _ := s.Score.Float64Value()
		score = fv.Float64
	}
	return evals.EvalScore{
		ID:             UUIDToString(s.ID.Bytes),
		ProjectID:      UUIDToString(s.ProjectID.Bytes),
		RunID:          UUIDToString(s.RunID.Bytes),
		TraceID:        s.TraceID,
		SpanID:         s.SpanID,
		MetricName:     s.MetricName,
		MetricKind:     s.MetricKind,
		Score:          score,
		Passed:         s.Passed,
		Reason:         s.Reason,
		Details:        string(s.Details),
		EvaluatorRunID: evalRunID,
		CreatedAt:      s.CreatedAt.Time,
	}
}

func storeEvalScoresToDomain(scores []store.EvalScore) []evals.EvalScore {
	result := make([]evals.EvalScore, 0, len(scores))
	for _, s := range scores {
		result = append(result, storeEvalScoreToDomain(s))
	}
	return result
}
