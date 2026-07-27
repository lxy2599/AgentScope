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

func (a *AgentQuerier) CreateAgentInstance(ctx context.Context, agentID, projectID, instanceID, instanceName, status, hostname string, pid int, metadata []byte) (agents.AgentInstance, error) {
	storeInst, err := a.q.CreateAgentInstance(ctx, store.CreateAgentInstanceParams{
		AgentID:      parsePGUUID(agentID),
		ProjectID:    parsePGUUID(projectID),
		InstanceID:   instanceID,
		InstanceName: instanceName,
		Status:       status,
		Hostname:     hostname,
		Pid:          int32(pid),
		Metadata:     normalizeJSONBytes(metadata),
	})
	if err != nil {
		return agents.AgentInstance{}, fmt.Errorf("create agent instance: %w", err)
	}
	return storeInstanceToDomain(storeInst), nil
}

func (a *AgentQuerier) UpsertAgentInstance(ctx context.Context, agentID, projectID, instanceID, instanceName, status, hostname string, pid int, metadata []byte) (agents.AgentInstance, error) {
	storeInst, err := a.q.UpsertAgentInstance(ctx, store.UpsertAgentInstanceParams{
		AgentID:      parsePGUUID(agentID),
		ProjectID:    parsePGUUID(projectID),
		InstanceID:   instanceID,
		InstanceName: instanceName,
		Status:       status,
		Hostname:     hostname,
		Pid:          int32(pid),
		Metadata:     normalizeJSONBytes(metadata),
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

func (a *AgentQuerier) ListAgentSummariesByProject(ctx context.Context, projectID string) ([]agents.AgentSummary, error) {
	rows, err := a.q.ListAgentSummariesByProject(ctx, parsePGUUID(projectID))
	if err != nil {
		return nil, fmt.Errorf("list agent summaries: %w", err)
	}
	result := make([]agents.AgentSummary, 0, len(rows))
	for _, r := range rows {
		summary := storeSummaryRowToDomain(r)
		result = append(result, summary)
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
	var activeRunID string
	if si.ActiveRunID.Valid {
		activeRunID = UUIDToString(si.ActiveRunID.Bytes)
	}
	return agents.AgentInstance{
		ID:              UUIDToString(si.ID.Bytes),
		AgentID:         UUIDToString(si.AgentID.Bytes),
		ProjectID:       UUIDToString(si.ProjectID.Bytes),
		InstanceID:      si.InstanceID,
		InstanceName:    si.InstanceName,
		Status:          si.Status,
		Hostname:        si.Hostname,
		PID:             int(si.Pid),
		ActiveRunID:     activeRunID,
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

// storeSummaryRowToDomain converts a store list summary row to an AgentSummary.
func storeSummaryRowToDomain(r store.ListAgentSummariesByProjectRow) agents.AgentSummary {
	p95 := r.P95LatencyMs
	var p95Ptr *int64
	if p95 > 0 {
		p95Ptr = &p95
	}

	cost := numericToFloat64(r.TotalCost)
	var costPtr *float64
	if cost > 0 {
		costPtr = &cost
	}

	meta := bytesToMap(r.Metadata)

	// Extract runtime contract fields from metadata
	framework, _ := meta["framework"].(string)
	runtime, _ := meta["runtime"].(string)
	source, _ := meta["source"].(string)
	environment, _ := meta["environment"].(string)
	agentVersion, _ := meta["agent_version"].(string)
	modelProvider, _ := meta["model_provider"].(string)
	modelName, _ := meta["model_name"].(string)
	harnessVersion, _ := meta["harness_version"].(string)
	promptVersion, _ := meta["prompt_version"].(string)

	// Derive current status summary
	statusSummary := deriveStatusSummary(r)

	return agents.AgentSummary{
		ID:                   r.AID,
		ProjectID:            r.AProjectID,
		Name:                 r.Name,
		Description:          r.Description,
		Status:               r.Status,
		Version:              int(r.Version),
		Metadata:             meta,
		CreatedAt:            r.ACreatedAt.Time,
		UpdatedAt:            r.AUpdatedAt.Time,
		Framework:            framework,
		Runtime:              runtime,
		Source:               source,
		Environment:          environment,
		AgentVersion:         agentVersion,
		ModelProvider:        modelProvider,
		ModelName:            modelName,
		HarnessVersion:       harnessVersion,
		PromptVersion:        promptVersion,
		InstanceCount:        int(r.InstanceCount),
		OnlineInstanceCount:  int(r.OnlineInstanceCount),
		ActiveRunCount:       int(r.ActiveRunCount),
		RecentRunCount:       int(r.RecentRunCount),
		RecentErrorCount:     int(r.RecentErrorCount),
		LastHeartbeatAt:      pgTimestamptzPtr(r.LastHeartbeatAt),
		LastRunAt:            pgTimestamptzPtr(r.LastRunAt),
		P95LatencyMs:         p95Ptr,
		ErrorRate:            numericToFloat64(r.ErrorRate),
		TotalCost:            costPtr,
		CurrentStatusSummary: statusSummary,
	}
}

// deriveStatusSummary computes the current_status_summary from raw stats.
func deriveStatusSummary(r store.ListAgentSummariesByProjectRow) string {
	if r.OnlineInstanceCount > 0 {
		if r.ActiveRunCount > 0 {
			return "running"
		}
		return "idle"
	}
	if r.InstanceCount > 0 {
		return "offline"
	}
	return "unknown"
}
