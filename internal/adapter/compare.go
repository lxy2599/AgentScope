package adapter

import (
	"context"
	"encoding/json"
	"fmt"

	"AgentScope/internal/experiments"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ComparisonQuerier implements experiments.CompareQuerier using store.Queries.
type ComparisonQuerier struct {
	q     *store.Queries
	pool  *pgxpool.Pool
	expQ  *ExperimentQuerier
}

// NewComparisonQuerier creates a new ComparisonQuerier.
func NewComparisonQuerier(q *store.Queries, pool *pgxpool.Pool, expQ *ExperimentQuerier) *ComparisonQuerier {
	return &ComparisonQuerier{q: q, pool: pool, expQ: expQ}
}

// CreateExperimentComparison creates a comparison and its case results in a transaction.
func (cq *ComparisonQuerier) CreateExperimentComparison(
	ctx context.Context,
	comparison experiments.Comparison,
	caseResults []experiments.ComparisonCaseResult,
) (experiments.Comparison, error) {
	tx, err := cq.pool.Begin(ctx)
	if err != nil {
		return experiments.Comparison{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	tq := cq.q.WithTx(tx)

	var summaryBytes, gatesBytes []byte
	if comparison.Summary != "" {
		summaryBytes = []byte(comparison.Summary)
	} else {
		summaryBytes = []byte("{}")
	}
	if comparison.RegressionGates != "" {
		gatesBytes = []byte(comparison.RegressionGates)
	} else {
		gatesBytes = []byte("{}")
	}

	storeComp, err := tq.CreateExperimentComparison(ctx, store.CreateExperimentComparisonParams{
		ProjectID:              parsePGUUID(comparison.ProjectID),
		BaselineExperimentID:   parsePGUUID(comparison.BaselineExperimentID),
		CandidateExperimentID:  parsePGUUID(comparison.CandidateExperimentID),
		ComparisonType:         string(comparison.ComparisonType),
		CreatedBy:              parsePGUUID(comparison.CreatedBy),
		Status:                 comparison.Status,
		Summary:                summaryBytes,
		RegressionGates:        gatesBytes,
	})
	if err != nil {
		return experiments.Comparison{}, fmt.Errorf("create comparison: %w", err)
	}

	for _, cr := range caseResults {
		var blRunID, candRunID pgtype.UUID
		if cr.BaselineRunID != "" {
			blRunID = parsePGUUID(cr.BaselineRunID)
		}
		if cr.CandidateRunID != "" {
			candRunID = parsePGUUID(cr.CandidateRunID)
		}

		detailsBytes := []byte("{}")
		if cr.Details != "" {
			detailsBytes = []byte(cr.Details)
		}

		_, err := tq.CreateComparisonCaseResult(ctx, store.CreateComparisonCaseResultParams{
			ComparisonID:       storeComp.ID,
			DatasetCaseID:      parsePGUUID(cr.DatasetCaseID),
			BaselineRunID:      blRunID,
			CandidateRunID:     candRunID,
			BaselineScore:      numericFromFloat64OrZero(cr.BaselineScore),
			CandidateScore:     numericFromFloat64OrZero(cr.CandidateScore),
			BaselinePassed:     pgBool(cr.BaselinePassed),
			CandidatePassed:    pgBool(cr.CandidatePassed),
			ScoreDiff:          numericFromFloat64OrZero(cr.ScoreDiff),
			Status:             cr.Status,
			BaselineLatencyMs:  pgInt8(cr.BaselineLatencyMs),
			CandidateLatencyMs: pgInt8(cr.CandidateLatencyMs),
			BaselineCost:       numericFromFloat64OrZero(cr.BaselineCost),
			CandidateCost:      numericFromFloat64OrZero(cr.CandidateCost),
			Details:            detailsBytes,
		})
		if err != nil {
			return experiments.Comparison{}, fmt.Errorf("create case result: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return experiments.Comparison{}, fmt.Errorf("commit tx: %w", err)
	}

	return storeComparisonToDomain(storeComp), nil
}

// GetExperimentComparison fetches a comparison with full detail.
func (cq *ComparisonQuerier) GetExperimentComparison(ctx context.Context, comparisonID string) (experiments.ComparisonDetail, error) {
	storeComp, err := cq.q.GetExperimentComparisonByID(ctx, parsePGUUID(comparisonID))
	if err != nil {
		return experiments.ComparisonDetail{}, fmt.Errorf("get comparison: %w", err)
	}

	// Fetch baseline and candidate experiments
	baseline, err := cq.expQ.GetExperiment(ctx, UUIDToString(storeComp.BaselineExperimentID.Bytes))
	if err != nil {
		return experiments.ComparisonDetail{}, fmt.Errorf("get baseline: %w", err)
	}
	candidate, err := cq.expQ.GetExperiment(ctx, UUIDToString(storeComp.CandidateExperimentID.Bytes))
	if err != nil {
		return experiments.ComparisonDetail{}, fmt.Errorf("get candidate: %w", err)
	}

	// Fetch case results
	storeResults, err := cq.q.ListComparisonCaseResults(ctx, storeComp.ID)
	if err != nil {
		return experiments.ComparisonDetail{}, fmt.Errorf("list case results: %w", err)
	}

	caseResults := make([]experiments.ComparisonCaseResult, 0, len(storeResults))
	for _, sr := range storeResults {
		cr := storeComparisonCaseResultToDomain(sr)
		caseResults = append(caseResults, cr)
	}

	// Parse summary
	comp := storeComparisonToDomain(storeComp)
	var summary experiments.ComparisonSummary
	if err := json.Unmarshal([]byte(comp.Summary), &summary); err != nil {
		summary = experiments.ComparisonSummary{}
	}

	return experiments.ComparisonDetail{
		Comparison:          comp,
		BaselineExperiment:  baseline,
		CandidateExperiment: candidate,
		ComparisonSummary:   summary,
		CaseResults:         caseResults,
	}, nil
}

// ListExperimentComparisons lists all comparisons for a project.
func (cq *ComparisonQuerier) ListExperimentComparisons(ctx context.Context, projectID string) ([]experiments.Comparison, error) {
	storeComps, err := cq.q.ListExperimentComparisonsByProject(ctx, parsePGUUID(projectID))
	if err != nil {
		return nil, fmt.Errorf("list comparisons: %w", err)
	}
	result := make([]experiments.Comparison, 0, len(storeComps))
	for _, sc := range storeComps {
		result = append(result, storeComparisonToDomain(sc))
	}
	return result, nil
}

// ---- Conversion helpers ----

func storeComparisonToDomain(sc store.ExperimentComparison) experiments.Comparison {
	comp := experiments.Comparison{
		ID:                    UUIDToString(sc.ID.Bytes),
		ProjectID:             UUIDToString(sc.ProjectID.Bytes),
		BaselineExperimentID:  UUIDToString(sc.BaselineExperimentID.Bytes),
		CandidateExperimentID: UUIDToString(sc.CandidateExperimentID.Bytes),
		ComparisonType:        experiments.ComparisonType(sc.ComparisonType),
		Status:                sc.Status,
		CreatedAt:             sc.CreatedAt.Time,
		UpdatedAt:             sc.UpdatedAt.Time,
		Summary:               string(sc.Summary),
		RegressionGates:       string(sc.RegressionGates),
	}
	if sc.CreatedBy.Valid {
		comp.CreatedBy = UUIDToString(sc.CreatedBy.Bytes)
	}
	return comp
}

func storeComparisonCaseResultToDomain(sr store.ComparisonCaseResult) experiments.ComparisonCaseResult {
	cr := experiments.ComparisonCaseResult{
		ID:                UUIDToString(sr.ID.Bytes),
		ComparisonID:      UUIDToString(sr.ComparisonID.Bytes),
		DatasetCaseID:     UUIDToString(sr.DatasetCaseID.Bytes),
		BaselineRunID:     "",
		CandidateRunID:    "",
		BaselineScore:     numericToFloat64(sr.BaselineScore),
		CandidateScore:    numericToFloat64(sr.CandidateScore),
		BaselinePassed:    sr.BaselinePassed.Bool,
		CandidatePassed:   sr.CandidatePassed.Bool,
		ScoreDiff:         numericToFloat64(sr.ScoreDiff),
		Status:            sr.Status,
		BaselineLatencyMs: sr.BaselineLatencyMs.Int64,
		CandidateLatencyMs: sr.CandidateLatencyMs.Int64,
		BaselineCost:      numericToFloat64(sr.BaselineCost),
		CandidateCost:     numericToFloat64(sr.CandidateCost),
		Details:           string(sr.Details),
		CreatedAt:         sr.CreatedAt.Time,
	}
	if sr.BaselineRunID.Valid {
		cr.BaselineRunID = UUIDToString(sr.BaselineRunID.Bytes)
	}
	if sr.CandidateRunID.Valid {
		cr.CandidateRunID = UUIDToString(sr.CandidateRunID.Bytes)
	}
	return cr
}

func numericFromFloat64OrZero(value float64) pgtype.Numeric {
	n, err := numericFromFloat64(value)
	if err != nil {
		return pgtype.Numeric{Valid: false}
	}
	return n
}
