package benchmarks

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

type fakeSuiteQuerier struct {
	created CreateSuiteParams
	suite   Suite
}

func (f *fakeSuiteQuerier) CreateBenchmarkSuite(_ context.Context, params CreateSuiteParams) (Suite, error) {
	f.created = params
	result := f.suite
	result.BenchmarkKey = params.BenchmarkKey
	return result, nil
}
func (f *fakeSuiteQuerier) GetBenchmarkSuite(_ context.Context, _, _ string) (Suite, error) {
	if f.suite.ID == "" {
		return Suite{}, errors.New("not found")
	}
	return f.suite, nil
}
func (f *fakeSuiteQuerier) ListBenchmarkSuites(_ context.Context, _ string) ([]Suite, error) {
	return []Suite{f.suite}, nil
}

func TestCreateSuiteNormalizesExecutionContract(t *testing.T) {
	fake := &fakeSuiteQuerier{suite: Suite{ID: "suite-1"}}
	svc := NewService(fake)
	_, err := svc.CreateSuite(context.Background(), CreateSuiteParams{
		ProjectID: "project-1", DatasetID: "dataset-1", BenchmarkKey: "tau2_bench", Name: "Tau2",
		Version: "2026.1", HarnessKind: "tau2", ExecutionKind: "tau2",
		DefaultExecutionConfig: json.RawMessage(` { "timeout_seconds" : 60 } `),
		RequiredCapabilities:   []string{"structured_tools", "structured_tools", "multi_turn"},
	})
	if err != nil {
		t.Fatalf("CreateSuite() error = %v", err)
	}
	if got, want := string(fake.created.DefaultExecutionConfig), `{"timeout_seconds":60}`; got != want {
		t.Fatalf("config = %s, want %s", got, want)
	}
	if got, want := len(fake.created.RequiredCapabilities), 2; got != want {
		t.Fatalf("capabilities = %#v", fake.created.RequiredCapabilities)
	}
}

func TestCreateSuiteRejectsInvalidIdentifiers(t *testing.T) {
	svc := NewService(&fakeSuiteQuerier{})
	_, err := svc.CreateSuite(context.Background(), CreateSuiteParams{ProjectID: "p", DatasetID: "d", BenchmarkKey: "Tau2", Name: "x", Version: "v", HarnessKind: "tau2", ExecutionKind: "tau2"})
	if err == nil {
		t.Fatal("CreateSuite() error = nil, want invalid benchmark key")
	}
}
