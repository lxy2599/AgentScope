package adapter

import (
	"context"
	"encoding/json"
	"fmt"

	"AgentScope/internal/benchmarks"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// BenchmarkSuiteQuerier persists benchmark suite definitions in PostgreSQL.
type BenchmarkSuiteQuerier struct {
	q *store.Queries
}

var _ benchmarks.Querier = (*BenchmarkSuiteQuerier)(nil)

func NewBenchmarkSuiteQuerier(q *store.Queries) *BenchmarkSuiteQuerier {
	return &BenchmarkSuiteQuerier{q: q}
}

func (q *BenchmarkSuiteQuerier) CreateBenchmarkSuite(ctx context.Context, params benchmarks.CreateSuiteParams) (benchmarks.Suite, error) {
	capabilities, err := json.Marshal(params.RequiredCapabilities)
	if err != nil {
		return benchmarks.Suite{}, fmt.Errorf("marshal required capabilities: %w", err)
	}
	suite, err := q.q.CreateBenchmarkSuite(ctx, store.CreateBenchmarkSuiteParams{
		ProjectID:              parsePGUUID(params.ProjectID),
		DatasetID:              parsePGUUID(params.DatasetID),
		BenchmarkKey:           params.BenchmarkKey,
		Name:                   params.Name,
		Description:            params.Description,
		Version:                params.Version,
		HarnessKind:            params.HarnessKind,
		ExecutionKind:          params.ExecutionKind,
		SourceUri:              pgText(params.SourceURI),
		SourceCommit:           pgText(params.SourceCommit),
		DefaultExecutionConfig: params.DefaultExecutionConfig,
		RequiredCapabilities:   capabilities,
		Metadata:               params.Metadata,
	})
	if err != nil {
		return benchmarks.Suite{}, err
	}
	return storeBenchmarkSuiteToDomain(suite)
}

func (q *BenchmarkSuiteQuerier) GetBenchmarkSuite(ctx context.Context, projectID, suiteID string) (benchmarks.Suite, error) {
	suite, err := q.q.GetBenchmarkSuiteByProject(ctx, store.GetBenchmarkSuiteByProjectParams{
		ID:        parsePGUUID(suiteID),
		ProjectID: parsePGUUID(projectID),
	})
	if err != nil {
		return benchmarks.Suite{}, err
	}
	return storeBenchmarkSuiteToDomain(suite)
}

func (q *BenchmarkSuiteQuerier) ListBenchmarkSuites(ctx context.Context, projectID string) ([]benchmarks.Suite, error) {
	suites, err := q.q.ListBenchmarkSuitesByProject(ctx, parsePGUUID(projectID))
	if err != nil {
		return nil, err
	}
	result := make([]benchmarks.Suite, 0, len(suites))
	for _, suite := range suites {
		domainSuite, err := storeBenchmarkSuiteToDomain(suite)
		if err != nil {
			return nil, err
		}
		result = append(result, domainSuite)
	}
	return result, nil
}

func storeBenchmarkSuiteToDomain(suite store.BenchmarkSuite) (benchmarks.Suite, error) {
	var capabilities []string
	if err := json.Unmarshal(suite.RequiredCapabilities, &capabilities); err != nil {
		return benchmarks.Suite{}, fmt.Errorf("decode benchmark suite capabilities: %w", err)
	}
	return benchmarks.Suite{
		ID:                     UUIDToString(suite.ID.Bytes),
		ProjectID:              UUIDToString(suite.ProjectID.Bytes),
		DatasetID:              UUIDToString(suite.DatasetID.Bytes),
		BenchmarkKey:           suite.BenchmarkKey,
		Name:                   suite.Name,
		Description:            suite.Description,
		Version:                suite.Version,
		HarnessKind:            suite.HarnessKind,
		ExecutionKind:          suite.ExecutionKind,
		SourceURI:              strOrZero(suite.SourceUri),
		SourceCommit:           strOrZero(suite.SourceCommit),
		DefaultExecutionConfig: json.RawMessage(suite.DefaultExecutionConfig),
		RequiredCapabilities:   capabilities,
		Metadata:               json.RawMessage(suite.Metadata),
		CreatedAt:              suite.CreatedAt.Time,
		UpdatedAt:              suite.UpdatedAt.Time,
	}, nil
}

func storeBenchmarkSuiteFromExperiment(suite store.BenchmarkSuite) (benchmarks.Suite, error) {
	return storeBenchmarkSuiteToDomain(suite)
}

func pgInt8FromPointer(value *int64) pgtype.Int8 {
	if value == nil {
		return pgtype.Int8{}
	}
	return pgtype.Int8{Int64: *value, Valid: true}
}
