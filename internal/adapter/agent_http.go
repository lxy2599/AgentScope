package adapter

import (
	"context"

	"AgentScope/internal/agents"
	"AgentScope/internal/httpapi"
)

// HTTPAgentService implements httpapi.AgentService using *agents.Service.
type HTTPAgentService struct {
	svc *agents.Service
}

var _ httpapi.AgentService = (*HTTPAgentService)(nil)

// NewHTTPAgentService creates a new HTTPAgentService adapter.
func NewHTTPAgentService(svc *agents.Service) *HTTPAgentService {
	return &HTTPAgentService{svc: svc}
}

func (h *HTTPAgentService) RegisterAgent(ctx context.Context, projectID, name, description string, metadata map[string]interface{}) (httpapi.AgentInfo, error) {
	a, err := h.svc.RegisterAgent(ctx, projectID, name, description, metadata)
	if err != nil {
		return httpapi.AgentInfo{}, err
	}
	return httpapi.AgentInfo{
		ID:          a.ID,
		ProjectID:   a.ProjectID,
		Name:        a.Name,
		Description: a.Description,
		Metadata:    a.Metadata,
		Version:     a.Version,
		Status:      a.Status,
		CreatedAt:   a.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   a.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}, nil
}

func (h *HTTPAgentService) ListAgents(ctx context.Context, projectID string) ([]httpapi.AgentInfo, error) {
	list, err := h.svc.ListAgents(ctx, projectID)
	if err != nil {
		return nil, err
	}
	result := make([]httpapi.AgentInfo, 0, len(list))
	for _, a := range list {
		result = append(result, httpapi.AgentInfo{
			ID:          a.ID,
			ProjectID:   a.ProjectID,
			Name:        a.Name,
			Description: a.Description,
			Metadata:    a.Metadata,
			Version:     a.Version,
			Status:      a.Status,
			CreatedAt:   a.CreatedAt.Format("2006-01-02T15:04:05Z"),
			UpdatedAt:   a.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	return result, nil
}

func (h *HTTPAgentService) GetAgentDetail(ctx context.Context, agentID string) (httpapi.AgentDetail, error) {
	a, err := h.svc.GetAgent(ctx, agentID)
	if err != nil {
		return httpapi.AgentDetail{}, err
	}

	instances, err := h.svc.ListInstances(ctx, agentID)
	if err != nil {
		return httpapi.AgentDetail{}, err
	}

	caps, err := h.svc.ListCapabilities(ctx, agentID)
	if err != nil {
		return httpapi.AgentDetail{}, err
	}

	agentInfo := httpapi.AgentInfo{
		ID:          a.ID,
		ProjectID:   a.ProjectID,
		Name:        a.Name,
		Description: a.Description,
		Metadata:    a.Metadata,
		Version:     a.Version,
		Status:      a.Status,
		CreatedAt:   a.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   a.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}

	instanceInfos := make([]httpapi.AgentInstanceInfo, 0, len(instances))
	for _, inst := range instances {
		instanceInfos = append(instanceInfos, httpapi.AgentInstanceInfo{
			ID:              inst.ID,
			ProjectID:       inst.ProjectID,
			InstanceName:    inst.InstanceName,
			ActiveRunID:     inst.ActiveRunID,
			AgentID:         inst.AgentID,
			InstanceID:      inst.InstanceID,
			Status:          inst.Status,
			Hostname:        inst.Hostname,
			PID:             inst.PID,
			Metadata:        inst.Metadata,
			StartedAt:       inst.StartedAt.Format("2006-01-02T15:04:05Z"),
			LastHeartbeatAt: inst.LastHeartbeatAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	capInfos := make([]httpapi.AgentCapabilityInfo, 0, len(caps))
	for _, cap := range caps {
		capInfos = append(capInfos, httpapi.AgentCapabilityInfo{
			ID:          cap.ID,
			AgentID:     cap.AgentID,
			Name:        cap.Name,
			Description: cap.Description,
			Metadata:    cap.Metadata,
			CreatedAt:   cap.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	return httpapi.AgentDetail{
		AgentInfo:    agentInfo,
		Instances:    instanceInfos,
		Capabilities: capInfos,
	}, nil
}

func (h *HTTPAgentService) GetAgentByID(ctx context.Context, agentID string) (httpapi.AgentInfo, error) {
	a, err := h.svc.GetAgent(ctx, agentID)
	if err != nil {
		return httpapi.AgentInfo{}, err
	}
	return httpapi.AgentInfo{
		ID:          a.ID,
		ProjectID:   a.ProjectID,
		Name:        a.Name,
		Description: a.Description,
		Metadata:    a.Metadata,
		Version:     a.Version,
		Status:      a.Status,
		CreatedAt:   a.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:   a.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}, nil
}

func (h *HTTPAgentService) RecordHeartbeat(ctx context.Context, agentID, projectID, instanceID, instanceName, status string, payload map[string]interface{}) error {
	return h.svc.RecordHeartbeat(ctx, agentID, projectID, instanceID, instanceName, status, payload)
}

func (h *HTTPAgentService) SetCapability(ctx context.Context, agentID, name, description string, metadata map[string]interface{}) error {
	return h.svc.SetCapability(ctx, agentID, name, description, metadata)
}
