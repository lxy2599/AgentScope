package config

import (
	"errors"
	"fmt"
	"time"
)

// IngestConfig holds configuration for the trace ingestion pipeline.
type IngestConfig struct {
	MaxBodySize        int           // 默认 10MB
	MaxSpans           int           // 默认 5000
	RateLimitEnabled   bool          // 默认 false（向后兼容）
	RateLimitCapacity  int           // 默认 100
	RateLimitRefill    int           // 默认 100
	RateLimitInterval  time.Duration // 默认 1s
}

// Config holds the application configuration loaded from environment variables.
type Config struct {
	HTTPAddr      string
	DatabaseURL   string
	ClickHouseDSN string
	SessionSecret string
	Ingest        IngestConfig
}

// Load reads configuration from the provided environment variable map.
// AGENTSCOPE_DATABASE_URL is required.
// AGENTSCOPE_SESSION_SECRET is required and must be at least 8 characters.
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
	if len(sessionSecret) < 8 {
		return Config{}, fmt.Errorf("AGENTSCOPE_SESSION_SECRET must be at least 8 characters, got %d", len(sessionSecret))
	}

	clickhouseDSN := env["AGENTSCOPE_CLICKHOUSE_DSN"]
	if clickhouseDSN == "" {
		clickhouseDSN = "clickhouse://localhost:19000/agentscope_traces"
	}

	ingestCfg := IngestConfig{
		MaxBodySize:       10 * 1024 * 1024, // 10MB
		MaxSpans:          5000,
		RateLimitEnabled:  env["AGENTSCOPE_INGEST_RATE_LIMIT_ENABLED"] == "true",
		RateLimitCapacity: 100,
		RateLimitRefill:   100,
		RateLimitInterval: time.Second,
	}

	return Config{
		HTTPAddr:      httpAddr,
		DatabaseURL:   databaseURL,
		ClickHouseDSN: clickhouseDSN,
		SessionSecret: sessionSecret,
		Ingest:        ingestCfg,
	}, nil
}
