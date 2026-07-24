package adapter

import (
	"context"
	"encoding/json"
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
var _ evals.GovernanceQuerier = (*EvalQuerier)(nil)

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

func (e *EvalQuerier) CreateRubricTemplate(ctx context.Context, projectID, name, description string) (evals.RubricTemplate, error) {
	row, err := e.q.CreateRubricTemplate(ctx, store.CreateRubricTemplateParams{
		ProjectID: parsePGUUID(projectID), Name: name, Description: description,
	})
	if err != nil {
		return evals.RubricTemplate{}, fmt.Errorf("create rubric template: %w", err)
	}
	return rubricTemplateToDomain(row), nil
}

func (e *EvalQuerier) CreateRubricVersion(ctx context.Context, projectID, templateID, rubricJSON, createdBy string) (evals.RubricTemplateVersion, error) {
	if e.pool == nil {
		return evals.RubricTemplateVersion{}, errors.New("pool not configured")
	}
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return evals.RubricTemplateVersion{}, err
	}
	defer tx.Rollback(ctx)
	tq := e.q.WithTx(tx)
	templateUUID := parsePGUUID(templateID)
	if _, err := tq.GetRubricTemplateByID(ctx, store.GetRubricTemplateByIDParams{
		ID: templateUUID, ProjectID: parsePGUUID(projectID),
	}); err != nil {
		return evals.RubricTemplateVersion{}, fmt.Errorf("rubric template not found: %w", err)
	}
	if _, err := tx.Exec(ctx, `SELECT id FROM rubric_templates WHERE id=$1 FOR UPDATE`, templateUUID); err != nil {
		return evals.RubricTemplateVersion{}, fmt.Errorf("lock rubric template: %w", err)
	}
	row, err := tq.CreateRubricTemplateVersion(ctx, store.CreateRubricTemplateVersionParams{
		TemplateID: templateUUID,
		Rubric:     []byte(rubricJSON),
		CreatedBy:  parsePGUUID(createdBy),
	})
	if err != nil {
		return evals.RubricTemplateVersion{}, fmt.Errorf("create rubric version: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return evals.RubricTemplateVersion{}, err
	}
	return rubricVersionToDomain(row), nil
}

func (e *EvalQuerier) PublishRubricVersion(ctx context.Context, projectID, versionID string) (evals.RubricTemplateVersion, error) {
	row, err := e.q.PublishRubricTemplateVersion(ctx, store.PublishRubricTemplateVersionParams{
		ID: parsePGUUID(versionID), ProjectID: parsePGUUID(projectID),
	})
	if err != nil {
		return evals.RubricTemplateVersion{}, fmt.Errorf("publish rubric version: %w", err)
	}
	return rubricVersionToDomain(row), nil
}

func (e *EvalQuerier) ListRubricTemplates(ctx context.Context, projectID string) ([]evals.RubricTemplate, error) {
	rows, err := e.q.ListRubricTemplatesByProject(ctx, parsePGUUID(projectID))
	if err != nil {
		return nil, err
	}
	result := make([]evals.RubricTemplate, 0, len(rows))
	for _, row := range rows {
		result = append(result, rubricTemplateToDomain(row))
	}
	return result, nil
}

func (e *EvalQuerier) ListRubricVersions(ctx context.Context, projectID, templateID string) ([]evals.RubricTemplateVersion, error) {
	rows, err := e.q.ListRubricTemplateVersions(ctx, store.ListRubricTemplateVersionsParams{
		TemplateID: templateIDToUUID(templateID), ProjectID: parsePGUUID(projectID),
	})
	if err != nil {
		return nil, err
	}
	result := make([]evals.RubricTemplateVersion, 0, len(rows))
	for _, row := range rows {
		result = append(result, rubricVersionToDomain(row))
	}
	return result, nil
}

func templateIDToUUID(value string) pgtype.UUID { return parsePGUUID(value) }

func (e *EvalQuerier) PersistJudgeAggregation(ctx context.Context, batch evals.JudgeVoteBatch, aggregation evals.JudgeAggregation) (evals.PersistedJudgeAggregation, error) {
	if e.pool == nil {
		return evals.PersistedJudgeAggregation{}, errors.New("pool not configured")
	}
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return evals.PersistedJudgeAggregation{}, err
	}
	defer tx.Rollback(ctx)
	tq := e.q.WithTx(tx)
	projectID := parsePGUUID(batch.ProjectID)
	runID := parsePGUUID(batch.RunID)
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM runs WHERE id=$1 AND project_id=$2)`, runID, projectID).Scan(&exists); err != nil || !exists {
		return evals.PersistedJudgeAggregation{}, errors.New("run not found in project")
	}

	for _, vote := range batch.Votes {
		score, passed := nullableVoteScore(vote.Score, vote.Passed)
		cost, err := nullableNumeric(vote.Cost)
		if err != nil {
			return evals.PersistedJudgeAggregation{}, err
		}
		if _, err := tq.CreateEvalJudgeVote(ctx, store.CreateEvalJudgeVoteParams{
			ProjectID: projectID, RunID: runID, MetricName: batch.MetricName,
			MetricKind: batch.MetricKind, EvaluatorID: parsePGUUID(batch.EvaluatorID),
			RubricVersionID: parsePGUUID(batch.RubricVersionID), Provider: vote.Provider,
			Model: vote.Model, PromptVersion: vote.PromptVersion, Score: score,
			Passed: passed, Reason: vote.Reason, RawResponseHash: vote.RawResponseHash,
			LatencyMs: nullableInt64(vote.LatencyMs), InputTokens: nullableInt64(vote.InputTokens),
			OutputTokens: nullableInt64(vote.OutputTokens), Cost: cost, Error: vote.Error,
			EssentialFailed: vote.EssentialFailed, VetoTriggered: vote.VetoTriggered,
			VetoReason: vote.VetoReason, Details: jsonOrEmpty(vote.Details),
		}); err != nil {
			return evals.PersistedJudgeAggregation{}, fmt.Errorf("create judge vote: %w", err)
		}
	}
	score, _ := numericFromFloat64(aggregation.Score)
	stddev, _ := numericFromFloat64(aggregation.StdDev)
	scoreRange, _ := numericFromFloat64(aggregation.Range)
	disagreement, _ := numericFromFloat64(aggregation.Disagreement)
	confidence, _ := numericFromFloat64(aggregation.Confidence)
	details, _ := json.Marshal(aggregation)
	row, err := tq.CreateEvalScoreAggregation(ctx, store.CreateEvalScoreAggregationParams{
		ProjectID: projectID, RunID: runID, MetricName: batch.MetricName,
		MetricKind: batch.MetricKind, RubricVersionID: parsePGUUID(batch.RubricVersionID),
		Method: aggregation.Method, RequiredVotes: int32(aggregation.RequiredVotes),
		ValidVoteCount: int32(aggregation.ValidVoteCount), ErrorVoteCount: int32(aggregation.ErrorVoteCount),
		Score: score, Passed: aggregation.Passed, Stddev: stddev, ScoreRange: scoreRange,
		Disagreement: disagreement, Confidence: confidence, LowConfidence: aggregation.LowConfidence,
		EssentialFailed: aggregation.EssentialFailed, VetoTriggered: aggregation.VetoTriggered,
		VetoReason: aggregation.VetoReason, Details: details,
	})
	if err != nil {
		return evals.PersistedJudgeAggregation{}, fmt.Errorf("create judge aggregation: %w", err)
	}
	reason := fmt.Sprintf("%s from %d valid Judge votes", aggregation.Method, aggregation.ValidVoteCount)
	if aggregation.LowConfidence {
		reason += "; quorum not met"
	}
	if _, err := tq.CreateEvalScore(ctx, store.CreateEvalScoreParams{
		ProjectID: projectID, RunID: runID, MetricName: batch.MetricName,
		MetricKind: batch.MetricKind, Score: score, Passed: aggregation.Passed,
		Reason: reason, Details: details,
	}); err != nil {
		return evals.PersistedJudgeAggregation{}, fmt.Errorf("create compatible eval score: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return evals.PersistedJudgeAggregation{}, err
	}
	return aggregationToDomain(row), nil
}

func (e *EvalQuerier) ListJudgeEvaluation(ctx context.Context, projectID, runID string) (evals.JudgeEvaluation, error) {
	votes, err := e.q.ListEvalJudgeVotesByRun(ctx, store.ListEvalJudgeVotesByRunParams{
		ProjectID: parsePGUUID(projectID), RunID: parsePGUUID(runID),
	})
	if err != nil {
		return evals.JudgeEvaluation{}, err
	}
	aggregations, err := e.q.ListEvalScoreAggregationsByRun(ctx, store.ListEvalScoreAggregationsByRunParams{
		ProjectID: parsePGUUID(projectID), RunID: parsePGUUID(runID),
	})
	if err != nil {
		return evals.JudgeEvaluation{}, err
	}
	result := evals.JudgeEvaluation{
		Votes:        make([]evals.PersistedJudgeVote, 0, len(votes)),
		Aggregations: make([]evals.PersistedJudgeAggregation, 0, len(aggregations)),
	}
	for _, vote := range votes {
		result.Votes = append(result.Votes, judgeVoteToDomain(vote))
	}
	for _, aggregation := range aggregations {
		result.Aggregations = append(result.Aggregations, aggregationToDomain(aggregation))
	}
	return result, nil
}

func (e *EvalQuerier) BeginCalibration(
	ctx context.Context,
	projectID, rubricVersionID string,
	sampleRunIDs []string,
	createdBy string,
) (evals.CalibrationRun, error) {
	if e.pool == nil {
		return evals.CalibrationRun{}, errors.New("pool not configured")
	}
	version, err := e.q.GetRubricTemplateVersion(ctx, store.GetRubricTemplateVersionParams{
		ID: parsePGUUID(rubricVersionID), ProjectID: parsePGUUID(projectID),
	})
	if err != nil || version.Status != "published" {
		return evals.CalibrationRun{}, errors.New("published rubric version not found")
	}
	row, err := e.q.CreateEvalCalibrationRun(ctx, store.CreateEvalCalibrationRunParams{
		ProjectID: parsePGUUID(projectID), RubricVersionID: parsePGUUID(rubricVersionID),
		SampleRunIds: parseUUIDSlice(sampleRunIDs), CreatedBy: parsePGUUID(createdBy),
	})
	if err != nil {
		return evals.CalibrationRun{}, fmt.Errorf("create calibration run: %w", err)
	}
	return calibrationRunToDomain(row), nil
}

func (e *EvalQuerier) LoadCalibrationSamples(
	ctx context.Context,
	projectID, rubricVersionID string,
	sampleRunIDs []string,
) ([]evals.CalibrationSample, error) {
	if e.pool == nil {
		return nil, errors.New("pool not configured")
	}
	runIDs := parseUUIDSlice(sampleRunIDs)
	rows, err := e.pool.Query(ctx, `
		SELECT run_id, gold_scores
		FROM annotation_items
		WHERE project_id=$1 AND is_gold=true AND run_id=ANY($2::uuid[])`,
		parsePGUUID(projectID), runIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("list Gold annotations: %w", err)
	}
	defer rows.Close()
	samples := make(map[string]*evals.CalibrationSample)
	for rows.Next() {
		var runID pgtype.UUID
		var gold []byte
		if err := rows.Scan(&runID, &gold); err != nil {
			return nil, err
		}
		id := UUIDToString(runID.Bytes)
		sample := &evals.CalibrationSample{RunID: id, GoldScores: map[string]float64{}, JudgeScores: map[string]float64{}}
		if err := json.Unmarshal(gold, &sample.GoldScores); err != nil {
			return nil, fmt.Errorf("decode Gold scores for run %s: %w", id, err)
		}
		samples[id] = sample
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	aggregationRows, err := e.pool.Query(ctx, `
		SELECT DISTINCT ON (run_id, metric_name) run_id, metric_name, score
		FROM eval_score_aggregations
		WHERE project_id=$1 AND rubric_version_id=$2 AND run_id=ANY($3::uuid[])
		ORDER BY run_id, metric_name, created_at DESC`,
		parsePGUUID(projectID), parsePGUUID(rubricVersionID), runIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("list Judge aggregations: %w", err)
	}
	defer aggregationRows.Close()
	for aggregationRows.Next() {
		var runID pgtype.UUID
		var metricName string
		var score pgtype.Numeric
		if err := aggregationRows.Scan(&runID, &metricName, &score); err != nil {
			return nil, err
		}
		if sample := samples[UUIDToString(runID.Bytes)]; sample != nil {
			sample.JudgeScores[metricName] = numericToFloat64(score)
		}
	}
	if err := aggregationRows.Err(); err != nil {
		return nil, err
	}
	result := make([]evals.CalibrationSample, 0, len(samples))
	for _, runID := range sampleRunIDs {
		if sample := samples[runID]; sample != nil {
			result = append(result, *sample)
		}
	}
	return result, nil
}

func (e *EvalQuerier) CompleteCalibration(
	ctx context.Context,
	run evals.CalibrationRun,
	report evals.CalibrationReport,
) (evals.CalibrationRun, error) {
	encoded, err := json.Marshal(report)
	if err != nil {
		return evals.CalibrationRun{}, err
	}
	row, err := e.q.CompleteEvalCalibrationRun(ctx, store.CompleteEvalCalibrationRunParams{
		ID: parsePGUUID(run.ID), ProjectID: parsePGUUID(run.ProjectID), Report: encoded,
	})
	if err != nil {
		return evals.CalibrationRun{}, err
	}
	return calibrationRunToDomain(row), nil
}

func (e *EvalQuerier) FailCalibration(ctx context.Context, run evals.CalibrationRun, reason string) error {
	report, _ := json.Marshal(map[string]any{"error": reason})
	_, err := e.q.FailEvalCalibrationRun(ctx, store.FailEvalCalibrationRunParams{
		ID: parsePGUUID(run.ID), ProjectID: parsePGUUID(run.ProjectID), Report: report,
	})
	return err
}

func (e *EvalQuerier) ListCalibrations(ctx context.Context, projectID string) ([]evals.CalibrationRun, error) {
	rows, err := e.q.ListEvalCalibrationRuns(ctx, parsePGUUID(projectID))
	if err != nil {
		return nil, err
	}
	result := make([]evals.CalibrationRun, 0, len(rows))
	for _, row := range rows {
		result = append(result, calibrationRunToDomain(row))
	}
	return result, nil
}

func calibrationRunToDomain(row store.EvalCalibrationRun) evals.CalibrationRun {
	report := evals.CalibrationReport{}
	_ = json.Unmarshal(row.Report, &report)
	return evals.CalibrationRun{
		ID: UUIDToString(row.ID.Bytes), ProjectID: UUIDToString(row.ProjectID.Bytes),
		RubricVersionID: UUIDToString(row.RubricVersionID.Bytes), Status: row.Status,
		SampleRunIDs: uuidSliceToStrings(row.SampleRunIds), Report: report,
		CreatedBy: optionalUUID(row.CreatedBy), CreatedAt: row.CreatedAt.Time,
		CompletedAt: pgTimestamptzPtr(row.CompletedAt),
	}
}

func parseUUIDSlice(values []string) []pgtype.UUID {
	result := make([]pgtype.UUID, 0, len(values))
	for _, value := range values {
		result = append(result, parsePGUUID(value))
	}
	return result
}

func uuidSliceToStrings(values []pgtype.UUID) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, optionalUUID(value))
	}
	return result
}

func rubricTemplateToDomain(row store.RubricTemplate) evals.RubricTemplate {
	return evals.RubricTemplate{
		ID: UUIDToString(row.ID.Bytes), ProjectID: UUIDToString(row.ProjectID.Bytes),
		Name: row.Name, Description: row.Description, CreatedAt: row.CreatedAt.Time,
		UpdatedAt: row.UpdatedAt.Time,
	}
}

func rubricVersionToDomain(row store.RubricTemplateVersion) evals.RubricTemplateVersion {
	createdBy := ""
	if row.CreatedBy.Valid {
		createdBy = UUIDToString(row.CreatedBy.Bytes)
	}
	return evals.RubricTemplateVersion{
		ID: UUIDToString(row.ID.Bytes), TemplateID: UUIDToString(row.TemplateID.Bytes),
		Version: int(row.Version), Status: row.Status, Rubric: string(row.Rubric),
		CreatedBy: createdBy, CreatedAt: row.CreatedAt.Time, PublishedAt: pgTimestamptzPtr(row.PublishedAt),
	}
}

func judgeVoteToDomain(row store.EvalJudgeVote) evals.PersistedJudgeVote {
	var score, cost *float64
	var passed *bool
	if row.Score.Valid {
		value := numericToFloat64(row.Score)
		score = &value
	}
	if row.Passed.Valid {
		value := row.Passed.Bool
		passed = &value
	}
	if row.Cost.Valid {
		value := numericToFloat64(row.Cost)
		cost = &value
	}
	return evals.PersistedJudgeVote{
		ID: UUIDToString(row.ID.Bytes), ProjectID: UUIDToString(row.ProjectID.Bytes),
		RunID: UUIDToString(row.RunID.Bytes), MetricName: row.MetricName, MetricKind: row.MetricKind,
		EvaluatorID: optionalUUID(row.EvaluatorID), RubricVersionID: optionalUUID(row.RubricVersionID),
		Provider: row.Provider, Model: row.Model, PromptVersion: row.PromptVersion,
		Score: score, Passed: passed, Reason: row.Reason, RawResponseHash: row.RawResponseHash,
		LatencyMs: pgInt8Ptr(row.LatencyMs), InputTokens: pgInt8Ptr(row.InputTokens),
		OutputTokens: pgInt8Ptr(row.OutputTokens), Cost: cost, Error: row.Error,
		EssentialFailed: row.EssentialFailed, VetoTriggered: row.VetoTriggered,
		VetoReason: row.VetoReason, Details: string(row.Details), CreatedAt: row.CreatedAt.Time,
	}
}

func aggregationToDomain(row store.EvalScoreAggregation) evals.PersistedJudgeAggregation {
	return evals.PersistedJudgeAggregation{
		ID: UUIDToString(row.ID.Bytes), ProjectID: UUIDToString(row.ProjectID.Bytes),
		RunID: UUIDToString(row.RunID.Bytes), MetricName: row.MetricName, MetricKind: row.MetricKind,
		RubricVersionID: optionalUUID(row.RubricVersionID), Details: string(row.Details),
		CreatedAt: row.CreatedAt.Time,
		Result: evals.JudgeAggregation{
			Method: row.Method, Score: numericToFloat64(row.Score), Passed: row.Passed,
			RequiredVotes: int(row.RequiredVotes), ValidVoteCount: int(row.ValidVoteCount),
			ErrorVoteCount: int(row.ErrorVoteCount), StdDev: numericToFloat64(row.Stddev),
			Range: numericToFloat64(row.ScoreRange), Disagreement: numericToFloat64(row.Disagreement),
			Confidence: numericToFloat64(row.Confidence), LowConfidence: row.LowConfidence,
			EssentialFailed: row.EssentialFailed, VetoTriggered: row.VetoTriggered,
			VetoReason: row.VetoReason,
		},
	}
}

func optionalUUID(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	return UUIDToString(value.Bytes)
}

func nullableVoteScore(score *float64, passed *bool) (pgtype.Numeric, pgtype.Bool) {
	if score == nil || passed == nil {
		return pgtype.Numeric{}, pgtype.Bool{}
	}
	numeric, _ := numericFromFloat64(*score)
	return numeric, pgtype.Bool{Bool: *passed, Valid: true}
}

func nullableNumeric(value *float64) (pgtype.Numeric, error) {
	if value == nil {
		return pgtype.Numeric{}, nil
	}
	return numericFromFloat64(*value)
}

func nullableInt64(value *int64) pgtype.Int8 {
	if value == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *value, Valid: true}
}

func jsonOrEmpty(value string) []byte {
	if value == "" {
		return []byte("{}")
	}
	return []byte(value)
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
