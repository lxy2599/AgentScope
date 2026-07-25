package config

import (
	"testing"
	"time"
)

const validSessionSecret = "0123456789abcdef0123456789abcdef"

func TestLoadIngestHealthBufferDefaultsAndOverrides(t *testing.T) {
	base := map[string]string{
		"AGENTSCOPE_DATABASE_URL":   "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET": validSessionSecret,
	}
	defaults, err := Load(base)
	if err != nil {
		t.Fatal(err)
	}
	if defaults.Ingest.HealthQueueCapacity != 4096 ||
		defaults.Ingest.HealthBatchSize != 500 ||
		defaults.Ingest.HealthFlushInterval != 200*time.Millisecond {
		t.Fatalf("ingest health defaults = %#v", defaults.Ingest)
	}

	custom := map[string]string{}
	for key, value := range base {
		custom[key] = value
	}
	custom["AGENTSCOPE_INGEST_HEALTH_QUEUE_CAPACITY"] = "8192"
	custom["AGENTSCOPE_INGEST_HEALTH_BATCH_SIZE"] = "250"
	custom["AGENTSCOPE_INGEST_HEALTH_FLUSH_INTERVAL"] = "75ms"
	cfg, err := Load(custom)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Ingest.HealthQueueCapacity != 8192 ||
		cfg.Ingest.HealthBatchSize != 250 ||
		cfg.Ingest.HealthFlushInterval != 75*time.Millisecond {
		t.Fatalf("custom ingest health buffer = %#v", cfg.Ingest)
	}
}

func TestLoadRealtimeDefaults(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":   "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET": validSessionSecret,
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Realtime.Enabled {
		t.Fatal("Realtime.Enabled = true, want false when Redis is not configured")
	}
	if cfg.Realtime.StreamMaxLen != 10000 {
		t.Fatalf("Realtime.StreamMaxLen = %d, want 10000", cfg.Realtime.StreamMaxLen)
	}
	if cfg.Realtime.RelayBatchSize != 100 {
		t.Fatalf("Realtime.RelayBatchSize = %d, want 100", cfg.Realtime.RelayBatchSize)
	}
}

func TestLoadRealtimeCustomConfig(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":            "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":          validSessionSecret,
		"AGENTSCOPE_REDIS_ADDR":              "redis.internal:6379",
		"AGENTSCOPE_REDIS_PASSWORD":          "secret",
		"AGENTSCOPE_REDIS_DB":                "2",
		"AGENTSCOPE_REALTIME_STREAM_MAX_LEN": "25000",
		"AGENTSCOPE_REALTIME_RELAY_BATCH":    "250",
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Realtime.Enabled || cfg.Realtime.RedisAddr != "redis.internal:6379" {
		t.Fatalf("Realtime = %#v", cfg.Realtime)
	}
	if cfg.Realtime.RedisPassword != "secret" || cfg.Realtime.RedisDB != 2 {
		t.Fatalf("Redis credentials/db not loaded: %#v", cfg.Realtime)
	}
	if cfg.Realtime.StreamMaxLen != 25000 || cfg.Realtime.RelayBatchSize != 250 {
		t.Fatalf("Realtime limits not loaded: %#v", cfg.Realtime)
	}
	if !cfg.Quotas.Enabled || cfg.Quotas.RedisNamespace != "agentscope" {
		t.Fatalf("Quotas = %#v, want enabled with default namespace", cfg.Quotas)
	}
}

func TestLoadQuotasCanBeDisabledWithRedisConfigured(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":    "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":  validSessionSecret,
		"AGENTSCOPE_REDIS_ADDR":      "redis.internal:6379",
		"AGENTSCOPE_QUOTAS_ENABLED":  "false",
		"AGENTSCOPE_QUOTA_NAMESPACE": "tenant-a",
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Quotas.Enabled || cfg.Quotas.RedisNamespace != "tenant-a" {
		t.Fatalf("Quotas = %#v", cfg.Quotas)
	}
}

func TestLoadQuotaLimiterTimeout(t *testing.T) {
	cfg, err := Load(map[string]string{
		"AGENTSCOPE_DATABASE_URL":          "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET":        validSessionSecret,
		"AGENTSCOPE_REDIS_ADDR":            "redis.internal:6379",
		"AGENTSCOPE_QUOTA_LIMITER_TIMEOUT": "125ms",
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Quotas.LimiterTimeout != 125*time.Millisecond {
		t.Fatalf("LimiterTimeout = %s, want 125ms", cfg.Quotas.LimiterTimeout)
	}
}

func TestLoadAnomalyDetectionDefaultsAndOverrides(t *testing.T) {
	base := map[string]string{
		"AGENTSCOPE_DATABASE_URL":   "postgres://localhost:5432/agentscope",
		"AGENTSCOPE_SESSION_SECRET": validSessionSecret,
	}
	defaults, err := Load(base)
	if err != nil {
		t.Fatal(err)
	}
	if !defaults.Anomalies.Enabled ||
		defaults.Anomalies.Interval != 5*time.Minute ||
		defaults.Anomalies.BaselineWindow != 24*time.Hour ||
		defaults.Anomalies.RecentWindow != time.Hour ||
		defaults.Anomalies.MinBaselineSamples != 6 ||
		defaults.Anomalies.Threshold != 6 {
		t.Fatalf("anomaly defaults = %#v", defaults.Anomalies)
	}

	custom := map[string]string{}
	for key, value := range base {
		custom[key] = value
	}
	custom["AGENTSCOPE_ANOMALY_ENABLED"] = "false"
	custom["AGENTSCOPE_ANOMALY_INTERVAL"] = "1m"
	custom["AGENTSCOPE_ANOMALY_BASELINE_WINDOW"] = "48h"
	custom["AGENTSCOPE_ANOMALY_RECENT_WINDOW"] = "30m"
	custom["AGENTSCOPE_ANOMALY_MIN_BASELINE_SAMPLES"] = "12"
	custom["AGENTSCOPE_ANOMALY_THRESHOLD"] = "8.5"
	cfg, err := Load(custom)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Anomalies.Enabled ||
		cfg.Anomalies.Interval != time.Minute ||
		cfg.Anomalies.BaselineWindow != 48*time.Hour ||
		cfg.Anomalies.RecentWindow != 30*time.Minute ||
		cfg.Anomalies.MinBaselineSamples != 12 ||
		cfg.Anomalies.Threshold != 8.5 {
		t.Fatalf("custom anomalies = %#v", cfg.Anomalies)
	}
}

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
