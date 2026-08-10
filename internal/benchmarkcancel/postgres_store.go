package benchmarkcancel

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const cancelledJobError = "cancelled by project administrator"

// PostgresStore is the authoritative implementation. It locks the experiment
// and its cancellable jobs before changing state, so a runner cannot commit a
// late result between the cancellation decision and its audit record.
type PostgresStore struct {
	pool *pgxpool.Pool
}

func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{pool: pool}
}

func (s *PostgresStore) CancelExperiment(ctx context.Context, request Request) (Result, error) {
	projectID, err := uuid.Parse(request.ProjectID)
	if err != nil {
		return Result{}, ErrExperimentNotFound
	}
	experimentID, err := uuid.Parse(request.ExperimentID)
	if err != nil {
		return Result{}, ErrExperimentNotFound
	}
	actorUserID, err := uuid.Parse(request.ActorUserID)
	if err != nil {
		return Result{}, ErrExperimentNotFound
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Result{}, fmt.Errorf("begin benchmark cancellation transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Both the project check and benchmark_suite_id condition are part of the
	// lookup. This endpoint cannot cancel a regular function-eval experiment.
	var lockedExperimentID uuid.UUID
	var experimentStatus string
	err = tx.QueryRow(ctx, `
		SELECT e.id, e.status
		FROM experiments e
		WHERE e.id = $1
		  AND e.project_id = $2
		  AND e.benchmark_suite_id IS NOT NULL
		  AND EXISTS (
			  SELECT 1
			  FROM project_members pm
			  WHERE pm.project_id = e.project_id
			    AND pm.user_id = $3
			    AND pm.role IN ('owner', 'admin')
		  )
		FOR UPDATE
	`, experimentID, projectID, actorUserID).Scan(&lockedExperimentID, &experimentStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{}, ErrExperimentNotFound
	}
	if err != nil {
		return Result{}, fmt.Errorf("lock benchmark experiment for cancellation: %w", err)
	}

	rows, err := tx.Query(ctx, `
		SELECT id, status
		FROM eval_jobs
		WHERE experiment_id = $1
		  AND status IN ('queued', 'running')
		ORDER BY created_at, id
		FOR UPDATE
	`, lockedExperimentID)
	if err != nil {
		return Result{}, fmt.Errorf("lock cancellable benchmark jobs: %w", err)
	}
	defer rows.Close()

	type job struct {
		id     uuid.UUID
		status string
	}
	jobs := make([]job, 0)
	for rows.Next() {
		var item job
		if err := rows.Scan(&item.id, &item.status); err != nil {
			return Result{}, fmt.Errorf("scan cancellable benchmark job: %w", err)
		}
		jobs = append(jobs, item)
	}
	if err := rows.Err(); err != nil {
		return Result{}, fmt.Errorf("iterate cancellable benchmark jobs: %w", err)
	}

	if len(jobs) == 0 {
		if err := tx.Commit(ctx); err != nil {
			return Result{}, fmt.Errorf("commit idempotent benchmark cancellation: %w", err)
		}
		return Result{ExperimentID: lockedExperimentID.String(), Status: experimentStatus}, nil
	}

	for _, item := range jobs {
		command, err := tx.Exec(ctx, `
			UPDATE eval_jobs
			SET status = 'cancelled',
			    runner_name = NULL,
			    lease_expires_at = NULL,
			    error = $2,
			    updated_at = now()
			WHERE id = $1
			  AND status IN ('queued', 'running')
		`, item.id, cancelledJobError)
		if err != nil {
			return Result{}, fmt.Errorf("cancel benchmark eval job: %w", err)
		}
		if command.RowsAffected() != 1 {
			return Result{}, fmt.Errorf("benchmark eval job changed while locked")
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO benchmark_eval_job_cancellation_audits (
				project_id, experiment_id, eval_job_id, actor_user_id, reason, previous_status
			) VALUES ($1, $2, $3, $4, $5, $6)
		`, projectID, lockedExperimentID, item.id, actorUserID, request.Reason, item.status); err != nil {
			return Result{}, fmt.Errorf("write benchmark cancellation audit: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE experiments
		SET status = 'cancelled', updated_at = now()
		WHERE id = $1 AND project_id = $2
	`, lockedExperimentID, projectID); err != nil {
		return Result{}, fmt.Errorf("mark benchmark experiment cancelled: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Result{}, fmt.Errorf("commit benchmark cancellation: %w", err)
	}
	return Result{
		ExperimentID:  lockedExperimentID.String(),
		Status:        "cancelled",
		CancelledJobs: len(jobs),
		AuditEvents:   len(jobs),
	}, nil
}

// ListAudit reads the retained job-level cancellation facts after confirming
// the requested experiment belongs to the supplied project and is a benchmark.
func (s *PostgresStore) ListAudit(ctx context.Context, projectIDValue, experimentIDValue string) ([]AuditEvent, error) {
	projectID, err := uuid.Parse(projectIDValue)
	if err != nil {
		return nil, ErrExperimentNotFound
	}
	experimentID, err := uuid.Parse(experimentIDValue)
	if err != nil {
		return nil, ErrExperimentNotFound
	}

	var exists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM experiments
			WHERE id = $1
			  AND project_id = $2
			  AND benchmark_suite_id IS NOT NULL
		)
	`, experimentID, projectID).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check benchmark experiment for cancellation audit: %w", err)
	}
	if !exists {
		return nil, ErrExperimentNotFound
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, project_id, experiment_id, eval_job_id, actor_user_id,
		       reason, previous_status, created_at
		FROM benchmark_eval_job_cancellation_audits
		WHERE project_id = $1 AND experiment_id = $2
		ORDER BY created_at ASC, id ASC
	`, projectID, experimentID)
	if err != nil {
		return nil, fmt.Errorf("list benchmark cancellation audit: %w", err)
	}
	defer rows.Close()

	events := make([]AuditEvent, 0)
	for rows.Next() {
		var id, eventProjectID, eventExperimentID, evalJobID, actorUserID uuid.UUID
		var reason, previousStatus string
		var createdAt time.Time
		if err := rows.Scan(&id, &eventProjectID, &eventExperimentID, &evalJobID, &actorUserID, &reason, &previousStatus, &createdAt); err != nil {
			return nil, fmt.Errorf("scan benchmark cancellation audit: %w", err)
		}
		events = append(events, AuditEvent{
			ID: id.String(), ProjectID: eventProjectID.String(), ExperimentID: eventExperimentID.String(), EvalJobID: evalJobID.String(),
			ActorUserID: actorUserID.String(), Reason: reason, PreviousStatus: previousStatus, CreatedAt: createdAt.UTC().Format(time.RFC3339),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate benchmark cancellation audit: %w", err)
	}
	return events, nil
}
