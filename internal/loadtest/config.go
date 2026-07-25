package loadtest

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type WorkloadProfile string

const (
	WorkloadRaw WorkloadProfile = "raw"
	WorkloadLLM WorkloadProfile = "llm"
)

type Target struct {
	ProjectID     string `json:"project_id"`
	Token         string `json:"token"`
	SessionCookie string `json:"session_cookie,omitempty"`
	AgentID       string `json:"agent_id,omitempty"`
	InstanceID    string `json:"instance_id,omitempty"`
	AgentName     string `json:"agent_name,omitempty"`
	AgentVersion  string `json:"agent_version,omitempty"`
	Environment   string `json:"environment,omitempty"`
}

type Thresholds struct {
	MaxP95Latency time.Duration `json:"-"`
	MaxErrorRate  float64       `json:"max_error_rate"`
	MaxDropRate   float64       `json:"max_drop_rate"`
}

type Config struct {
	ServerURL         string
	Targets           []Target
	Duration          time.Duration
	Requests          int
	Concurrency       int
	BatchSize         int
	RequestsPerSecond int
	Seed              int64
	HTTPTimeout       time.Duration
	EnableSSE         bool
	SSEAfterID        string
	EnableRuntime     bool
	Workload          WorkloadProfile
	Thresholds        Thresholds
}

func (c Config) Validate() error {
	switch {
	case strings.TrimSpace(c.ServerURL) == "":
		return errors.New("server URL is required")
	case len(c.Targets) == 0:
		return errors.New("at least one target is required")
	case c.Duration <= 0 && c.Requests <= 0:
		return errors.New("duration or requests must be positive")
	case c.Concurrency <= 0:
		return errors.New("concurrency must be positive")
	case c.BatchSize <= 0:
		return errors.New("batch size must be positive")
	case c.Workload != "" && c.Workload != WorkloadRaw && c.Workload != WorkloadLLM:
		return fmt.Errorf("unsupported workload %q", c.Workload)
	}
	for index, target := range c.Targets {
		if target.ProjectID == "" || target.Token == "" {
			return fmt.Errorf("target %d requires project_id and token", index)
		}
		if c.EnableSSE && target.SessionCookie == "" {
			return fmt.Errorf("target %d requires session_cookie when SSE is enabled", index)
		}
		if c.EnableRuntime && (target.AgentID == "" || target.InstanceID == "") {
			return fmt.Errorf("target %d requires agent_id and instance_id when runtime is enabled", index)
		}
	}
	return nil
}

func (c Config) workload() WorkloadProfile {
	if c.Workload == "" {
		return WorkloadRaw
	}
	return c.Workload
}

func LoadTargets(path string) ([]Target, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read targets: %w", err)
	}
	var targets []Target
	if err := json.Unmarshal(data, &targets); err != nil {
		return nil, fmt.Errorf("decode targets: %w", err)
	}
	return targets, nil
}
