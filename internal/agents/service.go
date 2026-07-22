package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ---- Domain types ----

// Agent represents a registered agent.
type Agent struct {
	ID          string                 `json:"id"`
	ProjectID   string                 `json:"project_id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Metadata    map[string]interface{} `json:"metadata"`
	Version     int                    `json:"version"`
	Status      string                 `json:"status"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// AgentInstance represents a running instance of an agent.
type AgentInstance struct {
	ID              string                 `json:"id"`
	AgentID         string                 `json:"agent_id"`
	ProjectID       string                 `json:"project_id"`
	InstanceID      string                 `json:"instance_id"`
	InstanceName    string                 `json:"instance_name"`
	Status          string                 `json:"status"`
	Hostname        string                 `json:"hostname"`
	PID             int                    `json:"pid"`
	ActiveRunID     string                 `json:"active_run_id,omitempty"`
	Metadata        map[string]interface{} `json:"metadata"`
	StartedAt       time.Time              `json:"started_at"`
	LastHeartbeatAt time.Time              `json:"last_heartbeat_at"`
}

// AgentCapability represents a capability declared by an agent.
type AgentCapability struct {
	ID          string                 `json:"id"`
	AgentID     string                 `json:"agent_id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Metadata    map[string]interface{} `json:"metadata"`
	CreatedAt   time.Time              `json:"created_at"`
}

// AgentSummaryResult is the raw result from the summary query, used for internal conversion.
type AgentSummaryResult struct {
	AID                 string
	AProjectID          string
	Name                string
	Description         string
	Status              string
	Version             int32
	Metadata            []byte
	ACreatedAt          time.Time
	AUpdatedAt          time.Time
	InstanceCount       int64
	OnlineInstanceCount int64
	LastHeartbeatAt     *time.Time
	ActiveRunCount      int64
	RecentRunCount      int64
	RecentErrorCount    int64
	LastRunAt           *time.Time
	P95LatencyMs        int64
	ErrorRate           string // from pgtype.Numeric serialization
	TotalCost           *float64
}

// AgentSummary is the domain type for the agent control center.
type AgentSummary struct {
	ID                  string                 `json:"id"`
	ProjectID           string                 `json:"project_id"`
	Name                string                 `json:"name"`
	Description         string                 `json:"description"`
	Status              string                 `json:"status"`
	Version             int                    `json:"version"`
	Metadata            map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt           time.Time              `json:"created_at"`
	UpdatedAt           time.Time              `json:"updated_at"`
	// Runtime contract summary from metadata
	Framework           string  `json:"framework,omitempty"`
	Runtime             string  `json:"runtime,omitempty"`
	Source              string  `json:"source,omitempty"`
	Environment         string  `json:"environment,omitempty"`
	AgentVersion        string  `json:"agent_version,omitempty"`
	ModelProvider       string  `json:"model_provider,omitempty"`
	ModelName           string  `json:"model_name,omitempty"`
	HarnessVersion      string  `json:"harness_version,omitempty"`
	PromptVersion       string  `json:"prompt_version,omitempty"`
	// Aggregated metrics
	InstanceCount       int       `json:"instance_count"`
	OnlineInstanceCount int       `json:"online_instance_count"`
	ActiveRunCount      int       `json:"active_run_count"`
	RecentRunCount      int       `json:"recent_run_count"`
	RecentErrorCount    int       `json:"recent_error_count"`
	LastHeartbeatAt     *time.Time `json:"last_heartbeat_at,omitempty"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	P95LatencyMs        *int64    `json:"p95_latency_ms,omitempty"`
	ErrorRate           float64   `json:"error_rate"`
	TotalCost           *float64  `json:"total_cost,omitempty"`
	// Derived online status
	CurrentStatusSummary string `json:"current_status_summary"`
}

// ---- Querier interface ----

// Querier defines the database operations the agent service needs.
type Querier interface {
	CreateAgent(ctx context.Context, projectID, name, description string, metadata []byte) (Agent, error)
	GetAgentByID(ctx context.Context, agentID string) (Agent, error)
	GetAgentByProjectAndName(ctx context.Context, projectID, name string) (Agent, error)
	ListAgentsByProject(ctx context.Context, projectID string) ([]Agent, error)
	UpdateAgentStatus(ctx context.Context, agentID, status string) error
	CreateAgentVersion(ctx context.Context, agentID string, version int, description string, metadata []byte) error
	CreateAgentInstance(ctx context.Context, agentID, projectID, instanceID, instanceName, status, hostname string, pid int, metadata []byte) (AgentInstance, error)
	UpdateAgentInstanceHeartbeat(ctx context.Context, agentID, instanceID, status string) (time.Time, error)
	ListAgentInstancesByAgent(ctx context.Context, agentID string) ([]AgentInstance, error)
	UpsertAgentInstance(ctx context.Context, agentID, projectID, instanceID, instanceName, status, hostname string, pid int, metadata []byte) (AgentInstance, error)
	CreateAgentHeartbeat(ctx context.Context, agentID, instanceID, status string, payload []byte) error
	SetCapability(ctx context.Context, agentID, name, description string, metadata []byte) error
	ListCapabilitiesByAgent(ctx context.Context, agentID string) ([]AgentCapability, error)
	ListAgentSummariesByProject(ctx context.Context, projectID string) ([]AgentSummary, error)
}

// ---- Service ----

// Service provides agent operations.
type Service struct {
	q Querier
}

// NewService creates a new agents Service.
func NewService(q Querier) *Service {
	return &Service{q: q}
}

// RegisterAgent creates a new agent, or returns the existing one if the same
// project_id + name already exists (idempotent).
func (s *Service) RegisterAgent(ctx context.Context, projectID, name, description string, metadata map[string]interface{}) (Agent, error) {
	if projectID == "" {
		return Agent{}, errors.New("projectID is required")
	}
	if name == "" {
		return Agent{}, errors.New("name is required")
	}

	metaBytes := marshalMetadata(metadata)

	agent, err := s.q.CreateAgent(ctx, projectID, name, description, metaBytes)
	if err != nil {
		// Check for unique constraint violation: project_id + name already exists.
		// Instead of parsing the database-specific error, try to fetch the existing agent.
		if existing, lookupErr := s.q.GetAgentByProjectAndName(ctx, projectID, name); lookupErr == nil {
			return existing, nil
		}
		return Agent{}, err
	}

	// Create initial version only for new agents
	if agent.Version == 1 {
		if err := s.q.CreateAgentVersion(ctx, agent.ID, agent.Version, description, metaBytes); err != nil {
			return Agent{}, fmt.Errorf("create initial version: %w", err)
		}
	}

	return agent, nil
}

// ListAgents returns all agents for a project.
func (s *Service) ListAgents(ctx context.Context, projectID string) ([]Agent, error) {
	if projectID == "" {
		return nil, errors.New("projectID is required")
	}
	return s.q.ListAgentsByProject(ctx, projectID)
}

// GetAgent returns an agent by ID.
func (s *Service) GetAgent(ctx context.Context, agentID string) (Agent, error) {
	if agentID == "" {
		return Agent{}, errors.New("agentID is required")
	}
	return s.q.GetAgentByID(ctx, agentID)
}

// GetAgentByName returns an agent by project ID and name.
func (s *Service) GetAgentByName(ctx context.Context, projectID, name string) (Agent, error) {
	if projectID == "" {
		return Agent{}, errors.New("projectID is required")
	}
	if name == "" {
		return Agent{}, errors.New("name is required")
	}
	return s.q.GetAgentByProjectAndName(ctx, projectID, name)
}

// UpdateAgentStatus updates an agent's status.
func (s *Service) UpdateAgentStatus(ctx context.Context, agentID, status string) error {
	if agentID == "" {
		return errors.New("agentID is required")
	}
	if status == "" {
		return errors.New("status is required")
	}
	return s.q.UpdateAgentStatus(ctx, agentID, status)
}

// RegisterInstance registers a new agent instance.
func (s *Service) RegisterInstance(ctx context.Context, agentID, projectID, instanceID, instanceName, hostname string, pid int, metadata map[string]interface{}) (AgentInstance, error) {
	if agentID == "" {
		return AgentInstance{}, errors.New("agentID is required")
	}
	if instanceID == "" {
		return AgentInstance{}, errors.New("instanceID is required")
	}

	metaBytes := marshalMetadata(metadata)
	return s.q.CreateAgentInstance(ctx, agentID, projectID, instanceID, instanceName, "online", hostname, pid, metaBytes)
}

// ListInstances returns all instances for an agent.
func (s *Service) ListInstances(ctx context.Context, agentID string) ([]AgentInstance, error) {
	if agentID == "" {
		return nil, errors.New("agentID is required")
	}
	return s.q.ListAgentInstancesByAgent(ctx, agentID)
}

// RecordHeartbeat records a heartbeat for an agent instance.
// If the instance does not exist, it is automatically created (upserted).
func (s *Service) RecordHeartbeat(ctx context.Context, agentID, projectID, instanceID, instanceName, status string, payload map[string]interface{}) error {
	if agentID == "" {
		return errors.New("agentID is required")
	}
	if instanceID == "" {
		return errors.New("instanceID is required")
	}
	if status == "" {
		status = "online"
	}

	payloadBytes := marshalMetadata(payload)

	// Upsert instance first (creates if not exists, updates if exists)
	upsertMeta := marshalMetadata(nil)
	_, err := s.q.UpsertAgentInstance(ctx, agentID, projectID, instanceID, instanceName, status, "", 0, upsertMeta)
	if err != nil {
		return err
	}

	// Record heartbeat history
	if err := s.q.CreateAgentHeartbeat(ctx, agentID, instanceID, status, payloadBytes); err != nil {
		return err
	}

	return nil
}

// SetCapability sets (or updates) a capability for an agent.
func (s *Service) SetCapability(ctx context.Context, agentID, name, description string, metadata map[string]interface{}) error {
	if agentID == "" {
		return errors.New("agentID is required")
	}
	if name == "" {
		return errors.New("name is required")
	}

	metaBytes := marshalMetadata(metadata)
	return s.q.SetCapability(ctx, agentID, name, description, metaBytes)
}

// ListCapabilities returns all capabilities for an agent.
func (s *Service) ListCapabilities(ctx context.Context, agentID string) ([]AgentCapability, error) {
	if agentID == "" {
		return nil, errors.New("agentID is required")
	}
	return s.q.ListCapabilitiesByAgent(ctx, agentID)
}

// ListAgentSummaries returns agent control center summaries for a project.
func (s *Service) ListAgentSummaries(ctx context.Context, projectID string) ([]AgentSummary, error) {
	if projectID == "" {
		return nil, errors.New("projectID is required")
	}
	return s.q.ListAgentSummariesByProject(ctx, projectID)
}

// ---- helpers ----

func marshalMetadata(meta map[string]interface{}) []byte {
	if meta == nil {
		return []byte("{}")
	}
	b, err := json.Marshal(meta)
	if err != nil {
		return []byte("{}")
	}
	return b
}
