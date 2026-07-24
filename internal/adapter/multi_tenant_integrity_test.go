package adapter

import (
	"context"
	"errors"
	"os"
	"testing"

	"AgentScope/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMultiResourceWritesRejectCrossProjectReferences(t *testing.T) {
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

	projectA := uuid.NewString()
	projectB := uuid.NewString()
	userID := uuid.NewString()
	datasetA := uuid.NewString()
	datasetB := uuid.NewString()
	caseA := uuid.NewString()
	caseB := uuid.NewString()
	evaluatorA := uuid.NewString()
	evaluatorB := uuid.NewString()
	baselineExperiment := uuid.NewString()
	candidateExperiment := uuid.NewString()
	foreignExperiment := uuid.NewString()
	baselineJob := uuid.NewString()
	candidateJob := uuid.NewString()
	runA := uuid.NewString()
	candidateRunA := uuid.NewString()
	runB := uuid.NewString()
	agentA := uuid.NewString()
	agentB := uuid.NewString()
	instanceA := uuid.NewString()
	instanceB := uuid.NewString()

	registerAdapterFixtureCleanup(t, pool, []string{projectA, projectB}, []string{userID})

	mustExecTenantFixture(t, pool, `
		INSERT INTO projects (id, name)
		VALUES ($1, 'tenant-integrity-a'), ($2, 'tenant-integrity-b')
	`, projectA, projectB)
	mustExecTenantFixture(t, pool, `
		INSERT INTO users (id, email, password_hash, display_name)
		VALUES ($1, $2, 'hash', 'Tenant Integrity')
	`, userID, "tenant-integrity-"+uuid.NewString()+"@example.invalid")
	mustExecTenantFixture(t, pool, `
		INSERT INTO datasets (id, project_id, name)
		VALUES
			($1, $2, 'tenant-integrity-dataset-a'),
			($3, $4, 'tenant-integrity-dataset-b')
	`, datasetA, projectA, datasetB, projectB)
	mustExecTenantFixture(t, pool, `
		INSERT INTO dataset_cases (id, dataset_id, input)
		VALUES ($1, $2, '{}'::jsonb), ($3, $4, '{}'::jsonb)
	`, caseA, datasetA, caseB, datasetB)
	mustExecTenantFixture(t, pool, `
		INSERT INTO evaluators (id, project_id, name, type)
		VALUES
			($1, $2, 'tenant-integrity-evaluator-a', 'rule'),
			($3, $4, 'tenant-integrity-evaluator-b', 'rule')
	`, evaluatorA, projectA, evaluatorB, projectB)
	mustExecTenantFixture(t, pool, `
		INSERT INTO experiments (
			id, project_id, dataset_id, evaluator_id, target_name, status, total_cases
		)
		VALUES
			($1, $2, $3, $4, 'tenant-integrity-baseline', 'completed', 1),
			($5, $2, $3, $4, 'tenant-integrity-candidate', 'completed', 1),
			($6, $7, $8, $9, 'tenant-integrity-foreign', 'completed', 1)
	`, baselineExperiment, projectA, datasetA, evaluatorA,
		candidateExperiment, foreignExperiment, projectB, datasetB, evaluatorB)
	mustExecTenantFixture(t, pool, `
		INSERT INTO eval_jobs (id, experiment_id, dataset_case_id, status)
		VALUES
			($1, $2, $3, 'succeeded'),
			($4, $5, $3, 'succeeded')
	`, baselineJob, baselineExperiment, caseA, candidateJob, candidateExperiment)
	mustExecTenantFixture(t, pool, `
		INSERT INTO runs (id, project_id, source, status, agent_name)
		VALUES
			($1, $2, 'eval', 'completed', 'tenant-integrity-agent-a'),
			($3, $2, 'eval', 'completed', 'tenant-integrity-candidate-a'),
			($4, $5, 'eval', 'completed', 'tenant-integrity-agent-b')
	`, runA, projectA, candidateRunA, runB, projectB)
	mustExecTenantFixture(t, pool, `
		INSERT INTO eval_case_results (eval_job_id, run_id, details)
		VALUES ($1, $2, '{}'::jsonb), ($3, $4, '{}'::jsonb)
	`, baselineJob, runA, candidateJob, candidateRunA)
	mustExecTenantFixture(t, pool, `
		INSERT INTO agents (id, project_id, name)
		VALUES
			($1, $2, 'tenant-integrity-agent-a'),
			($3, $4, 'tenant-integrity-agent-b')
	`, agentA, projectA, agentB, projectB)
	mustExecTenantFixture(t, pool, `
		INSERT INTO agent_instances (id, agent_id, project_id, instance_id)
		VALUES
			($1, $2, $3, 'tenant-integrity-instance-a'),
			($4, $5, $6, 'tenant-integrity-instance-b')
	`, instanceA, agentA, projectA, instanceB, agentB, projectB)

	q := store.New(pool)

	t.Run("experiment dataset and evaluator", func(t *testing.T) {
		baseParams := store.CreateExperimentParams{
			ProjectID:   parsePGUUID(projectA),
			DatasetID:   parsePGUUID(datasetA),
			EvaluatorID: parsePGUUID(evaluatorA),
			TargetName:  "tenant-integrity-valid-experiment",
			Status:      "pending",
			Metadata:    []byte("{}"),
		}

		foreignDataset := baseParams
		foreignDataset.DatasetID = parsePGUUID(datasetB)
		requireTenantWriteRejected(t, func() error {
			_, err := q.CreateExperiment(ctx, foreignDataset)
			return err
		})

		foreignEvaluator := baseParams
		foreignEvaluator.EvaluatorID = parsePGUUID(evaluatorB)
		requireTenantWriteRejected(t, func() error {
			_, err := q.CreateExperiment(ctx, foreignEvaluator)
			return err
		})

		if _, err := q.CreateExperiment(ctx, baseParams); err != nil {
			t.Fatalf("same-project CreateExperiment() error = %v", err)
		}
	})

	t.Run("eval job dataset case", func(t *testing.T) {
		requireTenantWriteRejected(t, func() error {
			_, err := q.CreateEvalJob(ctx, store.CreateEvalJobParams{
				ExperimentID:  parsePGUUID(baselineExperiment),
				DatasetCaseID: parsePGUUID(caseB),
				Status:        "queued",
			})
			return err
		})

		if _, err := q.CreateEvalJob(ctx, store.CreateEvalJobParams{
			ExperimentID:  parsePGUUID(baselineExperiment),
			DatasetCaseID: parsePGUUID(caseA),
			Status:        "queued",
		}); err != nil {
			t.Fatalf("same-dataset CreateEvalJob() error = %v", err)
		}
	})

	t.Run("eval case result run", func(t *testing.T) {
		requireTenantWriteRejected(t, func() error {
			_, err := q.CreateEvalCaseResult(ctx, store.CreateEvalCaseResultParams{
				EvalJobID: parsePGUUID(baselineJob),
				RunID:     parsePGUUID(runB),
				Details:   []byte("{}"),
			})
			return err
		})

		if _, err := q.CreateEvalCaseResult(ctx, store.CreateEvalCaseResultParams{
			EvalJobID: parsePGUUID(baselineJob),
			RunID:     parsePGUUID(runA),
			Details:   []byte("{}"),
		}); err != nil {
			t.Fatalf("same-project CreateEvalCaseResult() error = %v", err)
		}
	})

	var comparisonID pgtype.UUID
	t.Run("comparison experiments", func(t *testing.T) {
		params := store.CreateExperimentComparisonParams{
			ProjectID:             parsePGUUID(projectA),
			BaselineExperimentID:  parsePGUUID(baselineExperiment),
			CandidateExperimentID: parsePGUUID(foreignExperiment),
			ComparisonType:        "general",
			CreatedBy:             parsePGUUID(userID),
			Status:                "completed",
			Summary:               []byte("{}"),
			RegressionGates:       []byte("{}"),
		}
		requireTenantWriteRejected(t, func() error {
			_, err := q.CreateExperimentComparison(ctx, params)
			return err
		})

		params.CandidateExperimentID = parsePGUUID(candidateExperiment)
		comparison, err := q.CreateExperimentComparison(ctx, params)
		if err != nil {
			t.Fatalf("same-project CreateExperimentComparison() error = %v", err)
		}
		comparisonID = comparison.ID
	})

	t.Run("comparison case and runs", func(t *testing.T) {
		baseParams := store.CreateComparisonCaseResultParams{
			ComparisonID:   comparisonID,
			DatasetCaseID:  parsePGUUID(caseA),
			BaselineRunID:  parsePGUUID(runA),
			CandidateRunID: parsePGUUID(candidateRunA),
			Status:         "unchanged",
			Details:        []byte("{}"),
		}

		foreignCase := baseParams
		foreignCase.DatasetCaseID = parsePGUUID(caseB)
		requireTenantWriteRejected(t, func() error {
			_, err := q.CreateComparisonCaseResult(ctx, foreignCase)
			return err
		})

		foreignBaselineRun := baseParams
		foreignBaselineRun.BaselineRunID = parsePGUUID(runB)
		requireTenantWriteRejected(t, func() error {
			_, err := q.CreateComparisonCaseResult(ctx, foreignBaselineRun)
			return err
		})

		foreignCandidateRun := baseParams
		foreignCandidateRun.CandidateRunID = parsePGUUID(runB)
		requireTenantWriteRejected(t, func() error {
			_, err := q.CreateComparisonCaseResult(ctx, foreignCandidateRun)
			return err
		})

		if _, err := q.CreateComparisonCaseResult(ctx, baseParams); err != nil {
			t.Fatalf("matching CreateComparisonCaseResult() error = %v", err)
		}
	})

	t.Run("sampling rule agent", func(t *testing.T) {
		params := store.CreateTraceSamplingRuleParams{
			ProjectID:  parsePGUUID(projectA),
			AgentID:    parsePGUUID(agentB),
			Name:       "tenant-integrity-cross-agent",
			Enabled:    true,
			SampleRate: 1,
			Filters:    []byte("{}"),
		}
		requireTenantWriteRejected(t, func() error {
			_, err := q.CreateTraceSamplingRule(ctx, params)
			return err
		})

		params.AgentID = parsePGUUID(agentA)
		params.Name = "tenant-integrity-same-agent"
		if _, err := q.CreateTraceSamplingRule(ctx, params); err != nil {
			t.Fatalf("same-project CreateTraceSamplingRule() error = %v", err)
		}
	})

	t.Run("agent instance and heartbeat", func(t *testing.T) {
		instanceParams := store.CreateAgentInstanceParams{
			AgentID:      parsePGUUID(agentB),
			ProjectID:    parsePGUUID(projectA),
			InstanceID:   "tenant-integrity-cross-instance",
			InstanceName: "cross",
			Status:       "online",
			Metadata:     []byte("{}"),
		}
		requireTenantWriteRejected(t, func() error {
			_, err := q.CreateAgentInstance(ctx, instanceParams)
			return err
		})
		requireTenantWriteRejected(t, func() error {
			_, err := q.UpsertAgentInstance(ctx, store.UpsertAgentInstanceParams(instanceParams))
			return err
		})
		requireTenantWriteRejected(t, func() error {
			_, err := q.CreateAgentHeartbeat(ctx, store.CreateAgentHeartbeatParams{
				AgentID:    parsePGUUID(agentB),
				InstanceID: parsePGUUID(instanceA),
				Status:     "online",
				Payload:    []byte("{}"),
			})
			return err
		})

		if _, err := q.CreateAgentHeartbeat(ctx, store.CreateAgentHeartbeatParams{
			AgentID:    parsePGUUID(agentA),
			InstanceID: parsePGUUID(instanceA),
			Status:     "online",
			Payload:    []byte("{}"),
		}); err != nil {
			t.Fatalf("matching CreateAgentHeartbeat() error = %v", err)
		}
	})

	t.Run("agent instance active run", func(t *testing.T) {
		requireTenantWriteRejected(t, func() error {
			_, err := q.SetAgentInstanceActiveRunByExternalID(ctx, store.SetAgentInstanceActiveRunByExternalIDParams{
				ProjectID:   parsePGUUID(projectA),
				AgentID:     parsePGUUID(agentA),
				InstanceID:  "tenant-integrity-instance-a",
				ActiveRunID: parsePGUUID(runB),
			})
			return err
		})

		if _, err := q.SetAgentInstanceActiveRunByExternalID(ctx, store.SetAgentInstanceActiveRunByExternalIDParams{
			ProjectID:   parsePGUUID(projectA),
			AgentID:     parsePGUUID(agentA),
			InstanceID:  "tenant-integrity-instance-a",
			ActiveRunID: parsePGUUID(runA),
		}); err != nil {
			t.Fatalf("same-project SetAgentInstanceActiveRunByExternalID() error = %v", err)
		}

		if err := q.SetAgentInstanceActiveRun(ctx, store.SetAgentInstanceActiveRunParams{
			ID:          parsePGUUID(instanceA),
			ProjectID:   parsePGUUID(projectA),
			ActiveRunID: parsePGUUID(runB),
		}); err != nil {
			t.Fatalf("guarded SetAgentInstanceActiveRun() error = %v", err)
		}
		var persistedRun pgtype.UUID
		if err := pool.QueryRow(ctx,
			"SELECT active_run_id FROM agent_instances WHERE id = $1",
			instanceA,
		).Scan(&persistedRun); err != nil {
			t.Fatalf("read active run: %v", err)
		}
		if persistedRun.Bytes != parsePGUUID(runA).Bytes {
			t.Fatalf("foreign run replaced active_run_id: got %s, want %s",
				UUIDToString(persistedRun.Bytes), runA)
		}
	})

	t.Run("run state event references", func(t *testing.T) {
		baseParams := store.CreateRunStateEventParams{
			ProjectID:       parsePGUUID(projectA),
			RunID:           parsePGUUID(runA),
			AgentID:         parsePGUUID(agentA),
			AgentInstanceID: parsePGUUID(instanceA),
			ToStatus:        "running",
			Attributes:      []byte("{}"),
		}

		foreignRun := baseParams
		foreignRun.RunID = parsePGUUID(runB)
		requireTenantWriteRejected(t, func() error {
			_, err := q.CreateRunStateEvent(ctx, foreignRun)
			return err
		})

		foreignAgent := baseParams
		foreignAgent.AgentID = parsePGUUID(agentB)
		foreignAgent.AgentInstanceID = pgtype.UUID{}
		requireTenantWriteRejected(t, func() error {
			_, err := q.CreateRunStateEvent(ctx, foreignAgent)
			return err
		})

		foreignInstance := baseParams
		foreignInstance.AgentID = pgtype.UUID{}
		foreignInstance.AgentInstanceID = parsePGUUID(instanceB)
		requireTenantWriteRejected(t, func() error {
			_, err := q.CreateRunStateEvent(ctx, foreignInstance)
			return err
		})

		if _, err := q.CreateRunStateEvent(ctx, baseParams); err != nil {
			t.Fatalf("matching CreateRunStateEvent() error = %v", err)
		}
	})
}

func mustExecTenantFixture(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("create tenant-integrity fixture: %v", err)
	}
}

func requireTenantWriteRejected(t *testing.T, write func() error) {
	t.Helper()
	if err := write(); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-project write error = %v, want pgx.ErrNoRows", err)
	}
}
