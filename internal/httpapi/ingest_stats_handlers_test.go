package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ---- fake IngestStatsQuerier ----

type fakeIngestStats struct {
	projectStats  map[string]*IngestStatsDTO
	agentStats    map[string][]AgentStatsDTO
	queryErr      error
}

func newFakeIngestStats() *fakeIngestStats {
	return &fakeIngestStats{
		projectStats: make(map[string]*IngestStatsDTO),
		agentStats:   make(map[string][]AgentStatsDTO),
	}
}

func (f *fakeIngestStats) QueryProjectStats(ctx context.Context, projectID string, minutes int) (*IngestStatsDTO, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	s, ok := f.projectStats[projectID]
	if !ok {
		return &IngestStatsDTO{ProjectID: projectID, Minutes: minutes, CollectedAt: time.Now().UTC()}, nil
	}
	return s, nil
}

func (f *fakeIngestStats) QueryAgentStats(ctx context.Context, projectID string, minutes int) ([]AgentStatsDTO, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	s, ok := f.agentStats[projectID]
	if !ok {
		return []AgentStatsDTO{}, nil
	}
	return s, nil
}

// ---- Tests ----

func TestIngestStatsHandler_noAuth(t *testing.T) {
	deps := Dependencies{
		AuthService:   newFakeAuthService(),
		SessionSigner: testSigner,
	}
	router := NewRouter(deps)

	req := httptest.NewRequest(http.MethodGet, "/api/projects/proj-1/ingest-stats", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without session cookie, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestIngestStatsHandler_nonMember(t *testing.T) {
	auth := newFakeProjectAuth()
	auth.allow("user-1", "proj-2") // user-1 is member of proj-2, not proj-1

	deps := Dependencies{
		AuthService:   newFakeAuthService(),
		SessionSigner: testSigner,
		ProjectAuth:   auth,
	}
	router := NewRouter(deps)

	req := httptest.NewRequest(http.MethodGet, "/api/projects/proj-1/ingest-stats", nil)
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestIngestStatsHandler_success(t *testing.T) {
	stats := newFakeIngestStats()
	stats.projectStats["proj-1"] = &IngestStatsDTO{
		ProjectID:   "proj-1",
		Minutes:     15,
		CollectedAt: time.Now().UTC(),
		Spans:       100,
		Events:      50,
		Errors:      3,
		LLMs:        20,
		Tools:       10,
		Retrievals:  5,
		Skills:      2,
		Tokens:      15000,
		Cost:        0.05,
	}

	auth := newFakeProjectAuth()
	auth.allow("user-1", "proj-1")

	deps := Dependencies{
		AuthService:   newFakeAuthService(),
		SessionSigner: testSigner,
		ProjectAuth:   auth,
		IngestStats:   stats,
	}
	router := NewRouter(deps)

	req := httptest.NewRequest(http.MethodGet, "/api/projects/proj-1/ingest-stats", nil)
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result IngestStatsDTO
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if result.Spans != 100 {
		t.Errorf("spans = %d, want 100", result.Spans)
	}
	if result.Errors != 3 {
		t.Errorf("errors = %d, want 3", result.Errors)
	}
	if result.LLMs != 20 {
		t.Errorf("llms = %d, want 20", result.LLMs)
	}
	if result.Tokens != 15000 {
		t.Errorf("tokens = %d, want 15000", result.Tokens)
	}
}

func TestIngestAgentStatsHandler_nonMember(t *testing.T) {
	auth := newFakeProjectAuth()
	auth.allow("user-1", "proj-2") // user-1 is member of proj-2, not proj-1

	deps := Dependencies{
		AuthService:   newFakeAuthService(),
		SessionSigner: testSigner,
		ProjectAuth:   auth,
	}
	router := NewRouter(deps)

	req := httptest.NewRequest(http.MethodGet, "/api/projects/proj-1/ingest-stats/agents", nil)
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-member, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestIngestAgentStatsHandler_success(t *testing.T) {
	stats := newFakeIngestStats()
	stats.agentStats["proj-1"] = []AgentStatsDTO{
		{AgentName: "agent-a", ErrorCount: 5, RunCount: 10, P95Latency: 1.5},
		{AgentName: "agent-b", ErrorCount: 2, RunCount: 8, P95Latency: 2.3},
	}

	auth := newFakeProjectAuth()
	auth.allow("user-1", "proj-1")

	deps := Dependencies{
		AuthService:   newFakeAuthService(),
		SessionSigner: testSigner,
		ProjectAuth:   auth,
		IngestStats:   stats,
	}
	router := NewRouter(deps)

	req := httptest.NewRequest(http.MethodGet, "/api/projects/proj-1/ingest-stats/agents", nil)
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var result []AgentStatsDTO
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("got %d agent stats, want 2", len(result))
	}
	if result[0].AgentName != "agent-a" {
		t.Errorf("agent[0].AgentName = %q, want agent-a", result[0].AgentName)
	}
	if result[0].ErrorCount != 5 {
		t.Errorf("agent[0].ErrorCount = %d, want 5", result[0].ErrorCount)
	}
	if result[0].RunCount != 10 {
		t.Errorf("agent[0].RunCount = %d, want 10", result[0].RunCount)
	}
}

func TestIngestStatsHandler_withIngestStatsNil(t *testing.T) {
	auth := newFakeProjectAuth()
	auth.allow("user-1", "proj-1")

	deps := Dependencies{
		AuthService:   newFakeAuthService(),
		SessionSigner: testSigner,
		ProjectAuth:   auth,
		IngestStats:   nil, // not wired
	}
	router := NewRouter(deps)

	req := httptest.NewRequest(http.MethodGet, "/api/projects/proj-1/ingest-stats", nil)
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Should still return 200 with a placeholder message
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even without IngestStats, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestIngestAgentStatsHandler_withIngestStatsNil(t *testing.T) {
	auth := newFakeProjectAuth()
	auth.allow("user-1", "proj-1")

	deps := Dependencies{
		AuthService:   newFakeAuthService(),
		SessionSigner: testSigner,
		ProjectAuth:   auth,
		IngestStats:   nil, // not wired
	}
	router := NewRouter(deps)

	req := httptest.NewRequest(http.MethodGet, "/api/projects/proj-1/ingest-stats/agents", nil)
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 even without IngestStats, got %d: %s", rec.Code, rec.Body.String())
	}

	var result []AgentStatsDTO
	if err := json.NewDecoder(rec.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result) != 0 {
		t.Fatalf("expected empty array, got %d items", len(result))
	}
}
