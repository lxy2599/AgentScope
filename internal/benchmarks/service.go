// Package benchmarks contains benchmark suite and result domain types.
package benchmarks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

const FunctionExecutionKind = "function"

type Target struct {
	Name         string   `json:"name"`
	Version      string   `json:"version,omitempty"`
	Description  string   `json:"description,omitempty"`
	Framework    string   `json:"framework,omitempty"`
	Status       string   `json:"status,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

func DefaultTargets() []Target {
	return []Target{
		{Name: "openhands", Framework: "openhands", Status: "available", Description: "OpenHands software engineering agent", Capabilities: []string{"shell", "filesystem", "git", "structured_tools", "terminal"}},
		{Name: "openclaw", Framework: "openclaw", Status: "available", Description: "OpenClaw tool-using agent", Capabilities: []string{"shell", "filesystem", "structured_tools", "multi_turn"}},
	}
}

func FindTarget(name, version string) (Target, bool) {
	for _, target := range DefaultTargets() {
		if target.Name == name && (version == "" || version == target.Version) {
			return target, true
		}
	}
	return Target{}, false
}

func MatchCapabilities(required, provided []string) (matched, missing []string) {
	providedSet := make(map[string]struct{}, len(provided))
	for _, capability := range provided {
		providedSet[capability] = struct{}{}
	}
	for _, capability := range required {
		if _, ok := providedSet[capability]; ok {
			matched = append(matched, capability)
		} else {
			missing = append(missing, capability)
		}
	}
	return matched, missing
}

// Suite is a project-scoped benchmark definition backed by an existing dataset.
// The suite records the upstream harness identity and the default execution
// contract; an experiment snapshots that information before running jobs.
type Suite struct {
	ID                     string          `json:"id"`
	ProjectID              string          `json:"project_id"`
	DatasetID              string          `json:"dataset_id"`
	BenchmarkKey           string          `json:"benchmark_key"`
	Name                   string          `json:"name"`
	Description            string          `json:"description,omitempty"`
	Version                string          `json:"version"`
	HarnessKind            string          `json:"harness_kind"`
	ExecutionKind          string          `json:"execution_kind"`
	SourceURI              string          `json:"source_uri,omitempty"`
	SourceCommit           string          `json:"source_commit,omitempty"`
	DefaultExecutionConfig json.RawMessage `json:"default_execution_config"`
	RequiredCapabilities   []string        `json:"required_capabilities"`
	Metadata               json.RawMessage `json:"metadata"`
	CreatedAt              time.Time       `json:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"`
}

// Snapshot returns the suite values that must remain stable for a benchmark
// experiment even if the project later edits or replaces its suite definition.
func (s Suite) Snapshot() (json.RawMessage, error) {
	config, err := NormalizeJSONObject(s.DefaultExecutionConfig, "default_execution_config")
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"harness_kind":             s.HarnessKind,
		"execution_kind":           s.ExecutionKind,
		"source_uri":               s.SourceURI,
		"source_commit":            s.SourceCommit,
		"default_execution_config": json.RawMessage(config),
		"required_capabilities":    s.RequiredCapabilities,
		"metadata":                 json.RawMessage(s.Metadata),
	})
}

// CreateSuiteParams contains the project-owned inputs for a benchmark suite.
type CreateSuiteParams struct {
	ProjectID              string
	DatasetID              string
	BenchmarkKey           string
	Name                   string
	Description            string
	Version                string
	HarnessKind            string
	ExecutionKind          string
	SourceURI              string
	SourceCommit           string
	DefaultExecutionConfig json.RawMessage
	RequiredCapabilities   []string
	Metadata               json.RawMessage
}

// Querier is the persistence boundary used by Service.
type Querier interface {
	CreateBenchmarkSuite(ctx context.Context, params CreateSuiteParams) (Suite, error)
	GetBenchmarkSuite(ctx context.Context, projectID, suiteID string) (Suite, error)
	ListBenchmarkSuites(ctx context.Context, projectID string) ([]Suite, error)
}

// Service provides benchmark suite management operations.
type Service struct {
	q Querier
}

func NewService(q Querier) *Service {
	return &Service{q: q}
}

func (s *Service) CreateSuite(ctx context.Context, params CreateSuiteParams) (Suite, error) {
	if strings.TrimSpace(params.ProjectID) == "" {
		return Suite{}, errors.New("projectID is required")
	}
	if strings.TrimSpace(params.DatasetID) == "" {
		return Suite{}, errors.New("datasetID is required")
	}
	if !identifierPattern.MatchString(params.BenchmarkKey) {
		return Suite{}, errors.New("benchmark_key must be a lowercase identifier")
	}
	if strings.TrimSpace(params.Name) == "" {
		return Suite{}, errors.New("name is required")
	}
	if strings.TrimSpace(params.Version) == "" {
		return Suite{}, errors.New("version is required")
	}
	if !identifierPattern.MatchString(params.HarnessKind) {
		return Suite{}, errors.New("harness_kind must be a lowercase identifier")
	}
	if err := ValidateExecutionKind(params.ExecutionKind); err != nil {
		return Suite{}, err
	}

	config, err := NormalizeJSONObject(params.DefaultExecutionConfig, "default_execution_config")
	if err != nil {
		return Suite{}, err
	}
	metadata, err := NormalizeJSONObject(params.Metadata, "metadata")
	if err != nil {
		return Suite{}, err
	}
	capabilities, err := NormalizeCapabilities(params.RequiredCapabilities)
	if err != nil {
		return Suite{}, err
	}

	params.DefaultExecutionConfig = config
	params.Metadata = metadata
	params.RequiredCapabilities = capabilities
	suite, err := s.q.CreateBenchmarkSuite(ctx, params)
	if err != nil {
		return Suite{}, fmt.Errorf("create benchmark suite: %w", err)
	}
	return suite, nil
}

func (s *Service) GetSuite(ctx context.Context, projectID, suiteID string) (Suite, error) {
	if strings.TrimSpace(projectID) == "" {
		return Suite{}, errors.New("projectID is required")
	}
	if strings.TrimSpace(suiteID) == "" {
		return Suite{}, errors.New("suiteID is required")
	}
	suite, err := s.q.GetBenchmarkSuite(ctx, projectID, suiteID)
	if err != nil {
		return Suite{}, fmt.Errorf("get benchmark suite: %w", err)
	}
	return suite, nil
}

func (s *Service) ListSuites(ctx context.Context, projectID string) ([]Suite, error) {
	if strings.TrimSpace(projectID) == "" {
		return nil, errors.New("projectID is required")
	}
	suites, err := s.q.ListBenchmarkSuites(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list benchmark suites: %w", err)
	}
	return suites, nil
}

// ValidateExecutionKind validates a worker execution kind without restricting
// callers to a fixed registry. This allows new official harness adapters to be
// introduced without schema changes while preserving a stable identifier shape.
func ValidateExecutionKind(kind string) error {
	if !identifierPattern.MatchString(kind) {
		return errors.New("execution_kind must be a lowercase identifier")
	}
	return nil
}

// NormalizeJSONObject validates and compacts a JSON object. Empty input is
// represented by an empty object, which matches the database defaults.
func NormalizeJSONObject(raw json.RawMessage, field string) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var value map[string]json.RawMessage
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmt.Errorf("%s must be a JSON object: %w", field, err)
	}
	var normalized bytes.Buffer
	if err := json.Compact(&normalized, raw); err != nil {
		return nil, fmt.Errorf("compact %s: %w", field, err)
	}
	return normalized.Bytes(), nil
}

// NormalizeCapabilities validates, deduplicates, and preserves the order of
// required worker capability names.
func NormalizeCapabilities(capabilities []string) ([]string, error) {
	if len(capabilities) == 0 {
		return []string{}, nil
	}
	seen := make(map[string]struct{}, len(capabilities))
	result := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		if !identifierPattern.MatchString(capability) {
			return nil, fmt.Errorf("required capability %q must be a lowercase identifier", capability)
		}
		if _, ok := seen[capability]; ok {
			continue
		}
		seen[capability] = struct{}{}
		result = append(result, capability)
	}
	return result, nil
}
