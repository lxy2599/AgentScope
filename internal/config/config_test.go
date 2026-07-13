package config

import (
	"testing"
)

func TestLoadMissingDatabaseURL(t *testing.T) {
	_, err := Load(map[string]string{
		"AGENTSCOPE_SESSION_SECRET": "some-secret",
	})
	if err == nil {
		t.Fatal("expected error when AGENTSCOPE_DATABASE_URL is missing")
	}
}

func TestLoadDefaultHTTPAddr(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":  "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET": "some-secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("expected :8080, got %q", cfg.HTTPAddr)
	}
}

func TestLoadMissingSessionSecret(t *testing.T) {
	_, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL": "postgres://localhost:5432/agentscope",
	})
	if err == nil {
		t.Fatal("expected error when AGENTSCOPE_SESSION_SECRET is missing")
	}
}

func TestLoadShortSessionSecret(t *testing.T) {
	_, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":  "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET": "short",
	})
	if err == nil {
		t.Fatal("expected error when AGENTSCOPE_SESSION_SECRET is too short (< 8)")
	}
}
