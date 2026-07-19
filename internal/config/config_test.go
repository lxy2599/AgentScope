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
	if cfg.ClickHouseDSN != "clickhouse://localhost:19000/agentscope_traces" {
		t.Errorf("ClickHouseDSN = %q, want clickhouse://localhost:19000/agentscope_traces", cfg.ClickHouseDSN)
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

func TestLoadIngestMaxBodySize(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":            "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":          "some-secret",
		"AGENTSCOPE_INGEST_MAX_BODY_SIZE": "20971520",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ingest.MaxBodySize != 20971520 {
		t.Errorf("MaxBodySize = %d, want 20971520", cfg.Ingest.MaxBodySize)
	}
}

func TestLoadIngestMaxSpans(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":          "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":        "some-secret",
		"AGENTSCOPE_INGEST_MAX_SPANS": "10000",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ingest.MaxSpans != 10000 {
		t.Errorf("MaxSpans = %d, want 10000", cfg.Ingest.MaxSpans)
	}
}

func TestLoadIngestMaxBodySizeInvalid(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":            "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":          "some-secret",
		"AGENTSCOPE_INGEST_MAX_BODY_SIZE": "not-a-number",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ingest.MaxBodySize != 10*1024*1024 {
		t.Errorf("invalid value should fall back to default %d, got %d", 10*1024*1024, cfg.Ingest.MaxBodySize)
	}
}

func TestLoadIngestMaxSpansZero(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":          "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":        "some-secret",
		"AGENTSCOPE_INGEST_MAX_SPANS": "0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ingest.MaxSpans != 5000 {
		t.Errorf("zero value should fall back to default %d, got %d", 5000, cfg.Ingest.MaxSpans)
	}
}

func TestLoadIngestDefaults(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":   "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET": "some-secret",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ingest.MaxBodySize != 10*1024*1024 {
		t.Errorf("default MaxBodySize = %d, want %d", cfg.Ingest.MaxBodySize, 10*1024*1024)
	}
	if cfg.Ingest.MaxSpans != 5000 {
		t.Errorf("default MaxSpans = %d, want 5000", cfg.Ingest.MaxSpans)
	}
	if cfg.Ingest.RateLimitEnabled {
		t.Error("default RateLimitEnabled should be false")
	}
}
