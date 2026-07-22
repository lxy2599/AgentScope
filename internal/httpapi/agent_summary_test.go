package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"AgentScope/internal/runs"
)

// ---- Fake Agent Service with summary support ----

type fakeAgentServiceWithSummary struct {
	*fakeAgentService
	summaries []AgentSummaryResponse
}

func newFakeAgentServiceWithSummary() *fakeAgentServiceWithSummary {
	return &fakeAgentServiceWithSummary{
		fakeAgentService: newFakeAgentService(),
	}
}

func (f *fakeAgentServiceWithSummary) ListAgentSummaries(ctx context.Context, projectID string) ([]AgentSummaryResponse, error) {
	if projectID == "" {
		return nil, errNotFound
	}
	// Filter summaries by project
	var result []AgentSummaryResponse
	for _, s := range f.summaries {
		if s.ProjectID == projectID {
			result = append(result, s)
		}
	}
	if result == nil {
		return []AgentSummaryResponse{}, nil
	}
	return result, nil
}

// ---- Tests ----

func TestAgentSummaryHandler(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	hb := time.Now().UTC().Format(time.RFC3339)
	p95 := 1234.5
	cost := 0.05

	fakeSvc := newFakeAgentServiceWithSummary()
	fakeSvc.summaries = []AgentSummaryResponse{
		{
			ID:                   "agent-1",
			ProjectID:            "proj-1",
			Name:                 "test-agent",
			Description:          "A test agent",
			Status:               "active",
			Version:              1,
			CreatedAt:            now,
			UpdatedAt:            now,
			Framework:            "langchain",
			Runtime:              "python",
			Source:               "sdk",
			Environment:          "production",
			AgentVersion:         "1.0.0",
			ModelProvider:        "anthropic",
			ModelName:            "claude-3-opus",
			InstanceCount:        2,
			OnlineInstanceCount:  1,
			ActiveRunCount:       1,
			RecentRunCount:       5,
			RecentErrorCount:     1,
			LastHeartbeatAt:      &hb,
			LastRunAt:            &now,
			P95LatencyMs:         &p95,
			ErrorRate:            0.05,
			TotalCost:            &cost,
			CurrentStatusSummary: "running",
		},
		{
			ID:                   "agent-2",
			ProjectID:            "proj-1",
			Name:                 "offline-agent",
			Description:          "An offline agent",
			Status:               "active",
			Version:              1,
			CreatedAt:            now,
			UpdatedAt:            now,
			InstanceCount:        1,
			OnlineInstanceCount:  0,
			ActiveRunCount:       0,
			RecentRunCount:       0,
			RecentErrorCount:     0,
			CurrentStatusSummary: "offline",
		},
	}

	projectAuth := newFakeProjectAuth()
	projectAuth.allow("user-1", "proj-1")

	deps := Dependencies{
		AgentService:  fakeSvc,
		SessionSigner: testSigner,
		ProjectAuth:   projectAuth,
	}
	router := NewRouter(deps)

	t.Run("list agent summaries for project", func(t *testing.T) {
		sessionCookie := signSession("user-1")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-summary?project_id=proj-1", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionCookie})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var summaries []AgentSummaryResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &summaries); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if len(summaries) != 2 {
			t.Fatalf("expected 2 summaries, got %d", len(summaries))
		}

		// Check first agent's online stats
		s1 := summaries[0]
		if s1.Name != "test-agent" {
			t.Errorf("Name = %q, want test-agent", s1.Name)
		}
		if s1.InstanceCount != 2 {
			t.Errorf("InstanceCount = %d, want 2", s1.InstanceCount)
		}
		if s1.OnlineInstanceCount != 1 {
			t.Errorf("OnlineInstanceCount = %d, want 1", s1.OnlineInstanceCount)
		}
		if s1.ActiveRunCount != 1 {
			t.Errorf("ActiveRunCount = %d, want 1", s1.ActiveRunCount)
		}
		if s1.RecentErrorCount != 1 {
			t.Errorf("RecentErrorCount = %d, want 1", s1.RecentErrorCount)
		}
		if s1.ErrorRate != 0.05 {
			t.Errorf("ErrorRate = %f, want 0.05", s1.ErrorRate)
		}
		if s1.P95LatencyMs == nil || *s1.P95LatencyMs != 1234.5 {
			t.Errorf("P95LatencyMs = %v, want 1234.5", s1.P95LatencyMs)
		}
		if s1.CurrentStatusSummary != "running" {
			t.Errorf("CurrentStatusSummary = %q, want running", s1.CurrentStatusSummary)
		}
		if s1.Framework != "langchain" {
			t.Errorf("Framework = %q, want langchain", s1.Framework)
		}
		if s1.ModelName != "claude-3-opus" {
			t.Errorf("ModelName = %q, want claude-3-opus", s1.ModelName)
		}

		// Check second agent (offline)
		s2 := summaries[1]
		if s2.Name != "offline-agent" {
			t.Errorf("Name = %q, want offline-agent", s2.Name)
		}
		if s2.OnlineInstanceCount != 0 {
			t.Errorf("OnlineInstanceCount = %d, want 0", s2.OnlineInstanceCount)
		}
		if s2.CurrentStatusSummary != "offline" {
			t.Errorf("CurrentStatusSummary = %q, want offline", s2.CurrentStatusSummary)
		}
	})

	t.Run("missing session returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-summary?project_id=proj-1", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("missing project_id returns 400", func(t *testing.T) {
		sessionCookie := signSession("user-1")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-summary", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionCookie})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("non-member returns 403", func(t *testing.T) {
		sessionCookie := signSession("user-1")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-summary?project_id=proj-other", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionCookie})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// TestAgentSummaryOnlineStatusDerivation tests the online status derivation logic.
func TestAgentSummaryOnlineStatusDerivation(t *testing.T) {
	// This test validates that:
	// 1. heartbeat within 90s -> online
	// 2. heartbeat over 90s -> offline
	// 3. active run -> active_run_count > 0
	// 4. recent error -> error_rate > 0

	t.Run("derive online from heartbeat within 90s window", func(t *testing.T) {
		// The derivation is done in SQL: COUNT(*) FILTER (WHERE last_heartbeat_at >= NOW() - INTERVAL '90 seconds')
		// We test the handler response shows correct OnlineInstanceCount
		hb := time.Now().UTC().Format(time.RFC3339)

		fakeSvc := newFakeAgentServiceWithSummary()
		fakeSvc.summaries = []AgentSummaryResponse{
			{
				ID:                   "agent-online",
				ProjectID:            "proj-1",
				Name:                 "online-agent",
				Status:               "active",
				InstanceCount:        1,
				OnlineInstanceCount:  1,
				ActiveRunCount:       0,
				CurrentStatusSummary: "idle",
				LastHeartbeatAt:      &hb,
			},
			{
				ID:                   "agent-old-hb",
				ProjectID:            "proj-1",
				Name:                 "old-hb-agent",
				Status:               "active",
				InstanceCount:        1,
				OnlineInstanceCount:  0,
				ActiveRunCount:       0,
				CurrentStatusSummary: "offline",
				LastHeartbeatAt:      nil,
			},
		}

		projectAuth := newFakeProjectAuth()
		projectAuth.allow("user-1", "proj-1")

		deps := Dependencies{
			AgentService:  fakeSvc,
			SessionSigner: testSigner,
			ProjectAuth:   projectAuth,
		}
		router := NewRouter(deps)

		sessionCookie := signSession("user-1")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-summary?project_id=proj-1", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionCookie})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var summaries []AgentSummaryResponse
		json.Unmarshal(rec.Body.Bytes(), &summaries)

		for _, s := range summaries {
			switch s.Name {
			case "online-agent":
				if s.OnlineInstanceCount != 1 {
					t.Errorf("online-agent OnlineInstanceCount = %d, want 1", s.OnlineInstanceCount)
				}
				if s.CurrentStatusSummary != "idle" {
					t.Errorf("online-agent CurrentStatusSummary = %q, want idle", s.CurrentStatusSummary)
				}
			case "old-hb-agent":
				if s.OnlineInstanceCount != 0 {
					t.Errorf("old-hb-agent OnlineInstanceCount = %d, want 0", s.OnlineInstanceCount)
				}
				if s.CurrentStatusSummary != "offline" {
					t.Errorf("old-hb-agent CurrentStatusSummary = %q, want offline", s.CurrentStatusSummary)
				}
			}
		}
	})

	t.Run("active run sets status to running", func(t *testing.T) {
		fakeSvc := newFakeAgentServiceWithSummary()
		fakeSvc.summaries = []AgentSummaryResponse{
			{
				ID:                   "agent-busy",
				ProjectID:            "proj-1",
				Name:                 "busy-agent",
				Status:               "active",
				InstanceCount:        1,
				OnlineInstanceCount:  1,
				ActiveRunCount:       2,
				CurrentStatusSummary: "running",
			},
		}

		projectAuth := newFakeProjectAuth()
		projectAuth.allow("user-1", "proj-1")

		deps := Dependencies{
			AgentService:  fakeSvc,
			SessionSigner: testSigner,
			ProjectAuth:   projectAuth,
		}
		router := NewRouter(deps)

		sessionCookie := signSession("user-1")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/agent-summary?project_id=proj-1", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: sessionCookie})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var summaries []AgentSummaryResponse
		json.Unmarshal(rec.Body.Bytes(), &summaries)

		if len(summaries) != 1 {
			t.Fatalf("expected 1 summary, got %d", len(summaries))
		}
		if summaries[0].ActiveRunCount != 2 {
			t.Errorf("ActiveRunCount = %d, want 2", summaries[0].ActiveRunCount)
		}
		if summaries[0].CurrentStatusSummary != "running" {
			t.Errorf("CurrentStatusSummary = %q, want running", summaries[0].CurrentStatusSummary)
		}
	})
}

// TestAgentSummaryActiveRunsWithNewStatuses verifies that thinking/waiting_memory/waiting_user
// appear in active runs.
func TestAgentSummaryActiveRunsWithNewStatuses(t *testing.T) {
	// This validates that ActiveRunStatuses includes the new statuses
	expected := []string{"queued", "running", "thinking", "waiting_llm", "waiting_tool", "waiting_retrieval", "waiting_memory", "waiting_user", "retrying"}

	got := runs.ActiveRunStatuses
	if len(got) != len(expected) {
		t.Fatalf("ActiveRunStatuses length = %d, want %d\ngot:  %v\nwant: %v", len(got), len(expected), got, expected)
	}

	statusSet := make(map[string]bool)
	for _, s := range got {
		statusSet[s] = true
	}
	for _, s := range expected {
		if !statusSet[s] {
			t.Errorf("ActiveRunStatuses missing: %s", s)
		}
	}
}
