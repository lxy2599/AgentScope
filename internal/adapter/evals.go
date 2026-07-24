package adapter

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"AgentScope/internal/evals"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EvalQuerier implements evals.Querier using store.Queries.
type EvalQuerier struct {
	q    *store.Queries
	pool *pgxpool.Pool
}

// compile-time check
var _ evals.Querier = (*EvalQuerier)(nil)

// NewEvalQuerier creates a new EvalQuerier.
func NewEvalQuerier(q *store.Queries) *EvalQuerier {
	return &EvalQuerier{q: q}
}

// WithPool sets the pool for raw queries and returns the querier.
func (e *EvalQuerier) WithPool(pool *pgxpool.Pool) *EvalQuerier {
	e.pool = pool
	return e
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

	numericScore, err := numericFromFloat64(score.Score)
	if err != nil {
		return evals.EvalScore{}, fmt.Errorf("convert score to numeric: %w", err)
	}

	details := []byte(score.Details)
	if len(details) == 0 {
		details = []byte("{}")
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
		Details:        details,
		EvaluatorRunID: evaluatorRunID,
	})
	if err != nil {
		return evals.EvalScore{}, fmt.Errorf("create eval score: %w", err)
	}
	return storeEvalScoreToDomain(storeScore), nil
}

// CreateEvalScores inserts a batch atomically, rolling back every prior insert
// when any score fails.
func (e *EvalQuerier) CreateEvalScores(ctx context.Context, scores []evals.EvalScore) ([]evals.EvalScore, error) {
	if e.pool == nil {
		return nil, errors.New("eval score batch writes require a database pool")
	}

	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin eval score batch: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	txQuerier := &EvalQuerier{q: e.q.WithTx(tx)}
	results := make([]evals.EvalScore, 0, len(scores))
	for i, score := range scores {
		result, err := txQuerier.CreateEvalScore(ctx, score)
		if err != nil {
			return nil, fmt.Errorf("create eval score %d: %w", i, err)
		}
		results = append(results, result)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit eval score batch: %w", err)
	}
	return results, nil
}

// ListEvalScoresByRun returns all eval scores for a run.
func (e *EvalQuerier) ListEvalScoresByRun(ctx context.Context, projectID, runID string) ([]evals.EvalScore, error) {
	storeScores, err := e.q.ListEvalScoresByRun(ctx, store.ListEvalScoresByRunParams{
		RunID:     parsePGUUID(runID),
		ProjectID: parsePGUUID(projectID),
	})
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
func (e *EvalQuerier) AggregateEvalScoresByExperiment(ctx context.Context, projectID, experimentID string) ([]evals.MetricKindAggregation, error) {
	rows, err := e.q.AggregateEvalScoresByExperiment(ctx, store.AggregateEvalScoresByExperimentParams{
		ExperimentID: parsePGUUID(experimentID),
		ProjectID:    parsePGUUID(projectID),
	})
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

// UpdateMetricDefinition updates a metric definition using raw SQL.
func (e *EvalQuerier) UpdateMetricDefinition(ctx context.Context, projectID, definitionID, name, kind, config string) (evals.MetricDefinition, error) {
	if e.pool == nil {
		return evals.MetricDefinition{}, errors.New("pool not configured for raw queries")
	}
	var id, pid pgtype.UUID
	var created pgtype.Timestamptz
	err := e.pool.QueryRow(ctx,
		`UPDATE eval_metric_definitions SET name=$1, kind=$2, config=$3 WHERE id=$4 AND project_id=$5
		 RETURNING id, project_id, name, kind, config, created_at`,
		name, kind, []byte(config), parsePGUUID(definitionID), parsePGUUID(projectID),
	).Scan(&id, &pid, &name, &kind, &config, &created)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return evals.MetricDefinition{}, errors.New("metric definition not found")
		}
		return evals.MetricDefinition{}, fmt.Errorf("update metric definition: %w", err)
	}
	return evals.MetricDefinition{
		ID:        UUIDToString(id.Bytes),
		ProjectID: UUIDToString(pid.Bytes),
		Name:      name,
		Kind:      kind,
		Config:    config,
		CreatedAt: created.Time,
	}, nil
}

// DeleteMetricDefinition deletes a metric definition using raw SQL.
func (e *EvalQuerier) DeleteMetricDefinition(ctx context.Context, projectID, definitionID string) error {
	if e.pool == nil {
		return errors.New("pool not configured for raw queries")
	}
	tag, err := e.pool.Exec(ctx,
		`DELETE FROM eval_metric_definitions WHERE id=$1 AND project_id=$2`,
		parsePGUUID(definitionID), parsePGUUID(projectID),
	)
	if err != nil {
		return fmt.Errorf("delete metric definition: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("metric definition not found")
	}
	return nil
}

// DeleteEvalScoresByRun deletes all eval scores for a run.
func (e *EvalQuerier) DeleteEvalScoresByRun(ctx context.Context, runID string) error {
	if e.pool == nil {
		return errors.New("pool not configured for raw queries")
	}
	_, err := e.pool.Exec(ctx,
		`DELETE FROM eval_scores WHERE run_id=$1`,
		parsePGUUID(runID),
	)
	if err != nil {
		return fmt.Errorf("delete eval scores by run: %w", err)
	}
	return nil
}

// DeleteEvalScore deletes a single eval score.
func (e *EvalQuerier) DeleteEvalScore(ctx context.Context, projectID, scoreID string) error {
	if e.pool == nil {
		return errors.New("pool not configured for raw queries")
	}
	tag, err := e.pool.Exec(ctx,
		`DELETE FROM eval_scores WHERE id=$1 AND project_id=$2`,
		parsePGUUID(scoreID), parsePGUUID(projectID),
	)
	if err != nil {
		return fmt.Errorf("delete eval score: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errors.New("eval score not found")
	}
	return nil
}

// GetEvalScoreByID returns a single eval score by ID.
func (e *EvalQuerier) GetEvalScoreByID(ctx context.Context, scoreID string) (evals.EvalScore, error) {
	if e.pool == nil {
		return evals.EvalScore{}, errors.New("pool not configured for raw queries")
	}
	var s store.EvalScore
	err := e.pool.QueryRow(ctx,
		`SELECT id, project_id, run_id, trace_id, span_id, metric_name, metric_kind,
		        score, passed, reason, details, evaluator_run_id, created_at
		 FROM eval_scores WHERE id=$1`,
		parsePGUUID(scoreID),
	).Scan(
		&s.ID, &s.ProjectID, &s.RunID, &s.TraceID, &s.SpanID,
		&s.MetricName, &s.MetricKind, &s.Score, &s.Passed, &s.Reason,
		&s.Details, &s.EvaluatorRunID, &s.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return evals.EvalScore{}, errors.New("eval score not found")
		}
		return evals.EvalScore{}, fmt.Errorf("get eval score: %w", err)
	}
	return storeEvalScoreToDomain(s), nil
}

// ListEvalScoresByRuns returns eval scores for multiple runs (avoids N+1).
func (e *EvalQuerier) ListEvalScoresByRuns(ctx context.Context, projectID string, runIDs []string) ([]evals.EvalScore, error) {
	if len(runIDs) == 0 {
		return nil, nil
	}
	uuids := make([]pgtype.UUID, 0, len(runIDs))
	for _, rid := range runIDs {
		uuids = append(uuids, parsePGUUID(rid))
	}
	storeScores, err := e.q.ListEvalScoresByRunIDs(ctx, store.ListEvalScoresByRunIDsParams{
		ProjectID: parsePGUUID(projectID),
		Column2:   uuids,
	})
	if err != nil {
		return nil, fmt.Errorf("list eval scores by runs: %w", err)
	}
	return storeEvalScoresToDomain(storeScores), nil
}
