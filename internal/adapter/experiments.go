package adapter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"AgentScope/internal/benchmarks"
	"AgentScope/internal/experiments"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5"
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

// createEvaluationSnapshot freezes the task, evaluator, and harness inputs used
// by an experiment. It deliberately stores normalized JSON and a content hash so
// historical jobs never depend on mutable dataset/evaluator rows.
func createEvaluationSnapshot(ctx context.Context, tx pgx.Tx, experimentID, projectID, datasetID string, evaluatorID pgtype.UUID, targetName, targetVersion string, caseIDs []string, harness map[string]any) (string, string, error) {
	selected := make(map[string]bool, len(caseIDs))
	for _, id := range caseIDs {
		selected[id] = true
	}
	rows, err := tx.Query(ctx, `SELECT id, input, expected, metadata, expected_behavior, success_criteria, environment, allowed_tools, graders FROM dataset_cases WHERE dataset_id = $1 ORDER BY sort_order, id`, parsePGUUID(datasetID))
	if err != nil {
		return "", "", fmt.Errorf("query dataset cases for snapshot: %w", err)
	}
	defer rows.Close()
	cases := make([]map[string]any, 0, len(caseIDs))
	for rows.Next() {
		var id pgtype.UUID
		var input, expected, metadata, expectedBehavior, successCriteria, environment, allowedTools, graders []byte
		if err := rows.Scan(&id, &input, &expected, &metadata, &expectedBehavior, &successCriteria, &environment, &allowedTools, &graders); err != nil {
			return "", "", fmt.Errorf("scan dataset case snapshot: %w", err)
		}
		caseID := UUIDToString(id.Bytes)
		if !selected[caseID] {
			continue
		}
		cases = append(cases, map[string]any{
			"id": caseID, "input": rawMessageOrEmpty(input), "expected": nullableJSON(expected),
			"metadata": rawMessageOrEmpty(metadata), "expected_behavior": nullableJSON(expectedBehavior),
			"success_criteria": nullableJSON(successCriteria), "environment": rawMessageOrEmpty(environment),
			"allowed_tools": rawMessageOrDefault(allowedTools, "[]"), "graders": rawMessageOrDefault(graders, "[]"),
		})
	}
	if err := rows.Err(); err != nil {
		return "", "", fmt.Errorf("read dataset case snapshot: %w", err)
	}

	var evaluatorConfig json.RawMessage = []byte("null")
	if evaluatorID.Valid {
		if err := tx.QueryRow(ctx, `SELECT config FROM evaluators WHERE id = $1 AND project_id = $2`, evaluatorID, parsePGUUID(projectID)).Scan(&evaluatorConfig); err != nil {
			return "", "", fmt.Errorf("query evaluator snapshot: %w", err)
		}
	}
	payload := map[string]any{
		"schema_version": 1, "experiment_id": experimentID, "project_id": projectID, "dataset_id": datasetID,
		"case_ids": caseIDs, "cases": cases, "evaluator_id": nil, "evaluator_config": json.RawMessage(evaluatorConfig),
		"target": map[string]string{"name": targetName, "version": targetVersion}, "harness": harness,
	}
	if evaluatorID.Valid {
		payload["evaluator_id"] = UUIDToString(evaluatorID.Bytes)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", "", fmt.Errorf("encode evaluation snapshot: %w", err)
	}
	digest := sha256.Sum256(encoded)
	hash := hex.EncodeToString(digest[:])
	var snapshotID string
	if err := tx.QueryRow(ctx, `INSERT INTO evaluation_snapshots (experiment_id, schema_version, payload, content_hash) VALUES ($1, 1, $2, $3) RETURNING id::text`, parsePGUUID(experimentID), encoded, hash).Scan(&snapshotID); err != nil {
		return "", "", fmt.Errorf("persist evaluation snapshot: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE experiments SET evaluation_snapshot_id = $1 WHERE id = $2`, parsePGUUID(snapshotID), parsePGUUID(experimentID)); err != nil {
		return "", "", fmt.Errorf("link evaluation snapshot: %w", err)
	}
	return snapshotID, hash, nil
}

func rawMessageOrEmpty(value []byte) json.RawMessage {
	return rawMessageOrDefault(value, "{}")
}

func rawMessageOrDefault(value []byte, fallback string) json.RawMessage {
	if len(value) == 0 || string(value) == "null" {
		return json.RawMessage(fallback)
	}
	return json.RawMessage(value)
}

func nullableJSON(value []byte) any {
	if len(value) == 0 || string(value) == "null" {
		return nil
	}
	return json.RawMessage(value)
}

func (e *ExperimentQuerier) attachSnapshot(ctx context.Context, exp experiments.Experiment) experiments.Experiment {
	var id, hash string
	var payload []byte
	if err := e.pool.QueryRow(ctx, `SELECT s.id::text, s.content_hash, s.payload FROM evaluation_snapshots s WHERE s.experiment_id = $1`, parsePGUUID(exp.ID)).Scan(&id, &hash, &payload); err == nil {
		exp.EvaluationSnapshotID, exp.EvaluationSnapshotHash = id, hash
		var decoded map[string]any
		if json.Unmarshal(payload, &decoded) == nil {
			exp.EvaluationSnapshot = decoded
		}
	}
	return exp
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
	if _, _, err := createEvaluationSnapshot(ctx, tx, UUIDToString(storeExp.ID.Bytes), projectID, datasetID, evalID, targetName, targetVersion, caseIDs, map[string]any{}); err != nil {
		return experiments.Experiment{}, err
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

	domain := storeExpToDomain(storeExp)
	domain = e.attachSnapshot(ctx, domain)
	return domain, nil
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

	var storeExp store.Experiment
	var benchmarkExecutionKind string
	var benchmarkExecutionConfig []byte
	if params.BenchmarkSuiteID != "" {
		suiteRecord, err := tq.GetBenchmarkSuiteByProject(ctx, store.GetBenchmarkSuiteByProjectParams{
			ID:        parsePGUUID(params.BenchmarkSuiteID),
			ProjectID: parsePGUUID(params.ProjectID),
		})
		if err != nil {
			return experiments.Experiment{}, fmt.Errorf("get benchmark suite: %w", err)
		}
		if suiteRecord.DatasetID.Bytes != parsePGUUID(params.DatasetID).Bytes {
			return experiments.Experiment{}, fmt.Errorf("benchmark suite does not belong to dataset %s", params.DatasetID)
		}
		suite, err := storeBenchmarkSuiteToDomain(suiteRecord)
		if err != nil {
			return experiments.Experiment{}, err
		}
		benchmarkExecutionKind = suite.ExecutionKind
		if params.ExecutionKind != "" {
			benchmarkExecutionKind = params.ExecutionKind
		}
		if err := benchmarks.ValidateExecutionKind(benchmarkExecutionKind); err != nil {
			return experiments.Experiment{}, err
		}
		benchmarkExecutionConfig, err = mergeBenchmarkExecutionConfig(
			suite.DefaultExecutionConfig,
			params.ExecutionConfig,
		)
		if err != nil {
			return experiments.Experiment{}, err
		}
		storeExp, err = tq.CreateBenchmarkExperiment(ctx, store.CreateBenchmarkExperimentParams{
			ProjectID:      parsePGUUID(params.ProjectID),
			DatasetID:      parsePGUUID(params.DatasetID),
			EvaluatorID:    evalID,
			TargetName:     params.TargetName,
			TargetVersion:  pgText(params.TargetVersion),
			Status:         "pending",
			AggregateScore: pgtype.Numeric{Valid: false},
			TotalCases:     int32(len(params.CaseIDs) * params.TrialsPerCase),
			PassedCases:    0,
			FailedCases:    0,
			Metadata:       []byte("{}"),
			ModelProvider:  pgText(params.ModelProvider),
			ModelName:      pgText(params.ModelName),
			ModelVersion:   pgText(params.ModelVersion),
			PromptVersion:  pgText(params.PromptVersion),
			HarnessVersion: pgText(params.HarnessVersion),
			DatasetVersion: pgText(params.DatasetVersion),
			ID:             suiteRecord.ID,
			// Persist the same resolved, immutable execution document supplied to each
			// eval job. For a Run Template this retains catalog revision, artifact
			// digest, profile identities and task selection; persisting the mutable
			// suite snapshot here would break experiment-level reproducibility.
			BenchmarkConfig: benchmarkExecutionConfig,
			TrialsPerCase:   int32(params.TrialsPerCase),
			FeatureFlags:    featureFlags,
		})
		if err != nil {
			return experiments.Experiment{}, fmt.Errorf("create benchmark experiment: %w", err)
		}
	} else {
		storeExp, err = tq.CreateExperiment(ctx, store.CreateExperimentParams{
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

	harnessSnapshot := map[string]any{
		"model_provider": params.ModelProvider, "model_name": params.ModelName, "model_version": params.ModelVersion,
		"prompt_version": params.PromptVersion, "harness_version": params.HarnessVersion, "dataset_version": params.DatasetVersion,
		"toolset_version": params.ToolsetVersion, "skillset_version": params.SkillsetVersion, "rag_config_version": params.RAGConfigVersion,
		"feature_flags": json.RawMessage(featureFlags), "benchmark_suite_id": params.BenchmarkSuiteID,
		"benchmark_config": json.RawMessage(benchmarkExecutionConfig), "execution_kind": benchmarkExecutionKind,
	}
	snapshotID, snapshotHash, err := createEvaluationSnapshot(ctx, tx, UUIDToString(storeExp.ID.Bytes), params.ProjectID, params.DatasetID, evalID, params.TargetName, params.TargetVersion, params.CaseIDs, harnessSnapshot)
	if err != nil {
		return experiments.Experiment{}, err
	}

	for _, caseID := range params.CaseIDs {
		if params.BenchmarkSuiteID == "" {
			if _, err := tq.CreateEvalJob(ctx, store.CreateEvalJobParams{
				ExperimentID:  storeExp.ID,
				DatasetCaseID: parsePGUUID(caseID),
				Status:        "queued",
			}); err != nil {
				return experiments.Experiment{}, fmt.Errorf("create eval job for case %s: %w", caseID, err)
			}
			continue
		}
		for trialIndex := 0; trialIndex < params.TrialsPerCase; trialIndex++ {
			seed := pgtype.Int8{}
			if params.Seed != nil {
				seed = pgtype.Int8{Int64: *params.Seed + int64(trialIndex), Valid: true}
			}
			if _, err := tq.CreateBenchmarkEvalJob(ctx, store.CreateBenchmarkEvalJobParams{
				ExperimentID:    storeExp.ID,
				DatasetCaseID:   parsePGUUID(caseID),
				Status:          "queued",
				ExecutionKind:   benchmarkExecutionKind,
				ExecutionConfig: benchmarkExecutionConfig,
				TrialIndex:      int32(trialIndex),
				Seed:            seed,
			}); err != nil {
				return experiments.Experiment{}, fmt.Errorf("create benchmark eval job for case %s trial %d: %w", caseID, trialIndex, err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return experiments.Experiment{}, fmt.Errorf("commit tx: %w", err)
	}

	domain := storeExpToDomain(storeExp)
	domain.EvaluationSnapshotID, domain.EvaluationSnapshotHash = snapshotID, snapshotHash
	return e.attachSnapshot(ctx, domain), nil
}

func mergeBenchmarkExecutionConfig(defaultConfig []byte, override string) ([]byte, error) {
	normalizedDefault, err := benchmarks.NormalizeJSONObject(defaultConfig, "execution_config")
	if err != nil {
		return nil, err
	}
	if override == "" {
		return normalizedDefault, nil
	}
	normalizedOverride, err := benchmarks.NormalizeJSONObject([]byte(override), "execution_config")
	if err != nil {
		return nil, err
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(normalizedDefault, &merged); err != nil {
		return nil, fmt.Errorf("decode benchmark default execution config: %w", err)
	}
	var overrideValues map[string]json.RawMessage
	if err := json.Unmarshal(normalizedOverride, &overrideValues); err != nil {
		return nil, fmt.Errorf("decode benchmark execution config override: %w", err)
	}
	for key, value := range overrideValues {
		merged[key] = value
	}
	mergedConfig, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("encode merged benchmark execution config: %w", err)
	}
	return mergedConfig, nil
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
	return e.attachSnapshot(ctx, storeExpToDomain(storeExp)), nil
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
		result = append(result, e.attachSnapshot(ctx, storeExpToDomain(se)))
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
		evaluationStatus := ""
		var resultDetails struct {
			EvaluationStatus string `json:"evaluation_status"`
		}
		if len(sr.Details) > 0 && json.Unmarshal(sr.Details, &resultDetails) == nil {
			evaluationStatus = resultDetails.EvaluationStatus
		}
		result = append(result, experiments.EvalCaseResult{
			ID:               UUIDToString(sr.ID.Bytes),
			EvalJobID:        UUIDToString(sr.EvalJobID.Bytes),
			RunID:            runID,
			DatasetCaseID:    datasetCaseID,
			CaseInput:        string(sr.CaseInput),
			CaseExpected:     string(sr.CaseExpected),
			Score:            numericToFloat64(sr.Score),
			Passed:           sr.Passed.Bool,
			Reason:           strOrZero(sr.Reason),
			Details:          string(sr.Details),
			ActualOutput:     strOrZero(sr.ActualOutput),
			LatencyMs:        sr.LatencyMs.Int64,
			Cost:             numericToFloat64(sr.Cost),
			CreatedAt:        sr.CreatedAt.Time,
			EvaluationStatus: evaluationStatus,
		})
	}
	return result, nil
}

func storeExpToDomain(se store.Experiment) experiments.Experiment {
	evaluatorID := ""
	if se.EvaluatorID.Valid {
		evaluatorID = UUIDToString(se.EvaluatorID.Bytes)
	}
	benchmarkSuiteID := ""
	if se.BenchmarkSuiteID.Valid {
		benchmarkSuiteID = UUIDToString(se.BenchmarkSuiteID.Bytes)
	}
	return experiments.Experiment{
		ID:             UUIDToString(se.ID.Bytes),
		ProjectID:      UUIDToString(se.ProjectID.Bytes),
		DatasetID:      UUIDToString(se.DatasetID.Bytes),
		EvaluatorID:    evaluatorID,
		TargetName:     se.TargetName,
		TargetVersion:  strOrZero(se.TargetVersion),
		Status:         se.Status,
		TotalCases:     int(se.TotalCases),
		PassedCases:    int(se.PassedCases),
		FailedCases:    int(se.FailedCases),
		AggregateScore: numericToFloat64(se.AggregateScore),
		CreatedAt:      se.CreatedAt.Time,
		UpdatedAt:      se.UpdatedAt.Time,
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
		BenchmarkSuiteID: benchmarkSuiteID,
		BenchmarkKey:     strOrZero(se.BenchmarkKey),
		BenchmarkVersion: strOrZero(se.BenchmarkVersion),
		BenchmarkConfig:  string(se.BenchmarkConfig),
		TrialsPerCase:    int(se.TrialsPerCase),
	}
}

func storeJobToDomain(sj store.EvalJob) experiments.EvalJob {
	return experiments.EvalJob{
		ID:              UUIDToString(sj.ID.Bytes),
		ExperimentID:    UUIDToString(sj.ExperimentID.Bytes),
		DatasetCaseID:   UUIDToString(sj.DatasetCaseID.Bytes),
		Status:          sj.Status,
		RunnerName:      strOrZero(sj.RunnerName),
		LeaseExpiresAt:  pgTimestamptzPtr(sj.LeaseExpiresAt),
		Attempts:        int(sj.Attempts),
		Error:           strOrZero(sj.Error),
		CreatedAt:       sj.CreatedAt.Time,
		UpdatedAt:       sj.UpdatedAt.Time,
		ExecutionKind:   sj.ExecutionKind,
		ExecutionConfig: string(sj.ExecutionConfig),
		TrialIndex:      int(sj.TrialIndex),
		Seed:            pgInt8Ptr(sj.Seed),
	}
}
