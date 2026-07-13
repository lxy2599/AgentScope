package main

import (
	"fmt"
	"os"

	"github.com/AgentScope/agentscope/internal/config"
)

func main() {
	env := readEnv()
	cfg, err := config.Load(env)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("agentscope server configured for %s\n", cfg.HTTPAddr)
}

// readEnv reads all environment variables into a map.
func readEnv() map[string]string {
	env := make(map[string]string)
	for _, key := range []string{
		"AGENTSCOPE_HTTP_ADDR",
		"AGENTSCOPE_DATABASE_URL",
		"AGENTSCOPE_SESSION_SECRET",
	} {
		env[key] = os.Getenv(key)
	}
	return env
}
