package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"AgentScope/internal/quotas"
	"AgentScope/internal/realtime"
)

type fakeRealtimeSubscriber struct {
	mu        sync.Mutex
	projectID string
	afterID   string
	filter    realtime.SubscriptionFilter
	items     chan realtime.StreamItem
	err       error
}

func (f *fakeRealtimeSubscriber) Subscribe(
	_ context.Context,
	projectID, afterID string,
	filter realtime.SubscriptionFilter,
) (<-chan realtime.StreamItem, error) {
	f.mu.Lock()
	f.projectID = projectID
	f.afterID = afterID
	f.filter = filter
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return f.items, nil
}

type selectiveProjectAuth struct {
	err error
}

func (s selectiveProjectAuth) IsProjectMember(context.Context, string, string) error {
	return s.err
}

func TestRealtimeStreamRequiresAuthentication(t *testing.T) {
	router := NewRouter(Dependencies{
		SessionSigner:      testSigner,
		ProjectAuth:        selectiveProjectAuth{},
		RealtimeSubscriber: &fakeRealtimeSubscriber{},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/projects/project-1/events/stream", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestRealtimeStreamRejectsNonMember(t *testing.T) {
	router := NewRouter(Dependencies{
		SessionSigner:      testSigner,
		ProjectAuth:        selectiveProjectAuth{err: errors.New("not a member")},
		RealtimeSubscriber: &fakeRealtimeSubscriber{},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/projects/project-1/events/stream", nil)
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestRealtimeStreamUnavailable(t *testing.T) {
	router := NewRouter(Dependencies{
		SessionSigner: testSigner,
		ProjectAuth:   selectiveProjectAuth{},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/projects/project-1/events/stream", nil)
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "realtime_unavailable") {
		t.Fatalf("response = %s, want realtime_unavailable", rec.Body.String())
	}
}

func TestRealtimeStreamQuotaFailureIsClosed(t *testing.T) {
	quotaService := &fakeQuotaService{sse: quotas.Decision{
		Allowed:   false,
		Dimension: quotas.DimensionSSEConnections,
		Limit:     1,
		Remaining: 0,
		Degraded:  true,
	}}
	router := NewRouter(Dependencies{
		SessionSigner:      testSigner,
		ProjectAuth:        selectiveProjectAuth{},
		RealtimeSubscriber: &fakeRealtimeSubscriber{},
		QuotaService:       quotaService,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/projects/project-1/events/stream", nil)
	request.AddCookie(signCookie(t, "user-1"))
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503: %s", response.Code, response.Body.String())
	}
}

func TestRealtimeStreamReleasesQuotaAfterDisconnect(t *testing.T) {
	items := make(chan realtime.StreamItem)
	close(items)
	quotaService := &fakeQuotaService{sse: quotas.Decision{
		Allowed:   true,
		Dimension: quotas.DimensionSSEConnections,
		Limit:     1,
		Remaining: 0,
	}}
	router := NewRouter(Dependencies{
		SessionSigner:      testSigner,
		ProjectAuth:        selectiveProjectAuth{},
		RealtimeSubscriber: &fakeRealtimeSubscriber{items: items},
		QuotaService:       quotaService,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/projects/project-1/events/stream", nil)
	request.AddCookie(signCookie(t, "user-1"))
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if quotaService.releaseCount != 1 {
		t.Fatalf("release count = %d, want 1", quotaService.releaseCount)
	}
}

func TestRealtimeStreamEncodesEventAndPassesFilters(t *testing.T) {
	items := make(chan realtime.StreamItem, 1)
	items <- realtime.StreamItem{Event: &realtime.Event{
		ID:              42,
		SchemaVersion:   realtime.SchemaVersionV1,
		ProjectID:       "project-1",
		RunID:           "run-1",
		AgentID:         "agent-1",
		Type:            realtime.EventRunStateChanged,
		OccurredAt:      time.Date(2026, 7, 24, 9, 0, 0, 0, time.UTC),
		Payload:         json.RawMessage(`{"status":"thinking"}`),
		PayloadRedacted: true,
	}}
	close(items)
	subscriber := &fakeRealtimeSubscriber{items: items}
	router := NewRouter(Dependencies{
		SessionSigner:      testSigner,
		ProjectAuth:        selectiveProjectAuth{},
		RealtimeSubscriber: subscriber,
	})
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/projects/project-1/events/stream?run_id=run-1&agent_id=agent-1&types=run.state.changed,run.completed",
		nil,
	)
	req.Header.Set("Last-Event-ID", "41")
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"id: 42\n",
		"event: run.state.changed\n",
		`"project_id":"project-1"`,
		`"payload_redacted":true`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q:\n%s", want, body)
		}
	}
	if subscriber.projectID != "project-1" || subscriber.afterID != "41" {
		t.Fatalf("subscriber identity = project %q cursor %q", subscriber.projectID, subscriber.afterID)
	}
	if subscriber.filter.RunID != "run-1" || subscriber.filter.AgentID != "agent-1" {
		t.Fatalf("subscriber filter = %#v", subscriber.filter)
	}
	if _, ok := subscriber.filter.Types[realtime.EventRunCompleted]; !ok {
		t.Fatalf("subscriber types = %#v", subscriber.filter.Types)
	}
}

func TestRealtimeStreamEncodesReset(t *testing.T) {
	items := make(chan realtime.StreamItem, 1)
	items <- realtime.StreamItem{Reset: true, ResetReason: realtime.ResetCursorExpired}
	close(items)
	router := NewRouter(Dependencies{
		SessionSigner:      testSigner,
		ProjectAuth:        selectiveProjectAuth{},
		RealtimeSubscriber: &fakeRealtimeSubscriber{items: items},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/projects/project-1/events/stream", nil)
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "event: reset\n") ||
		!strings.Contains(body, `"reason":"cursor_expired"`) {
		t.Fatalf("reset response = %q", body)
	}
}

func TestRealtimeStreamSendsHeartbeat(t *testing.T) {
	items := make(chan realtime.StreamItem)
	subscriber := &fakeRealtimeSubscriber{items: items}
	router := NewRouter(Dependencies{
		SessionSigner:      testSigner,
		ProjectAuth:        selectiveProjectAuth{},
		RealtimeSubscriber: subscriber,
		RealtimeHeartbeat:  time.Millisecond,
	})
	req := httptest.NewRequest(http.MethodGet, "/api/projects/project-1/events/stream", nil)
	ctx, cancel := context.WithCancel(req.Context())
	timer := time.AfterFunc(20*time.Millisecond, cancel)
	defer timer.Stop()
	req = req.WithContext(ctx)
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), ": heartbeat") {
		t.Fatalf("response has no heartbeat: %q", rec.Body.String())
	}
}
