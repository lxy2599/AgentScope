package agents

import (
	"testing"
)

func TestValidateCapabilityMetadata(t *testing.T) {
	t.Run("nil metadata is valid", func(t *testing.T) {
		if err := validateCapabilityMetadata(nil); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("empty metadata is valid", func(t *testing.T) {
		if err := validateCapabilityMetadata(map[string]interface{}{}); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("no kind field is valid", func(t *testing.T) {
		if err := validateCapabilityMetadata(map[string]interface{}{"version": "1.0"}); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("known kind tool is valid", func(t *testing.T) {
		if err := validateCapabilityMetadata(map[string]interface{}{"kind": "tool"}); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("known kind skill is valid", func(t *testing.T) {
		if err := validateCapabilityMetadata(map[string]interface{}{"kind": "skill"}); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("known kind knowledge_base is valid", func(t *testing.T) {
		if err := validateCapabilityMetadata(map[string]interface{}{"kind": "knowledge_base"}); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("known kind memory is valid", func(t *testing.T) {
		if err := validateCapabilityMetadata(map[string]interface{}{"kind": "memory"}); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("known kind guardrail is valid", func(t *testing.T) {
		if err := validateCapabilityMetadata(map[string]interface{}{"kind": "guardrail"}); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("known kind model is valid", func(t *testing.T) {
		if err := validateCapabilityMetadata(map[string]interface{}{"kind": "model"}); err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
	})

	t.Run("unknown kind is rejected", func(t *testing.T) {
		if err := validateCapabilityMetadata(map[string]interface{}{"kind": "unknown"}); err == nil {
			t.Fatal("expected error for unknown kind 'unknown'")
		}
	})

	t.Run("non-string kind is rejected", func(t *testing.T) {
		if err := validateCapabilityMetadata(map[string]interface{}{"kind": 42}); err == nil {
			t.Fatal("expected error for non-string kind")
		}
	})
}
