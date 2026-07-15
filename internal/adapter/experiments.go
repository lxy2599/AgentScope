package adapter

import (
	"context"

	"AgentScope/internal/experiments"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// ExperimentQuerier implements experiments.Querier using store.Queries.
type ExperimentQuerier struct {
	q *store.Queries
}

// compile-time check
var _ experiments.Querier = (*ExperimentQuerier)(nil)

// NewExperimentQuerier creates a new ExperimentQuerier.
func NewExperimentQuerier(q *store.Queries) *ExperimentQuerier {
	return &ExperimentQuerier{q: q}
}

func (e *ExperimentQuerier) CreateExperiment(ctx context.Context, projectID, datasetID, evaluatorID, targetName, targetVersion string, totalCases int) (experiments.Experiment, error) {
	var evalID pgtype.UUID
	if evaluatorID != "" {
		evalID = parsePGUUID(evaluatorID)
	}

	storeExp, err := e.q.CreateExperiment(ctx, store.CreateExperimentParams{
		ProjectID:     parsePGUUID(projectID),
		DatasetID:     parsePGUUID(datasetID),
		EvaluatorID:   evalID,
		TargetName:    targetName,
		TargetVersion: pgText(targetVersion),
		Status:        "pending",
		AggregateScore: pgtype.Numeric{Valid: false},
		TotalCases:    int32(totalCases),
		PassedCases:   0,
		FailedCases:   0,
		Metadata:      []byte("{}"),
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
