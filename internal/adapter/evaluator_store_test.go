package adapter

import (
	"encoding/json"
	"testing"
)

// TestValidateEvalCreatorConfig tests that evaluator config validation rejects bad configs.
func TestValidateEvalCreatorConfig(t *testing.T) {
	t.Run("invalid_json", func(t *testing.T) {
		err := ValidateEvalCreatorConfig("{bad json")
		if err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("missing_type", func(t *testing.T) {
		err := ValidateEvalCreatorConfig(`{"other":"value"}`)
		if err == nil {
			t.Fatal("expected error for missing type")
		}
	})

	t.Run("unsupported_type", func(t *testing.T) {
		err := ValidateEvalCreatorConfig(`{"type":"nonexistent"}`)
		if err == nil {
			t.Fatal("expected error for unsupported type")
		}
	})

	t.Run("valid_single_config", func(t *testing.T) {
		err := ValidateEvalCreatorConfig(`{"type":"exact"}`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("valid_llm_judge_config", func(t *testing.T) {
		cfg := map[string]interface{}{
			"type":        "llm_judge",
			"provider":    "openai-compatible",
			"model":       "gpt-4o",
			"api_key_env": "OPENAI_API_KEY",
			"rubric":      "Evaluate the answer.",
		}
		b, _ := json.Marshal(cfg)
		err := ValidateEvalCreatorConfig(string(b))
		if err != nil {
			t.Fatalf("unexpected error for valid llm_judge: %v", err)
		}
	})

	t.Run("multi_metrics_invalid_entry", func(t *testing.T) {
		cfg := map[string]interface{}{
			"type": "exact",
			"multi_metrics": []interface{}{
				map[string]interface{}{
					"metric_name": "test",
					"metric_kind": "nonexistent_kind",
				},
			},
		}
		b, _ := json.Marshal(cfg)
		err := ValidateEvalCreatorConfig(string(b))
		if err == nil {
			t.Fatal("expected error for invalid metric_kind in multi_metrics")
		}
	})

	t.Run("multi_metrics_valid", func(t *testing.T) {
		cfg := map[string]interface{}{
			"type": "exact",
			"multi_metrics": []interface{}{
				map[string]interface{}{
					"type":        "exact",
					"metric_name": "final-answer",
					"metric_kind": "final_answer",
				},
				map[string]interface{}{
					"type":        "latency_threshold",
					"max_ms":      5000,
					"metric_name": "latency",
					"metric_kind": "efficiency",
				},
			},
		}
		b, _ := json.Marshal(cfg)
		err := ValidateEvalCreatorConfig(string(b))
		if err != nil {
			t.Fatalf("unexpected error for valid multi_metrics: %v", err)
		}
	})

	t.Run("multi_metrics_missing_type", func(t *testing.T) {
		cfg := map[string]interface{}{
			"type": "exact",
			"multi_metrics": []interface{}{
				map[string]interface{}{
					"metric_name": "test",
					"metric_kind": "final_answer",
				},
			},
		}
		b, _ := json.Marshal(cfg)
		err := ValidateEvalCreatorConfig(string(b))
		if err == nil {
			t.Fatal("expected error for missing type in multi_metrics entry")
		}
	})

	t.Run("multi_type_with_metrics", func(t *testing.T) {
		cfg := map[string]interface{}{
			"type":           "multi",
			"aggregation":    "weighted_average",
			"pass_threshold": 0.65,
			"multi_metrics": []interface{}{
				map[string]interface{}{
					"type":        "exact",
					"metric_name": "final",
					"metric_kind": "final_answer",
					"weight":      0.4,
				},
				map[string]interface{}{
					"type":        "latency_threshold",
					"max_ms":      5000,
					"metric_name": "latency",
					"metric_kind": "efficiency",
					"weight":      0.6,
				},
			},
		}
		b, _ := json.Marshal(cfg)
		err := ValidateEvalCreatorConfig(string(b))
		if err != nil {
			t.Fatalf("expected no error for type:multi with multi_metrics, got: %v", err)
		}
	})
}
