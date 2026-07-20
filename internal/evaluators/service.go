package evaluators

import (
	"fmt"
	"regexp"
	"strings"
)

// Supported evaluator types.
var supportedTypes = map[string]bool{
	"exact":              true,
	"contains":           true,
	"not_contains":       true,
	"regex":              true,
	"numeric_threshold":  true,
	"latency_threshold":  true,
	"llm_judge":          true,
	"multi":              true,
}

// Supported LLM judge providers.
var supportedJudgeProviders = map[string]bool{
	"openai-compatible":   true,
	"anthropic-compatible": true,
}

// validEnvNameRegex matches environment variable names: uppercase letters,
// digits, and underscores, starting with a letter or underscore.
var validEnvNameRegex = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// ValidateEvaluatorConfig validates an evaluator configuration map.
//
// For rule-based evaluators (exact, contains, etc.), only the "type" field
// is checked for validity.
//
// For "llm_judge" evaluators, additional constraints are enforced:
//   - provider must be "openai-compatible" or "anthropic-compatible"
//   - model, api_key_env, and rubric are required
//   - api_key_env must be a valid environment variable name and must not
//     look like a plaintext API key
//   - pass_threshold (if set) must be in [0.0, 1.0]
//   - temperature (if set) must be >= 0
//   - max_tokens (if set) must be > 0
func ValidateEvaluatorConfig(cfg map[string]any) error {
	evalType, ok := cfg["type"].(string)
	if !ok || evalType == "" {
		return fmt.Errorf("evaluator config: missing or invalid 'type' field")
	}

	if !supportedTypes[evalType] {
		return fmt.Errorf("evaluator config: unsupported type %q", evalType)
	}

	if evalType == "llm_judge" {
		return validateLLMJudgeConfig(cfg)
	}

	return nil
}

func validateLLMJudgeConfig(cfg map[string]any) error {
	// Validate provider
	provider, ok := cfg["provider"].(string)
	if !ok || provider == "" {
		return fmt.Errorf("llm_judge config: missing or empty 'provider' field")
	}
	if !supportedJudgeProviders[provider] {
		return fmt.Errorf(
			"llm_judge config: unsupported provider %q (must be %s or %s)",
			provider, "openai-compatible", "anthropic-compatible",
		)
	}

	// Validate model
	model, ok := cfg["model"].(string)
	if !ok || model == "" {
		return fmt.Errorf("llm_judge config: missing or empty 'model' field")
	}
	_ = model

	// Validate api_key_env
	apiKeyEnv, ok := cfg["api_key_env"].(string)
	if !ok || apiKeyEnv == "" {
		return fmt.Errorf("llm_judge config: missing or empty 'api_key_env' field")
	}
	if err := validateAPIKeyEnv(apiKeyEnv, cfg); err != nil {
		return err
	}

	// Validate rubric
	rubric, ok := cfg["rubric"].(string)
	if !ok || rubric == "" {
		return fmt.Errorf("llm_judge config: missing or empty 'rubric' field")
	}
	_ = rubric

	// Validate optional fields
	if err := validateFloatInRange(cfg, "pass_threshold", 0.0, 1.0); err != nil {
		return err
	}
	if err := validateFloatMin(cfg, "temperature", 0.0); err != nil {
		return err
	}
	if err := validateIntPositive(cfg, "max_tokens"); err != nil {
		return err
	}

	return nil
}

// validateAPIKeyEnv checks that the api_key_env value is a valid environment
// variable name and does not look like a plaintext API key. The error message
// must not contain the value itself to avoid leaking secrets.
func validateAPIKeyEnv(value string, cfg map[string]any) error {
	// Reject values that look like plaintext API keys
	if strings.HasPrefix(value, "sk-") {
		return fmt.Errorf("llm_judge config: 'api_key_env' must be an environment variable name, not a key value")
	}
	if strings.HasPrefix(value, "sk-ant-") {
		return fmt.Errorf("llm_judge config: 'api_key_env' must be an environment variable name, not a key value")
	}
	if strings.ContainsAny(value, " \t\n\r") {
		return fmt.Errorf("llm_judge config: 'api_key_env' contains whitespace")
	}

	// Validate that it's a valid environment variable name format
	if !validEnvNameRegex.MatchString(value) {
		return fmt.Errorf("llm_judge config: 'api_key_env' must be a valid environment variable name (uppercase letters, digits, underscores)")
	}

	return nil
}

// toFloat64 extracts a float64 from a map value that may be float64, int, or int64.
func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

// validateFloatInRange checks that a map key is numeric and in [min, max].
func validateFloatInRange(cfg map[string]any, key string, min, max float64) error {
	v, ok := cfg[key]
	if !ok {
		return nil // optional, not present
	}
	f, ok := toFloat64(v)
	if !ok {
		return fmt.Errorf("llm_judge config: '%s' must be a number", key)
	}
	if f < min || f > max {
		return fmt.Errorf(
			"llm_judge config: '%s' must be between %v and %v, got %v",
			key, min, max, f,
		)
	}
	return nil
}

// validateFloatMin checks that a map key is numeric and >= min.
func validateFloatMin(cfg map[string]any, key string, min float64) error {
	v, ok := cfg[key]
	if !ok {
		return nil // optional, not present
	}
	f, ok := toFloat64(v)
	if !ok {
		return fmt.Errorf("llm_judge config: '%s' must be a number", key)
	}
	if f < min {
		return fmt.Errorf(
			"llm_judge config: '%s' must be >= %v, got %v",
			key, min, f,
		)
	}
	return nil
}

// validateIntPositive checks that a map key is numeric and > 0.
func validateIntPositive(cfg map[string]any, key string) error {
	v, ok := cfg[key]
	if !ok {
		return nil // optional, not present
	}
	f, ok := toFloat64(v)
	if !ok {
		return fmt.Errorf("llm_judge config: '%s' must be a number", key)
	}
	if f <= 0 {
		return fmt.Errorf(
			"llm_judge config: '%s' must be positive, got %v",
			key, f,
		)
	}
	return nil
}
