package store

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOpen(t *testing.T) {
	dbURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	pool.Close()
}

// requireTestDatabase fails the test if the database name in databaseURL
// does not end with "_test". This prevents accidental destructive operations
// on production databases.
func requireTestDatabase(t *testing.T, databaseURL string) {
	t.Helper()
	u, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("cannot parse AGENTSCOPE_TEST_DATABASE_URL: %v", err)
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if dbName == "" {
		t.Fatal("AGENTSCOPE_TEST_DATABASE_URL must include a database name")
	}
	if !strings.HasSuffix(dbName, "_test") {
		t.Fatalf("AGENTSCOPE_TEST_DATABASE_URL must point to a database ending with '_test'; got %q (parsed: %q)",
			databaseURL, dbName)
	}
}

func runMigration(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	b, err := os.ReadFile("../../migrations/00001_init.sql")
	if err != nil {
		t.Fatalf("read migration file: %v", err)
	}
	ctx := context.Background()
	if _, err := pool.Exec(ctx, string(b)); err != nil {
		t.Fatalf("run migration: %v", err)
	}
}

func dropAllTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	stmt := `DROP TABLE IF EXISTS
	annotation_items,
		annotation_queues,
		trace_sampling_rules,
		run_state_events,
		agent_heartbeats,
		agent_capabilities,
		agent_instances,
		agent_versions,
		agents,
		eval_metric_definitions,
		eval_scores,
		eval_case_results,
		eval_jobs,
		experiments,
		evaluators,
		dataset_cases,
		datasets,
		runs,
		project_tokens,
		project_members,
		projects,
		users
	CASCADE`
	if _, err := pool.Exec(ctx, stmt); err != nil {
		t.Fatalf("drop tables: %v", err)
	}
}

func now() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now(), Valid: true}
}

// ============================================================
// Test helpers — shared setup for complex scenarios
// ============================================================

type testFixture struct {
	q      *Queries
	pool   *pgxpool.Pool
	projID pgtype.UUID
	userID pgtype.UUID
}

func setupMinimal(t *testing.T, pool *pgxpool.Pool) testFixture {
	t.Helper()
	q := New(pool)
	user, err := q.CreateUser(context.Background(), CreateUserParams{
		Email:        "fixture@test.com",
		PasswordHash: []byte("hash"),
		DisplayName:  "Fixture",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	proj, err := q.CreateProject(context.Background(), CreateProjectParams{
		Name: "Fixture Project",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	_, err = q.AddProjectMember(context.Background(), AddProjectMemberParams{
		ProjectID: proj.ID,
		UserID:    user.ID,
		Role:      "owner",
	})
	if err != nil {
		t.Fatalf("AddProjectMember: %v", err)
	}
	return testFixture{q: q, pool: pool, projID: proj.ID, userID: user.ID}
}

func setupEvalFixture(t *testing.T, pool *pgxpool.Pool) (testFixture, pgtype.UUID, pgtype.UUID) {
	t.Helper()
	f := setupMinimal(t, pool)
	ctx := context.Background()

	ds, err := f.q.CreateDataset(ctx, CreateDatasetParams{
		ProjectID: f.projID,
		Name:      "Test Dataset",
	})
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}

	dsCase, err := f.q.CreateDatasetCase(ctx, CreateDatasetCaseParams{
		DatasetID: ds.ID,
		Input:     []byte(`{"query":"hello"}`),
		Metadata:  []byte("{}"),
	})
	if err != nil {
		t.Fatalf("CreateDatasetCase: %v", err)
	}

	eval, err := f.q.CreateEvaluator(ctx, CreateEvaluatorParams{
		ProjectID: f.projID,
		Name:      "exact-match",
		Type:      "rule",
		Config:    []byte(`{"type":"exact"}`),
	})
	if err != nil {
		t.Fatalf("CreateEvaluator: %v", err)
	}

	exp, err := f.q.CreateExperiment(ctx, CreateExperimentParams{
		ProjectID:   f.projID,
		DatasetID:   ds.ID,
		EvaluatorID: pgtype.UUID{Bytes: eval.ID.Bytes, Valid: true},
		TargetName:  "test-target",
		Status:      "pending",
		TotalCases:  1,
		Metadata:    []byte("{}"),
	})
	if err != nil {
		t.Fatalf("CreateExperiment: %v", err)
	}

	return f, exp.ID, dsCase.ID
}

func leaseFuture() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now().Add(2 * time.Minute), Valid: true}
}

// ============================================================
// Issue 1: ClaimEvalJob lease expiry
// ============================================================

func TestClaimEvalJobLease(t *testing.T) {
	dbURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer pool.Close()

	requireTestDatabase(t, dbURL)
	dropAllTables(t, pool)
	runMigration(t, pool)

	f, expID, caseID := setupEvalFixture(t, pool)
	q := f.q

	// ----------------------------------------------------------
	// Scenario A: queued job → claim succeeds, attempts 0→1
	// ----------------------------------------------------------
	t.Run("claim_queued_job", func(t *testing.T) {
		job, err := q.CreateEvalJob(ctx, CreateEvalJobParams{
			ExperimentID:  expID,
			DatasetCaseID: caseID,
			Status:        "queued",
		})
		if err != nil {
			t.Fatalf("CreateEvalJob: %v", err)
		}
		if job.Attempts != 0 {
			t.Fatalf("initial attempts = %d, want 0", job.Attempts)
		}

		leaseUntil := leaseFuture()
		claimed, err := q.ClaimEvalJob(ctx, ClaimEvalJobParams{
			ID:             job.ID,
			RunnerName:     pgtype.Text{String: "runner-a", Valid: true},
			LeaseExpiresAt: leaseUntil,
		})
		if err != nil {
			t.Fatalf("ClaimEvalJob (queued): %v", err)
		}
		if claimed.Status != "running" {
			t.Errorf("status = %q, want running", claimed.Status)
		}
		if claimed.Attempts != 1 {
			t.Errorf("attempts = %d, want 1", claimed.Attempts)
		}
		if claimed.RunnerName.String != "runner-a" {
			t.Errorf("runner_name = %q, want runner-a", claimed.RunnerName.String)
		}

		// ----------------------------------------------------------
		// Scenario B: running + unexpired lease → cannot claim
		// ----------------------------------------------------------
		t.Run("cannot_claim_unexpired", func(t *testing.T) {
			_, err := q.ClaimEvalJob(ctx, ClaimEvalJobParams{
				ID:             job.ID,
				RunnerName:     pgtype.Text{String: "runner-b", Valid: true},
				LeaseExpiresAt: leaseUntil,
			})
			if !errors.Is(err, pgx.ErrNoRows) {
				t.Errorf("expected pgx.ErrNoRows for unexpired lease, got %v", err)
			}
		})

		// ----------------------------------------------------------
		// Scenario C: expired lease → can claim, attempts increments
		// ----------------------------------------------------------
		t.Run("claim_expired", func(t *testing.T) {
			// Force lease expiry
			_, err := pool.Exec(ctx,
				"UPDATE eval_jobs SET lease_expires_at = now() - interval '1 hour' WHERE id = $1",
				job.ID)
			if err != nil {
				t.Fatalf("force lease expiry: %v", err)
			}

			reclaimed, err := q.ClaimEvalJob(ctx, ClaimEvalJobParams{
				ID:             job.ID,
				RunnerName:     pgtype.Text{String: "runner-b", Valid: true},
				LeaseExpiresAt: leaseUntil,
			})
			if err != nil {
				t.Fatalf("ClaimEvalJob (expired): %v", err)
			}
			if reclaimed.Attempts != 2 {
				t.Errorf("attempts after reclaim = %d, want 2", reclaimed.Attempts)
			}
			if reclaimed.RunnerName.String != "runner-b" {
				t.Errorf("runner_name after reclaim = %q, want runner-b", reclaimed.RunnerName.String)
			}
		})
	})

	// ----------------------------------------------------------
	// Scenario D: NULL lease → claim must fail
	// ----------------------------------------------------------
	t.Run("reject_null_lease", func(t *testing.T) {
		job, err := q.CreateEvalJob(ctx, CreateEvalJobParams{
			ExperimentID:  expID,
			DatasetCaseID: caseID,
			Status:        "queued",
		})
		if err != nil {
			t.Fatalf("CreateEvalJob: %v", err)
		}

		_, err = q.ClaimEvalJob(ctx, ClaimEvalJobParams{
			ID:             job.ID,
			RunnerName:     pgtype.Text{String: "runner-x", Valid: true},
			LeaseExpiresAt: pgtype.Timestamptz{Valid: false},
		})
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Errorf("expected pgx.ErrNoRows for NULL lease, got %v", err)
		}

		// Verify job is still queued (not running)
		unclaimed, err := q.GetEvalJobByID(ctx, job.ID)
		if err != nil {
			t.Fatalf("GetEvalJobByID: %v", err)
		}
		if unclaimed.Status != "queued" {
			t.Errorf("job status after NULL-lease claim = %q, want queued", unclaimed.Status)
		}
		if unclaimed.Attempts != 0 {
			t.Errorf("job attempts after NULL-lease claim = %d, want 0", unclaimed.Attempts)
		}
	})
}

// ============================================================
// Original integration tests (with requireTestDatabase guard)
// ============================================================

func TestCreateUserAndProjectAndRun(t *testing.T) {
	dbURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer pool.Close()

	requireTestDatabase(t, dbURL)
	dropAllTables(t, pool)
	runMigration(t, pool)

	q := New(pool)

	// === User ===
	user, err := q.CreateUser(ctx, CreateUserParams{
		Email:        "test@example.com",
		PasswordHash: []byte("$2a$10$fakehashvalue1234567890"),
		DisplayName:  "Test User",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if !user.ID.Valid {
		t.Fatal("user.ID not set")
	}

	// === Project ===
	proj, err := q.CreateProject(ctx, CreateProjectParams{
		Name:        "Test Project",
		Description: pgtype.Text{Valid: false},
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if !proj.ID.Valid {
		t.Fatal("project.ID not set")
	}

	// === Project Member ===
	_, err = q.AddProjectMember(ctx, AddProjectMemberParams{
		ProjectID: proj.ID,
		UserID:    user.ID,
		Role:      "owner",
	})
	if err != nil {
		t.Fatalf("AddProjectMember: %v", err)
	}

	// === Run ===
	run, err := q.CreateRun(ctx, CreateRunParams{
		ProjectID:    proj.ID,
		Source:       "live",
		Status:       "running",
		AgentName:    "test-agent",
		AgentVersion: pgtype.Text{Valid: false},
		Metadata:     []byte("{}"),
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if !run.ID.Valid {
		t.Fatal("run.ID not set")
	}
	if run.Source != "live" {
		t.Errorf("run.Source = %q", run.Source)
	}
	if run.Status != "running" {
		t.Errorf("run.Status = %q", run.Status)
	}

	// === ListRunsByProject ===
	runs, err := q.ListRunsByProject(ctx, proj.ID)
	if err != nil {
		t.Fatalf("ListRunsByProject: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("got %d runs", len(runs))
	}

	// === GetRunByID ===
	got, err := q.GetRunByID(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRunByID: %v", err)
	}
	if got.AgentName != "test-agent" {
		t.Errorf("AgentName=%q", got.AgentName)
	}

	// === Project token ===
	token, err := q.CreateProjectToken(ctx, CreateProjectTokenParams{
		ProjectID:   proj.ID,
		TokenHash:   []byte("sha256hashvalue"),
		TokenPrefix: "ascope_abc",
		Scope:       "otlp:write",
	})
	if err != nil {
		t.Fatalf("CreateProjectToken: %v", err)
	}
	if !token.ID.Valid {
		t.Fatal("token.ID not set")
	}

	// === GetTokenByHash ===
	foundToken, err := q.GetTokenByHash(ctx, []byte("sha256hashvalue"))
	if err != nil {
		t.Fatalf("GetTokenByHash: %v", err)
	}
	if foundToken.Scope != "otlp:write" {
		t.Errorf("scope=%q", foundToken.Scope)
	}

	// === ListProjectTokens ===
	tokens, err := q.ListProjectTokens(ctx, proj.ID)
	if err != nil {
		t.Fatalf("ListProjectTokens: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("got %d tokens", len(tokens))
	}
}

func TestCreateEvalRunAndResults(t *testing.T) {
	dbURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer pool.Close()

	requireTestDatabase(t, dbURL)
	dropAllTables(t, pool)
	runMigration(t, pool)

	q := New(pool)

	// === User + Project ===
	user, err := q.CreateUser(ctx, CreateUserParams{
		Email:        "eval-test@example.com",
		PasswordHash: []byte("hash"),
		DisplayName:  "Eval Tester",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	proj, err := q.CreateProject(ctx, CreateProjectParams{
		Name: "Eval Project",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	_, err = q.AddProjectMember(ctx, AddProjectMemberParams{
		ProjectID: proj.ID,
		UserID:    user.ID,
		Role:      "member",
	})
	if err != nil {
		t.Fatalf("AddProjectMember: %v", err)
	}

	// === Dataset ===
	ds, err := q.CreateDataset(ctx, CreateDatasetParams{
		ProjectID: proj.ID,
		Name:      "Test Dataset",
	})
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}

	// === Dataset case ===
	dsCase, err := q.CreateDatasetCase(ctx, CreateDatasetCaseParams{
		DatasetID: ds.ID,
		Input:     []byte(`{"query":"hello"}`),
		Metadata:  []byte("{}"),
	})
	if err != nil {
		t.Fatalf("CreateDatasetCase: %v", err)
	}

	// === Evaluator ===
	eval, err := q.CreateEvaluator(ctx, CreateEvaluatorParams{
		ProjectID: proj.ID,
		Name:      "exact-match",
		Type:      "rule",
		Config:    []byte(`{"type":"exact"}`),
	})
	if err != nil {
		t.Fatalf("CreateEvaluator: %v", err)
	}

	// === Experiment ===
	exp, err := q.CreateExperiment(ctx, CreateExperimentParams{
		ProjectID:   proj.ID,
		DatasetID:   ds.ID,
		EvaluatorID: pgtype.UUID{Bytes: eval.ID.Bytes, Valid: true},
		TargetName:  "test-target",
		Status:      "pending",
		TotalCases:  1,
		Metadata:    []byte("{}"),
	})
	if err != nil {
		t.Fatalf("CreateExperiment: %v", err)
	}

	// === Eval job ===
	job, err := q.CreateEvalJob(ctx, CreateEvalJobParams{
		ExperimentID:  exp.ID,
		DatasetCaseID: dsCase.ID,
		Status:        "queued",
	})
	if err != nil {
		t.Fatalf("CreateEvalJob: %v", err)
	}

	claimLease := time.Now().Add(2 * time.Minute)

	// === Claim eval job (with future lease) ===
	claimed, err := q.ClaimEvalJob(ctx, ClaimEvalJobParams{
		ID:             job.ID,
		RunnerName:     pgtype.Text{String: "test-runner", Valid: true},
		LeaseExpiresAt: pgtype.Timestamptz{Time: claimLease, Valid: true},
	})
	if err != nil {
		t.Fatalf("ClaimEvalJob: %v", err)
	}
	if claimed.Status != "running" {
		t.Errorf("claimed.Status = %q", claimed.Status)
	}
	if claimed.Attempts != 1 {
		t.Errorf("claimed.Attempts = %d", claimed.Attempts)
	}

	// === Eval-run ===
	evalRun, err := q.CreateRun(ctx, CreateRunParams{
		ProjectID: proj.ID,
		Source:    "eval",
		Status:    "completed",
		AgentName: "test-target",
		Metadata:  []byte("{}"),
	})
	if err != nil {
		t.Fatalf("CreateRun (eval): %v", err)
	}

	// === Eval case result (with run_id) ===
	result, err := q.CreateEvalCaseResult(ctx, CreateEvalCaseResultParams{
		EvalJobID:    job.ID,
		RunID:        pgtype.UUID{Bytes: evalRun.ID.Bytes, Valid: true},
		Score:        pgtype.Numeric{Valid: false},
		Passed:       pgtype.Bool{Bool: true, Valid: true},
		Reason:       pgtype.Text{String: "exact match", Valid: true},
		Details:      []byte("{}"),
		ActualOutput: pgtype.Text{String: "hello", Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateEvalCaseResult: %v", err)
	}
	if !result.ID.Valid {
		t.Fatal("result.ID not set")
	}
	if !result.RunID.Valid {
		t.Error("result.RunID not set")
	}

	// === ListEvalResultsByExperiment ===
	results, err := q.ListEvalResultsByExperiment(ctx, exp.ID)
	if err != nil {
		t.Fatalf("ListEvalResultsByExperiment: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("got %d results", len(results))
	}
	r := results[0]
	if !r.DatasetCaseID.Valid {
		t.Error("expected DatasetCaseID to be set")
	}
}

// ============================================================
// Run state events and active runs
// ============================================================

func TestRunStateEventsAndActiveRuns(t *testing.T) {
	dbURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}
	requireTestDatabase(t, dbURL)

	ctx := context.Background()
	pool, err := Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()
	dropAllTables(t, pool)
	runMigration(t, pool)

	q := New(pool)
	f := setupMinimal(t, pool)

	run, err := q.CreateRun(ctx, CreateRunParams{
		ProjectID:    f.projID,
		Source:       "live",
		Status:       "queued",
		AgentName:    "test-agent",
		InputSummary: pgtype.Text{String: "hello", Valid: true},
		Metadata:     []byte("{}"),
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	updated, err := q.UpdateRunStatus(ctx, UpdateRunStatusParams{
		ID:        run.ID,
		ProjectID: f.projID,
		Status:    "running",
		Error:     pgtype.Text{Valid: false},
	})
	if err != nil {
		t.Fatalf("UpdateRunStatus: %v", err)
	}
	if updated.Status != "running" {
		t.Fatalf("updated.Status = %q, want running", updated.Status)
	}

	if _, err := q.CreateRunStateEvent(ctx, CreateRunStateEventParams{
		ProjectID:  f.projID,
		RunID:      run.ID,
		FromStatus: pgtype.Text{String: "queued", Valid: true},
		ToStatus:   "running",
		Message:    pgtype.Text{String: "started", Valid: true},
		Attributes: []byte(`{"source":"test"}`),
	}); err != nil {
		t.Fatalf("CreateRunStateEvent: %v", err)
	}

	events, err := q.ListRunStateEventsByRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListRunStateEventsByRun: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].ToStatus != "running" {
		t.Fatalf("ToStatus = %q, want running", events[0].ToStatus)
	}

	active, err := q.ListActiveRunsByProject(ctx, ListActiveRunsByProjectParams{
		ProjectID: f.projID,
		Limit:     50,
		Offset:    0,
	})
	if err != nil {
		t.Fatalf("ListActiveRunsByProject: %v", err)
	}
	if len(active) != 1 {
		t.Fatalf("got %d active runs, want 1", len(active))
	}
}

// ============================================================
// Runtime run lifecycle store tests
// ============================================================

func TestRuntimeRunLifecycleStore(t *testing.T) {
	dbURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}
	requireTestDatabase(t, dbURL)

	ctx := context.Background()
	pool, err := Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()
	dropAllTables(t, pool)
	runMigration(t, pool)

	q := New(pool)
	f := setupMinimal(t, pool)

	// StartRuntimeRun
	started, err := q.StartRuntimeRun(ctx, StartRuntimeRunParams{
		ProjectID:    f.projID,
		Status:       "running",
		AgentName:    "test-agent",
		InputSummary: pgtype.Text{String: "hello world", Valid: true},
		Metadata:     []byte("{}"),
	})
	if err != nil {
		t.Fatalf("StartRuntimeRun: %v", err)
	}
	if !started.ID.Valid {
		t.Fatal("started run ID not set")
	}
	if started.Status != "running" {
		t.Fatalf("started status = %q, want running", started.Status)
	}
	if !started.InputSummary.Valid || started.InputSummary.String != "hello world" {
		t.Fatalf("input_summary = %q, want 'hello world'", started.InputSummary.String)
	}

	// CompleteRuntimeRun
	completed, err := q.CompleteRuntimeRun(ctx, CompleteRuntimeRunParams{
		ID:            started.ID,
		ProjectID:     f.projID,
		OutputSummary: pgtype.Text{String: "completed output", Valid: true},
		LatencyMs:     pgtype.Int8{Int64: 1234, Valid: true},
	})
	if err != nil {
		t.Fatalf("CompleteRuntimeRun: %v", err)
	}
	if completed.Status != "completed" {
		t.Fatalf("completed status = %q, want completed", completed.Status)
	}
	if !completed.OutputSummary.Valid || completed.OutputSummary.String != "completed output" {
		t.Fatalf("output_summary = %q", completed.OutputSummary.String)
	}
	if completed.LatencyMs.Int64 != 1234 {
		t.Fatalf("latency_ms = %d, want 1234", completed.LatencyMs.Int64)
	}
	if !completed.EndedAt.Valid {
		t.Fatal("ended_at not set after complete")
	}

	// FailRuntimeRun
	run2, err := q.StartRuntimeRun(ctx, StartRuntimeRunParams{
		ProjectID: f.projID,
		Status:    "running",
		AgentName: "fail-agent",
		Metadata:  []byte("{}"),
	})
	if err != nil {
		t.Fatalf("StartRuntimeRun (fail test): %v", err)
	}

	failed, err := q.FailRuntimeRun(ctx, FailRuntimeRunParams{
		ID:        run2.ID,
		ProjectID: f.projID,
		Error:     pgtype.Text{String: "oops", Valid: true},
		LatencyMs: pgtype.Int8{Int64: 500, Valid: true},
	})
	if err != nil {
		t.Fatalf("FailRuntimeRun: %v", err)
	}
	if failed.Status != "failed" {
		t.Fatalf("failed status = %q, want failed", failed.Status)
	}
	if !failed.Error.Valid || failed.Error.String != "oops" {
		t.Fatalf("error = %q", failed.Error.String)
	}
	if failed.LatencyMs.Int64 != 500 {
		t.Fatalf("latency_ms = %d, want 500", failed.LatencyMs.Int64)
	}
	if !failed.EndedAt.Valid {
		t.Fatal("ended_at not set after fail")
	}
}

// ============================================================
// Multi-dimensional eval score store tests
// ============================================================

func TestCreateAndListMetricDefinitions(t *testing.T) {
	dbURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}
	requireTestDatabase(t, dbURL)

	ctx := context.Background()
	pool, err := Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()
	dropAllTables(t, pool)
	runMigration(t, pool)
	// Run the new migration too
	if err := runMigration2(t, pool); err != nil {
		t.Fatalf("runMigration2: %v", err)
	}

	q := New(pool)
	f := setupMinimal(t, pool)

	t.Run("create_and_list", func(t *testing.T) {
		m1, err := q.CreateMetricDefinition(ctx, CreateMetricDefinitionParams{
			ProjectID: f.projID,
			Name:      "final-answer-quality",
			Kind:      "final_answer",
			Config:    []byte(`{"type":"exact"}`),
		})
		if err != nil {
			t.Fatalf("CreateMetricDefinition: %v", err)
		}
		if !m1.ID.Valid {
			t.Fatal("metric definition ID not set")
		}
		if m1.Kind != "final_answer" {
			t.Errorf("kind = %q, want final_answer", m1.Kind)
		}

		m2, err := q.CreateMetricDefinition(ctx, CreateMetricDefinitionParams{
			ProjectID: f.projID,
			Name:      "tool-correctness",
			Kind:      "tool_correctness",
			Config:    []byte(`{"type":"contains","value":"success"}`),
		})
		if err != nil {
			t.Fatalf("CreateMetricDefinition (2): %v", err)
		}
		_ = m2

		list, err := q.ListMetricDefinitionsByProject(ctx, f.projID)
		if err != nil {
			t.Fatalf("ListMetricDefinitionsByProject: %v", err)
		}
		if len(list) != 2 {
			t.Fatalf("got %d definitions, want 2", len(list))
		}
	})

	t.Run("project_isolation", func(t *testing.T) {
		proj2, err := q.CreateProject(ctx, CreateProjectParams{
			Name: "Isolated Project",
		})
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}

		list, err := q.ListMetricDefinitionsByProject(ctx, proj2.ID)
		if err != nil {
			t.Fatalf("ListMetricDefinitionsByProject: %v", err)
		}
		if len(list) != 0 {
			t.Errorf("expected 0 definitions in isolated project, got %d", len(list))
		}
	})
}

func TestCreateAndListEvalScores(t *testing.T) {
	dbURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}
	requireTestDatabase(t, dbURL)

	ctx := context.Background()
	pool, err := Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()
	dropAllTables(t, pool)
	runMigration(t, pool)
	if err := runMigration2(t, pool); err != nil {
		t.Fatalf("runMigration2: %v", err)
	}

	q := New(pool)
	f := setupMinimal(t, pool)

	// Create a run for scores to reference
	run, err := q.CreateRun(ctx, CreateRunParams{
		ProjectID:    f.projID,
		Source:       "eval",
		Status:       "completed",
		AgentName:    "test-agent",
		InputSummary: pgtype.Text{String: "test input", Valid: true},
		Metadata:     []byte("{}"),
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	t.Run("create_scores", func(t *testing.T) {
		s1, err := q.CreateEvalScore(ctx, CreateEvalScoreParams{
			ProjectID:  f.projID,
			RunID:      run.ID,
			TraceID:    "trace-123",
			SpanID:     "",
			MetricName: "final-answer-quality",
			MetricKind: "final_answer",
			Passed:     true,
			Reason:     "Exact match",
			Details:    []byte(`{"type":"exact"}`),
		})
		if err != nil {
			t.Fatalf("CreateEvalScore: %v", err)
		}
		if !s1.ID.Valid {
			t.Fatal("score ID not set")
		}
		if s1.Passed != true {
			t.Errorf("passed = %v, want true", s1.Passed)
		}
		if s1.MetricKind != "final_answer" {
			t.Errorf("metric_kind = %q, want final_answer", s1.MetricKind)
		}
	})

	t.Run("list_by_run", func(t *testing.T) {
		// Set a valid score value using numeric
		var numericScore pgtype.Numeric
		if err := numericScore.Scan("0.95"); err != nil {
			t.Fatalf("scan numeric: %v", err)
		}

		_, err := q.CreateEvalScore(ctx, CreateEvalScoreParams{
			ProjectID:   f.projID,
			RunID:       run.ID,
			TraceID:     "trace-456",
			SpanID:      "span-001",
			MetricName:  "tool-correctness",
			MetricKind:  "tool_correctness",
			Score:       numericScore,
			Passed:      false,
			Reason:      "Tool returned error",
			Details:     []byte(`{}`),
		})
		if err != nil {
			t.Fatalf("CreateEvalScore (2): %v", err)
		}

		scores, err := q.ListEvalScoresByRun(ctx, ListEvalScoresByRunParams{
			RunID:     run.ID,
			ProjectID: f.projID,
		})
		if err != nil {
			t.Fatalf("ListEvalScoresByRun: %v", err)
		}
		if len(scores) != 2 {
			t.Fatalf("got %d scores, want 2", len(scores))
		}

		// Find the tool_correctness score
		var toolScore EvalScore
		for _, s := range scores {
			if s.MetricKind == "tool_correctness" {
				toolScore = s
				break
			}
		}
		if toolScore.MetricKind == "" {
			t.Fatal("tool_correctness score not found")
		}
		if toolScore.Score.Valid {
			fv, _ := toolScore.Score.Float64Value()
			if fv.Float64 != 0.95 {
				t.Errorf("score = %f, want 0.95", fv.Float64)
			}
		}
		if toolScore.Passed != false {
			t.Errorf("passed = %v, want false", toolScore.Passed)
		}
		if toolScore.Reason != "Tool returned error" {
			t.Errorf("reason = %q", toolScore.Reason)
		}
	})

	t.Run("project_isolation", func(t *testing.T) {
		proj2, err := q.CreateProject(ctx, CreateProjectParams{
			Name: "Isolated Project For Scores",
		})
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}

		scores, err := q.ListEvalScoresByProject(ctx, proj2.ID)
		if err != nil {
			t.Fatalf("ListEvalScoresByProject: %v", err)
		}
		if len(scores) != 0 {
			t.Errorf("expected 0 in isolated project, got %d", len(scores))
		}
	})

	t.Run("count_by_run", func(t *testing.T) {
		count, err := q.CountEvalScoresByRun(ctx, run.ID)
		if err != nil {
			t.Fatalf("CountEvalScoresByRun: %v", err)
		}
		if count != 2 {
			t.Errorf("count = %d, want 2", count)
		}
	})
}

func TestCreateAnnotationItem_Store(t *testing.T) {
	dbURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}
	requireTestDatabase(t, dbURL)

	ctx := context.Background()
	pool, err := Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer pool.Close()
	dropAllTables(t, pool)
	runMigration(t, pool)
	if err := runMigration2(t, pool); err != nil {
		t.Fatalf("runMigration2: %v", err)
	}
	if err := runMigration3(t, pool); err != nil {
		t.Fatalf("runMigration3: %v", err)
	}

	q := New(pool)
	f := setupMinimal(t, pool)

	// Create a run in the same project
	run, err := q.CreateRun(ctx, CreateRunParams{
		ProjectID:    f.projID,
		Source:       "live",
		Status:       "completed",
		AgentName:    "test-agent",
		InputSummary: pgtype.Text{String: "test input", Valid: true},
		Metadata:     []byte("{}"),
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	t.Run("success_with_project_id", func(t *testing.T) {
		queue, err := q.CreateAnnotationQueue(ctx, CreateAnnotationQueueParams{
			ProjectID: f.projID,
			Name:      "test-queue",
		})
		if err != nil {
			t.Fatalf("CreateAnnotationQueue: %v", err)
		}

		item, err := q.CreateAnnotationItem(ctx, CreateAnnotationItemParams{
			ID:      queue.ID,
			RunID:   run.ID,
			TraceID: "trace-1",
			Status:  "pending",
			Priority: 1,
			Scores:  []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAnnotationItem: %v", err)
		}
		if !item.ProjectID.Valid {
			t.Fatal("ProjectID not set")
		}
		if item.ProjectID.Bytes != f.projID.Bytes {
			t.Errorf("ProjectID = %x, want %x", item.ProjectID.Bytes, f.projID.Bytes)
		}
		if !item.RunID.Valid || item.RunID.Bytes != run.ID.Bytes {
			t.Errorf("RunID mismatch")
		}
		if item.Status != "pending" {
			t.Errorf("status = %q, want pending", item.Status)
		}
	})

	t.Run("cross_project_fails", func(t *testing.T) {
		// Create a second project
		proj2, err := q.CreateProject(ctx, CreateProjectParams{
			Name: "Other Project",
		})
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}

		// Create queue in project 2
		queue2, err := q.CreateAnnotationQueue(ctx, CreateAnnotationQueueParams{
			ProjectID: proj2.ID,
			Name:      "other-queue",
		})
		if err != nil {
			t.Fatalf("CreateAnnotationQueue: %v", err)
		}

		// Run is in project 1, queue is in project 2 → should fail
		_, err = q.CreateAnnotationItem(ctx, CreateAnnotationItemParams{
			ID:      queue2.ID,
			RunID:   run.ID,
			TraceID: "trace-cross",
			Status:  "pending",
			Priority: 5,
			Scores:  []byte("{}"),
		})
		if err == nil {
			t.Fatal("expected error for cross-project queue/run, got nil")
		}
	})
}
func runMigration2(t *testing.T, pool *pgxpool.Pool) error {
	t.Helper()
	b, err := os.ReadFile("../../migrations/00002_eval_metrics.sql")
	if err != nil {
		return err
	}
	_, err = pool.Exec(context.Background(), string(b))
	return err
}

func runMigration3(t *testing.T, pool *pgxpool.Pool) error {
	t.Helper()
	b, err := os.ReadFile("../../migrations/00003_online_eval.sql")
	if err != nil {
		return err
	}
	_, err = pool.Exec(context.Background(), string(b))
	return err
}
