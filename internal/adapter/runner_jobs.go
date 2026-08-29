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

	// Serialize summary updates for jobs belonging to the same experiment.
	// Without this lock, concurrent runners can each compute a summary that
	// does not include the other runner's uncommitted result and then overwrite
	// the experiment with stale counts.
	if _, err := tq.LockExperimentForSummary(ctx, job.ExperimentID); err != nil {
		return fmt.Errorf("lock experiment: %w", err)
	}

	// Prepare result params
	var runIDUUID pgtype.UUID
	if req.RunID != "" {
		runIDUUID = parsePGUUID(req.RunID)
		if !runIDUUID.Valid {
			return errors.New("invalid run ID")
		}
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

	// Complete the associated run through a project-scoped update before
	// persisting result data. This update and every subsequent write use the
	// same transaction, so a later failure rolls the run completion back too.
	if runIDUUID.Valid {
		if _, err := tq.CompleteRunnerRun(ctx, store.CompleteRunnerRunParams{
			ID:        runIDUUID,
			ProjectID: parsePGUUID(projectID),
			LatencyMs: pgtype.Int8{Int64: req.LatencyMs, Valid: req.LatencyMs > 0},
		}); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errors.New("run not found or project mismatch")
			}
			return fmt.Errorf("complete run: %w", err)
		}
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

	// Create multi-dimensional eval scores if provided
	for _, es := range req.EvalScores {
		var esScoreNum pgtype.Numeric
		if err := esScoreNum.Scan(fmt.Sprintf("%f", es.Score)); err != nil {
			return fmt.Errorf("eval score conversion: %w", err)
		}

		var esDetails []byte
		if es.Details != nil {
			esDetails = es.Details
		} else {
			esDetails = []byte("{}")
		}

		if _, err := tq.CreateEvalScore(ctx, store.CreateEvalScoreParams{
			ProjectID:  parsePGUUID(projectID),
			RunID:      runIDUUID,
			TraceID:    es.TraceID,
			SpanID:     es.SpanID,
			MetricName: es.MetricName,
			MetricKind: es.MetricKind,
			Score:      esScoreNum,
			Passed:     es.Passed,
			Reason:     es.Reason,
			Details:    esDetails,
		}); err != nil {
			return fmt.Errorf("create eval score: %w", err)
		}
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

	if err := updateExperimentEvaluationSummary(ctx, tq, job.ExperimentID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

// RefreshExperimentSummary serializes and recomputes an experiment summary
// after a job reaches a terminal state outside SubmitResultByProject, such as
// a target or evaluator execution failure.
func (r *RunnerQuerier) RefreshExperimentSummary(ctx context.Context, experimentID string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	tq := r.q.WithTx(tx)
	experimentUUID := parsePGUUID(experimentID)
	if _, err := tq.LockExperimentForSummary(ctx, experimentUUID); err != nil {
		return fmt.Errorf("lock experiment: %w", err)
	}
	if err := updateExperimentEvaluationSummary(ctx, tq, experimentUUID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func updateExperimentEvaluationSummary(ctx context.Context, q *store.Queries, experimentID pgtype.UUID) error {
	summary, err := q.GetExperimentEvaluationSummary(ctx, experimentID)
	if err != nil {
		return fmt.Errorf("get experiment evaluation summary: %w", err)
	}

	status := "running"
	if summary.QueuedCases == 0 && summary.RunningCases == 0 {
		status = "completed"
	}

	if _, err := q.UpdateExperimentEvaluationSummary(ctx, store.UpdateExperimentEvaluationSummaryParams{
		ID:             experimentID,
		Status:         status,
		PassedCases:    int32(summary.PassedCases),
		FailedCases:    int32(summary.FailedCases),
		AggregateScore: summary.AggregateScore,
	}); err != nil {
		return fmt.Errorf("update experiment evaluation summary: %w", err)
	}
	return nil
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
		Expected: jsonbStringToGoString(c.Expected),
		Metadata: c.Metadata,
	}
	// Semantic task fields are read directly so this remains compatible with
	// generated sqlc models created before migration 00025.
	var expectedBehavior, successCriteria, environment, allowedTools, graders []byte
	if err := r.pool.QueryRow(ctx, `SELECT expected_behavior, success_criteria, environment, allowed_tools, graders FROM dataset_cases WHERE id = $1`, parsePGUUID(caseID)).Scan(&expectedBehavior, &successCriteria, &environment, &allowedTools, &graders); err == nil {
		data.ExpectedBehavior = expectedBehavior
		data.SuccessCriteria = successCriteria
		data.Environment = environment
		data.AllowedTools = allowedTools
		data.Graders = graders
	}
	return data, nil
}

// GetExperimentEvaluationSnapshot returns the immutable snapshot payload used
// by an experiment. It is intentionally separate from generated sqlc queries
// so older generated store models remain source-compatible.
func (r *RunnerQuerier) GetExperimentEvaluationSnapshot(ctx context.Context, experimentID string) (string, string, json.RawMessage, error) {
	var id, hash string
	var payload []byte
	err := r.pool.QueryRow(ctx, `SELECT id::text, content_hash, payload FROM evaluation_snapshots WHERE experiment_id = $1`, parsePGUUID(experimentID)).Scan(&id, &hash, &payload)
	if err != nil {
		return "", "", nil, err
	}
	return id, hash, json.RawMessage(payload), nil
}

// GetEvaluatorConfig returns the config JSON for an evaluator owned by the project.
func (r *RunnerQuerier) GetEvaluatorConfig(ctx context.Context, evaluatorID, projectID string) (json.RawMessage, error) {
	var config []byte
	err := r.pool.QueryRow(
		ctx,
		"SELECT config FROM evaluators WHERE id = $1 AND project_id = $2",
		parsePGUUID(evaluatorID),
		parsePGUUID(projectID),
	).Scan(&config)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errors.New("evaluator not found")
		}
		return nil, fmt.Errorf("get evaluator config: %w", err)
	}
	return json.RawMessage(config), nil
}

// jsonbStringToGoString converts a JSONB string column value to a Go *string.
//
// PostgreSQL JSONB columns store strings as JSON-quoted values (e.g. the Go
// string `可以退款` is stored as the JSONB value `"可以退款"`).  When the
// pgx driver reads this value, the bytes include the surrounding double quotes.
// This function properly unmarshals them, stripping the JSON quoting so the
// caller receives the plain Go string.
func jsonbStringToGoString(b []byte) *string {
	if b == nil {
		return nil
	}
	// First try to JSON-unmarshal as a Go string (handles JSONB quoting).
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		return &s
	}
	// Fallback: return raw bytes as a string (for values that were
	// stored as plain text rather than JSONB strings).
	raw := string(b)
	return &raw
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
