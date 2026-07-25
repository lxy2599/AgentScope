package config

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"AgentScope/internal/auth"
)

// IngestConfig holds configuration for the trace ingestion pipeline.
type IngestConfig struct {
	MaxBodySize         int           // 默认 10MB
	MaxSpans            int           // 默认 5000
	RateLimitEnabled    bool          // 默认 false（向后兼容）
	RateLimitCapacity   int           // 默认 100
	RateLimitRefill     int           // 默认 100
	RateLimitInterval   time.Duration // 默认 1s
	HealthQueueCapacity int
	HealthBatchSize     int
	HealthFlushInterval time.Duration
}

// RealtimeConfig configures Redis Streams and the PostgreSQL outbox relay.
type RealtimeConfig struct {
	Enabled           bool
	RedisAddr         string
	RedisPassword     string
	RedisDB           int
	StreamMaxLen      int64
	RelayBatchSize    int
	RelayPollInterval time.Duration
	DedupeTTL         time.Duration
}

// QuotaConfig controls distributed project quota enforcement.
type QuotaConfig struct {
	Enabled        bool
	RedisNamespace string
	LimiterTimeout time.Duration
}

// AnomalyConfig controls periodic operational anomaly detection.
type AnomalyConfig struct {
	Enabled            bool
	Interval           time.Duration
	BaselineWindow     time.Duration
	RecentWindow       time.Duration
	MinBaselineSamples int
	Threshold          float64
}

// Config holds the application configuration loaded from environment variables.
type Config struct {
	HTTPAddr      string
	DatabaseURL   string
	ClickHouseDSN string
	SessionSecret string
	SessionTTL    time.Duration
	SecureCookies bool
	Ingest        IngestConfig
	Realtime      RealtimeConfig
	Quotas        QuotaConfig
	Anomalies     AnomalyConfig
}

// Load reads configuration from the provided environment variable map.
// AGENTSCOPE_DATABASE_URL is required.
// AGENTSCOPE_SESSION_SECRET is required and must be at least 32 bytes.
// AGENTSCOPE_SESSION_TTL defaults to seven days and must be a positive Go duration.
// AGENTSCOPE_SECURE_COOKIES defaults to false and accepts only "true" or "false".
// AGENTSCOPE_HTTP_ADDR defaults to ":8080".
// AGENTSCOPE_CLICKHOUSE_DSN defaults to "clickhouse://localhost:9000/agentscope_traces".
func Load(env map[string]string) (Config, error) {
	httpAddr := env["AGENTSCOPE_HTTP_ADDR"]
	if httpAddr == "" {
		httpAddr = ":8080"
	}

	databaseURL := env["AGENTSCOPE_DATABASE_URL"]
	if databaseURL == "" {
		return Config{}, errors.New("AGENTSCOPE_DATABASE_URL is required")
	}

	sessionSecret := env["AGENTSCOPE_SESSION_SECRET"]
	if sessionSecret == "" {
		return Config{}, errors.New("AGENTSCOPE_SESSION_SECRET is required")
	}
	if len(sessionSecret) < auth.MinSessionSecretLen {
		return Config{}, fmt.Errorf(
			"AGENTSCOPE_SESSION_SECRET must be at least %d bytes, got %d",
			auth.MinSessionSecretLen,
			len(sessionSecret),
		)
	}

	sessionTTL := auth.DefaultSessionTTL
	if value := env["AGENTSCOPE_SESSION_TTL"]; value != "" {
		parsed, err := time.ParseDuration(value)
		if err != nil || parsed <= 0 {
			return Config{}, errors.New("AGENTSCOPE_SESSION_TTL must be a positive duration")
		}
		sessionTTL = parsed
	}

	secureCookies := false
	switch value := env["AGENTSCOPE_SECURE_COOKIES"]; value {
	case "":
	case "true":
		secureCookies = true
	case "false":
	default:
		return Config{}, errors.New(`AGENTSCOPE_SECURE_COOKIES must be "true" or "false"`)
	}

	clickhouseDSN := env["AGENTSCOPE_CLICKHOUSE_DSN"]
	if clickhouseDSN == "" {
		clickhouseDSN = "clickhouse://localhost:19000/agentscope_traces"
	}

	ingestCfg := IngestConfig{
		MaxBodySize:         10 * 1024 * 1024, // 10MB
		MaxSpans:            5000,
		RateLimitEnabled:    env["AGENTSCOPE_INGEST_RATE_LIMIT_ENABLED"] == "true",
		RateLimitCapacity:   100,
		RateLimitRefill:     100,
		RateLimitInterval:   time.Second,
		HealthQueueCapacity: 4096,
		HealthBatchSize:     500,
		HealthFlushInterval: 200 * time.Millisecond,
	}

	if v := env["AGENTSCOPE_INGEST_MAX_BODY_SIZE"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ingestCfg.MaxBodySize = n
		}
	}
	if v := env["AGENTSCOPE_INGEST_MAX_SPANS"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ingestCfg.MaxSpans = n
		}
	}
	if v := env["AGENTSCOPE_INGEST_RATE_LIMIT_CAPACITY"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ingestCfg.RateLimitCapacity = n
		}
	}
	if v := env["AGENTSCOPE_INGEST_RATE_LIMIT_REFILL"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ingestCfg.RateLimitRefill = n
		}
	}
	if v := env["AGENTSCOPE_INGEST_RATE_LIMIT_INTERVAL"]; v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			ingestCfg.RateLimitInterval = d
		}
	}
	if v := env["AGENTSCOPE_INGEST_HEALTH_QUEUE_CAPACITY"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ingestCfg.HealthQueueCapacity = n
		}
	}
	if v := env["AGENTSCOPE_INGEST_HEALTH_BATCH_SIZE"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ingestCfg.HealthBatchSize = n
		}
	}
	if v := env["AGENTSCOPE_INGEST_HEALTH_FLUSH_INTERVAL"]; v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			ingestCfg.HealthFlushInterval = d
		}
	}

	realtimeCfg := RealtimeConfig{
		RedisAddr:         env["AGENTSCOPE_REDIS_ADDR"],
		RedisPassword:     env["AGENTSCOPE_REDIS_PASSWORD"],
		StreamMaxLen:      10000,
		RelayBatchSize:    100,
		RelayPollInterval: 250 * time.Millisecond,
		DedupeTTL:         7 * 24 * time.Hour,
	}
	realtimeCfg.Enabled = realtimeCfg.RedisAddr != ""
	if v := env["AGENTSCOPE_REDIS_DB"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			realtimeCfg.RedisDB = n
		}
	}
	if v := env["AGENTSCOPE_REALTIME_STREAM_MAX_LEN"]; v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			realtimeCfg.StreamMaxLen = n
		}
	}
	if v := env["AGENTSCOPE_REALTIME_RELAY_BATCH"]; v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			realtimeCfg.RelayBatchSize = n
		}
	}
	if v := env["AGENTSCOPE_REALTIME_RELAY_POLL_INTERVAL"]; v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			realtimeCfg.RelayPollInterval = d
		}
	}
	if v := env["AGENTSCOPE_REALTIME_DEDUPE_TTL"]; v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			realtimeCfg.DedupeTTL = d
		}
	}

	quotaCfg := QuotaConfig{
		Enabled:        realtimeCfg.RedisAddr != "",
		RedisNamespace: "agentscope",
		LimiterTimeout: 250 * time.Millisecond,
	}
	if env["AGENTSCOPE_QUOTAS_ENABLED"] == "false" {
		quotaCfg.Enabled = false
	}
	if value := env["AGENTSCOPE_QUOTA_NAMESPACE"]; value != "" {
		quotaCfg.RedisNamespace = value
	}
	if value := env["AGENTSCOPE_QUOTA_LIMITER_TIMEOUT"]; value != "" {
		if parsed, err := time.ParseDuration(value); err == nil && parsed > 0 {
			quotaCfg.LimiterTimeout = parsed
		}
	}

	anomalyCfg := AnomalyConfig{
		Enabled:            env["AGENTSCOPE_ANOMALY_ENABLED"] != "false",
		Interval:           5 * time.Minute,
		BaselineWindow:     24 * time.Hour,
		RecentWindow:       time.Hour,
		MinBaselineSamples: 6,
		Threshold:          6,
	}
	if value := env["AGENTSCOPE_ANOMALY_INTERVAL"]; value != "" {
		if parsed, err := time.ParseDuration(value); err == nil && parsed > 0 {
			anomalyCfg.Interval = parsed
		}
	}
	if value := env["AGENTSCOPE_ANOMALY_BASELINE_WINDOW"]; value != "" {
		if parsed, err := time.ParseDuration(value); err == nil && parsed > 0 {
			anomalyCfg.BaselineWindow = parsed
		}
	}
	if value := env["AGENTSCOPE_ANOMALY_RECENT_WINDOW"]; value != "" {
		if parsed, err := time.ParseDuration(value); err == nil && parsed > 0 {
			anomalyCfg.RecentWindow = parsed
		}
	}
	if value := env["AGENTSCOPE_ANOMALY_MIN_BASELINE_SAMPLES"]; value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed > 0 {
			anomalyCfg.MinBaselineSamples = parsed
		}
	}
	if value := env["AGENTSCOPE_ANOMALY_THRESHOLD"]; value != "" {
		if parsed, err := strconv.ParseFloat(value, 64); err == nil && parsed > 0 {
			anomalyCfg.Threshold = parsed
		}
	}

	return Config{
		HTTPAddr:      httpAddr,
		DatabaseURL:   databaseURL,
		ClickHouseDSN: clickhouseDSN,
		SessionSecret: sessionSecret,
		SessionTTL:    sessionTTL,
		SecureCookies: secureCookies,
		Ingest:        ingestCfg,
		Realtime:      realtimeCfg,
		Quotas:        quotaCfg,
		Anomalies:     anomalyCfg,
	}, nil
}
