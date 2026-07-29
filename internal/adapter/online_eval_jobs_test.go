package adapter

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"AgentScope/internal/onlinerval"
	"AgentScope/internal/store"
	"github.com/google/uuid"
)

func TestOnlineEvalJobRepositoryRejectsStaleWorkerLease(t *testing.T) {
	databaseURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}
	requireAdapterTestDatabase(t, databaseURL)
	ctx := context.Background()
	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	applyAdapterMigration(t, pool, "../../migrations/00018_online_eval_jobs.sql")
	applyAdapterMigration(t, pool, "../../migrations/00019_online_eval_job_leases.sql")

	projectID, runID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO projects (id, name) VALUES ($1, $2)`, projectID, "online-job-lease-"+projectID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, project_id, source, status, agent_name) VALUES ($1, $2, 'live', 'completed', 'lease-test-agent')`, runID, projectID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM projects WHERE id = $1`, projectID) })

	repo := NewOnlineEvalJobRepository(pool)
	now := time.Now().UTC()
	job, err := repo.Enqueue(ctx, projectID.String(), runID.String(), "trace-lease", onlinerval.DefaultJobEvaluatorVersion)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate A holding a lease, then a stale-claim recovery and B taking over.
	leaseA, leaseB := uuid.NewString(), uuid.NewString()
	if _, err := pool.Exec(ctx, `UPDATE online_eval_jobs SET status='running', claimed_by='worker-a', lease_token=$2::uuid, claimed_at=$3 WHERE id=$1`, job.ID, leaseA, now.Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE online_eval_jobs SET claimed_by='worker-b', lease_token=$2::uuid, claimed_at=$3 WHERE id=$1`, job.ID, leaseB, now); err != nil {
		t.Fatal(err)
	}

	if err := repo.MarkSkipped(ctx, job.ID, "worker-a", leaseA, "", "not_selected", now); !errors.Is(err, onlinerval.ErrJobLeaseLost) {
		t.Fatalf("stale worker MarkSkipped error = %v, want ErrJobLeaseLost", err)
	}
	if err := repo.MarkSkipped(ctx, job.ID, "worker-b", leaseB, "", "not_selected", now); err != nil {
		t.Fatalf("current worker MarkSkipped: %v", err)
	}
	persisted, err := repo.Get(ctx, projectID.String(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != onlinerval.JobSkipped || persisted.ClaimedBy != "" || persisted.LeaseToken != "" {
		t.Fatalf("terminal job = %#v", persisted)
	}
}

// Complete writes scores and optional annotation data before it advances the
// Job to succeeded. The lease check therefore must run in the same transaction:
// a stale Worker must leave no partial score or annotation side effects behind.
func TestOnlineEvalJobRepositoryRollsBackStaleWorkerCompletion(t *testing.T) {
	databaseURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}
	requireAdapterTestDatabase(t, databaseURL)
	ctx := context.Background()
	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	applyAdapterMigration(t, pool, "../../migrations/00018_online_eval_jobs.sql")
	applyAdapterMigration(t, pool, "../../migrations/00019_online_eval_job_leases.sql")

	projectID, runID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO projects (id, name) VALUES ($1, $2)`, projectID, "online-job-stale-complete-"+projectID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, project_id, source, status, trace_id, agent_name) VALUES ($1, $2, 'live', 'completed', 'trace-stale-complete', 'lease-test-agent')`, runID, projectID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM projects WHERE id = $1`, projectID) })

	repo := NewOnlineEvalJobRepository(pool)
	now := time.Now().UTC()
	job, err := repo.Enqueue(ctx, projectID.String(), runID.String(), "trace-stale-complete", onlinerval.DefaultJobEvaluatorVersion)
	if err != nil {
		t.Fatal(err)
	}
	staleLease, currentLease := uuid.NewString(), uuid.NewString()
	if _, err := pool.Exec(ctx, `UPDATE online_eval_jobs SET status='running', claimed_by='worker-b', lease_token=$2::uuid, claimed_at=$3 WHERE id=$1`, job.ID, currentLease, now); err != nil {
		t.Fatal(err)
	}

	completion := onlinerval.Completion{
		Job: job,
		Run: &onlinerval.Run{ID: runID.String(), ProjectID: projectID.String(), TraceID: "trace-stale-complete", AgentName: "lease-test-agent"},
		Scores: []onlinerval.EvalScore{{
			ProjectID: projectID.String(), RunID: runID.String(), TraceID: "trace-stale-complete",
			MetricName: "completion_status", MetricKind: "efficiency", Score: 0.0, Passed: false, Reason: "stale worker test",
		}},
		EvaluationSource: "trace_heuristic",
		CreateAnnotation: true,
	}
	if _, err := repo.Complete(ctx, completion, "worker-a", staleLease, now); !errors.Is(err, onlinerval.ErrJobLeaseLost) {
		t.Fatalf("stale worker Complete error = %v, want ErrJobLeaseLost", err)
	}

	var scoreCount, annotationCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM eval_scores WHERE online_eval_job_id = $1`, job.ID).Scan(&scoreCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM annotation_items WHERE online_eval_job_id = $1`, job.ID).Scan(&annotationCount); err != nil {
		t.Fatal(err)
	}
	if scoreCount != 0 || annotationCount != 0 {
		t.Fatalf("stale completion leaked side effects: scores=%d annotations=%d", scoreCount, annotationCount)
	}
	persisted, err := repo.Get(ctx, projectID.String(), job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if persisted.Status != onlinerval.JobRunning || persisted.ClaimedBy != "worker-b" || persisted.LeaseToken != currentLease {
		t.Fatalf("stale completion changed current lease: %#v", persisted)
	}
}
