package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"AgentScope/internal/agents"
	"AgentScope/internal/store"
)

// AgentQuerier implements agents.Querier using store.Queries.
type AgentQuerier struct {
	q *store.Queries
}

var _ agents.Querier = (*AgentQuerier)(nil)

// NewAgentQuerier creates a new AgentQuerier.
func NewAgentQuerier(q *store.Queries) *AgentQuerier {
	return &AgentQuerier{q: q}
}

func (a *AgentQuerier) CreateAgent(ctx context.Context, projectID, name, description string, metadata []byte) (agents.Agent, error) {
	storeAgent, err := a.q.CreateAgent(ctx, store.CreateAgentParams{
		ProjectID:   parsePGUUID(projectID),
		Name:        name,
		Description: description,
		Metadata:    normalizeJSONBytes(metadata),
	})
	if err != nil {
		return agents.Agent{}, fmt.Errorf("create agent: %w", err)
	}
	return storeAgentToDomain(storeAgent), nil
}

func (a *AgentQuerier) GetAgentByID(ctx context.Context, agentID string) (agents.Agent, error) {
	storeAgent, err := a.q.GetAgentByID(ctx, parsePGUUID(agentID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return agents.Agent{}, fmt.Errorf("agent not found: %s", agentID)
		}
		return agents.Agent{}, fmt.Errorf("get agent: %w", err)
	}
	return storeAgentToDomain(storeAgent), nil
}

func (a *AgentQuerier) GetAgentByProjectAndName(ctx context.Context, projectID, name string) (agents.Agent, error) {
	storeAgent, err := a.q.GetAgentByProjectAndName(ctx, store.GetAgentByProjectAndNameParams{
		ProjectID: parsePGUUID(projectID),
		Name:      name,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return agents.Agent{}, fmt.Errorf("agent not found: %s/%s", projectID, name)
		}
		return agents.Agent{}, fmt.Errorf("get agent by name: %w", err)
	}
	return storeAgentToDomain(storeAgent), nil
}

func (a *AgentQuerier) ListAgentsByProject(ctx context.Context, projectID string) ([]agents.Agent, error) {
	storeAgents, err := a.q.ListAgentsByProject(ctx, parsePGUUID(projectID))
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	result := make([]agents.Agent, 0, len(storeAgents))
	for _, sa := range storeAgents {
		result = append(result, storeAgentToDomain(sa))
	}
	return result, nil
}

func (a *AgentQuerier) UpdateAgentStatus(ctx context.Context, agentID, status string) error {
	_, err := a.q.UpdateAgentStatus(ctx, store.UpdateAgentStatusParams{
		ID:     parsePGUUID(agentID),
		Status: status,
	})
	if err != nil {
		return fmt.Errorf("update agent status: %w", err)
	}
	return nil
}

func (a *AgentQuerier) CreateAgentVersion(ctx context.Context, agentID string, version int, description string, metadata []byte) error {
	_, err := a.q.CreateAgentVersion(ctx, store.CreateAgentVersionParams{
		AgentID:     parsePGUUID(agentID),
		Version:     int32(version),
		Description: description,
		Metadata:    normalizeJSONBytes(metadata),
	})
	if err != nil {
		return fmt.Errorf("create agent version: %w", err)
	}
	return nil
}

func (a *AgentQuerier) CreateAgentInstance(ctx context.Context, agentID, instanceID, status, hostname string, pid int, metadata []byte) (agents.AgentInstance, error) {
	storeInst, err := a.q.CreateAgentInstance(ctx, store.CreateAgentInstanceParams{
		AgentID:    parsePGUUID(agentID),
		InstanceID: instanceID,
		Status:     status,
		Hostname:   hostname,
		Pid:        int32(pid),
		Metadata:   normalizeJSONBytes(metadata),
	})
	if err != nil {
		return agents.AgentInstance{}, fmt.Errorf("create agent instance: %w", err)
	}
	return storeInstanceToDomain(storeInst), nil
}

func (a *AgentQuerier) UpsertAgentInstance(ctx context.Context, agentID, instanceID, status, hostname string, pid int, metadata []byte) (agents.AgentInstance, error) {
	storeInst, err := a.q.UpsertAgentInstance(ctx, store.UpsertAgentInstanceParams{
		AgentID:    parsePGUUID(agentID),
		InstanceID: instanceID,
		Status:     status,
		Hostname:   hostname,
		Pid:        int32(pid),
		Metadata:   normalizeJSONBytes(metadata),
	})
	if err != nil {
		return agents.AgentInstance{}, fmt.Errorf("upsert agent instance: %w", err)
	}
	return storeInstanceToDomain(storeInst), nil
}

func (a *AgentQuerier) UpdateAgentInstanceHeartbeat(ctx context.Context, agentID, instanceID, status string) (time.Time, error) {
	storeInst, err := a.q.UpdateAgentInstanceHeartbeat(ctx, store.UpdateAgentInstanceHeartbeatParams{
		AgentID:    parsePGUUID(agentID),
		InstanceID: instanceID,
		Status:     status,
	})
	if err != nil {
		return time.Time{}, fmt.Errorf("update instance heartbeat: %w", err)
	}
	return storeInst.LastHeartbeatAt.Time, nil
}

func (a *AgentQuerier) ListAgentInstancesByAgent(ctx context.Context, agentID string) ([]agents.AgentInstance, error) {
	storeInstances, err := a.q.ListAgentInstancesByAgent(ctx, parsePGUUID(agentID))
	if err != nil {
		return nil, fmt.Errorf("list instances: %w", err)
	}
	result := make([]agents.AgentInstance, 0, len(storeInstances))
	for _, si := range storeInstances {
		result = append(result, storeInstanceToDomain(si))
	}
	return result, nil
}

func (a *AgentQuerier) CreateAgentHeartbeat(ctx context.Context, agentID, instanceID, status string, payload []byte) error {
	// Resolve the VARCHAR instance_id to the internal UUID for the FK constraint
	agentUUID := parsePGUUID(agentID)
	storeInst, err := a.q.GetAgentInstanceByAgentAndInstanceID(ctx, store.GetAgentInstanceByAgentAndInstanceIDParams{
		AgentID:    agentUUID,
		InstanceID: instanceID,
	})
	if err != nil {
		return fmt.Errorf("look up instance UUID: %w", err)
	}

	_, err = a.q.CreateAgentHeartbeat(ctx, store.CreateAgentHeartbeatParams{
		AgentID:    agentUUID,
		InstanceID: storeInst.ID,
		Status:     status,
		Payload:    normalizeJSONBytes(payload),
	})
	if err != nil {
		return fmt.Errorf("create heartbeat: %w", err)
	}
	return nil
}

func (a *AgentQuerier) SetCapability(ctx context.Context, agentID, name, description string, metadata []byte) error {
	_, err := a.q.CreateAgentCapability(ctx, store.CreateAgentCapabilityParams{
		AgentID:     parsePGUUID(agentID),
		Name:        name,
		Description: description,
		Metadata:    normalizeJSONBytes(metadata),
	})
	if err != nil {
		return fmt.Errorf("set capability: %w", err)
	}
	return nil
}

func (a *AgentQuerier) ListCapabilitiesByAgent(ctx context.Context, agentID string) ([]agents.AgentCapability, error) {
	storeCaps, err := a.q.ListAgentCapabilitiesByAgent(ctx, parsePGUUID(agentID))
	if err != nil {
		return nil, fmt.Errorf("list capabilities: %w", err)
	}
	result := make([]agents.AgentCapability, 0, len(storeCaps))
	for _, sc := range storeCaps {
		result = append(result, storeCapabilityToDomain(sc))
	}
	return result, nil
}

// ---- store-to-domain converters ----

func storeAgentToDomain(sa store.Agent) agents.Agent {
	return agents.Agent{
		ID:          UUIDToString(sa.ID.Bytes),
		ProjectID:   UUIDToString(sa.ProjectID.Bytes),
		Name:        sa.Name,
		Description: sa.Description,
		Metadata:    bytesToMap(sa.Metadata),
		Version:     int(sa.Version),
		Status:      sa.Status,
		CreatedAt:   sa.CreatedAt.Time,
		UpdatedAt:   sa.UpdatedAt.Time,
	}
}

func storeInstanceToDomain(si store.AgentInstance) agents.AgentInstance {
	return agents.AgentInstance{
		ID:              UUIDToString(si.ID.Bytes),
		AgentID:         UUIDToString(si.AgentID.Bytes),
		InstanceID:      si.InstanceID,
		Status:          si.Status,
		Hostname:        si.Hostname,
		PID:             int(si.Pid),
		Metadata:        bytesToMap(si.Metadata),
		StartedAt:       si.StartedAt.Time,
		LastHeartbeatAt: si.LastHeartbeatAt.Time,
	}
}

func storeCapabilityToDomain(sc store.AgentCapability) agents.AgentCapability {
	return agents.AgentCapability{
		ID:          UUIDToString(sc.ID.Bytes),
		AgentID:     UUIDToString(sc.AgentID.Bytes),
		Name:        sc.Name,
		Description: sc.Description,
		Metadata:    bytesToMap(sc.Metadata),
		CreatedAt:   sc.CreatedAt.Time,
	}
}

func bytesToMap(data []byte) map[string]interface{} {
	if len(data) == 0 {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	return m
}

func normalizeJSONBytes(data []byte) []byte {
	if len(data) == 0 {
		return []byte("{}")
	}
	return data
}
