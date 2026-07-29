package adapter

import (
	"context"
	"os"
	"testing"

	"AgentScope/internal/store"
	"github.com/google/uuid"
)

func TestRuntimeTerminalTransitionAtomicallyEnqueuesOnlineEvaluation(t *testing.T) {
	databaseURL := getenvTestDatabase(t)
	ctx := context.Background()
	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	applyAdapterMigration(t, pool, "../../migrations/00018_online_eval_jobs.sql")
	applyAdapterMigration(t, pool, "../../migrations/00019_online_eval_job_leases.sql")

	projectID, runID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO projects (id, name) VALUES ($1, $2)`, projectID, "runtime-online-job-"+projectID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, project_id, source, status, trace_id, agent_name) VALUES ($1, $2, 'live', 'running', 'trace-atomic', 'atomic-agent')`, runID, projectID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM projects WHERE id = $1`, projectID) })

	querier := NewRuntimeRunQuerier(store.New(pool), pool)
	if !querier.AtomicTerminalRunEnabled() {
		t.Fatal("production runtime querier must enable atomic terminal transitions")
	}
	updated, err := querier.CompleteRuntimeRunAndEnqueue(ctx, runID.String(), projectID.String(), "done", 42, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "completed" || updated.Output != "done" {
		t.Fatalf("updated run = %#v", updated)
	}

	var events, jobs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM run_state_events WHERE run_id = $1 AND to_status = 'completed'`, runID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM online_eval_jobs WHERE project_id = $1 AND run_id = $2`, projectID, runID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if events != 1 || jobs != 1 {
		t.Fatalf("terminal transition persisted events=%d jobs=%d, want 1/1", events, jobs)
	}
}

func getenvTestDatabase(t *testing.T) string {
	t.Helper()
	databaseURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}
	requireAdapterTestDatabase(t, databaseURL)
	return databaseURL
}

func TestRuntimeFailedTransitionAtomicallyEnqueuesOnlineEvaluation(t *testing.T) {
	databaseURL := getenvTestDatabase(t)
	ctx := context.Background()
	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	applyAdapterMigration(t, pool, "../../migrations/00018_online_eval_jobs.sql")
	applyAdapterMigration(t, pool, "../../migrations/00019_online_eval_job_leases.sql")

	projectID, runID := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO projects (id, name) VALUES ($1, $2)`, projectID, "runtime-online-fail-"+projectID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO runs (id, project_id, source, status, trace_id, agent_name) VALUES ($1, $2, 'live', 'running', 'trace-atomic-fail', 'atomic-agent')`, runID, projectID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM projects WHERE id = $1`, projectID) })

	querier := NewRuntimeRunQuerier(store.New(pool), pool)
	updated, err := querier.FailRuntimeRunAndEnqueue(ctx, runID.String(), projectID.String(), "tool timeout", 42, "", "", []byte(`{"source":"sdk"}`))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "failed" || updated.Error != "tool timeout" {
		t.Fatalf("updated run = %#v", updated)
	}

	var events, jobs int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM run_state_events WHERE run_id = $1 AND to_status = 'failed' AND message = 'tool timeout'`, runID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM online_eval_jobs WHERE project_id = $1 AND run_id = $2 AND trace_id = 'trace-atomic-fail'`, projectID, runID).Scan(&jobs); err != nil {
		t.Fatal(err)
	}
	if events != 1 || jobs != 1 {
		t.Fatalf("failed terminal transition persisted events=%d jobs=%d, want 1/1", events, jobs)
	}
}
