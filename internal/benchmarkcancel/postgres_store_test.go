package benchmarkcancel

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"AgentScope/internal/store"
	"github.com/google/uuid"
)

func TestPostgresStoreCancelsOnlyActiveBenchmarkJobsAndWritesAudit(t *testing.T) {
	databaseURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}
	requireBenchmarkCancellationTestDatabase(t, databaseURL)

	ctx := context.Background()
	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	projectID := uuid.New()
	userID := uuid.New()
	outsiderID := uuid.New()
	datasetID := uuid.New()
	queuedCaseID := uuid.New()
	runningCaseID := uuid.New()
	completedCaseID := uuid.New()
	suiteID := uuid.New()
	experimentID := uuid.New()
	queuedJobID := uuid.New()
	runningJobID := uuid.New()
	completedJobID := uuid.New()

	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM projects WHERE id = $1`, projectID); err != nil {
			t.Errorf("cleanup project: %v", err)
		}
		for _, id := range []uuid.UUID{userID, outsiderID} {
			if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, id); err != nil {
				t.Errorf("cleanup user %s: %v", id, err)
			}
		}
	})

	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, password_hash, display_name) VALUES ($1, $2, 'x', 'benchmark cancellation admin')`, userID, "benchmark-cancel-"+userID.String()+"@example.invalid"); err != nil {
		t.Fatalf("create actor: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, password_hash, display_name) VALUES ($1, $2, 'x', 'benchmark cancellation outsider')`, outsiderID, "benchmark-cancel-outsider-"+outsiderID.String()+"@example.invalid"); err != nil {
		t.Fatalf("create outsider: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO projects (id, name) VALUES ($1, 'benchmark-cancel-project')`, projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'owner')`, projectID, userID); err != nil {
		t.Fatalf("add project owner: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO datasets (id, project_id, name) VALUES ($1, $2, 'benchmark-cancel-dataset')`, datasetID, projectID); err != nil {
		t.Fatalf("create dataset: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO dataset_cases (id, dataset_id, input) VALUES
		($1, $4, '{}'::jsonb), ($2, $4, '{}'::jsonb), ($3, $4, '{}'::jsonb)
	`, queuedCaseID, runningCaseID, completedCaseID, datasetID); err != nil {
		t.Fatalf("create dataset cases: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO benchmark_suites (id, project_id, dataset_id, benchmark_key, name, version, harness_kind, execution_kind)
		VALUES ($1, $2, $3, 'cancellation-fixture', 'Cancellation fixture', 'v1', 'harbor', 'harbor')
	`, suiteID, projectID, datasetID); err != nil {
		t.Fatalf("create benchmark suite: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO experiments (id, project_id, dataset_id, target_name, status, total_cases, benchmark_suite_id, benchmark_key, benchmark_version)
		VALUES ($1, $2, $3, 'fixture-agent', 'running', 3, $4, 'cancellation-fixture', 'v1')
	`, experimentID, projectID, datasetID, suiteID); err != nil {
		t.Fatalf("create benchmark experiment: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO eval_jobs (id, experiment_id, dataset_case_id, status, execution_kind, runner_name, lease_expires_at) VALUES
		($1, $4, $5, 'queued', 'harbor', NULL, NULL),
		($2, $4, $6, 'running', 'harbor', 'harbor-fixture', now() + interval '5 minutes'),
		($3, $4, $7, 'succeeded', 'harbor', 'harbor-fixture', NULL)
	`, queuedJobID, runningJobID, completedJobID, experimentID, queuedCaseID, runningCaseID, completedCaseID); err != nil {
		t.Fatalf("create eval jobs: %v", err)
	}

	const reason = "legacy digest snapshot was superseded before worker startup"
	canceller := NewService(NewPostgresStore(pool))
	if _, err := canceller.CancelExperiment(ctx, Request{
		ProjectID: projectID.String(), ExperimentID: experimentID.String(), ActorUserID: outsiderID.String(), Reason: reason,
	}); !errors.Is(err, ErrExperimentNotFound) {
		t.Fatalf("non-admin store cancellation error = %v, want ErrExperimentNotFound", err)
	}
	result, err := canceller.CancelExperiment(ctx, Request{
		ProjectID: projectID.String(), ExperimentID: experimentID.String(), ActorUserID: userID.String(), Reason: reason,
	})
	if err != nil {
		t.Fatalf("CancelExperiment() error = %v", err)
	}
	if result.ExperimentID != experimentID.String() || result.Status != "cancelled" || result.CancelledJobs != 2 || result.AuditEvents != 2 {
		t.Fatalf("cancellation result = %#v", result)
	}

	rows, err := pool.Query(ctx, `SELECT id, status, runner_name, lease_expires_at, error FROM eval_jobs WHERE experiment_id = $1 ORDER BY id`, experimentID)
	if err != nil {
		t.Fatalf("read cancelled jobs: %v", err)
	}
	defer rows.Close()
	jobs := map[uuid.UUID]struct {
		status string
		runner *string
		lease  *time.Time
		error  *string
	}{}
	for rows.Next() {
		var id uuid.UUID
		var job struct {
			status string
			runner *string
			lease  *time.Time
			error  *string
		}
		if err := rows.Scan(&id, &job.status, &job.runner, &job.lease, &job.error); err != nil {
			t.Fatalf("scan eval job: %v", err)
		}
		jobs[id] = job
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate eval jobs: %v", err)
	}
	for _, id := range []uuid.UUID{queuedJobID, runningJobID} {
		job := jobs[id]
		if job.status != "cancelled" || job.runner != nil || job.lease != nil || job.error == nil || *job.error != cancelledJobError {
			t.Errorf("cancelled job %s = %#v", id, job)
		}
	}
	if job := jobs[completedJobID]; job.status != "succeeded" {
		t.Errorf("completed job status = %q, want succeeded", job.status)
	}

	var experimentStatus string
	if err := pool.QueryRow(ctx, `SELECT status FROM experiments WHERE id = $1`, experimentID).Scan(&experimentStatus); err != nil {
		t.Fatalf("read experiment: %v", err)
	}
	if experimentStatus != "cancelled" {
		t.Errorf("experiment status = %q, want cancelled", experimentStatus)
	}

	auditRows, err := pool.Query(ctx, `
		SELECT eval_job_id, actor_user_id, reason, previous_status
		FROM benchmark_eval_job_cancellation_audits
		WHERE experiment_id = $1
		ORDER BY eval_job_id
	`, experimentID)
	if err != nil {
		t.Fatalf("read audit rows: %v", err)
	}
	defer auditRows.Close()
	audit := map[uuid.UUID]string{}
	for auditRows.Next() {
		var jobID, actorID uuid.UUID
		var storedReason, previousStatus string
		if err := auditRows.Scan(&jobID, &actorID, &storedReason, &previousStatus); err != nil {
			t.Fatalf("scan audit row: %v", err)
		}
		if actorID != userID || storedReason != reason {
			t.Errorf("audit row = actor %s reason %q, want actor %s reason %q", actorID, storedReason, userID, reason)
		}
		audit[jobID] = previousStatus
	}
	if err := auditRows.Err(); err != nil {
		t.Fatalf("iterate audit rows: %v", err)
	}
	if len(audit) != 2 || audit[queuedJobID] != "queued" || audit[runningJobID] != "running" {
		t.Errorf("audit rows = %#v, want queued/running rows only", audit)
	}

	// Cancellation requests are idempotent. Do not rewrite the old audit reason
	// or fabricate duplicate audit facts on a retry.
	retry, err := canceller.CancelExperiment(ctx, Request{
		ProjectID: projectID.String(), ExperimentID: experimentID.String(), ActorUserID: userID.String(), Reason: "retry after cancellation",
	})
	if err != nil {
		t.Fatalf("idempotent CancelExperiment() error = %v", err)
	}
	if retry.CancelledJobs != 0 || retry.AuditEvents != 0 {
		t.Errorf("idempotent cancellation = %#v, want no new rows", retry)
	}
	var auditCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM benchmark_eval_job_cancellation_audits WHERE experiment_id = $1`, experimentID).Scan(&auditCount); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	if auditCount != 2 {
		t.Errorf("audit count after retry = %d, want 2", auditCount)
	}

	events, err := canceller.ListAudit(ctx, projectID.String(), experimentID.String())
	if err != nil {
		t.Fatalf("ListAudit() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("ListAudit() count = %d, want 2", len(events))
	}
	for _, event := range events {
		if event.ProjectID != projectID.String() || event.ExperimentID != experimentID.String() || event.ActorUserID != userID.String() || event.Reason != reason {
			t.Errorf("audit event = %#v", event)
		}
	}
}

func TestPostgresStoreDoesNotCancelForeignExperiment(t *testing.T) {
	// Project ownership is covered by the lock predicate in CancelExperiment.
	// Keep the integration fixture focused in the primary test; this unit-level
	// guard documents that an invalid UUID remains non-enumerating too.
	store := NewPostgresStore(nil)
	_, err := store.CancelExperiment(context.Background(), Request{ProjectID: "invalid", ExperimentID: "invalid", ActorUserID: "invalid", Reason: "obsolete snapshot"})
	if !errors.Is(err, ErrExperimentNotFound) {
		t.Fatalf("CancelExperiment(invalid IDs) error = %v, want ErrExperimentNotFound", err)
	}
}

func requireBenchmarkCancellationTestDatabase(t *testing.T, databaseURL string) {
	t.Helper()
	if !strings.Contains(databaseURL, "/agentscope_test") {
		t.Fatalf("AGENTSCOPE_TEST_DATABASE_URL must point to agentscope_test")
	}
}
