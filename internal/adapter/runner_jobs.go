package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"AgentScope/internal/runnerapi"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RunnerQuerier implements runnerapi.Querier using store.Queries.
type RunnerQuerier struct {
	q    *store.Queries
	pool *pgxpool.Pool
}

// compile-time check
var _ runnerapi.Querier = (*RunnerQuerier)(nil)

// NewRunnerQuerier creates a new RunnerQuerier.
func NewRunnerQuerier(q *store.Queries, pool *pgxpool.Pool) *RunnerQuerier {
	return &RunnerQuerier{q: q, pool: pool}
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

func (r *RunnerQuerier) UpdateJobStatusByProject(ctx context.Context, jobID, status, errorMsg, projectID, runnerName string) error {
	_, err := r.q.UpdateEvalJobStatusByProject(ctx, store.UpdateEvalJobStatusByProjectParams{
		ID:         parsePGUUID(jobID),
		Status:     status,
		Error:      pgText(errorMsg),
		ProjectID:  parsePGUUID(projectID),
		RunnerName: pgText(runnerName),
	})
	return err
}

// normalizeResultDetails ensures details is never nil so PostgreSQL NOT NULL
// constraint is not violated. If details is nil or empty, returns {} (empty JSON).
func normalizeResultDetails(details []byte) []byte {
	if len(details) == 0 {
		return []byte("{}")
	}
	return details
}

// SubmitResultByProject atomically verifies project boundary, inserts the result,
// updates the job status, and updates the experiment counts within a single transaction.
func (r *RunnerQuerier) SubmitResultByProject(ctx context.Context, projectID, runnerName, jobID string, req runnerapi.SubmitResultRequest) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	tq := r.q.WithTx(tx)

	// Lock and verify job with project boundary
	job, err := tq.GetEvalJobLockedByProject(ctx, store.GetEvalJobLockedByProjectParams{
		ID:        parsePGUUID(jobID),
		ProjectID: parsePGUUID(projectID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("job not found or project mismatch")
		}
		return fmt.Errorf("get job: %w", err)
	}

	// Verify runner name
	if strOrZero(job.RunnerName) != runnerName {
		return errors.New("job not claimed by this runner")
	}
	// Verify status
	if job.Status != "running" {
		return errors.New("job is not in running state")
	}

	// Prepare result params
	var runIDUUID pgtype.UUID
	if req.RunID != "" {
		runIDUUID = parsePGUUID(req.RunID)
	}

	// Use numericFromFloat64 (via string) to avoid pgtype.Numeric.Scan(float64) error
	scoreNum, err := numericFromFloat64(req.Score)
	if err != nil {
		return fmt.Errorf("score conversion: %w", err)
	}

	// Cost: 0 means "not provided" → keep NULL
	costNum, err := numericFromFloat64Ptr(req.Cost)
	if err != nil {
		return fmt.Errorf("cost conversion: %w", err)
	}

	// Insert result
	if _, err := tq.CreateEvalCaseResult(ctx, store.CreateEvalCaseResultParams{
		EvalJobID:    parsePGUUID(jobID),
		RunID:        runIDUUID,
		Score:        scoreNum,
		Passed:       pgtype.Bool{Bool: req.Passed, Valid: true},
		Reason:       pgText(req.Reason),
		Details:      normalizeResultDetails(req.Details),
		ActualOutput: pgText(req.ActualOutput),
		LatencyMs:    pgtype.Int8{Int64: req.LatencyMs, Valid: req.LatencyMs > 0},
		Cost:         costNum,
	}); err != nil {
		return fmt.Errorf("create result: %w", err)
	}

	// Update job status to succeeded with project boundary check (defense-in-depth)
	if _, err := tq.UpdateEvalJobStatusByProject(ctx, store.UpdateEvalJobStatusByProjectParams{
		ID:         parsePGUUID(jobID),
		Status:     "succeeded",
		Error:      pgtype.Text{Valid: false},
		ProjectID:  parsePGUUID(projectID),
		RunnerName: pgText(runnerName),
	}); err != nil {
		return fmt.Errorf("update job status: %w", err)
	}

	// Mark the associated run as completed if run_id was provided
	if runIDUUID.Valid {
		existingRun, err := tq.GetRunByID(ctx, runIDUUID)
		if err == nil {
			nowTime := time.Now()
			var endedAt pgtype.Timestamptz
			_ = endedAt.Scan(nowTime)
			if _, uErr := tq.UpdateRun(ctx, store.UpdateRunParams{
				ID:        runIDUUID,
				AgentName: existingRun.AgentName,
				AgentVersion: existingRun.AgentVersion,
				Status:    "completed",
				EndedAt:   endedAt,
				LatencyMs: pgtype.Int8{Int64: req.LatencyMs, Valid: req.LatencyMs > 0},
			}); uErr != nil {
				return fmt.Errorf("update run status: %w", uErr)
			}
		}
	}

	// Count job statuses for experiment
	rows, err := tq.GetExperimentJobStatusCounts(ctx, job.ExperimentID)
	if err != nil {
		return fmt.Errorf("get job status counts: %w", err)
	}

	counts := make(map[string]int64, len(rows))
	for _, row := range rows {
		counts[row.Status] = row.Count
	}

	// Determine new experiment status
	newStatus := "running"
	queued := counts["queued"]
	running := counts["running"]
	if queued == 0 && running == 0 {
		newStatus = "completed"
	}

	succeeded := int(counts["succeeded"])
	failed := int(counts["failed"])

	// Update experiment counts
	if _, err := tq.UpdateExperimentStatus(ctx, store.UpdateExperimentStatusParams{
		ID:          job.ExperimentID,
		Status:      newStatus,
		PassedCases: int32(succeeded),
		FailedCases: int32(failed),
	}); err != nil {
		return fmt.Errorf("update experiment status: %w", err)
	}

	return tx.Commit(ctx)
}

func (r *RunnerQuerier) CreateResult(ctx context.Context, jobID, runID string, score float64, passed bool, reason string, details []byte, actualOutput string, latencyMs int64, cost float64) error {
	var runIDUUID pgtype.UUID
	if runID != "" {
		runIDUUID = parsePGUUID(runID)
	}

	scoreNum, err := numericFromFloat64(score)
	if err != nil {
		return fmt.Errorf("score conversion: %w", err)
	}

	costNum, err := numericFromFloat64Ptr(cost)
	if err != nil {
		return fmt.Errorf("cost conversion: %w", err)
	}

	_, err = r.q.CreateEvalCaseResult(ctx, store.CreateEvalCaseResultParams{
		EvalJobID:    parsePGUUID(jobID),
		RunID:        runIDUUID,
		Score:        scoreNum,
		Passed:       pgtype.Bool{Bool: passed, Valid: true},
		Reason:       pgText(reason),
		Details:      normalizeResultDetails(details),
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
	return UUIDToString(job.ExperimentID.Bytes), nil
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

// GetExperimentTargetVersion returns the target name, target version, and evaluator ID for an experiment.
func (r *RunnerQuerier) GetExperimentTargetVersion(ctx context.Context, experimentID string) (string, string, string, error) {
	exp, err := r.q.GetExperimentByID(ctx, parsePGUUID(experimentID))
	if err != nil {
		return "", "", "", fmt.Errorf("get experiment: %w", err)
	}
	evaluatorID := ""
	if exp.EvaluatorID.Valid {
		evaluatorID = UUIDToString(exp.EvaluatorID.Bytes)
	}
	return exp.TargetName, strOrZero(exp.TargetVersion), evaluatorID, nil
}

// GetDatasetCaseData returns the input, expected, and metadata for a dataset case.
func (r *RunnerQuerier) GetDatasetCaseData(ctx context.Context, caseID string) (runnerapi.DatasetCaseData, error) {
	c, err := r.q.GetDatasetCaseByID(ctx, parsePGUUID(caseID))
	if err != nil {
		return runnerapi.DatasetCaseData{}, fmt.Errorf("get dataset case: %w", err)
	}
	data := runnerapi.DatasetCaseData{
		Input:    string(c.Input),
		Expected: nullableBytesToStringPtr(c.Expected),
		Metadata: c.Metadata,
	}
	return data, nil
}

// GetEvaluatorConfig returns the config JSON for an evaluator by ID.
func (r *RunnerQuerier) GetEvaluatorConfig(ctx context.Context, evaluatorID string) (json.RawMessage, error) {
	var config []byte
	err := r.pool.QueryRow(ctx, "SELECT config FROM evaluators WHERE id = $1", parsePGUUID(evaluatorID)).Scan(&config)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("evaluator not found")
		}
		return nil, fmt.Errorf("get evaluator config: %w", err)
	}
	return json.RawMessage(config), nil
}

func nullableBytesToStringPtr(b []byte) *string {
	if b == nil {
		return nil
	}
	s := string(b)
	return &s
}

func runnerStoreJobToDomain(sj store.EvalJob) runnerapi.EvalJob {
	return runnerapi.EvalJob{
		ID:             UUIDToString(sj.ID.Bytes),
		ExperimentID:   UUIDToString(sj.ExperimentID.Bytes),
		DatasetCaseID:  UUIDToString(sj.DatasetCaseID.Bytes),
		Status:         sj.Status,
		RunnerName:     strOrZero(sj.RunnerName),
		LeaseExpiresAt: pgTimestamptzPtr(sj.LeaseExpiresAt),
		Attempts:       int(sj.Attempts),
		Error:          strOrZero(sj.Error),
		CreatedAt:      sj.CreatedAt.Time,
		UpdatedAt:      sj.UpdatedAt.Time,
	}
}
