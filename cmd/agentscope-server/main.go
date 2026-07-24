package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
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
	for _, entry := range os.Environ() {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	return env
}
