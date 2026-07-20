package evaluators

import (
	"strings"
	"testing"
)

func TestValidateExactConfig(t *testing.T) {
	cfg := map[string]any{
		"type": "exact",
	}
	err := ValidateEvaluatorConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateContainsConfig(t *testing.T) {
	cfg := map[string]any{
		"type": "contains",
	}
	err := ValidateEvaluatorConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateNotContainsConfig(t *testing.T) {
	cfg := map[string]any{
		"type": "not_contains",
	}
	err := ValidateEvaluatorConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateRegexConfig(t *testing.T) {
	cfg := map[string]any{
		"type": "regex",
	}
	err := ValidateEvaluatorConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateNumericThresholdConfig(t *testing.T) {
	cfg := map[string]any{
		"type": "numeric_threshold",
	}
	err := ValidateEvaluatorConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateLatencyThresholdConfig(t *testing.T) {
	cfg := map[string]any{
		"type": "latency_threshold",
	}
	err := ValidateEvaluatorConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateUnknownType(t *testing.T) {
	cfg := map[string]any{
		"type": "unknown_type",
	}
	err := ValidateEvaluatorConfig(cfg)
	if err == nil {
		t.Fatal("expected error for unknown type, got nil")
	}
}

func TestValidateMissingType(t *testing.T) {
	cfg := map[string]any{}
	err := ValidateEvaluatorConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing type, got nil")
	}
}

// ---- LLM judge validation ----

func TestValidateLLMJudgeValid(t *testing.T) {
	cfg := map[string]any{
		"type":         "llm_judge",
		"provider":     "openai-compatible",
		"model":        "gpt-4o",
		"api_key_env":  "OPENAI_API_KEY",
		"rubric":       "be correct",
	}
	err := ValidateEvaluatorConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateLLMJudgeAnthropic(t *testing.T) {
	cfg := map[string]any{
		"type":        "llm_judge",
		"provider":    "anthropic-compatible",
		"model":       "claude-sonnet-4-20250514",
		"api_key_env": "ANTHROPIC_API_KEY",
		"rubric":      "be accurate",
	}
	err := ValidateEvaluatorConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error for anthropic provider, got: %v", err)
	}
}

func TestValidateLLMJudgeInvalidProvider(t *testing.T) {
	cfg := map[string]any{
		"type":        "llm_judge",
		"provider":    "google-compatible",
		"model":       "gemini-pro",
		"api_key_env": "GOOGLE_API_KEY",
		"rubric":      "be correct",
	}
	err := ValidateEvaluatorConfig(cfg)
	if err == nil {
		t.Fatal("expected error for invalid provider, got nil")
	}
}

func TestValidateLLMJudgeMissingModel(t *testing.T) {
	cfg := map[string]any{
		"type":        "llm_judge",
		"provider":    "openai-compatible",
		"api_key_env": "OPENAI_API_KEY",
		"rubric":      "be correct",
	}
	err := ValidateEvaluatorConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing model, got nil")
	}
}

func TestValidateLLMJudgeMissingAPIKeyEnv(t *testing.T) {
	cfg := map[string]any{
		"type":     "llm_judge",
		"provider": "openai-compatible",
		"model":    "gpt-4o",
		"rubric":   "be correct",
	}
	err := ValidateEvaluatorConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing api_key_env, got nil")
	}
}

func TestValidateLLMJudgeAPIKeyEnvLooksLikePlaintextKey(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{"starts with sk-", "sk-proj-abc123"},
		{"starts with sk-ant-", "sk-ant-abc123"},
		{"contains space", "OPENAI KEY"},
		{"contains newline", "OPENAI_KEY\n"},
		{"contains tab", "OPENAI\tKEY"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := map[string]any{
				"type":        "llm_judge",
				"provider":    "openai-compatible",
				"model":       "gpt-4o",
				"api_key_env": tt.value,
				"rubric":      "be correct",
			}
			err := ValidateEvaluatorConfig(cfg)
			if err == nil {
				t.Fatalf("expected error for api_key_env %q, got nil", tt.value)
			}
		})
	}
}

func TestValidateLLMJudgeAPIKeyEnvValidNames(t *testing.T) {
	tests := []string{
		"OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
		"MY_CUSTOM_KEY_123",
		"A",
		"A1",
	}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			cfg := map[string]any{
				"type":        "llm_judge",
				"provider":    "openai-compatible",
				"model":       "gpt-4o",
				"api_key_env": name,
				"rubric":      "be correct",
			}
			err := ValidateEvaluatorConfig(cfg)
			if err != nil {
				t.Fatalf("expected no error for api_key_env %q, got: %v", name, err)
			}
		})
	}
}

func TestValidateLLMJudgeMissingRubric(t *testing.T) {
	cfg := map[string]any{
		"type":        "llm_judge",
		"provider":    "openai-compatible",
		"model":       "gpt-4o",
		"api_key_env": "OPENAI_API_KEY",
	}
	err := ValidateEvaluatorConfig(cfg)
	if err == nil {
		t.Fatal("expected error for missing rubric, got nil")
	}
}

func TestValidateLLMJudgePassThresholdInRange(t *testing.T) {
	cfg := map[string]any{
		"type":           "llm_judge",
		"provider":       "openai-compatible",
		"model":          "gpt-4o",
		"api_key_env":    "OPENAI_API_KEY",
		"rubric":         "be correct",
		"pass_threshold": 0.7,
	}
	err := ValidateEvaluatorConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error for valid pass_threshold, got: %v", err)
	}
}

func TestValidateLLMJudgePassThresholdBelowZero(t *testing.T) {
	cfg := map[string]any{
		"type":           "llm_judge",
		"provider":       "openai-compatible",
		"model":          "gpt-4o",
		"api_key_env":    "OPENAI_API_KEY",
		"rubric":         "be correct",
		"pass_threshold": -0.1,
	}
	err := ValidateEvaluatorConfig(cfg)
	if err == nil {
		t.Fatal("expected error for negative pass_threshold, got nil")
	}
}

func TestValidateLLMJudgePassThresholdAboveOne(t *testing.T) {
	cfg := map[string]any{
		"type":           "llm_judge",
		"provider":       "openai-compatible",
		"model":          "gpt-4o",
		"api_key_env":    "OPENAI_API_KEY",
		"rubric":         "be correct",
		"pass_threshold": 1.5,
	}
	err := ValidateEvaluatorConfig(cfg)
	if err == nil {
		t.Fatal("expected error for pass_threshold > 1.0, got nil")
	}
}

func TestValidateLLMJudgePassThresholdExactOne(t *testing.T) {
	cfg := map[string]any{
		"type":           "llm_judge",
		"provider":       "openai-compatible",
		"model":          "gpt-4o",
		"api_key_env":    "OPENAI_API_KEY",
		"rubric":         "be correct",
		"pass_threshold": 1.0,
	}
	err := ValidateEvaluatorConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error for pass_threshold=1.0, got: %v", err)
	}
}

func TestValidateLLMJudgePassThresholdExactZero(t *testing.T) {
	cfg := map[string]any{
		"type":           "llm_judge",
		"provider":       "openai-compatible",
		"model":          "gpt-4o",
		"api_key_env":    "OPENAI_API_KEY",
		"rubric":         "be correct",
		"pass_threshold": 0.0,
	}
	err := ValidateEvaluatorConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error for pass_threshold=0.0, got: %v", err)
	}
}

func TestValidateLLMJudgeNegativeTemperature(t *testing.T) {
	cfg := map[string]any{
		"type":        "llm_judge",
		"provider":    "openai-compatible",
		"model":       "gpt-4o",
		"api_key_env": "OPENAI_API_KEY",
		"rubric":      "be correct",
		"temperature": -0.1,
	}
	err := ValidateEvaluatorConfig(cfg)
	if err == nil {
		t.Fatal("expected error for negative temperature, got nil")
	}
}

func TestValidateLLMJudgeValidTemperature(t *testing.T) {
	cfg := map[string]any{
		"type":        "llm_judge",
		"provider":    "openai-compatible",
		"model":       "gpt-4o",
		"api_key_env": "OPENAI_API_KEY",
		"rubric":      "be correct",
		"temperature": 0.5,
	}
	err := ValidateEvaluatorConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error for valid temperature, got: %v", err)
	}
}

func TestValidateLLMJudgeZeroTemperature(t *testing.T) {
	cfg := map[string]any{
		"type":        "llm_judge",
		"provider":    "openai-compatible",
		"model":       "gpt-4o",
		"api_key_env": "OPENAI_API_KEY",
		"rubric":      "be correct",
		"temperature": 0.0,
	}
	err := ValidateEvaluatorConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error for temperature=0, got: %v", err)
	}
}

func TestValidateLLMJudgeMaxTokensZero(t *testing.T) {
	cfg := map[string]any{
		"type":        "llm_judge",
		"provider":    "openai-compatible",
		"model":       "gpt-4o",
		"api_key_env": "OPENAI_API_KEY",
		"rubric":      "be correct",
		"max_tokens":  0,
	}
	err := ValidateEvaluatorConfig(cfg)
	if err == nil {
		t.Fatal("expected error for max_tokens=0, got nil")
	}
}

func TestValidateLLMJudgeMaxTokensNegative(t *testing.T) {
	cfg := map[string]any{
		"type":        "llm_judge",
		"provider":    "openai-compatible",
		"model":       "gpt-4o",
		"api_key_env": "OPENAI_API_KEY",
		"rubric":      "be correct",
		"max_tokens":  -1,
	}
	err := ValidateEvaluatorConfig(cfg)
	if err == nil {
		t.Fatal("expected error for negative max_tokens, got nil")
	}
}

func TestValidateLLMJudgeValidMaxTokens(t *testing.T) {
	cfg := map[string]any{
		"type":        "llm_judge",
		"provider":    "openai-compatible",
		"model":       "gpt-4o",
		"api_key_env": "OPENAI_API_KEY",
		"rubric":      "be correct",
		"max_tokens":  1024,
	}
	err := ValidateEvaluatorConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error for valid max_tokens, got: %v", err)
	}
}

func TestValidateErrorDoesNotContainAPIKey(t *testing.T) {
	cfg := map[string]any{
		"type":    "llm_judge",
		"provider": "openai-compatible",
		"model":   "gpt-4o",
		"api_key_env": "sk-proj-mysecretkey123",
		"rubric":  "be correct",
	}
	err := ValidateEvaluatorConfig(cfg)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	msg := err.Error()
	if strings.Contains(msg, "sk-proj") {
		t.Fatalf("error message should not contain the key value, got: %s", msg)
	}
}

// ---- Multi evaluator validation ----

func TestValidateMultiConfig(t *testing.T) {
	cfg := map[string]any{
		"type":         "multi",
		"aggregation":  "weighted_average",
		"pass_threshold": 0.65,
	}
	err := ValidateEvaluatorConfig(cfg)
	if err != nil {
		t.Fatalf("expected no error for 'multi' type, got: %v", err)
	}

	// The multi type should accept any additional config fields
	cfgFull := map[string]any{
		"type":           "multi",
		"aggregation":    "weighted_average",
		"pass_threshold": 0.65,
		"multi_metrics": []any{
			map[string]any{"type": "exact", "metric_name": "final", "metric_kind": "final_answer", "weight": 0.4},
		},
	}
	err = ValidateEvaluatorConfig(cfgFull)
	if err != nil {
		t.Fatalf("expected no error for full multi config, got: %v", err)
	}
}
