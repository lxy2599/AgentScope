package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"AgentScope/internal/app"
	"AgentScope/internal/config"
)

func main() {
	env := readEnv()
	cfg, err := config.Load(env)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	server, err := app.NewServer(ctx, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating server: %v\n", err)
		os.Exit(1)
	}

	// Start server in a goroutine so we can listen for shutdown signals.
	go func() {
		log.Printf("agentscope server listening on %s", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			os.Exit(1)
		}
	}()

	// Block until a signal is received.
	<-ctx.Done()

	log.Println("shutting down gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		fmt.Fprintf(os.Stderr, "shutdown error: %v\n", err)
		os.Exit(1)
	}

	log.Println("server stopped")
}

// readEnv reads all environment variables into a map.
func readEnv() map[string]string {
	env := make(map[string]string)
	for _, key := range []string{
		"AGENTSCOPE_HTTP_ADDR",
		"AGENTSCOPE_DATABASE_URL",
		"AGENTSCOPE_SESSION_SECRET",
		"AGENTSCOPE_CLICKHOUSE_DSN",
		"AGENTSCOPE_INGEST_MAX_BODY_SIZE",
		"AGENTSCOPE_INGEST_MAX_SPANS",
		"AGENTSCOPE_INGEST_RATE_LIMIT_ENABLED",
		"AGENTSCOPE_INGEST_RATE_LIMIT_CAPACITY",
		"AGENTSCOPE_INGEST_RATE_LIMIT_REFILL",
		"AGENTSCOPE_INGEST_RATE_LIMIT_INTERVAL",
		"AGENTSCOPE_REDIS_ADDR",
		"AGENTSCOPE_REDIS_PASSWORD",
		"AGENTSCOPE_REDIS_DB",
		"AGENTSCOPE_REALTIME_STREAM_MAX_LEN",
		"AGENTSCOPE_REALTIME_RELAY_BATCH",
		"AGENTSCOPE_REALTIME_RELAY_POLL_INTERVAL",
		"AGENTSCOPE_REALTIME_DEDUPE_TTL",
	} {
		env[key] = os.Getenv(key)
	}
	return env
}
