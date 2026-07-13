package store

import (
	"context"
	"os"
	"testing"
	"time"

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

func now() pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Now(), Valid: true}
}

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

	// === Span ===
	span, err := q.CreateSpan(ctx, CreateSpanParams{
		RunID:      run.ID,
		TraceID:    "abc123",
		SpanID:     "span001",
		Name:       "test-span",
		Kind:       "INTERNAL",
		StatusCode: "UNSET",
		StartTime:  now(),
		Attributes: []byte("{}"),
	})
	if err != nil {
		t.Fatalf("CreateSpan: %v", err)
	}
	if !span.ID.Valid {
		t.Fatal("span.ID not set")
	}
	if span.SpanID != "span001" {
		t.Errorf("got SpanID=%q", span.SpanID)
	}

	// === GetSpanByID ===
	gotSpan, err := q.GetSpanByID(ctx, span.ID)
	if err != nil {
		t.Fatalf("GetSpanByID: %v", err)
	}
	if gotSpan.SpanID != "span001" {
		t.Errorf("got SpanID=%q", gotSpan.SpanID)
	}

	// === ListSpansByRunID ===
	spans, err := q.ListSpansByRunID(ctx, run.ID)
	if err != nil {
		t.Fatalf("ListSpansByRunID: %v", err)
	}
	if len(spans) != 1 {
		t.Fatalf("got %d spans", len(spans))
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

	// === Claim eval job ===
	claimed, err := q.ClaimEvalJob(ctx, ClaimEvalJobParams{
		ID:              job.ID,
		RunnerName:      pgtype.Text{String: "test-runner", Valid: true},
		LeaseExpiresAt:  pgtype.Timestamptz{Valid: false},
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
}

func dropAllTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	stmt := `DROP TABLE IF EXISTS
		eval_case_results,
		eval_jobs,
		experiments,
		evaluators,
		dataset_cases,
		datasets,
		span_events,
		spans,
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
