package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// ---- Agent SDK API (bearer token auth) ----

// registerAgentRequest is the request body for SDK agent registration.
type registerAgentRequest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// handleAgentRegister handles POST /api/v1/agents/register
func handleAgentRegister(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := authenticateAgentToken(w, r, deps)
		if !ok {
			return
		}

		var req registerAgentRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "missing_name", "Agent name is required")
			return
		}

		agent, err := deps.AgentService.RegisterAgent(r.Context(), projectID, req.Name, req.Description, req.Metadata)
		if err != nil {
			writeError(w, http.StatusBadRequest, "register_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, agent)
	}
}

// heartbeatRequest is the request body for agent heartbeat.
type agentHeartbeatRequest struct {
	InstanceID string                 `json:"instance_id"`
	Status     string                 `json:"status,omitempty"`
	Payload    map[string]interface{} `json:"payload,omitempty"`
}

// handleAgentHeartbeat handles POST /api/v1/agents/{agentID}/heartbeat
func handleAgentHeartbeat(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := authenticateAgentToken(w, r, deps)
		if !ok {
			return
		}

		agentID := chi.URLParam(r, "agentID")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "missing_agent_id", "Agent ID is required")
			return
		}

		var req agentHeartbeatRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}
		if req.InstanceID == "" {
			writeError(w, http.StatusBadRequest, "missing_instance_id", "Instance ID is required")
			return
		}

		// Verify agent belongs to the token's project
		agentInfo, err := deps.AgentService.GetAgentByID(r.Context(), agentID)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "Agent not found")
			return
		}
		if agentInfo.ProjectID != projectID {
			writeError(w, http.StatusForbidden, "forbidden", "Agent does not belong to this project")
			return
		}

		if err := deps.AgentService.RecordHeartbeat(r.Context(), agentID, req.InstanceID, req.Status, req.Payload); err != nil {
			writeError(w, http.StatusBadRequest, "heartbeat_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ---- UI Agent API (session auth) ----

// handleAgentList handles GET /api/v1/agents
func handleAgentList(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		projectID := r.URL.Query().Get("project_id")
		if projectID == "" {
			writeError(w, http.StatusBadRequest, "missing_project_id", "project_id query parameter is required")
			return
		}

		// Verify user is a member of the project
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, projectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a member of this project")
			return
		}

		agents, err := deps.AgentService.ListAgents(r.Context(), projectID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", "Failed to list agents")
			return
		}

		writeJSON(w, http.StatusOK, agents)
	}
}

// handleAgentDetail handles GET /api/v1/agents/{agentID}
func handleAgentDetail(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		agentID := chi.URLParam(r, "agentID")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "missing_agent_id", "Agent ID is required")
			return
		}

		detail, err := deps.AgentService.GetAgentDetail(r.Context(), agentID)
		if err != nil {
			if err == errNotFound {
				writeError(w, http.StatusNotFound, "not_found", "Agent not found")
				return
			}
			writeError(w, http.StatusInternalServerError, "detail_failed", "Failed to get agent detail")
			return
		}


		// Verify user is a member of the agent's project
		if err := deps.ProjectAuth.IsProjectMember(r.Context(), userID, detail.ProjectID); err != nil {
			writeError(w, http.StatusForbidden, "forbidden", "Not a member of this project")
			return
		}

		writeJSON(w, http.StatusOK, detail)
	}
}

// ---- Capabilities API (bearer token auth) ----

// setCapabilityRequest is the request body for setting a capability.
type setCapabilityRequest struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// handleSetCapability handles POST /api/v1/agents/{agentID}/capabilities
func handleSetCapability(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID, ok := authenticateAgentToken(w, r, deps)
		if !ok {
			return
		}

		agentID := chi.URLParam(r, "agentID")
		if agentID == "" {
			writeError(w, http.StatusBadRequest, "missing_agent_id", "Agent ID is required")
			return
		}

		// Verify agent belongs to the token's project
		agentInfo, err := deps.AgentService.GetAgentByID(r.Context(), agentID)
		if err != nil {
			writeError(w, http.StatusNotFound, "not_found", "Agent not found")
			return
		}
		if agentInfo.ProjectID != projectID {
			writeError(w, http.StatusForbidden, "forbidden", "Agent does not belong to this project")
			return
		}

		var req setCapabilityRequest
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
			return
		}
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "missing_name", "Capability name is required")
			return
		}

		if err := deps.AgentService.SetCapability(r.Context(), agentID, req.Name, req.Description, req.Metadata); err != nil {
			writeError(w, http.StatusBadRequest, "capability_failed", err.Error())
			return
		}

		writeJSON(w, http.StatusCreated, map[string]string{"status": "created"})
	}
}

// ---- auth helper ----

// authenticateAgentToken extracts the bearer token and authenticates with scope "agent:register".
func authenticateAgentToken(w http.ResponseWriter, r *http.Request, deps Dependencies) (string, bool) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" || len(authHeader) < 7 || authHeader[:7] != "Bearer " {
		writeError(w, http.StatusUnauthorized, "unauthorized", "Missing or invalid authorization header")
		return "", false
	}
	plainToken := authHeader[7:]

	projectID, _, err := deps.TokenAuth.AuthenticateToken(r.Context(), plainToken, "agent:register")
	if err != nil {
		writeError(w, http.StatusForbidden, "forbidden", "Token lacks required scope or is invalid")
		return "", false
	}

	return projectID, true
}
