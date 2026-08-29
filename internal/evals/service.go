package evals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"
)

// ---- Domain types ----

// MetricDefinition represents a registered metric definition for a project.
type MetricDefinition struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Config    string    `json:"config,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// RubricDimension represents a single dimension in a Rubric.
type RubricDimension struct {
	Name        string  `json:"name"`
	Weight      float64 `json:"weight"`
	Description string  `json:"description,omitempty"`
}

// ScoringLevel represents a scoring level definition.
type ScoringLevel struct {
	Level       int    `json:"level"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// RubricConfig represents a structured rubric for LLM-as-Judge evaluation.
type RubricConfig struct {
	Dimensions     []RubricDimension `json:"dimensions"`
	ScoringLevels  []ScoringLevel    `json:"scoring_levels,omitempty"`
	EssentialItems []string          `json:"essential_items,omitempty"`
	OptionalItems  []string          `json:"optional_items,omitempty"`
	PitfallItems   []string          `json:"pitfall_items,omitempty"`
	VetoItems      []string          `json:"veto_items,omitempty"`
	Examples       []interface{}     `json:"examples,omitempty"`
	EdgeCases      []string          `json:"edge_cases,omitempty"`
}

// MetricDefinitionConfig is the parsed configuration of a MetricDefinition.
type MetricDefinitionConfig struct {
	Type                string        `json:"type"`
	Weight              float64       `json:"weight,omitempty"`
	PassThreshold       float64       `json:"pass_threshold,omitempty"`
	Enabled             bool          `json:"enabled,omitempty"`
	Veto                bool          `json:"veto,omitempty"`
	Rubric              *RubricConfig `json:"rubric,omitempty"`
	JudgeProvider       string        `json:"judge_provider,omitempty"`
	JudgeModel          string        `json:"judge_model,omitempty"`
	CalibrationRequired bool          `json:"calibration_required,omitempty"`
}

// EvalScore represents a single multi-dimensional evaluation score.
type EvalScore struct {
	ID               string    `json:"id"`
	ProjectID        string    `json:"project_id"`
	RunID            string    `json:"run_id"`
	TraceID          string    `json:"trace_id"`
	SpanID           string    `json:"span_id,omitempty"`
	MetricName       string    `json:"metric_name"`
	MetricKind       string    `json:"metric_kind"`
	Score            float64   `json:"score"`
	Passed           bool      `json:"passed"`
	Reason           string    `json:"reason"`
	Details          string    `json:"details,omitempty"`
	EvaluatorRunID   string    `json:"evaluator_run_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	EvaluationStatus string    `json:"evaluation_status,omitempty"`
	EvidenceStatus   string    `json:"evidence_status,omitempty"`
}

// MetricKindAggregation represents aggregated scores by metric kind.
type MetricKindAggregation struct {
	MetricKind  string  `json:"metric_kind"`
	TotalCount  int64   `json:"total_count"`
	PassedCount int64   `json:"passed_count"`
	AvgScore    float64 `json:"avg_score"`
}

// Supported metric kinds
var SupportedMetricKinds = map[string]bool{
	"final_answer":         true,
	"trajectory":           true,
	"tool_correctness":     true,
	"argument_correctness": true,
	"rag_groundedness":     true,
	"safety":               true,
	"efficiency":           true,
	"helpfulness":          true,
}

// MaxEvalScoreBatchSize bounds a single batch write.
const MaxEvalScoreBatchSize = 500

var (
	ErrEmptyEvalScoreBatch    = errors.New("eval score batch must not be empty")
	ErrEvalScoreBatchTooLarge = errors.New("eval score batch exceeds maximum size")
)

// ---- Config validation ----

// ValidateMetricDefinitionConfig validates the config fields of a metric definition.
// It checks weight, pass_threshold, rubric structure, and veto settings.
func ValidateMetricDefinitionConfig(config string) error {
	if config == "" {
		return nil // empty config is allowed for simple metrics
	}
	var cfg MetricDefinitionConfig
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return fmt.Errorf("invalid config JSON: %w", err)
	}
	return validateMetricDefinitionConfig(cfg)
}

func validateMetricDefinitionConfig(cfg MetricDefinitionConfig) error {
	// Validate weight
	if !isFiniteUnitInterval(cfg.Weight) {
		return fmt.Errorf("weight must be in [0.0, 1.0], got %f", cfg.Weight)
	}

	// Validate pass_threshold
	if !isFiniteUnitInterval(cfg.PassThreshold) {
		return fmt.Errorf("pass_threshold must be in [0.0, 1.0], got %f", cfg.PassThreshold)
	}

	// Validate rubric structure if present
	if cfg.Rubric != nil {
		if err := validateRubricStructure(cfg.Rubric); err != nil {
			return fmt.Errorf("invalid rubric: %w", err)
		}
	}

	// Validate judge fields if present
	if cfg.JudgeProvider != "" {
		validProviders := map[string]bool{
			"openai-compatible":    true,
			"anthropic-compatible": true,
		}
		if !validProviders[cfg.JudgeProvider] {
			return fmt.Errorf("unsupported judge_provider %q", cfg.JudgeProvider)
		}
	}

	return nil
}

func isFiniteUnitInterval(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0.0 && value <= 1.0
}

func validateRubricStructure(r *RubricConfig) error {
	if r == nil {
		return errors.New("rubric config is nil")
	}
	if len(r.Dimensions) == 0 {
		return errors.New("rubric must have at least one dimension")
	}

	dimNames := make(map[string]bool)
	for _, d := range r.Dimensions {
		if d.Name == "" {
			return errors.New("each rubric dimension must have a name")
		}
		if dimNames[d.Name] {
			return fmt.Errorf("duplicate dimension name %q", d.Name)
		}
		dimNames[d.Name] = true
		if !isFiniteUnitInterval(d.Weight) {
			return fmt.Errorf("dimension %q weight must be in [0.0, 1.0]", d.Name)
		}
	}

	return nil // dimension weights don't need to sum to 1.0 for flexibility
}

// ---- Config validation ----

// ---- Querier interface ----

// Querier defines the database operations the evals service needs.
type Querier interface {
	CreateMetricDefinition(ctx context.Context, projectID, name, kind, config string) (MetricDefinition, error)
	UpdateMetricDefinition(ctx context.Context, projectID, definitionID, name, kind, config string) (MetricDefinition, error)
	DeleteMetricDefinition(ctx context.Context, projectID, definitionID string) error
	ListMetricDefinitions(ctx context.Context, projectID string) ([]MetricDefinition, error)
	GetMetricDefinition(ctx context.Context, definitionID string) (MetricDefinition, error)

	CreateEvalScore(ctx context.Context, score EvalScore) (EvalScore, error)
	CreateEvalScores(ctx context.Context, scores []EvalScore) ([]EvalScore, error)
	ListEvalScoresByRun(ctx context.Context, projectID, runID string) ([]EvalScore, error)
	ListEvalScoresByRuns(ctx context.Context, projectID string, runIDs []string) ([]EvalScore, error)
	ListEvalScoresByProject(ctx context.Context, projectID string) ([]EvalScore, error)
	AggregateEvalScoresByExperiment(ctx context.Context, projectID, experimentID string) ([]MetricKindAggregation, error)
	DeleteEvalScoresByRun(ctx context.Context, runID string) error
	DeleteEvalScore(ctx context.Context, projectID, scoreID string) error
	GetEvalScoreByID(ctx context.Context, scoreID string) (EvalScore, error)
}

// ---- Service ----

// Service provides eval score operations.
type Service struct {
	q Querier
}

// NewService creates a new evals Service.
func NewService(q Querier) *Service {
	return &Service{q: q}
}

// ---- Metric Definition methods ----

// CreateMetricDefinition creates a new metric definition with project isolation.
func (s *Service) CreateMetricDefinition(ctx context.Context, projectID, name, kind, config string) (MetricDefinition, error) {
	if projectID == "" {
		return MetricDefinition{}, errors.New("projectID is required")
	}
	if name == "" {
		return MetricDefinition{}, errors.New("name is required")
	}
	if kind == "" {
		return MetricDefinition{}, errors.New("kind is required")
	}
	if !SupportedMetricKinds[kind] {
		return MetricDefinition{}, fmt.Errorf("unsupported metric kind %q", kind)
	}

	// Validate config for rubric/weight/threshold
	if err := ValidateMetricDefinitionConfig(config); err != nil {
		return MetricDefinition{}, fmt.Errorf("config validation: %w", err)
	}

	return s.q.CreateMetricDefinition(ctx, projectID, name, kind, config)
}

// ListMetricDefinitions returns all metric definitions for a project.
func (s *Service) ListMetricDefinitions(ctx context.Context, projectID string) ([]MetricDefinition, error) {
	if projectID == "" {
		return nil, errors.New("projectID is required")
	}
	return s.q.ListMetricDefinitions(ctx, projectID)
}

// GetMetricDefinition returns a single metric definition by ID.
func (s *Service) GetMetricDefinition(ctx context.Context, definitionID string) (MetricDefinition, error) {
	if definitionID == "" {
		return MetricDefinition{}, errors.New("definitionID is required")
	}
	return s.q.GetMetricDefinition(ctx, definitionID)
}

// UpdateMetricDefinition updates a metric definition.
func (s *Service) UpdateMetricDefinition(ctx context.Context, projectID, definitionID, name, kind, config string) (MetricDefinition, error) {
	if projectID == "" {
		return MetricDefinition{}, errors.New("projectID is required")
	}
	if definitionID == "" {
		return MetricDefinition{}, errors.New("definitionID is required")
	}
	if name == "" {
		return MetricDefinition{}, errors.New("name is required")
	}
	if kind != "" && !SupportedMetricKinds[kind] {
		return MetricDefinition{}, fmt.Errorf("unsupported metric kind %q", kind)
	}

	// Validate config for rubric/weight/threshold
	if err := ValidateMetricDefinitionConfig(config); err != nil {
		return MetricDefinition{}, fmt.Errorf("config validation: %w", err)
	}

	return s.q.UpdateMetricDefinition(ctx, projectID, definitionID, name, kind, config)
}

// DeleteMetricDefinition deletes a metric definition.
func (s *Service) DeleteMetricDefinition(ctx context.Context, projectID, definitionID string) error {
	if projectID == "" {
		return errors.New("projectID is required")
	}
	if definitionID == "" {
		return errors.New("definitionID is required")
	}
	return s.q.DeleteMetricDefinition(ctx, projectID, definitionID)
}

// ---- Eval Score methods ----

// ValidateEvalScore validates fields shared by single and batch writes.
func ValidateEvalScore(score EvalScore) error {
	if score.ProjectID == "" {
		return errors.New("projectID is required")
	}
	if score.RunID == "" {
		return errors.New("runID is required")
	}
	if score.MetricName == "" {
		return errors.New("metricName is required")
	}
	if score.MetricKind == "" {
		return errors.New("metricKind is required")
	}
	if !SupportedMetricKinds[score.MetricKind] {
		return fmt.Errorf("unsupported metric kind %q", score.MetricKind)
	}
	if !isFiniteUnitInterval(score.Score) {
		return fmt.Errorf("score %f out of range [0.0, 1.0]", score.Score)
	}
	return nil
}

// CreateEvalScore creates a score with validation of project and run.
func (s *Service) CreateEvalScore(ctx context.Context, score EvalScore) (EvalScore, error) {
	if err := ValidateEvalScore(score); err != nil {
		return EvalScore{}, err
	}
	return s.q.CreateEvalScore(ctx, score)
}

// CreateEvalScores validates an entire bounded batch before issuing one
// atomic batch call to the querier.
func (s *Service) CreateEvalScores(ctx context.Context, scores []EvalScore) ([]EvalScore, error) {
	if len(scores) == 0 {
		return nil, ErrEmptyEvalScoreBatch
	}
	if len(scores) > MaxEvalScoreBatchSize {
		return nil, ErrEvalScoreBatchTooLarge
	}
	for i, score := range scores {
		if err := ValidateEvalScore(score); err != nil {
			return nil, fmt.Errorf("score %d: %w", i, err)
		}
	}
	return s.q.CreateEvalScores(ctx, scores)
}

// ListEvalScoresByRun returns all eval scores for a run.
func (s *Service) ListEvalScoresByRun(ctx context.Context, projectID, runID string) ([]EvalScore, error) {
	if projectID == "" {
		return nil, errors.New("projectID is required")
	}
	if runID == "" {
		return nil, errors.New("runID is required")
	}
	return s.q.ListEvalScoresByRun(ctx, projectID, runID)
}

// ListEvalScoresByRuns returns eval scores for multiple runs (avoids N+1).
func (s *Service) ListEvalScoresByRuns(ctx context.Context, projectID string, runIDs []string) ([]EvalScore, error) {
	if projectID == "" {
		return nil, errors.New("projectID is required")
	}
	if len(runIDs) == 0 {
		return nil, errors.New("runIDs is required")
	}
	return s.q.ListEvalScoresByRuns(ctx, projectID, runIDs)
}

// ListEvalScoresByProject returns all eval scores for a project.
func (s *Service) ListEvalScoresByProject(ctx context.Context, projectID string) ([]EvalScore, error) {
	if projectID == "" {
		return nil, errors.New("projectID is required")
	}
	return s.q.ListEvalScoresByProject(ctx, projectID)
}

// AggregateEvalScoresByExperiment aggregates scores by metric kind for a given experiment.
func (s *Service) AggregateEvalScoresByExperiment(ctx context.Context, projectID, experimentID string) ([]MetricKindAggregation, error) {
	if projectID == "" {
		return nil, errors.New("projectID is required")
	}
	if experimentID == "" {
		return nil, errors.New("experimentID is required")
	}
	return s.q.AggregateEvalScoresByExperiment(ctx, projectID, experimentID)
}

// GetEvalScore returns a single eval score by ID.
func (s *Service) GetEvalScore(ctx context.Context, scoreID string) (EvalScore, error) {
	if scoreID == "" {
		return EvalScore{}, errors.New("scoreID is required")
	}
	return s.q.GetEvalScoreByID(ctx, scoreID)
}

// DeleteEvalScoresByRun deletes all eval scores for a run.
func (s *Service) DeleteEvalScoresByRun(ctx context.Context, runID string) error {
	if runID == "" {
		return errors.New("runID is required")
	}
	return s.q.DeleteEvalScoresByRun(ctx, runID)
}

// DeleteEvalScore deletes a single eval score.
func (s *Service) DeleteEvalScore(ctx context.Context, projectID, scoreID string) error {
	if projectID == "" {
		return errors.New("projectID is required")
	}
	if scoreID == "" {
		return errors.New("scoreID is required")
	}
	return s.q.DeleteEvalScore(ctx, projectID, scoreID)
}
