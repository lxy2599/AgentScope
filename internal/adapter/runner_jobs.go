package adapter

import (
	"context"
	"time"

	"AgentScope/internal/runnerapi"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// RunnerQuerier implements runnerapi.Querier using store.Queries.
type RunnerQuerier struct {
	q *store.Queries
}

// compile-time check
var _ runnerapi.Querier = (*RunnerQuerier)(nil)

// NewRunnerQuerier creates a new RunnerQuerier.
func NewRunnerQuerier(q *store.Queries) *RunnerQuerier {
	return &RunnerQuerier{q: q}
}

func (r *RunnerQuerier) ClaimNextJob(ctx context.Context, projectID, runnerName string, leaseExpiresAt time.Time) (runnerapi.EvalJob, error) {
	storeJob, err := r.q.ClaimNextEvalJob(ctx, store.ClaimNextEvalJobParams{
		ProjectID:      parsePGUUID(projectID),
		RunnerName:     pgText(runnerName),
		LeaseExpiresAt: pgTimestamptz(leaseExpiresAt),
	})
	if err != nil {
		return runnerapi.EvalJob{}, err
	}
	return runnerStoreJobToDomain(storeJob), nil
}

func (r *RunnerQuerier) GetJobByID(ctx context.Context, jobID string) (runnerapi.EvalJob, error) {
	storeJob, err := r.q.GetEvalJobByID(ctx, parsePGUUID(jobID))
	if err != nil {
		return runnerapi.EvalJob{}, err
	}
	return runnerStoreJobToDomain(storeJob), nil
}

func (r *RunnerQuerier) HeartbeatJobByProject(ctx context.Context, jobID, runnerName string, leaseExpiresAt time.Time, projectID string) error {
	_, err := r.q.HeartbeatEvalJobByProject(ctx, store.HeartbeatEvalJobByProjectParams{
		ID:             parsePGUUID(jobID),
		LeaseExpiresAt: pgTimestamptz(leaseExpiresAt),
		RunnerName:     pgText(runnerName),
		ProjectID:      parsePGUUID(projectID),
	})
	return err
}

func (r *RunnerQuerier) UpdateJobStatusByProject(ctx context.Context, jobID, status, errorMsg, projectID string) error {
	_, err := r.q.UpdateEvalJobStatusByProject(ctx, store.UpdateEvalJobStatusByProjectParams{
		ID:        parsePGUUID(jobID),
		Status:    status,
		Error:     pgText(errorMsg),
		ProjectID: parsePGUUID(projectID),
	})
	return err
}

func (r *RunnerQuerier) CreateResult(ctx context.Context, jobID, runID string, score float64, passed bool, reason string, details []byte, actualOutput string, latencyMs int64, cost float64) error {
	var runIDUUID pgtype.UUID
	if runID != "" {
		runIDUUID = parsePGUUID(runID)
	}

	var scoreNum pgtype.Numeric
	if score != 0 {
		_ = scoreNum.Scan(score)
	}

	var costNum pgtype.Numeric
	if cost != 0 {
		_ = costNum.Scan(cost)
	}

	_, err := r.q.CreateEvalCaseResult(ctx, store.CreateEvalCaseResultParams{
		EvalJobID:    parsePGUUID(jobID),
		RunID:        runIDUUID,
		Score:        scoreNum,
		Passed:       pgtype.Bool{Bool: passed, Valid: true},
		Reason:       pgText(reason),
		Details:      details,
		ActualOutput: pgText(actualOutput),
		LatencyMs:    pgtype.Int8{Int64: latencyMs, Valid: latencyMs > 0},
		Cost:         costNum,
	})
	return err
}

func (r *RunnerQuerier) GetJobExperimentID(ctx context.Context, jobID string) (string, error) {
	job, err := r.q.GetEvalJobByID(ctx, parsePGUUID(jobID))
	if err != nil {
		return "", err
	}
	return uuidToString(job.ExperimentID.Bytes), nil
}

func (r *RunnerQuerier) UpdateExperimentStatus(ctx context.Context, experimentID, status string, passedCases, failedCases int) error {
	_, err := r.q.UpdateExperimentStatus(ctx, store.UpdateExperimentStatusParams{
		ID:          parsePGUUID(experimentID),
		Status:      status,
		PassedCases: int32(passedCases),
		FailedCases: int32(failedCases),
	})
	return err
}

func (r *RunnerQuerier) GetJobStatusCounts(ctx context.Context, experimentID string) (map[string]int, error) {
	rows, err := r.q.GetExperimentJobStatusCounts(ctx, parsePGUUID(experimentID))
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[row.Status] = int(row.Count)
	}
	return counts, nil
}

func runnerStoreJobToDomain(sj store.EvalJob) runnerapi.EvalJob {
	return runnerapi.EvalJob{
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
