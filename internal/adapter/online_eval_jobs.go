package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"AgentScope/internal/onlinerval"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// OnlineEvalJobRepository is the PostgreSQL source of truth for durable online evaluation work.
type OnlineEvalJobRepository struct{ pool *pgxpool.Pool }

func NewOnlineEvalJobRepository(pool *pgxpool.Pool) *OnlineEvalJobRepository {
	return &OnlineEvalJobRepository{pool: pool}
}

var _ onlinerval.JobRepository = (*OnlineEvalJobRepository)(nil)

const onlineEvalJobColumns = `id::text, project_id::text, run_id::text, trace_id, evaluator_version, status,
  COALESCE(sampling_rule_id::text,''), sampling_reason, evaluation_source, fallback_reason,
  attempt_count, max_attempts, available_at, COALESCE(claimed_by,''), claimed_at, started_at, finished_at,
  last_error, COALESCE(annotation_item_id::text,''), created_at, updated_at`

type jobScanner interface{ Scan(...any) error }

func scanOnlineEvalJob(row jobScanner) (onlinerval.Job, error) {
	var job onlinerval.Job
	var status string
	if err := row.Scan(&job.ID, &job.ProjectID, &job.RunID, &job.TraceID, &job.EvaluatorVersion, &status,
		&job.SamplingRuleID, &job.SamplingReason, &job.EvaluationSource, &job.FallbackReason,
		&job.AttemptCount, &job.MaxAttempts, &job.AvailableAt, &job.ClaimedBy, &job.ClaimedAt, &job.StartedAt, &job.FinishedAt,
		&job.LastError, &job.AnnotationItemID, &job.CreatedAt, &job.UpdatedAt); err != nil {
		return onlinerval.Job{}, err
	}
	job.Status = onlinerval.JobStatus(status)
	return job, nil
}
func (r *OnlineEvalJobRepository) Enqueue(ctx context.Context, projectID, runID, traceID, evaluatorVersion string) (onlinerval.Job, error) {
	row := r.pool.QueryRow(ctx, `INSERT INTO online_eval_jobs (project_id,run_id,trace_id,evaluator_version)
VALUES ($1,$2,$3,$4)
ON CONFLICT (project_id,run_id,evaluator_version) DO UPDATE SET trace_id=EXCLUDED.trace_id, updated_at=now()
RETURNING `+onlineEvalJobColumns, parsePGUUID(projectID), parsePGUUID(runID), traceID, evaluatorVersion)
	job, err := scanOnlineEvalJob(row)
	return job, mapOnlineEvalJobErr(err)
}
func (r *OnlineEvalJobRepository) Claim(ctx context.Context, workerID string, limit int, now time.Time) ([]onlinerval.Job, error) {
	if limit < 1 {
		return []onlinerval.Job{}, nil
	}
	rows, err := r.pool.Query(ctx, `WITH candidates AS (
 SELECT id FROM online_eval_jobs WHERE status IN ('queued','retry_wait') AND available_at <= $1
 ORDER BY available_at, created_at LIMIT $2 FOR UPDATE SKIP LOCKED
) UPDATE online_eval_jobs j SET status='running', claimed_by=$3, claimed_at=$1, started_at=COALESCE(j.started_at,$1), attempt_count=j.attempt_count+1, updated_at=$1
FROM candidates c WHERE j.id=c.id
RETURNING j.id::text, j.project_id::text, j.run_id::text, j.trace_id, j.evaluator_version, j.status,
  COALESCE(j.sampling_rule_id::text,''), j.sampling_reason, j.evaluation_source, j.fallback_reason,
  j.attempt_count, j.max_attempts, j.available_at, COALESCE(j.claimed_by,''), j.claimed_at, j.started_at, j.finished_at,
  j.last_error, COALESCE(j.annotation_item_id::text,''), j.created_at, j.updated_at`, now, limit, workerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := []onlinerval.Job{}
	for rows.Next() {
		job, err := scanOnlineEvalJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}
func (r *OnlineEvalJobRepository) MarkSkipped(ctx context.Context, jobID, ruleID, reason string, at time.Time) error {
	result, err := r.pool.Exec(ctx, `UPDATE online_eval_jobs SET status='skipped', sampling_rule_id=NULLIF($2,'')::uuid, sampling_reason=$3, finished_at=$4, claimed_by=NULL, claimed_at=NULL, updated_at=$4 WHERE id=$1 AND status='running'`, parsePGUUID(jobID), ruleID, reason, at)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return onlinerval.ErrJobNotFound
	}
	return nil
}
func (r *OnlineEvalJobRepository) Complete(ctx context.Context, completion onlinerval.Completion, at time.Time) (onlinerval.Job, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return onlinerval.Job{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, score := range completion.Scores {
		if _, err := tx.Exec(ctx, `INSERT INTO eval_scores (project_id,run_id,trace_id,metric_name,metric_kind,score,passed,reason,details,online_eval_job_id)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,'{}',$9)
ON CONFLICT (online_eval_job_id,metric_name,metric_kind) WHERE online_eval_job_id IS NOT NULL
DO UPDATE SET trace_id=EXCLUDED.trace_id,score=EXCLUDED.score,passed=EXCLUDED.passed,reason=EXCLUDED.reason,created_at=now()`,
			parsePGUUID(score.ProjectID), parsePGUUID(score.RunID), score.TraceID, score.MetricName, score.MetricKind, score.Score, score.Passed, score.Reason, parsePGUUID(completion.Job.ID)); err != nil {
			return onlinerval.Job{}, fmt.Errorf("upsert online eval score: %w", err)
		}
	}
	annotationID := ""
	if completion.CreateAnnotation {
		annotationID, err = ensureOnlineEvalAnnotation(ctx, tx, completion, at)
		if err != nil {
			return onlinerval.Job{}, err
		}
	}
	row := tx.QueryRow(ctx, `UPDATE online_eval_jobs SET status='succeeded', sampling_rule_id=NULLIF($2,'')::uuid, sampling_reason=$3,evaluation_source=$4,fallback_reason=$5,annotation_item_id=NULLIF($6,'')::uuid,finished_at=$7,claimed_by=NULL,claimed_at=NULL,last_error='',updated_at=$7 WHERE id=$1 AND status='running' RETURNING `+onlineEvalJobColumns,
		parsePGUUID(completion.Job.ID), completion.Job.SamplingRuleID, completion.Job.SamplingReason, completion.EvaluationSource, completion.FallbackReason, annotationID, at)
	job, err := scanOnlineEvalJob(row)
	if err != nil {
		return onlinerval.Job{}, mapOnlineEvalJobErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return onlinerval.Job{}, err
	}
	return job, nil
}
func ensureOnlineEvalAnnotation(ctx context.Context, tx pgx.Tx, completion onlinerval.Completion, at time.Time) (string, error) {
	var existing string
	err := tx.QueryRow(ctx, `SELECT id::text FROM annotation_items WHERE online_eval_job_id=$1`, parsePGUUID(completion.Job.ID)).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", err
	}
	var queueID string
	err = tx.QueryRow(ctx, `SELECT id::text FROM annotation_queues WHERE project_id=$1 AND name='线上评测待标注' ORDER BY created_at LIMIT 1`, parsePGUUID(completion.Job.ProjectID)).Scan(&queueID)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO annotation_queues(project_id,name,description) VALUES($1,'线上评测待标注','自动创建：线上评测低分或失败 Run 的人工回流队列') RETURNING id::text`, parsePGUUID(completion.Job.ProjectID)).Scan(&queueID)
	}
	if err != nil {
		return "", err
	}
	scores, _ := json.Marshal(completion.Scores)
	row := tx.QueryRow(ctx, `INSERT INTO annotation_items(queue_id,project_id,run_id,trace_id,status,priority,scores,agent_name,error,online_eval_job_id,created_at,updated_at)
VALUES($1,$2,$3,$4,'pending',5,$5,$6,$7,$8,$9,$9)
ON CONFLICT (project_id,run_id) WHERE status IN ('pending','in_progress')
DO UPDATE SET scores=EXCLUDED.scores,online_eval_job_id=COALESCE(annotation_items.online_eval_job_id,EXCLUDED.online_eval_job_id),updated_at=EXCLUDED.updated_at
RETURNING id::text`, parsePGUUID(queueID), parsePGUUID(completion.Job.ProjectID), parsePGUUID(completion.Job.RunID), completion.Job.TraceID, scores, completion.Run.AgentName, completion.Run.Error, parsePGUUID(completion.Job.ID), at)
	if err := row.Scan(&existing); err != nil {
		return "", err
	}
	return existing, nil
}
func (r *OnlineEvalJobRepository) Retry(ctx context.Context, jobID, workerID, lastError string, availableAt time.Time, deadLetter bool, at time.Time) error {
	status := "retry_wait"
	if deadLetter {
		status = "dead_letter"
		availableAt = at
	}
	result, err := r.pool.Exec(ctx, `UPDATE online_eval_jobs SET status=$2,last_error=$3,available_at=$4,claimed_by=NULL,claimed_at=NULL,finished_at=CASE WHEN $2='dead_letter' THEN $5 ELSE NULL END,updated_at=$5 WHERE id=$1 AND status='running' AND claimed_by=$6`, parsePGUUID(jobID), status, lastError, availableAt, at, workerID)
	if err != nil {
		return err
	}
	if result.RowsAffected() == 0 {
		return onlinerval.ErrJobNotFound
	}
	return nil
}
func (r *OnlineEvalJobRepository) RecoverStaleClaims(ctx context.Context, claimedBefore, now time.Time) (int64, error) {
	result, err := r.pool.Exec(ctx, `UPDATE online_eval_jobs SET status='retry_wait',available_at=$2,claimed_by=NULL,claimed_at=NULL,last_error=CASE WHEN last_error='' THEN 'worker lease expired; retry scheduled' ELSE last_error END,updated_at=$2 WHERE status='running' AND claimed_at < $1`, claimedBefore, now)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected(), nil
}
func (r *OnlineEvalJobRepository) Get(ctx context.Context, projectID, jobID string) (onlinerval.Job, error) {
	job, err := scanOnlineEvalJob(r.pool.QueryRow(ctx, `SELECT `+onlineEvalJobColumns+` FROM online_eval_jobs WHERE id=$1 AND project_id=$2`, parsePGUUID(jobID), parsePGUUID(projectID)))
	return job, mapOnlineEvalJobErr(err)
}
func (r *OnlineEvalJobRepository) List(ctx context.Context, projectID, runID string, limit int) ([]onlinerval.Job, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	rows, err := r.pool.Query(ctx, `SELECT `+onlineEvalJobColumns+` FROM online_eval_jobs WHERE project_id=$1 AND ($2='' OR run_id::text=$2) ORDER BY created_at DESC LIMIT $3`, parsePGUUID(projectID), runID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []onlinerval.Job{}
	for rows.Next() {
		job, err := scanOnlineEvalJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, job)
	}
	return out, rows.Err()
}
func (r *OnlineEvalJobRepository) RetryManually(ctx context.Context, projectID, jobID string, now time.Time) (onlinerval.Job, error) {
	row := r.pool.QueryRow(ctx, `UPDATE online_eval_jobs SET status='queued',available_at=$3,claimed_by=NULL,claimed_at=NULL,started_at=NULL,finished_at=NULL,last_error='',updated_at=$3 WHERE id=$1 AND project_id=$2 AND status IN ('failed','dead_letter') RETURNING `+onlineEvalJobColumns, parsePGUUID(jobID), parsePGUUID(projectID), now)
	job, err := scanOnlineEvalJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		if _, getErr := r.Get(ctx, projectID, jobID); getErr == nil {
			return onlinerval.Job{}, onlinerval.ErrJobNotRetryable
		}
	}
	return job, mapOnlineEvalJobErr(err)
}
func mapOnlineEvalJobErr(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return onlinerval.ErrJobNotFound
	}
	return err
}
