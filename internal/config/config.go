package config

import (
	"errors"
	"fmt"
)

// Config holds the application configuration loaded from environment variables.
type Config struct {
	HTTPAddr      string
	DatabaseURL   string
	ClickHouseDSN string
	SessionSecret string
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

	return Config{
		HTTPAddr:      httpAddr,
		DatabaseURL:   databaseURL,
		ClickHouseDSN: clickhouseDSN,
		SessionSecret: sessionSecret,
	}, nil
}
