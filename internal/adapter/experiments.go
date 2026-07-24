package adapter

import (
	"context"
	"fmt"

	"AgentScope/internal/experiments"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ExperimentQuerier implements experiments.Querier using store.Queries with
// PostgreSQL transaction support.
type ExperimentQuerier struct {
	q    *store.Queries
	pool *pgxpool.Pool
}

// compile-time check
var _ experiments.Querier = (*ExperimentQuerier)(nil)

type evaluatorProjectQuerier interface {
	GetEvaluatorByID(ctx context.Context, id pgtype.UUID) (store.Evaluator, error)
}

type datasetProjectQuerier interface {
	GetDatasetByID(ctx context.Context, id pgtype.UUID) (store.Dataset, error)
}

func evaluatorIDForProject(
	ctx context.Context,
	q evaluatorProjectQuerier,
	evaluatorID string,
	projectID string,
) (pgtype.UUID, error) {
	if evaluatorID == "" {
		return pgtype.UUID{}, nil
	}

	evaluatorUUID := parsePGUUID(evaluatorID)
	projectUUID := parsePGUUID(projectID)
	if !evaluatorUUID.Valid || !projectUUID.Valid {
		return pgtype.UUID{}, fmt.Errorf("evaluator is invalid or does not belong to the project")
	}

	evaluator, err := q.GetEvaluatorByID(ctx, evaluatorUUID)
	if err != nil || !evaluator.ProjectID.Valid || evaluator.ProjectID.Bytes != projectUUID.Bytes {
		return pgtype.UUID{}, fmt.Errorf("evaluator is invalid or does not belong to the project")
	}
	return evaluatorUUID, nil
}

func validateDatasetProject(
	ctx context.Context,
	q datasetProjectQuerier,
	datasetID string,
	projectID string,
) error {
	datasetUUID := parsePGUUID(datasetID)
	projectUUID := parsePGUUID(projectID)
	if !datasetUUID.Valid || !projectUUID.Valid {
		return fmt.Errorf("dataset is invalid or does not belong to the project")
	}

	dataset, err := q.GetDatasetByID(ctx, datasetUUID)
	if err != nil || !dataset.ProjectID.Valid || dataset.ProjectID.Bytes != projectUUID.Bytes {
		return fmt.Errorf("dataset is invalid or does not belong to the project")
	}
	return nil
}

// NewExperimentQuerier creates a new ExperimentQuerier backed by a connection pool.
func NewExperimentQuerier(q *store.Queries, pool *pgxpool.Pool) *ExperimentQuerier {
	return &ExperimentQuerier{q: q, pool: pool}
}

// CreateExperimentWithJobs creates an experiment and all its eval jobs in a single
// transaction. If any step fails or any caseID does not belong to the dataset,
// the entire transaction is rolled back.
func (e *ExperimentQuerier) CreateExperimentWithJobs(ctx context.Context, projectID, datasetID, evaluatorID, targetName, targetVersion string, caseIDs []string) (experiments.Experiment, error) {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return experiments.Experiment{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	tq := e.q.WithTx(tx)

	if err := validateDatasetProject(ctx, tq, datasetID, projectID); err != nil {
		return experiments.Experiment{}, err
	}

	// Verify provided caseIDs belong to the dataset
	validCaseIDs, err := tq.ListDatasetCaseIDsByDataset(ctx, parsePGUUID(datasetID))
	if err != nil {
		return experiments.Experiment{}, fmt.Errorf("list dataset case ids: %w", err)
	}
	validSet := make(map[string]bool, len(validCaseIDs))
	for _, id := range validCaseIDs {
		validSet[UUIDToString(id.Bytes)] = true
	}
	for _, caseID := range caseIDs {
		if !validSet[caseID] {
			return experiments.Experiment{}, fmt.Errorf("case %s does not belong to dataset %s", caseID, datasetID)
		}
	}

	evalID, err := evaluatorIDForProject(ctx, tq, evaluatorID, projectID)
	if err != nil {
		return experiments.Experiment{}, err
	}

	storeExp, err := tq.CreateExperiment(ctx, store.CreateExperimentParams{
		ProjectID:      parsePGUUID(projectID),
		DatasetID:      parsePGUUID(datasetID),
		EvaluatorID:    evalID,
		TargetName:     targetName,
		TargetVersion:  pgText(targetVersion),
		Status:         "pending",
		AggregateScore: pgtype.Numeric{Valid: false},
		TotalCases:     int32(len(caseIDs)),
		PassedCases:    0,
		FailedCases:    0,
		Metadata:       []byte("{}"),
	})
	if err != nil {
		return experiments.Experiment{}, fmt.Errorf("create experiment: %w", err)
	}

	for _, caseID := range caseIDs {
		if _, err := tq.CreateEvalJob(ctx, store.CreateEvalJobParams{
			ExperimentID:  storeExp.ID,
			DatasetCaseID: parsePGUUID(caseID),
			Status:        "queued",
		}); err != nil {
			return experiments.Experiment{}, fmt.Errorf("create eval job for case %s: %w", caseID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return experiments.Experiment{}, fmt.Errorf("commit tx: %w", err)
	}

	return storeExpToDomain(storeExp), nil
}

// CreateExperimentWithJobsAndMetadata creates an experiment with harness metadata
// and all eval jobs in a single transaction. Returns error if any step fails.
func (e *ExperimentQuerier) CreateExperimentWithJobsAndMetadata(ctx context.Context, params experiments.CreateExperimentHarnessParams) (experiments.Experiment, error) {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return experiments.Experiment{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	tq := e.q.WithTx(tx)

	if err := validateDatasetProject(ctx, tq, params.DatasetID, params.ProjectID); err != nil {
		return experiments.Experiment{}, err
	}

	// Verify case IDs belong to the dataset
	validCaseIDs, err := tq.ListDatasetCaseIDsByDataset(ctx, parsePGUUID(params.DatasetID))
	if err != nil {
		return experiments.Experiment{}, fmt.Errorf("list dataset case ids: %w", err)
	}
	validSet := make(map[string]bool, len(validCaseIDs))
	for _, id := range validCaseIDs {
		validSet[UUIDToString(id.Bytes)] = true
	}
	for _, caseID := range params.CaseIDs {
		if !validSet[caseID] {
			return experiments.Experiment{}, fmt.Errorf("case %s does not belong to dataset %s", caseID, params.DatasetID)
		}
	}

	evalID, err := evaluatorIDForProject(ctx, tq, params.EvaluatorID, params.ProjectID)
	if err != nil {
		return experiments.Experiment{}, err
	}

	featureFlags := []byte("{}")
	if params.FeatureFlags != "" {
		featureFlags = []byte(params.FeatureFlags)
	}

	storeExp, err := tq.CreateExperiment(ctx, store.CreateExperimentParams{
		ProjectID:      parsePGUUID(params.ProjectID),
		DatasetID:      parsePGUUID(params.DatasetID),
		EvaluatorID:    evalID,
		TargetName:     params.TargetName,
		TargetVersion:  pgText(params.TargetVersion),
		Status:         "pending",
		AggregateScore: pgtype.Numeric{Valid: false},
		TotalCases:     int32(len(params.CaseIDs)),
		PassedCases:    0,
		FailedCases:    0,
		Metadata:       []byte("{}"),
		ModelProvider:  pgText(params.ModelProvider),
		ModelName:      pgText(params.ModelName),
		ModelVersion:   pgText(params.ModelVersion),
		PromptVersion:  pgText(params.PromptVersion),
		HarnessVersion: pgText(params.HarnessVersion),
		DatasetVersion: pgText(params.DatasetVersion),
		FeatureFlags:   featureFlags,
	})
	if err != nil {
		return experiments.Experiment{}, fmt.Errorf("create experiment: %w", err)
	}

	// Save toolset/skillset/rag_config via separate update within the same transaction
	_, err = tq.UpdateExperimentHarnessMetadata(ctx, store.UpdateExperimentHarnessMetadataParams{
		ID:               storeExp.ID,
		ModelProvider:    pgText(params.ModelProvider),
		ModelName:        pgText(params.ModelName),
		ModelVersion:     pgText(params.ModelVersion),
		PromptVersion:    pgText(params.PromptVersion),
		HarnessVersion:   pgText(params.HarnessVersion),
		DatasetVersion:   pgText(params.DatasetVersion),
		ToolsetVersion:   pgText(params.ToolsetVersion),
		SkillsetVersion:  pgText(params.SkillsetVersion),
		RagConfigVersion: pgText(params.RAGConfigVersion),
		FeatureFlags:     featureFlags,
	})
	if err != nil {
		return experiments.Experiment{}, fmt.Errorf("update harness metadata: %w", err)
	}

	for _, caseID := range params.CaseIDs {
		if _, err := tq.CreateEvalJob(ctx, store.CreateEvalJobParams{
			ExperimentID:  storeExp.ID,
			DatasetCaseID: parsePGUUID(caseID),
			Status:        "queued",
		}); err != nil {
			return experiments.Experiment{}, fmt.Errorf("create eval job for case %s: %w", caseID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return experiments.Experiment{}, fmt.Errorf("commit tx: %w", err)
	}

	return storeExpToDomain(storeExp), nil
}

// --- Legacy methods (kept for interface compliance) ---

func (e *ExperimentQuerier) CreateExperiment(ctx context.Context, projectID, datasetID, evaluatorID, targetName, targetVersion string, totalCases int) (experiments.Experiment, error) {
	var evalID pgtype.UUID
	if evaluatorID != "" {
		evalID = parsePGUUID(evaluatorID)
	}

	storeExp, err := e.q.CreateExperiment(ctx, store.CreateExperimentParams{
		ProjectID:      parsePGUUID(projectID),
		DatasetID:      parsePGUUID(datasetID),
		EvaluatorID:    evalID,
		TargetName:     targetName,
		TargetVersion:  pgText(targetVersion),
		Status:         "pending",
		AggregateScore: pgtype.Numeric{Valid: false},
		TotalCases:     int32(totalCases),
		PassedCases:    0,
		FailedCases:    0,
		Metadata:       []byte("{}"),
	})
	if err != nil {
		return experiments.Experiment{}, err
	}
	return storeExpToDomain(storeExp), nil
}

func (e *ExperimentQuerier) GetExperiment(ctx context.Context, experimentID string) (experiments.Experiment, error) {
	storeExp, err := e.q.GetExperimentByID(ctx, parsePGUUID(experimentID))
	if err != nil {
		return experiments.Experiment{}, err
	}
	return storeExpToDomain(storeExp), nil
}

func (e *ExperimentQuerier) CreateEvalJob(ctx context.Context, experimentID, datasetCaseID, status string) (experiments.EvalJob, error) {
	storeJob, err := e.q.CreateEvalJob(ctx, store.CreateEvalJobParams{
		ExperimentID:  parsePGUUID(experimentID),
		DatasetCaseID: parsePGUUID(datasetCaseID),
		Status:        status,
	})
	if err != nil {
		return experiments.EvalJob{}, err
	}
	return storeJobToDomain(storeJob), nil
}

func (e *ExperimentQuerier) ListEvalJobs(ctx context.Context, experimentID string) ([]experiments.EvalJob, error) {
	storeJobs, err := e.q.ListEvalJobsByExperiment(ctx, parsePGUUID(experimentID))
	if err != nil {
		return nil, err
	}
	result := make([]experiments.EvalJob, 0, len(storeJobs))
	for _, sj := range storeJobs {
		result = append(result, storeJobToDomain(sj))
	}
	return result, nil
}

func (e *ExperimentQuerier) GetJobStatusCounts(ctx context.Context, experimentID string) (map[string]int, error) {
	rows, err := e.q.GetExperimentJobStatusCounts(ctx, parsePGUUID(experimentID))
	if err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(rows))
	for _, row := range rows {
		counts[row.Status] = int(row.Count)
	}
	return counts, nil
}

func (e *ExperimentQuerier) UpdateExperimentCounts(ctx context.Context, experimentID string, status string, passedCases, failedCases int) error {
	_, err := e.q.UpdateExperimentStatus(ctx, store.UpdateExperimentStatusParams{
		ID:          parsePGUUID(experimentID),
		Status:      status,
		PassedCases: int32(passedCases),
		FailedCases: int32(failedCases),
	})
	return err
}

func (e *ExperimentQuerier) UpdateExperimentQualityCounts(ctx context.Context, experimentID string, qualityPassCases, qualityFailedCases int, aggregateScore float64) error {
	_, err := e.pool.Exec(ctx,
		`UPDATE experiments SET quality_pass_cases=$1, quality_failed_cases=$2, aggregate_score=$3, updated_at=now() WHERE id=$4`,
		int32(qualityPassCases), int32(qualityFailedCases), fmt.Sprintf("%f", aggregateScore), parsePGUUID(experimentID),
	)
	return err
}

func (e *ExperimentQuerier) UpdateExperimentHarnessMetadata(ctx context.Context, experimentID, modelProvider, modelName, modelVersion, promptVersion, harnessVersion, datasetVersion string, featureFlags []byte) error {
	_, err := e.q.UpdateExperimentHarnessMetadata(ctx, store.UpdateExperimentHarnessMetadataParams{
		ID:               parsePGUUID(experimentID),
		ModelProvider:    pgText(modelProvider),
		ModelName:        pgText(modelName),
		ModelVersion:     pgText(modelVersion),
		PromptVersion:    pgText(promptVersion),
		HarnessVersion:   pgText(harnessVersion),
		DatasetVersion:   pgText(datasetVersion),
		ToolsetVersion:   pgtype.Text{Valid: false},
		SkillsetVersion:  pgtype.Text{Valid: false},
		RagConfigVersion: pgtype.Text{Valid: false},
		FeatureFlags:     featureFlags,
	})
	return err
}

// UpdateFullExperimentHarnessMetadata updates ALL harness metadata fields on an experiment.
func (e *ExperimentQuerier) UpdateFullExperimentHarnessMetadata(ctx context.Context, experimentID, modelProvider, modelName, modelVersion, promptVersion, harnessVersion, datasetVersion, toolsetVersion, skillsetVersion, ragConfigVersion string, featureFlags []byte) error {
	_, err := e.q.UpdateExperimentHarnessMetadata(ctx, store.UpdateExperimentHarnessMetadataParams{
		ID:               parsePGUUID(experimentID),
		ModelProvider:    pgText(modelProvider),
		ModelName:        pgText(modelName),
		ModelVersion:     pgText(modelVersion),
		PromptVersion:    pgText(promptVersion),
		HarnessVersion:   pgText(harnessVersion),
		DatasetVersion:   pgText(datasetVersion),
		ToolsetVersion:   pgText(toolsetVersion),
		SkillsetVersion:  pgText(skillsetVersion),
		RagConfigVersion: pgText(ragConfigVersion),
		FeatureFlags:     featureFlags,
	})
	return err
}

func (e *ExperimentQuerier) ListExperiments(ctx context.Context, projectID string) ([]experiments.Experiment, error) {
	storeExps, err := e.q.ListExperimentsByProject(ctx, parsePGUUID(projectID))
	if err != nil {
		return nil, err
	}
	result := make([]experiments.Experiment, 0, len(storeExps))
	for _, se := range storeExps {
		result = append(result, storeExpToDomain(se))
	}
	return result, nil
}

func (e *ExperimentQuerier) ListEvalResults(ctx context.Context, experimentID string) ([]experiments.EvalCaseResult, error) {
	storeResults, err := e.q.ListEvalResultsByExperiment(ctx, parsePGUUID(experimentID))
	if err != nil {
		return nil, err
	}
	result := make([]experiments.EvalCaseResult, 0, len(storeResults))
	for _, sr := range storeResults {
		runID := ""
		if sr.RunID.Valid {
			runID = UUIDToString(sr.RunID.Bytes)
		}
		datasetCaseID := ""
		if sr.DatasetCaseID.Valid {
			datasetCaseID = UUIDToString(sr.DatasetCaseID.Bytes)
		}
		result = append(result, experiments.EvalCaseResult{
			ID:            UUIDToString(sr.ID.Bytes),
			EvalJobID:     UUIDToString(sr.EvalJobID.Bytes),
			RunID:         runID,
			DatasetCaseID: datasetCaseID,
			Score:         numericToFloat64(sr.Score),
			Passed:        sr.Passed.Bool,
			Reason:        strOrZero(sr.Reason),
			ActualOutput:  strOrZero(sr.ActualOutput),
			LatencyMs:     sr.LatencyMs.Int64,
			Cost:          numericToFloat64(sr.Cost),
			CreatedAt:     sr.CreatedAt.Time,
		})
	}
	return result, nil
}

func storeExpToDomain(se store.Experiment) experiments.Experiment {
	evaluatorID := ""
	if se.EvaluatorID.Valid {
		evaluatorID = UUIDToString(se.EvaluatorID.Bytes)
	}
	return experiments.Experiment{
		ID:            UUIDToString(se.ID.Bytes),
		ProjectID:     UUIDToString(se.ProjectID.Bytes),
		DatasetID:     UUIDToString(se.DatasetID.Bytes),
		EvaluatorID:   evaluatorID,
		TargetName:    se.TargetName,
		TargetVersion: strOrZero(se.TargetVersion),
		Status:        se.Status,
		TotalCases:    int(se.TotalCases),
		PassedCases:   int(se.PassedCases),
		FailedCases:   int(se.FailedCases),
		CreatedAt:     se.CreatedAt.Time,
		UpdatedAt:     se.UpdatedAt.Time,
		// Harness metadata
		ModelProvider:    strOrZero(se.ModelProvider),
		ModelName:        strOrZero(se.ModelName),
		ModelVersion:     strOrZero(se.ModelVersion),
		PromptVersion:    strOrZero(se.PromptVersion),
		HarnessVersion:   strOrZero(se.HarnessVersion),
		DatasetVersion:   strOrZero(se.DatasetVersion),
		ToolsetVersion:   strOrZero(se.ToolsetVersion),
		SkillsetVersion:  strOrZero(se.SkillsetVersion),
		RAGConfigVersion: strOrZero(se.RagConfigVersion),
		FeatureFlags:     string(se.FeatureFlags),
	}
}

func storeJobToDomain(sj store.EvalJob) experiments.EvalJob {
	return experiments.EvalJob{
		ID:             UUIDToString(sj.ID.Bytes),
		ExperimentID:   UUIDToString(sj.ExperimentID.Bytes),
		DatasetCaseID:  UUIDToString(sj.DatasetCaseID.Bytes),
		Status:         sj.Status,
		RunnerName:     strOrZero(sj.RunnerName),
		LeaseExpiresAt: pgTimestamptzPtr(sj.LeaseExpiresAt),
		Attempts:       int(sj.Attempts),
		Error:          strOrZero(sj.Error),
		CreatedAt:      sj.CreatedAt.Time,
		UpdatedAt:      sj.UpdatedAt.Time,
	}
}
