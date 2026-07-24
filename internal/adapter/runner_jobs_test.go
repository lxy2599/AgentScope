package adapter

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"AgentScope/internal/runnerapi"
	"AgentScope/internal/store"
	"github.com/google/uuid"
)

func TestSubmitResultByProjectRejectsForeignRunAtomically(t *testing.T) {
	dbURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}
	requireAdapterTestDatabase(t, dbURL)

	ctx := context.Background()
	pool, err := store.Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	projectID := uuid.NewString()
	foreignProjectID := uuid.NewString()
	datasetID := uuid.NewString()
	caseID := uuid.NewString()
	experimentID := uuid.NewString()
	jobID := uuid.NewString()
	foreignRunID := uuid.NewString()

	// Delete the owning job/result tree before the foreign run it may
	// reference when this regression test is run against vulnerable code.
	registerAdapterFixtureCleanup(t, pool, []string{projectID, foreignProjectID}, nil)

	if _, err := pool.Exec(ctx, `
		INSERT INTO projects (id, name)
		VALUES ($1, 'runner-boundary-owner'), ($2, 'runner-boundary-foreign')
	`, projectID, foreignProjectID); err != nil {
		t.Fatalf("create projects: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO datasets (id, project_id, name)
		VALUES ($1, $2, 'runner-boundary-dataset')
	`, datasetID, projectID); err != nil {
		t.Fatalf("create dataset: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO dataset_cases (id, dataset_id, input)
		VALUES ($1, $2, '{}'::jsonb)
	`, caseID, datasetID); err != nil {
		t.Fatalf("create dataset case: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO experiments (
			id, project_id, dataset_id, target_name, status, total_cases
		)
		VALUES ($1, $2, $3, 'runner-boundary-target', 'running', 1)
	`, experimentID, projectID, datasetID); err != nil {
		t.Fatalf("create experiment: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO eval_jobs (
			id, experiment_id, dataset_case_id, status, runner_name,
			lease_expires_at
		)
		VALUES ($1, $2, $3, 'running', 'runner-boundary', $4)
	`, jobID, experimentID, caseID, time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("create eval job: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO runs (
			id, project_id, source, status, agent_name, started_at
		)
		VALUES ($1, $2, 'eval', 'running', 'foreign-agent', now())
	`, foreignRunID, foreignProjectID); err != nil {
		t.Fatalf("create foreign run: %v", err)
	}

	q := NewRunnerQuerier(store.New(pool), pool)
	err = q.SubmitResultByProject(
		ctx,
		projectID,
		"runner-boundary",
		jobID,
		runnerapi.SubmitResultRequest{
			RunID:        foreignRunID,
			Score:        0.9,
			Passed:       true,
			Reason:       "must be rejected",
			ActualOutput: "foreign output",
			LatencyMs:    123,
		},
	)
	if err == nil {
		t.Error("SubmitResultByProject accepted a run from another project")
	} else if !strings.Contains(err.Error(), "run not found or project mismatch") {
		t.Errorf("SubmitResultByProject error = %q, want non-enumerating project mismatch", err)
	}

	var runStatus string
	var endedAt *time.Time
	var latencyMs *int64
	if err := pool.QueryRow(ctx, `
		SELECT status, ended_at, latency_ms
		FROM runs
		WHERE id = $1
	`, foreignRunID).Scan(&runStatus, &endedAt, &latencyMs); err != nil {
		t.Fatalf("read foreign run: %v", err)
	}
	if runStatus != "running" {
		t.Errorf("foreign run status = %q, want running", runStatus)
	}
	if endedAt != nil {
		t.Errorf("foreign run ended_at = %v, want NULL", endedAt)
	}
	if latencyMs != nil {
		t.Errorf("foreign run latency_ms = %v, want NULL", *latencyMs)
	}

	var resultCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM eval_case_results
		WHERE eval_job_id = $1
	`, jobID).Scan(&resultCount); err != nil {
		t.Fatalf("count eval results: %v", err)
	}
	if resultCount != 0 {
		t.Errorf("eval result count = %d, want 0 after rollback", resultCount)
	}

	var jobStatus string
	if err := pool.QueryRow(ctx, `
		SELECT status
		FROM eval_jobs
		WHERE id = $1
	`, jobID).Scan(&jobStatus); err != nil {
		t.Fatalf("read eval job: %v", err)
	}
	if jobStatus != "running" {
		t.Errorf("eval job status = %q, want running after rollback", jobStatus)
	}

	var experimentStatus string
	var passedCases, failedCases int
	if err := pool.QueryRow(ctx, `
		SELECT status, passed_cases, failed_cases
		FROM experiments
		WHERE id = $1
	`, experimentID).Scan(&experimentStatus, &passedCases, &failedCases); err != nil {
		t.Fatalf("read experiment: %v", err)
	}
	if experimentStatus != "running" || passedCases != 0 || failedCases != 0 {
		t.Errorf(
			"experiment state = (%q, passed=%d, failed=%d), want (running, 0, 0)",
			experimentStatus,
			passedCases,
			failedCases,
		)
	}
}

func TestClaimRejectsForeignEvaluatorConfig(t *testing.T) {
	dbURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}
	requireAdapterTestDatabase(t, dbURL)

	ctx := context.Background()
	pool, err := store.Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	projectID := uuid.NewString()
	foreignProjectID := uuid.NewString()
	datasetID := uuid.NewString()
	caseID := uuid.NewString()
	evaluatorID := uuid.NewString()
	experimentID := uuid.NewString()
	jobID := uuid.NewString()
	const foreignSecret = "foreign-evaluator-secret"

	registerAdapterFixtureCleanup(t, pool, []string{projectID, foreignProjectID}, nil)

	if _, err := pool.Exec(ctx, `
		INSERT INTO projects (id, name)
		VALUES ($1, 'claim-boundary-owner'), ($2, 'claim-boundary-foreign')
	`, projectID, foreignProjectID); err != nil {
		t.Fatalf("create projects: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO datasets (id, project_id, name)
		VALUES ($1, $2, 'claim-boundary-dataset')
	`, datasetID, projectID); err != nil {
		t.Fatalf("create dataset: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO dataset_cases (id, dataset_id, input)
		VALUES ($1, $2, '{}'::jsonb)
	`, caseID, datasetID); err != nil {
		t.Fatalf("create dataset case: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO evaluators (id, project_id, name, type, config)
		VALUES (
			$1, $2, 'claim-boundary-foreign-evaluator', 'rule',
			'{"secret":"foreign-evaluator-secret"}'::jsonb
		)
	`, evaluatorID, foreignProjectID); err != nil {
		t.Fatalf("create foreign evaluator: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO experiments (
			id, project_id, dataset_id, evaluator_id, target_name, status, total_cases
		)
		VALUES ($1, $2, $3, $4, 'claim-boundary-target', 'pending', 1)
	`, experimentID, projectID, datasetID, evaluatorID); err != nil {
		t.Fatalf("create experiment: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO eval_jobs (id, experiment_id, dataset_case_id, status)
		VALUES ($1, $2, $3, 'queued')
	`, jobID, experimentID, caseID); err != nil {
		t.Fatalf("create eval job: %v", err)
	}

	svc := runnerapi.NewService(NewRunnerQuerier(store.New(pool), pool))
	resp, err := svc.Claim(ctx, projectID, "claim-boundary-runner")
	if err == nil {
		t.Fatal("Claim exposed an evaluator config owned by another project")
	}
	if strings.Contains(string(resp.EvalConfig), foreignSecret) {
		t.Errorf("Claim response leaked foreign evaluator config: %s", resp.EvalConfig)
	}
	for _, sensitive := range []string{projectID, foreignProjectID, evaluatorID, foreignSecret} {
		if strings.Contains(err.Error(), sensitive) {
			t.Errorf("Claim error %q leaked sensitive value %q", err, sensitive)
		}
	}
}

func requireAdapterTestDatabase(t *testing.T, databaseURL string) {
	t.Helper()
	u, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse AGENTSCOPE_TEST_DATABASE_URL: %v", err)
	}
	dbName := strings.TrimPrefix(u.Path, "/")
	if !strings.HasSuffix(dbName, "_test") {
		t.Fatalf("AGENTSCOPE_TEST_DATABASE_URL must point to a database ending with _test, got %q", dbName)
	}
}

func TestNormalizeResultDetailsDefaultsEmptyJSON(t *testing.T) {
	got := normalizeResultDetails(nil)
	if string(got) != "{}" {
		t.Fatalf("normalizeResultDetails(nil) = %q, want {}", string(got))
	}

	got = normalizeResultDetails([]byte{})
	if string(got) != "{}" {
		t.Fatalf("normalizeResultDetails(empty) = %q, want {}", string(got))
	}
}

func TestNormalizeResultDetailsKeepsProvidedJSON(t *testing.T) {
	got := normalizeResultDetails([]byte(`{"score":1}`))
	if string(got) != `{"score":1}` {
		t.Fatalf("normalizeResultDetails(provided) = %q", string(got))
	}
}

func TestNumericFromFloat64_zero(t *testing.T) {
	n, err := numericFromFloat64(0.0)
	if err != nil {
		t.Fatalf("numericFromFloat64(0.0) error = %v", err)
	}
	if !n.Valid {
		t.Fatal("numericFromFloat64(0.0) is not Valid")
	}
	f, _ := n.Float64Value()
	if f.Float64 != 0.0 {
		t.Fatalf("numericFromFloat64(0.0) = %v, want 0.0", f.Float64)
	}
}

func TestNumericFromFloat64_one(t *testing.T) {
	n, err := numericFromFloat64(1.0)
	if err != nil {
		t.Fatalf("numericFromFloat64(1.0) error = %v", err)
	}
	if !n.Valid {
		t.Fatal("numericFromFloat64(1.0) is not Valid")
	}
	f, _ := n.Float64Value()
	if f.Float64 != 1.0 {
		t.Fatalf("numericFromFloat64(1.0) = %v, want 1.0", f.Float64)
	}
}

func TestNumericFromFloat64_decimal(t *testing.T) {
	n, err := numericFromFloat64(0.95)
	if err != nil {
		t.Fatalf("numericFromFloat64(0.95) error = %v", err)
	}
	if !n.Valid {
		t.Fatal("numericFromFloat64(0.95) is not Valid")
	}
	f, _ := n.Float64Value()
	if f.Float64 != 0.95 {
		t.Fatalf("numericFromFloat64(0.95) = %v, want 0.95", f.Float64)
	}
}

func TestNumericFromFloat64Ptr_zero_returns_invalid(t *testing.T) {
	n, err := numericFromFloat64Ptr(0.0)
	if err != nil {
		t.Fatalf("numericFromFloat64Ptr(0.0) error = %v", err)
	}
	if n.Valid {
		t.Fatal("numericFromFloat64Ptr(0.0) should be invalid (NULL)")
	}
}

func TestNumericFromFloat64Ptr_nonzero_is_valid(t *testing.T) {
	n, err := numericFromFloat64Ptr(42.5)
	if err != nil {
		t.Fatalf("numericFromFloat64Ptr(42.5) error = %v", err)
	}
	if !n.Valid {
		t.Fatal("numericFromFloat64Ptr(42.5) is not Valid")
	}
	f, _ := n.Float64Value()
	if f.Float64 != 42.5 {
		t.Fatalf("numericFromFloat64Ptr(42.5) = %v, want 42.5", f.Float64)
	}
}
