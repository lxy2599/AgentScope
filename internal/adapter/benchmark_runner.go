package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"AgentScope/internal/benchmarks"
	"AgentScope/internal/httpapi"
	"AgentScope/internal/runnerapi"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BenchmarkRunnerService extends the existing runner endpoint with explicit
// execution-kind claims. It shares eval_jobs and its lease semantics with the
// legacy function runner; no second queue or runner API is introduced.
type BenchmarkRunnerService struct {
	q    *store.Queries
	pool *pgxpool.Pool
}

func NewBenchmarkRunnerService(q *store.Queries, pool *pgxpool.Pool) *BenchmarkRunnerService {
	return &BenchmarkRunnerService{q: q, pool: pool}
}

func (s *BenchmarkRunnerService) Claim(ctx context.Context, projectID, runnerName string, executionKinds []string) (httpapi.ClaimResponse, error) {
	return s.claim(ctx, projectID, runnerName, "", executionKinds)
}

func (s *BenchmarkRunnerService) ClaimForExperiment(ctx context.Context, projectID, runnerName, experimentID string, executionKinds []string) (httpapi.ClaimResponse, error) {
	return s.claim(ctx, projectID, runnerName, experimentID, executionKinds)
}

func (s *BenchmarkRunnerService) claim(ctx context.Context, projectID, runnerName, experimentID string, executionKinds []string) (httpapi.ClaimResponse, error) {
	if len(executionKinds) == 0 {
		return httpapi.ClaimResponse{}, errors.New("at least one execution_kind is required")
	}
	for _, kind := range executionKinds {
		if err := benchmarks.ValidateExecutionKind(kind); err != nil {
			return httpapi.ClaimResponse{}, err
		}
	}
	var job store.EvalJob
	var err error
	if experimentID == "" {
		job, err = s.q.ClaimNextBenchmarkEvalJob(ctx, store.ClaimNextBenchmarkEvalJobParams{
			ProjectID:      parsePGUUID(projectID),
			RunnerName:     pgText(runnerName),
			LeaseExpiresAt: pgTimestamptz(time.Now().Add(runnerapi.DefaultLeaseDuration)),
			Column4:        executionKinds,
		})
	} else {
		job, err = s.q.ClaimNextBenchmarkEvalJobForExperiment(ctx, store.ClaimNextBenchmarkEvalJobForExperimentParams{
			ProjectID:      parsePGUUID(projectID),
			ExperimentID:   parsePGUUID(experimentID),
			RunnerName:     pgText(runnerName),
			LeaseExpiresAt: pgTimestamptz(time.Now().Add(runnerapi.DefaultLeaseDuration)),
			Column5:        executionKinds,
		})
	}
	if err != nil {
		return httpapi.ClaimResponse{}, fmt.Errorf("claim benchmark job: %w", err)
	}
	claim, err := s.q.GetBenchmarkClaimContext(ctx, store.GetBenchmarkClaimContextParams{
		ID:        job.ID,
		ProjectID: parsePGUUID(projectID),
	})
	if err != nil {
		return httpapi.ClaimResponse{}, fmt.Errorf("get benchmark claim context: %w", err)
	}
	var capabilities []string
	if err := json.Unmarshal(claim.RequiredCapabilities, &capabilities); err != nil {
		return httpapi.ClaimResponse{}, fmt.Errorf("decode required capabilities: %w", err)
	}
	if !claim.BenchmarkSuiteID.Valid {
		return httpapi.ClaimResponse{}, errors.New("claimed benchmark job has no benchmark suite")
	}
	caseInput := ""
	if decoded := jsonbStringToGoString(claim.CaseInput); decoded != nil {
		caseInput = *decoded
	}
	return httpapi.ClaimResponse{
		Job: httpapi.EvalJob{
			ID:              UUIDToString(claim.EvalJobID.Bytes),
			ExperimentID:    UUIDToString(claim.ExperimentID.Bytes),
			DatasetCaseID:   UUIDToString(claim.DatasetCaseID.Bytes),
			Status:          claim.JobStatus,
			ExecutionKind:   claim.ExecutionKind,
			ExecutionConfig: json.RawMessage(claim.ExecutionConfig),
			TrialIndex:      int(claim.TrialIndex),
			Seed:            pgInt8Ptr(claim.Seed),
		},
		Status:        claim.JobStatus,
		LeaseDuration: runnerapi.DefaultLeaseDuration.String(),
		CaseInput:     caseInput,
		CaseExpected:  jsonbStringToGoString(claim.CaseExpected),
		CaseMetadata:  json.RawMessage(claim.CaseMetadata),
		TargetName:    claim.TargetName,
		TargetVersion: strOrZero(claim.TargetVersion),
		EvalConfig:    json.RawMessage(claim.EvaluatorConfig),
		Benchmark: &httpapi.BenchmarkClaimContext{
			SuiteID:              UUIDToString(claim.BenchmarkSuiteID.Bytes),
			BenchmarkKey:         strOrZero(claim.BenchmarkKey),
			Name:                 strOrZero(claim.BenchmarkName),
			Version:              strOrZero(claim.BenchmarkVersion),
			HarnessKind:          strOrZero(claim.HarnessKind),
			SourceURI:            strOrZero(claim.SourceUri),
			SourceCommit:         strOrZero(claim.SourceCommit),
			RequiredCapabilities: capabilities,
			ExperimentConfig:     json.RawMessage(claim.BenchmarkConfig),
			TrialsPerCase:        int(claim.TrialsPerCase),
		},
	}, nil
}

// Fail records a structured benchmark failure and marks the same eval_job
// failed. It is used only when callers submit benchmark metadata; legacy
// failures remain handled by runnerapi unchanged.
func (s *BenchmarkRunnerService) Fail(ctx context.Context, jobID, projectID, runnerName, errorMsg string, details json.RawMessage, result *benchmarks.ResultDetails) error {
	mergedDetails, err := benchmarks.EmbedResultDetails(details, result)
	if err != nil {
		return err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	tq := s.q.WithTx(tx)
	job, err := tq.GetEvalJobLockedByProject(ctx, store.GetEvalJobLockedByProjectParams{ID: parsePGUUID(jobID), ProjectID: parsePGUUID(projectID)})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("job not found or project mismatch")
		}
		return fmt.Errorf("get job: %w", err)
	}
	if strOrZero(job.RunnerName) != runnerName || job.Status != "running" {
		return errors.New("job is not claimed by this runner")
	}
	if _, err := tq.LockExperimentForSummary(ctx, job.ExperimentID); err != nil {
		return fmt.Errorf("lock experiment: %w", err)
	}
	if _, err := tq.CreateEvalCaseResult(ctx, store.CreateEvalCaseResultParams{
		EvalJobID: parsePGUUID(jobID),
		Score:     pgtype.Numeric{},
		Passed:    pgtype.Bool{Bool: false, Valid: true},
		Reason:    pgText(errorMsg),
		Details:   mergedDetails,
	}); err != nil {
		return fmt.Errorf("create benchmark failure result: %w", err)
	}
	if _, err := tq.UpdateEvalJobStatusByProject(ctx, store.UpdateEvalJobStatusByProjectParams{
		ID:         parsePGUUID(jobID),
		Status:     "failed",
		Error:      pgText(errorMsg),
		ProjectID:  parsePGUUID(projectID),
		RunnerName: pgText(runnerName),
	}); err != nil {
		return fmt.Errorf("mark benchmark job failed: %w", err)
	}
	if err := updateExperimentEvaluationSummary(ctx, tq, job.ExperimentID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
