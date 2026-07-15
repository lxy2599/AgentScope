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

func TestLoadDefaultClickHouseDSN(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":   "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET": "some-secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClickHouseDSN != "clickhouse://localhost:9000/agentscope_traces" {
		t.Errorf("ClickHouseDSN = %q, want clickhouse://localhost:9000/agentscope_traces", cfg.ClickHouseDSN)
	}
}

func TestLoadCustomClickHouseDSN(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":    "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":  "some-secret",
		"AGENTSCOPE_CLICKHOUSE_DSN": "clickhouse://custom:8123/mydb",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.ClickHouseDSN != "clickhouse://custom:8123/mydb" {
		t.Errorf("ClickHouseDSN = %q, want clickhouse://custom:8123/mydb", cfg.ClickHouseDSN)
	}
}
