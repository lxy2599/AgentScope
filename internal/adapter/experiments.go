package adapter

import (
	"context"
	"fmt"

	"AgentScope/internal/experiments"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ExperimentQuerier implements experiments.Querier using store.Queries with
// PostgreSQL transaction support.
type ExperimentQuerier struct {
	q    *store.Queries
	pool *pgxpool.Pool
}

// compile-time check
var _ experiments.Querier = (*ExperimentQuerier)(nil)

// NewExperimentQuerier creates a new ExperimentQuerier backed by a connection pool.
func NewExperimentQuerier(q *store.Queries, pool *pgxpool.Pool) *ExperimentQuerier {
	return &ExperimentQuerier{q: q, pool: pool}
}

// CreateExperimentWithJobs creates an experiment and all its eval jobs in a single
// transaction. If any step fails or any caseID does not belong to the dataset,
// the entire transaction is rolled back.
func (e *ExperimentQuerier) CreateExperimentWithJobs(ctx context.Context, projectID, datasetID, evaluatorID, targetName, targetVersion string, caseIDs []string) (experiments.Experiment, error) {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return experiments.Experiment{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	tq := e.q.WithTx(tx)

	// Verify provided caseIDs belong to the dataset
	validCaseIDs, err := tq.ListDatasetCaseIDsByDataset(ctx, parsePGUUID(datasetID))
	if err != nil {
		return experiments.Experiment{}, fmt.Errorf("list dataset case ids: %w", err)
	}
	validSet := make(map[string]bool, len(validCaseIDs))
	for _, id := range validCaseIDs {
		validSet[uuidToString(id.Bytes)] = true
	}
	for _, caseID := range caseIDs {
		if !validSet[caseID] {
			return experiments.Experiment{}, fmt.Errorf("case %s does not belong to dataset %s", caseID, datasetID)
		}
	}

	var evalID pgtype.UUID
	if evaluatorID != "" {
		evalID = parsePGUUID(evaluatorID)
	}

	storeExp, err := tq.CreateExperiment(ctx, store.CreateExperimentParams{
		ProjectID:      parsePGUUID(projectID),
		DatasetID:      parsePGUUID(datasetID),
		EvaluatorID:    evalID,
		TargetName:     targetName,
		TargetVersion:  pgText(targetVersion),
		Status:         "pending",
		AggregateScore: pgtype.Numeric{Valid: false},
		TotalCases:     int32(len(caseIDs)),
		PassedCases:    0,
		FailedCases:    0,
		Metadata:       []byte("{}"),
	})
	if err != nil {
		return experiments.Experiment{}, fmt.Errorf("create experiment: %w", err)
	}

	for _, caseID := range caseIDs {
		if _, err := tq.CreateEvalJob(ctx, store.CreateEvalJobParams{
			ExperimentID:  storeExp.ID,
			DatasetCaseID: parsePGUUID(caseID),
			Status:        "queued",
		}); err != nil {
			return experiments.Experiment{}, fmt.Errorf("create eval job for case %s: %w", caseID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return experiments.Experiment{}, fmt.Errorf("commit tx: %w", err)
	}

	return storeExpToDomain(storeExp), nil
}

// --- Legacy methods (kept for interface compliance) ---

func (e *ExperimentQuerier) CreateExperiment(ctx context.Context, projectID, datasetID, evaluatorID, targetName, targetVersion string, totalCases int) (experiments.Experiment, error) {
	var evalID pgtype.UUID
	if evaluatorID != "" {
		evalID = parsePGUUID(evaluatorID)
	}

	storeExp, err := e.q.CreateExperiment(ctx, store.CreateExperimentParams{
		ProjectID:      parsePGUUID(projectID),
		DatasetID:      parsePGUUID(datasetID),
		EvaluatorID:    evalID,
		TargetName:     targetName,
		TargetVersion:  pgText(targetVersion),
		Status:         "pending",
		AggregateScore: pgtype.Numeric{Valid: false},
		TotalCases:     int32(totalCases),
		PassedCases:    0,
		FailedCases:    0,
		Metadata:       []byte("{}"),
	})
	if err != nil {
		return experiments.Experiment{}, err
	}
	return storeExpToDomain(storeExp), nil
}

func (e *ExperimentQuerier) GetExperiment(ctx context.Context, experimentID string) (experiments.Experiment, error) {
	storeExp, err := e.q.GetExperimentByID(ctx, parsePGUUID(experimentID))
	if err != nil {
		return experiments.Experiment{}, err
	}
	return storeExpToDomain(storeExp), nil
}

func (e *ExperimentQuerier) CreateEvalJob(ctx context.Context, experimentID, datasetCaseID, status string) (experiments.EvalJob, error) {
	storeJob, err := e.q.CreateEvalJob(ctx, store.CreateEvalJobParams{
		ExperimentID:  parsePGUUID(experimentID),
		DatasetCaseID: parsePGUUID(datasetCaseID),
		Status:        status,
	})
	if err != nil {
		return experiments.EvalJob{}, err
	}
	return storeJobToDomain(storeJob), nil
}

func (e *ExperimentQuerier) ListEvalJobs(ctx context.Context, experimentID string) ([]experiments.EvalJob, error) {
	storeJobs, err := e.q.ListEvalJobsByExperiment(ctx, parsePGUUID(experimentID))
	if err != nil {
		return nil, err
	}
	result := make([]experiments.EvalJob, 0, len(storeJobs))
	for _, sj := range storeJobs {
		result = append(result, storeJobToDomain(sj))
	}
	return result, nil
}

func (e *ExperimentQuerier) GetJobStatusCounts(ctx context.Context, experimentID string) (map[string]int, error) {
	rows, err := e.q.GetExperimentJobStatusCounts(ctx, parsePGUUID(experimentID))
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[row.Status] = int(row.Count)
	}
	return counts, nil
}

func (e *ExperimentQuerier) UpdateExperimentCounts(ctx context.Context, experimentID string, status string, passedCases, failedCases int) error {
	_, err := e.q.UpdateExperimentStatus(ctx, store.UpdateExperimentStatusParams{
		ID:          parsePGUUID(experimentID),
		Status:      status,
		PassedCases: int32(passedCases),
		FailedCases: int32(failedCases),
	})
	return err
}

func (e *ExperimentQuerier) ListExperiments(ctx context.Context, projectID string) ([]experiments.Experiment, error) {
	storeExps, err := e.q.ListExperimentsByProject(ctx, parsePGUUID(projectID))
	if err != nil {
		return nil, err
	}
	result := make([]experiments.Experiment, 0, len(storeExps))
	for _, se := range storeExps {
		result = append(result, storeExpToDomain(se))
	}
	return result, nil
}

func (e *ExperimentQuerier) ListEvalResults(ctx context.Context, experimentID string) ([]experiments.EvalCaseResult, error) {
	storeResults, err := e.q.ListEvalResultsByExperiment(ctx, parsePGUUID(experimentID))
	if err != nil {
		return nil, err
	}
	result := make([]experiments.EvalCaseResult, 0, len(storeResults))
	for _, sr := range storeResults {
		runID := ""
		if sr.RunID.Valid {
			runID = uuidToString(sr.RunID.Bytes)
		}
		datasetCaseID := ""
		if sr.DatasetCaseID.Valid {
			datasetCaseID = uuidToString(sr.DatasetCaseID.Bytes)
		}
		result = append(result, experiments.EvalCaseResult{
			ID:            uuidToString(sr.ID.Bytes),
			EvalJobID:     uuidToString(sr.EvalJobID.Bytes),
			RunID:         runID,
			DatasetCaseID: datasetCaseID,
			Score:         numericToFloat64(sr.Score),
			Passed:        sr.Passed.Bool,
			Reason:        strOrZero(sr.Reason),
			ActualOutput:  strOrZero(sr.ActualOutput),
			LatencyMs:     sr.LatencyMs.Int64,
			Cost:          numericToFloat64(sr.Cost),
			CreatedAt:     sr.CreatedAt.Time,
		})
	}
	return result, nil
}

func storeExpToDomain(se store.Experiment) experiments.Experiment {
	evaluatorID := ""
	if se.EvaluatorID.Valid {
		evaluatorID = uuidToString(se.EvaluatorID.Bytes)
	}
	return experiments.Experiment{
		ID:            uuidToString(se.ID.Bytes),
		ProjectID:     uuidToString(se.ProjectID.Bytes),
		DatasetID:     uuidToString(se.DatasetID.Bytes),
		EvaluatorID:   evaluatorID,
		TargetName:    se.TargetName,
		TargetVersion: strOrZero(se.TargetVersion),
		Status:        se.Status,
		TotalCases:    int(se.TotalCases),
		PassedCases:   int(se.PassedCases),
		FailedCases:   int(se.FailedCases),
		CreatedAt:     se.CreatedAt.Time,
		UpdatedAt:     se.UpdatedAt.Time,
	}
}

func storeJobToDomain(sj store.EvalJob) experiments.EvalJob {
	return experiments.EvalJob{
		ID:             uuidToString(sj.ID.Bytes),
		ExperimentID:   uuidToString(sj.ExperimentID.Bytes),
		DatasetCaseID:  uuidToString(sj.DatasetCaseID.Bytes),
		Status:         sj.Status,
		RunnerName:     strOrZero(sj.RunnerName),
		LeaseExpiresAt: pgTimestamptzPtr(sj.LeaseExpiresAt),
		Attempts:       int(sj.Attempts),
		Error:          strOrZero(sj.Error),
		CreatedAt:      sj.CreatedAt.Time,
		UpdatedAt:      sj.UpdatedAt.Time,
	}
}
