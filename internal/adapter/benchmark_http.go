package adapter

import (
	"context"
	"encoding/json"

	"AgentScope/internal/benchmarks"
	"AgentScope/internal/httpapi"
)

// HTTPBenchmarkSuitesService adapts benchmark suite domain operations for the HTTP API.
type HTTPBenchmarkSuitesService struct {
	svc *benchmarks.Service
}

var _ httpapi.BenchmarkSuitesService = (*HTTPBenchmarkSuitesService)(nil)

func NewHTTPBenchmarkSuitesService(svc *benchmarks.Service) *HTTPBenchmarkSuitesService {
	return &HTTPBenchmarkSuitesService{svc: svc}
}

func (h *HTTPBenchmarkSuitesService) CreateSuite(ctx context.Context, params benchmarks.CreateSuiteParams) (httpapi.BenchmarkSuite, error) {
	suite, err := h.svc.CreateSuite(ctx, params)
	if err != nil {
		return httpapi.BenchmarkSuite{}, err
	}
	return benchmarkSuiteToHTTP(suite), nil
}

func (h *HTTPBenchmarkSuitesService) GetSuite(ctx context.Context, projectID, suiteID string) (httpapi.BenchmarkSuite, error) {
	suite, err := h.svc.GetSuite(ctx, projectID, suiteID)
	if err != nil {
		return httpapi.BenchmarkSuite{}, err
	}
	return benchmarkSuiteToHTTP(suite), nil
}

func (h *HTTPBenchmarkSuitesService) ListSuites(ctx context.Context, projectID string) ([]httpapi.BenchmarkSuite, error) {
	suites, err := h.svc.ListSuites(ctx, projectID)
	if err != nil {
		return nil, err
	}
	result := make([]httpapi.BenchmarkSuite, 0, len(suites))
	for _, suite := range suites {
		result = append(result, benchmarkSuiteToHTTP(suite))
	}
	return result, nil
}

func benchmarkSuiteToHTTP(suite benchmarks.Suite) httpapi.BenchmarkSuite {
	return httpapi.BenchmarkSuite{
		ID:                     suite.ID,
		ProjectID:              suite.ProjectID,
		DatasetID:              suite.DatasetID,
		BenchmarkKey:           suite.BenchmarkKey,
		Name:                   suite.Name,
		Description:            suite.Description,
		Version:                suite.Version,
		HarnessKind:            suite.HarnessKind,
		ExecutionKind:          suite.ExecutionKind,
		SourceURI:              suite.SourceURI,
		SourceCommit:           suite.SourceCommit,
		DefaultExecutionConfig: json.RawMessage(suite.DefaultExecutionConfig),
		RequiredCapabilities:   suite.RequiredCapabilities,
		Metadata:               json.RawMessage(suite.Metadata),
		CreatedAt:              suite.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"),
		UpdatedAt:              suite.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z"),
	}
}
