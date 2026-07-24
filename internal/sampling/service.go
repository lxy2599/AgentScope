package sampling

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
)

const hashSeed = "AgentScope:sampling:v1"

var (
	ErrInvalidSampleRate = errors.New("sample_rate must be between 0.0 and 1.0")
	ErrInvalidFilters    = errors.New("filters must be a valid JSON object")
	ErrInvalidEnabled    = errors.New("enabled must be a boolean")
	ErrNameRequired      = errors.New("name is required")
	ErrNoUpdateFields    = errors.New("at least one sampling rule field is required")
	ErrRuleNotFound      = errors.New("sampling rule not found")
)

// ---- Domain types ----

// SamplingRule represents a trace sampling rule.
type SamplingRule struct {
	ID             string  `json:"id"`
	ProjectID      string  `json:"project_id"`
	AgentID        string  `json:"agent_id,omitempty"`
	Name           string  `json:"name"`
	Enabled        bool    `json:"enabled"`
	SampleRate     float32 `json:"sample_rate"`
	Filters        string  `json:"filters"`
	Strategy       string  `json:"strategy"`
	DailyBudget    int     `json:"daily_budget"`
	Stratification string  `json:"stratification"`
}

// CreateRuleParams contains the fields needed to create a sampling rule.
type CreateRuleParams struct {
	ProjectID      string
	AgentID        string
	Name           string
	SampleRate     float32
	Filters        string
	Strategy       string
	DailyBudget    int
	Stratification string
}

// UpdateRuleParams contains optional fields for a partial sampling rule update.
// A non-nil field is updated; a nil field is left unchanged.
type UpdateRuleParams struct {
	Name       *string
	Enabled    *bool
	SampleRate *float32
	Filters    *string
}

// Querier defines the database operations the sampling service needs.
type Querier interface {
	CreateSamplingRule(ctx context.Context, params CreateRuleParams) (SamplingRule, error)
	ListSamplingRulesByProject(ctx context.Context, projectID string) ([]SamplingRule, error)
	GetSamplingRuleByID(ctx context.Context, ruleID string) (SamplingRule, error)
	UpdateSamplingRuleByProject(ctx context.Context, projectID, ruleID string, params UpdateRuleParams) (SamplingRule, error)
	DeleteSamplingRule(ctx context.Context, ruleID string) error
	ListEnabledRulesForRun(ctx context.Context, projectID, agentID string) ([]SamplingRule, error)
	ReserveSamplingDecision(ctx context.Context, ruleID, runID, stratum string, dailyBudget, stratumBudget int) (bool, error)
}

// Service provides sampling rule operations.
type Service struct {
	q Querier
}

// NewService creates a new sampling Service.
func NewService(q Querier) *Service {
	return &Service{
		q: q,
	}
}

// ValidateCreateRuleParams validates and normalizes sampling rule creation input.
func ValidateCreateRuleParams(params CreateRuleParams) (CreateRuleParams, error) {
	if params.ProjectID == "" {
		return CreateRuleParams{}, errors.New("projectID is required")
	}
	if strings.TrimSpace(params.Name) == "" {
		return CreateRuleParams{}, ErrNameRequired
	}
	if err := validateSampleRate(params.SampleRate); err != nil {
		return CreateRuleParams{}, err
	}
	filters, err := normalizeFilters(params.Filters)
	if err != nil {
		return CreateRuleParams{}, err
	}
	params.Filters = filters
	return params, nil
}

// ValidateUpdateRuleParams validates and normalizes a partial sampling rule update.
func ValidateUpdateRuleParams(params UpdateRuleParams) (UpdateRuleParams, error) {
	if params.Name == nil && params.Enabled == nil && params.SampleRate == nil && params.Filters == nil {
		return UpdateRuleParams{}, ErrNoUpdateFields
	}
	if params.Name != nil && strings.TrimSpace(*params.Name) == "" {
		return UpdateRuleParams{}, ErrNameRequired
	}
	if params.SampleRate != nil {
		if err := validateSampleRate(*params.SampleRate); err != nil {
			return UpdateRuleParams{}, err
		}
	}
	if params.Filters != nil {
		filters, err := normalizeFilters(*params.Filters)
		if err != nil {
			return UpdateRuleParams{}, err
		}
		params.Filters = &filters
	}
	return params, nil
}

// IsValidationError reports whether err is a stable client input error.
func IsValidationError(err error) bool {
	return errors.Is(err, ErrInvalidSampleRate) ||
		errors.Is(err, ErrInvalidFilters) ||
		errors.Is(err, ErrInvalidEnabled) ||
		errors.Is(err, ErrNameRequired) ||
		errors.Is(err, ErrNoUpdateFields)
}

func validateSampleRate(sampleRate float32) error {
	rate := float64(sampleRate)
	if math.IsNaN(rate) || math.IsInf(rate, 0) || sampleRate < 0.0 || sampleRate > 1.0 {
		return ErrInvalidSampleRate
	}
	return nil
}

func normalizeFilters(filters string) (string, error) {
	filters = strings.TrimSpace(filters)
	if filters == "" {
		return "{}", nil
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(filters), &object); err != nil || object == nil {
		return "", ErrInvalidFilters
	}
	return filters, nil
}

// CreateRule creates a new sampling rule with validation.
func (s *Service) CreateRule(ctx context.Context, params CreateRuleParams) (SamplingRule, error) {
	params, err := ValidateCreateRuleParams(params)
	if err != nil {
		return SamplingRule{}, err
	}
	if params.Strategy == "" {
		params.Strategy = "random"
	}
	if !supportedStrategies[params.Strategy] {
		return SamplingRule{}, errors.New("unsupported sampling strategy")
	}
	if params.DailyBudget < 0 {
		return SamplingRule{}, errors.New("daily_budget cannot be negative")
	}
	if params.Stratification == "" {
		params.Stratification = "{}"
	}
	if params.Filters == "" {
		params.Filters = "{}"
	}
	if !json.Valid([]byte(params.Stratification)) {
		return SamplingRule{}, errors.New("stratification must be valid JSON")
	}
	return s.q.CreateSamplingRule(ctx, params)
}

// ListRules returns all sampling rules for a project.
func (s *Service) ListRules(ctx context.Context, projectID string) ([]SamplingRule, error) {
	if projectID == "" {
		return nil, errors.New("projectID is required")
	}
	return s.q.ListSamplingRulesByProject(ctx, projectID)
}

// GetRule returns a single sampling rule by ID.
func (s *Service) GetRule(ctx context.Context, ruleID string) (SamplingRule, error) {
	if ruleID == "" {
		return SamplingRule{}, errors.New("ruleID is required")
	}
	return s.q.GetSamplingRuleByID(ctx, ruleID)
}

// UpdateRule atomically updates the explicitly provided fields of a sampling rule.
func (s *Service) UpdateRule(ctx context.Context, projectID, ruleID string, params UpdateRuleParams) (SamplingRule, error) {
	if projectID == "" {
		return SamplingRule{}, errors.New("projectID is required")
	}
	if ruleID == "" {
		return SamplingRule{}, errors.New("ruleID is required")
	}
	params, err := ValidateUpdateRuleParams(params)
	if err != nil {
		return SamplingRule{}, err
	}
	return s.q.UpdateSamplingRuleByProject(ctx, projectID, ruleID, params)
}

// DeleteRule deletes a sampling rule.
func (s *Service) DeleteRule(ctx context.Context, ruleID string) error {
	if ruleID == "" {
		return errors.New("ruleID is required")
	}
	return s.q.DeleteSamplingRule(ctx, ruleID)
}

// ShouldSample determines if a run should be sampled based on enabled rules.
// Returns (true, matchedRule) if the run should be sampled, (false, nil) otherwise.
// For rules with sample_rate < 1.0, random sampling is applied.
func (s *Service) ShouldSample(ctx context.Context, projectID, agentID string, runFilters map[string]any) (bool, *SamplingRule, error) {
	if projectID == "" {
		return false, nil, errors.New("projectID is required")
	}

	rules, err := s.q.ListEnabledRulesForRun(ctx, projectID, agentID)
	if err != nil {
		return false, nil, fmt.Errorf("list enabled rules: %w", err)
	}

	for _, rule := range rules {
		if !matchFilters(rule.Filters, runFilters) {
			continue
		}
		if !matchStrategy(rule.Strategy, rule.Filters, runFilters) {
			continue
		}

		// Sample rate check
		if rule.SampleRate <= 0.0 {
			continue
		}
		if rule.SampleRate < 1.0 && sampleHash(projectID, agentID, rule.ID, runFilters) >= rule.SampleRate {
			continue
		}

		runID, _ := runFilters["run_id"].(string)
		if runID == "" {
			return true, &rule, nil
		}
		stratum, stratumBudget := resolveStratum(rule.Stratification, runFilters)
		reserved, err := s.q.ReserveSamplingDecision(
			ctx, rule.ID, runID, stratum, rule.DailyBudget, stratumBudget,
		)
		if err != nil {
			return false, nil, fmt.Errorf("reserve sampling budget: %w", err)
		}
		if reserved {
			return true, &rule, nil
		}
	}

	return false, nil, nil
}

var supportedStrategies = map[string]bool{
	"random": true, "failure": true, "high_cost": true,
	"low_confidence": true, "high_disagreement": true, "combined": true,
}

func matchStrategy(strategy, filtersJSON string, runFilters map[string]any) bool {
	if strategy == "" || strategy == "random" {
		return true
	}
	filters := decodeNumberMap(filtersJSON)
	failure := runFilters["status"] != "completed" || stringValue(runFilters["error"]) != ""
	highCost := numberValue(runFilters["cost"]) >= numberDefault(filters, "min_cost", 1)
	lowConfidence := numberValue(runFilters["confidence"]) <= numberDefault(filters, "max_confidence", 0.6)
	highDisagreement := numberValue(runFilters["disagreement"]) >= numberDefault(filters, "min_disagreement", 0.3)
	switch strategy {
	case "failure":
		return failure
	case "high_cost":
		return highCost
	case "low_confidence":
		return lowConfidence
	case "high_disagreement":
		return highDisagreement
	case "combined":
		return failure || highCost || lowConfidence || highDisagreement
	default:
		return false
	}
}

type stratificationConfig struct {
	Field   string         `json:"field"`
	Budgets map[string]int `json:"budgets"`
}

func resolveStratum(raw string, runFilters map[string]any) (string, int) {
	var config stratificationConfig
	if json.Unmarshal([]byte(raw), &config) != nil || config.Field == "" {
		return "", 0
	}
	value := fmt.Sprint(runFilters[config.Field])
	return value, config.Budgets[value]
}

func decodeNumberMap(raw string) map[string]any {
	result := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &result)
	return result
}

func numberDefault(values map[string]any, key string, fallback float64) float64 {
	if value, ok := values[key].(float64); ok {
		return value
	}
	return fallback
}

func numberValue(value any) float64 {
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	default:
		return 0
	}
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

// sampleHash produces a deterministic [0, 1) float32 from the given inputs
// using SHA-256. It is concurrent-safe and reproducible.
func sampleHash(projectID, agentID, ruleID string, runFilters map[string]any) float32 {
	// Build a deterministic input string
	input := hashSeed + "|" + projectID + "|" + agentID + "|" + ruleID

	// Include run_id and trace_id for per-run uniqueness
	if runID, ok := runFilters["run_id"].(string); ok && runID != "" {
		input += "|run:" + runID
	}
	if traceID, ok := runFilters["trace_id"].(string); ok && traceID != "" {
		input += "|trace:" + traceID
	}

	// Hash the input
	h := sha256.Sum256([]byte(input))

	// Use the first 8 bytes as a uint64, then normalize to [0, 1)
	val := binary.BigEndian.Uint64(h[:8])
	return float32(float64(val) / float64(math.MaxUint64))
}

// matchFilters checks if the rule's filter criteria match the run's attributes.
// The rule filters are a JSON object with optional fields:
//   - status: string - match run status
//   - agent_id: string - match agent ID
//   - min_latency_ms: number - minimum latency threshold
//   - has_error: boolean - whether run has an error
func matchFilters(ruleFiltersJSON string, runFilters map[string]any) bool {
	if ruleFiltersJSON == "" || ruleFiltersJSON == "{}" || ruleFiltersJSON == "null" {
		return true
	}

	var ruleFilters map[string]any
	if err := json.Unmarshal([]byte(ruleFiltersJSON), &ruleFilters); err != nil {
		return true // Invalid filters default to pass-through
	}

	if len(ruleFilters) == 0 {
		return true
	}

	// Check status filter
	if status, ok := ruleFilters["status"].(string); ok && status != "" {
		runStatus, _ := runFilters["status"].(string)
		if runStatus != status {
			return false
		}
	}

	// Check min_latency_ms filter
	if minLatency, ok := ruleFilters["min_latency_ms"].(float64); ok && minLatency > 0 {
		runLatency, ok := runFilters["latency_ms"].(float64)
		if !ok {
			// If run has no latency and filter requires min, skip
			return false
		}
		if runLatency < minLatency {
			return false
		}
	}

	// Check has_error filter
	if hasErr, ok := ruleFilters["has_error"].(bool); ok {
		runErr, hasRunErr := runFilters["error"].(string)
		if hasErr && (!hasRunErr || runErr == "") {
			return false
		}
		if !hasErr && hasRunErr && runErr != "" {
			return false
		}
	}

	return true
}
