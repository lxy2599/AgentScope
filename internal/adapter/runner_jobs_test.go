package adapter

import (
	"context"
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

func TestSubmitResultByProjectAggregatesEvaluationOutcome(t *testing.T) {
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
	datasetID := uuid.NewString()
	caseID := uuid.NewString()
	experimentID := uuid.NewString()
	jobID := uuid.NewString()

	registerAdapterFixtureCleanup(t, pool, []string{projectID}, nil)

	if _, err := pool.Exec(ctx, `
		INSERT INTO projects (id, name)
		VALUES ($1, 'runner-result-aggregation')
	`, projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO datasets (id, project_id, name)
		VALUES ($1, $2, 'runner-result-aggregation-dataset')
	`, datasetID, projectID); err != nil {
		t.Fatalf("create dataset: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO dataset_cases (id, dataset_id, input, expected)
		VALUES ($1, $2, '"question"'::jsonb, '"expected"'::jsonb)
	`, caseID, datasetID); err != nil {
		t.Fatalf("create dataset case: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO experiments (
			id, project_id, dataset_id, target_name, status, total_cases
		)
		VALUES ($1, $2, $3, 'runner-result-aggregation-target', 'running', 1)
	`, experimentID, projectID, datasetID); err != nil {
		t.Fatalf("create experiment: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO eval_jobs (
			id, experiment_id, dataset_case_id, status, runner_name,
			lease_expires_at
		)
		VALUES ($1, $2, $3, 'running', 'runner-result-aggregation', $4)
	`, jobID, experimentID, caseID, time.Now().Add(time.Minute)); err != nil {
		t.Fatalf("create eval job: %v", err)
	}

	q := NewRunnerQuerier(store.New(pool), pool)
	if err := q.SubmitResultByProject(
		ctx,
		projectID,
		"runner-result-aggregation",
		jobID,
		runnerapi.SubmitResultRequest{
			Score:        0,
			Passed:       false,
			Reason:       "exact mismatch",
			ActualOutput: "wrong answer",
		},
	); err != nil {
		t.Fatalf("SubmitResultByProject: %v", err)
	}

	var jobStatus string
	if err := pool.QueryRow(ctx, `
		SELECT status FROM eval_jobs WHERE id = $1
	`, jobID).Scan(&jobStatus); err != nil {
		t.Fatalf("read eval job: %v", err)
	}
	if jobStatus != "succeeded" {
		t.Errorf("eval job status = %q, want succeeded", jobStatus)
	}

	var resultPassed bool
	var resultScore float64
	if err := pool.QueryRow(ctx, `
		SELECT passed, score::float8
		FROM eval_case_results
		WHERE eval_job_id = $1
	`, jobID).Scan(&resultPassed, &resultScore); err != nil {
		t.Fatalf("read eval result: %v", err)
	}
	if resultPassed {
		t.Error("eval result passed = true, want false")
	}
	if resultScore != 0 {
		t.Errorf("eval result score = %v, want 0", resultScore)
	}

	var experimentStatus string
	var passedCases, failedCases int
	var aggregateScore *float64
	if err := pool.QueryRow(ctx, `
		SELECT status, passed_cases, failed_cases, aggregate_score::float8
		FROM experiments
		WHERE id = $1
	`, experimentID).Scan(
		&experimentStatus,
		&passedCases,
		&failedCases,
		&aggregateScore,
	); err != nil {
		t.Fatalf("read experiment: %v", err)
	}
	if experimentStatus != "completed" || passedCases != 0 || failedCases != 1 {
		t.Errorf(
			"experiment state = (%q, passed=%d, failed=%d), want (completed, 0, 1)",
			experimentStatus,
			passedCases,
			failedCases,
		)
	}
	if aggregateScore == nil {
		t.Fatal("experiment aggregate_score = NULL, want 0")
	}
	if *aggregateScore != 0 {
		t.Errorf("experiment aggregate_score = %v, want 0", *aggregateScore)
	}

	readQ := NewExperimentQuerier(store.New(pool), pool)
	results, err := readQ.ListEvalResults(ctx, experimentID)
	if err != nil {
		t.Fatalf("ListEvalResults: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("ListEvalResults returned %d rows, want 1", len(results))
	}
	if results[0].CaseInput != `"question"` || results[0].CaseExpected != `"expected"` {
		t.Errorf(
			"human-readable case = (%q, %q), want JSON strings for question and expected",
			results[0].CaseInput,
			results[0].CaseExpected,
		)
	}
}

func TestSubmitResultByProjectSerializesConcurrentExperimentSummaries(t *testing.T) {
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
	datasetID := uuid.NewString()
	experimentID := uuid.NewString()
	caseIDs := []string{uuid.NewString(), uuid.NewString()}
	jobIDs := []string{uuid.NewString(), uuid.NewString()}
	runnerNames := []string{"concurrent-runner-pass", "concurrent-runner-fail"}

	registerAdapterFixtureCleanup(t, pool, []string{projectID}, nil)

	if _, err := pool.Exec(ctx, `
		INSERT INTO projects (id, name)
		VALUES ($1, 'runner-concurrent-summary')
	`, projectID); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO datasets (id, project_id, name)
		VALUES ($1, $2, 'runner-concurrent-summary-dataset')
	`, datasetID, projectID); err != nil {
		t.Fatalf("create dataset: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO dataset_cases (id, dataset_id, input, expected)
		VALUES
			($1, $3, '"question-1"'::jsonb, '"expected-1"'::jsonb),
			($2, $3, '"question-2"'::jsonb, '"expected-2"'::jsonb)
	`, caseIDs[0], caseIDs[1], datasetID); err != nil {
		t.Fatalf("create dataset cases: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO experiments (
			id, project_id, dataset_id, target_name, status, total_cases
		)
		VALUES ($1, $2, $3, 'runner-concurrent-summary-target', 'running', 2)
	`, experimentID, projectID, datasetID); err != nil {
		t.Fatalf("create experiment: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO eval_jobs (
			id, experiment_id, dataset_case_id, status, runner_name,
			lease_expires_at
		)
		VALUES
			($1, $3, $4, 'running', $6, $8),
			($2, $3, $5, 'running', $7, $8)
	`,
		jobIDs[0], jobIDs[1], experimentID,
		caseIDs[0], caseIDs[1], runnerNames[0], runnerNames[1],
		time.Now().Add(time.Minute),
	); err != nil {
		t.Fatalf("create eval jobs: %v", err)
	}

	q := NewRunnerQuerier(store.New(pool), pool)
	errCh := make(chan error, 2)
	go func() {
		errCh <- q.SubmitResultByProject(
			ctx,
			projectID,
			runnerNames[0],
			jobIDs[0],
			runnerapi.SubmitResultRequest{
				Score:        1,
				Passed:       true,
				Reason:       "exact match",
				ActualOutput: "expected-1",
			},
		)
	}()
	go func() {
		errCh <- q.SubmitResultByProject(
			ctx,
			projectID,
			runnerNames[1],
			jobIDs[1],
			runnerapi.SubmitResultRequest{
				Score:        0,
				Passed:       false,
				Reason:       "exact mismatch",
				ActualOutput: "wrong answer",
			},
		)
	}()
	for range 2 {
		if err := <-errCh; err != nil {
			t.Fatalf("SubmitResultByProject: %v", err)
		}
	}

	var status string
	var passedCases, failedCases int
	var aggregateScore float64
	if err := pool.QueryRow(ctx, `
		SELECT status, passed_cases, failed_cases, aggregate_score::float8
		FROM experiments
		WHERE id = $1
	`, experimentID).Scan(
		&status,
		&passedCases,
		&failedCases,
		&aggregateScore,
	); err != nil {
		t.Fatalf("read experiment: %v", err)
	}
	if status != "completed" || passedCases != 1 || failedCases != 1 || aggregateScore != 0.5 {
		t.Errorf(
			"experiment summary = (%q, passed=%d, failed=%d, score=%v), want (completed, 1, 1, 0.5)",
			status,
			passedCases,
			failedCases,
			aggregateScore,
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
