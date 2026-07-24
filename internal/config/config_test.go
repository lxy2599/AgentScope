package config

import (
	"testing"
	"time"
)

const validSessionSecret = "0123456789abcdef0123456789abcdef"

func TestLoadMissingDatabaseURL(t *testing.T) {
	_, err := Load(map[string]string{
		"AGENTSCOPE_SESSION_SECRET": validSessionSecret,
	})
	if err == nil {
		t.Fatal("expected error when AGENTSCOPE_DATABASE_URL is missing")
	}
}

func TestLoadDefaultHTTPAddr(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":   "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET": validSessionSecret,
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
		"AGENTSCOPE_DATABASE_URL":   "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET": "short",
	})
	if err == nil {
		t.Fatal("expected error when AGENTSCOPE_SESSION_SECRET is too short (< 32 bytes)")
	}
}

func TestLoadSessionDefaults(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":   "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET": validSessionSecret,
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SessionTTL != 7*24*time.Hour {
		t.Fatalf("SessionTTL = %v, want 168h", cfg.SessionTTL)
	}
	if cfg.SecureCookies {
		t.Fatal("SecureCookies = true, want false by default")
	}
}

func TestLoadSessionOverrides(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":   "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET": validSessionSecret,
		"AGENTSCOPE_SESSION_TTL":    "12h30m",
		"AGENTSCOPE_SECURE_COOKIES": "true",
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.SessionTTL != 12*time.Hour+30*time.Minute {
		t.Fatalf("SessionTTL = %v, want 12h30m", cfg.SessionTTL)
	}
	if !cfg.SecureCookies {
		t.Fatal("SecureCookies = false, want true")
	}
}

func TestLoadRejectsInvalidSessionTTL(t *testing.T) {
	for _, value := range []string{"not-a-duration", "0s", "-1h"} {
		t.Run(value, func(t *testing.T) {
			_, err := Load(map[string]string{
				"AGENTSCOPE_DATABASE_URL":   "postgres://localhost:5432/agentscope",
				"AGENTSCOPE_SESSION_SECRET": validSessionSecret,
				"AGENTSCOPE_SESSION_TTL":    value,
			})
			if err == nil {
				t.Fatalf("Load() accepted AGENTSCOPE_SESSION_TTL=%q", value)
			}
		})
	}
}

func TestLoadSecureCookiesStrictBool(t *testing.T) {
	for _, value := range []string{"TRUE", "1", "yes", "False"} {
		t.Run(value, func(t *testing.T) {
			_, err := Load(map[string]string{
				"AGENTSCOPE_DATABASE_URL":   "postgres://localhost:5432/agentscope",
				"AGENTSCOPE_SESSION_SECRET": validSessionSecret,
				"AGENTSCOPE_SECURE_COOKIES": value,
			})
			if err == nil {
				t.Fatalf("Load() accepted AGENTSCOPE_SECURE_COOKIES=%q", value)
			}
		})
	}

	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":   "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET": validSessionSecret,
		"AGENTSCOPE_SECURE_COOKIES": "false",
	})
	if err != nil {
		t.Fatalf("Load(false) error = %v", err)
	}
	if cfg.SecureCookies {
		t.Fatal("SecureCookies = true, want false")
	}
}

func TestLoadDefaultClickHouseDSN(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":   "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET": validSessionSecret,
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
		"AGENTSCOPE_DATABASE_URL":   "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET": validSessionSecret,
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
		"AGENTSCOPE_DATABASE_URL":         "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":       validSessionSecret,
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
		"AGENTSCOPE_DATABASE_URL":     "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":   validSessionSecret,
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
		"AGENTSCOPE_DATABASE_URL":         "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":       validSessionSecret,
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
		"AGENTSCOPE_DATABASE_URL":     "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":   validSessionSecret,
		"AGENTSCOPE_INGEST_MAX_SPANS": "0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ingest.MaxSpans != 5000 {
		t.Errorf("zero value should fall back to default %d, got %d", 5000, cfg.Ingest.MaxSpans)
	}
}

func TestLoadRateLimitCapacity(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":               "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":             validSessionSecret,
		"AGENTSCOPE_INGEST_RATE_LIMIT_CAPACITY": "200",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ingest.RateLimitCapacity != 200 {
		t.Errorf("RateLimitCapacity = %d, want 200", cfg.Ingest.RateLimitCapacity)
	}
}

func TestLoadRateLimitCapacityInvalid(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":               "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":             validSessionSecret,
		"AGENTSCOPE_INGEST_RATE_LIMIT_CAPACITY": "not-a-number",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ingest.RateLimitCapacity != 100 {
		t.Errorf("invalid capacity should fall back to default %d, got %d", 100, cfg.Ingest.RateLimitCapacity)
	}
}

func TestLoadRateLimitCapacityZero(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":               "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":             validSessionSecret,
		"AGENTSCOPE_INGEST_RATE_LIMIT_CAPACITY": "0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ingest.RateLimitCapacity != 100 {
		t.Errorf("zero capacity should fall back to default %d, got %d", 100, cfg.Ingest.RateLimitCapacity)
	}
}

func TestLoadRateLimitCapacityNegative(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":               "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":             validSessionSecret,
		"AGENTSCOPE_INGEST_RATE_LIMIT_CAPACITY": "-50",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ingest.RateLimitCapacity != 100 {
		t.Errorf("negative capacity should fall back to default %d, got %d", 100, cfg.Ingest.RateLimitCapacity)
	}
}

func TestLoadRateLimitRefill(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":             "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":           validSessionSecret,
		"AGENTSCOPE_INGEST_RATE_LIMIT_REFILL": "50",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ingest.RateLimitRefill != 50 {
		t.Errorf("RateLimitRefill = %d, want 50", cfg.Ingest.RateLimitRefill)
	}
}

func TestLoadRateLimitRefillInvalid(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":             "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":           validSessionSecret,
		"AGENTSCOPE_INGEST_RATE_LIMIT_REFILL": "bad",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ingest.RateLimitRefill != 100 {
		t.Errorf("invalid refill should fall back to default %d, got %d", 100, cfg.Ingest.RateLimitRefill)
	}
}

func TestLoadRateLimitRefillZero(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":             "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":           validSessionSecret,
		"AGENTSCOPE_INGEST_RATE_LIMIT_REFILL": "0",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ingest.RateLimitRefill != 100 {
		t.Errorf("zero refill should fall back to default %d, got %d", 100, cfg.Ingest.RateLimitRefill)
	}
}

func TestLoadRateLimitInterval(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":               "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":             validSessionSecret,
		"AGENTSCOPE_INGEST_RATE_LIMIT_INTERVAL": "500ms",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ingest.RateLimitInterval != 500*time.Millisecond {
		t.Errorf("RateLimitInterval = %v, want 500ms", cfg.Ingest.RateLimitInterval)
	}
}

func TestLoadRateLimitIntervalInvalid(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":               "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":             validSessionSecret,
		"AGENTSCOPE_INGEST_RATE_LIMIT_INTERVAL": "not-a-duration",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ingest.RateLimitInterval != time.Second {
		t.Errorf("invalid interval should fall back to default %v, got %v", time.Second, cfg.Ingest.RateLimitInterval)
	}
}

func TestLoadRateLimitIntervalZero(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":               "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":             validSessionSecret,
		"AGENTSCOPE_INGEST_RATE_LIMIT_INTERVAL": "0s",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ingest.RateLimitInterval != time.Second {
		t.Errorf("zero interval should fall back to default %v, got %v", time.Second, cfg.Ingest.RateLimitInterval)
	}
}

func TestLoadRateLimitIntervalNegative(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":               "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":             validSessionSecret,
		"AGENTSCOPE_INGEST_RATE_LIMIT_INTERVAL": "-5s",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Ingest.RateLimitInterval != time.Second {
		t.Errorf("negative interval should fall back to default %v, got %v", time.Second, cfg.Ingest.RateLimitInterval)
	}
}

func TestLoadIngestDefaults(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":   "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET": validSessionSecret,
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
