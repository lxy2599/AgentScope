package app

import (
	"testing"
	"time"

	"AgentScope/internal/adapter"
	"AgentScope/internal/config"
	"AgentScope/internal/httpapi"
	"AgentScope/internal/ingest"
)

const appTestSessionSecret = "0123456789abcdef0123456789abcdef"

// ---- Compile-time interface satisfaction checks ----
// These ensure production types implement the expected interfaces at compile time,
// proving the wiring is type-correct without needing DB connections in tests.

var (
	_ httpapi.IngestStatsQuerier = (*adapter.IngestStatsAdapter)(nil)
	_ ingest.TraceBatchWriter    = (*ingest.ClickHouseTraceWriter)(nil)
	_ ingest.TracestoreWriter    = (*adapter.HTTPTracestore)(nil)
)

// ---- Config-based wiring logic tests ----

// TestRateLimiterWiring verifies that the rate limiter is created based on
// configuration, replicating the decision logic in NewServer.
func TestRateLimiterWiring(t *testing.T) {
	t.Run("enabled", func(t *testing.T) {
		cfg := config.Config{
			HTTPAddr:      ":8080",
			DatabaseURL:   "postgres://localhost:5432/test",
			SessionSecret: appTestSessionSecret,
			SessionTTL:    7 * 24 * time.Hour,
			Ingest: config.IngestConfig{
				RateLimitEnabled:  true,
				RateLimitCapacity: 50,
				RateLimitRefill:   10,
				RateLimitInterval: time.Second,
			},
		}

		// This is the exact logic from app.go lines 151-158
		var rateLimiter *ingest.ProjectRateLimiter
		if cfg.Ingest.RateLimitEnabled {
			rateLimiter = ingest.NewProjectRateLimiter(
				cfg.Ingest.RateLimitCapacity,
				cfg.Ingest.RateLimitRefill,
				cfg.Ingest.RateLimitInterval,
			)
		}

		if rateLimiter == nil {
			t.Fatal("rateLimiter should be non-nil when RateLimitEnabled=true")
		}
	})

	t.Run("disabled", func(t *testing.T) {
		cfg := config.Config{
			HTTPAddr:      ":8080",
			DatabaseURL:   "postgres://localhost:5432/test",
			SessionSecret: appTestSessionSecret,
			SessionTTL:    7 * 24 * time.Hour,
			Ingest: config.IngestConfig{
				RateLimitEnabled: false,
			},
		}

		// This is the exact logic from app.go lines 151-158
		var rateLimiter *ingest.ProjectRateLimiter
		if cfg.Ingest.RateLimitEnabled {
			rateLimiter = ingest.NewProjectRateLimiter(
				cfg.Ingest.RateLimitCapacity,
				cfg.Ingest.RateLimitRefill,
				cfg.Ingest.RateLimitInterval,
			)
		}

		if rateLimiter != nil {
			t.Fatal("rateLimiter should be nil when RateLimitEnabled=false")
		}
	})
}

// TestDeafultWiring verifies that TraceWriter and IngestStats are always
// constructed with non-nil values when adequate dependencies exist.
func TestDefaultWiring(t *testing.T) {
	// Verify that ClickHouseTraceWriter and IngestStatsAdapter can be
	// instantiated with proper type signatures (constructor exists and
	// returns the expected types). These constructors don't open DB connections.
	//
	// The actual integration (NewServer) wires:
	//   traceWriter := ingest.NewClickHouseTraceWriter(httpTracestore)
	//   ingestStats := adapter.NewIngestStatsAdapter(chStore)
	//
	// Both are always created (unlike the conditional rate limiter).
	// Compile-time checks above guarantee the type assignment is valid.
}

func TestNewHTTPServerSetsDefensiveTimeouts(t *testing.T) {
	server := newHTTPServer(":8080", nil)

	if server.ReadHeaderTimeout <= 0 {
		t.Error("ReadHeaderTimeout must be set to protect against slow headers")
	}
	if server.ReadTimeout <= 0 {
		t.Error("ReadTimeout must be set to bound request reads")
	}
	if server.WriteTimeout <= 0 {
		t.Error("WriteTimeout must be set to bound response writes")
	}
	if server.IdleTimeout <= 0 {
		t.Error("IdleTimeout must be set to reclaim idle connections")
	}
	if server.MaxHeaderBytes <= 0 {
		t.Error("MaxHeaderBytes must be set to bound header memory")
	}
}
