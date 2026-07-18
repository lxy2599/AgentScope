package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---- Fake Agent Service ----

type fakeAgentService struct {
	agents       map[string]AgentInfo
	instances    map[string][]AgentInstanceInfo
	capabilities map[string][]AgentCapabilityInfo
	counter      int
}

func newFakeAgentService() *fakeAgentService {
	return &fakeAgentService{
		agents:       make(map[string]AgentInfo),
		instances:    make(map[string][]AgentInstanceInfo),
		capabilities: make(map[string][]AgentCapabilityInfo),
	}
}

func (f *fakeAgentService) RegisterAgent(ctx context.Context, projectID, name, description string, metadata map[string]interface{}) (AgentInfo, error) {
	f.counter++
	id := "agent-" + itoa(f.counter)
	a := AgentInfo{
		ID:          id,
		ProjectID:   projectID,
		Name:        name,
		Description: description,
		Status:      "active",
		Version:     1,
		Metadata:    metadata,
	}
	f.agents[id] = a
	return a, nil
}

func (f *fakeAgentService) ListAgents(ctx context.Context, projectID string) ([]AgentInfo, error) {
	var result []AgentInfo
	for _, a := range f.agents {
		if a.ProjectID == projectID {
			result = append(result, a)
		}
	}
	return result, nil
}

func (f *fakeAgentService) GetAgentDetail(ctx context.Context, agentID string) (AgentDetail, error) {
	a, ok := f.agents[agentID]
	if !ok {
		return AgentDetail{}, errNotFound
	}
	instances := f.instances[agentID]
	if instances == nil {
		instances = []AgentInstanceInfo{}
	}
	caps := f.capabilities[agentID]
	if caps == nil {
		caps = []AgentCapabilityInfo{}
	}
	return AgentDetail{
		AgentInfo:    a,
		Instances:    instances,
		Capabilities: caps,
	}, nil
}

func (f *fakeAgentService) RecordHeartbeat(ctx context.Context, agentID, instanceID, status string, payload map[string]interface{}) error {
	if _, ok := f.agents[agentID]; !ok {
		return errNotFound
	}
	return nil
}

func (f *fakeAgentService) SetCapability(ctx context.Context, agentID, name, description string, metadata map[string]interface{}) error {
	if _, ok := f.agents[agentID]; !ok {
		return errNotFound
	}
	return nil
}

// ---- Tests ----

func TestAgentRegisterHandler(t *testing.T) {
	fakeSvc := newFakeAgentService()
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_valid_agent_token", "proj-1", []string{"agent:register"})

	deps := Dependencies{
		TokenAuth:    tokenAuth,
		AgentService: fakeSvc,
	}
	router := NewRouter(deps)

	t.Run("successful register", func(t *testing.T) {
		body := `{"name":"test-agent","description":"A test agent","metadata":{"framework":"langchain"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer ascope_valid_agent_token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp AgentInfo
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if resp.Name != "test-agent" {
			t.Errorf("Name = %q, want test-agent", resp.Name)
		}
		if resp.Status != "active" {
			t.Errorf("Status = %q, want active", resp.Status)
		}
	})

	t.Run("missing name returns 400", func(t *testing.T) {
		body := `{"description":"no name"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer ascope_valid_agent_token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("missing token returns 401", func(t *testing.T) {
		body := `{"name":"no-token-agent"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("wrong scope returns 403", func(t *testing.T) {
		body := `{"name":"wrong-scope-agent"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/register", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer ascope_valid_agent_token")
		// Temporarily remove the right scope by using a token with wrong scope
		wrongAuth := newFakeTokenAuth()
		wrongAuth.addToken("ascope_wrong_scope", "proj-1", []string{"otlp:write"})
		deps2 := Dependencies{
			TokenAuth:    wrongAuth,
			AgentService: fakeSvc,
		}
		router2 := NewRouter(deps2)

		req2 := httptest.NewRequest(http.MethodPost, "/api/v1/agents/register", strings.NewReader(body))
		req2.Header.Set("Content-Type", "application/json")
		req2.Header.Set("Authorization", "Bearer ascope_wrong_scope")
		rec2 := httptest.NewRecorder()
		router2.ServeHTTP(rec2, req2)

		if rec2.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec2.Code)
		}
	})
}

func TestAgentHeartbeatHandler(t *testing.T) {
	fakeSvc := newFakeAgentService()
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_valid_hb_token", "proj-1", []string{"agent:register"})

	deps := Dependencies{
		TokenAuth:    tokenAuth,
		AgentService: fakeSvc,
	}
	router := NewRouter(deps)

	t.Run("successful heartbeat", func(t *testing.T) {
		// First register an agent
		registerBody := `{"name":"hb-agent"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/register", strings.NewReader(registerBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer ascope_valid_hb_token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("register failed: %d", rec.Code)
		}
		var regResp AgentInfo
		json.Unmarshal(rec.Body.Bytes(), &regResp)

		// Send heartbeat
		hbBody := `{"instance_id":"inst-1","status":"online","payload":{"cpu":0.5}}`
		hbReq := httptest.NewRequest(http.MethodPost, "/api/v1/agents/"+regResp.ID+"/heartbeat", strings.NewReader(hbBody))
		hbReq.Header.Set("Content-Type", "application/json")
		hbReq.Header.Set("Authorization", "Bearer ascope_valid_hb_token")
		hbRec := httptest.NewRecorder()
		router.ServeHTTP(hbRec, hbReq)

		if hbRec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", hbRec.Code, hbRec.Body.String())
		}
	})

	t.Run("missing token returns 401", func(t *testing.T) {
		hbBody := `{"instance_id":"inst-2"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/nonexistent/heartbeat", strings.NewReader(hbBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("missing instance_id returns 400", func(t *testing.T) {
		hbBody := `{"status":"online"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/test/heartbeat", strings.NewReader(hbBody))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer ascope_valid_hb_token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestAgentListHandler(t *testing.T) {
	fakeSvc := newFakeAgentService()
	deps := Dependencies{
		AgentService: fakeSvc,
		SessionSigner: testSigner,
	}
	router := NewRouter(deps)

	// Register agents through fake bypass
	fakeSvc.RegisterAgent(nil, "proj-1", "agent-a", "Agent A", nil)
	fakeSvc.RegisterAgent(nil, "proj-1", "agent-b", "Agent B", nil)

	t.Run("list agents with session", func(t *testing.T) {
		sessionCookie := signSession("user-1")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents?project_id=proj-1", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionCookie})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var agents []AgentInfo
		if err := json.Unmarshal(rec.Body.Bytes(), &agents); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if len(agents) != 2 {
			t.Errorf("expected 2 agents, got %d", len(agents))
		}
	})

	t.Run("missing session returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents?project_id=proj-1", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("missing project_id returns 400", func(t *testing.T) {
		sessionCookie := signSession("user-1")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionCookie})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestAgentDetailHandler(t *testing.T) {
	fakeSvc := newFakeAgentService()
	fakeSvc.RegisterAgent(nil, "proj-1", "detail-agent", "Detail test", nil)

	deps := Dependencies{
		AgentService:  fakeSvc,
		SessionSigner: testSigner,
	}
	router := NewRouter(deps)

	t.Run("get agent detail", func(t *testing.T) {
		sessionCookie := signSession("user-1")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/agent-1", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionCookie})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var detail AgentDetail
		if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		if detail.Name != "detail-agent" {
			t.Errorf("Name = %q", detail.Name)
		}
		if detail.Instances == nil {
			t.Error("Instances should be non-nil")
		}
	})

	t.Run("non-existent agent returns 404", func(t *testing.T) {
		sessionCookie := signSession("user-1")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/nonexistent", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionCookie})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func signSession(userID string) string {
	s, _ := testSigner.SignUserID(userID)
	return s
}
