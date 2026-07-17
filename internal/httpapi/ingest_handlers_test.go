package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"AgentScope/internal/ingest"
	"AgentScope/internal/runs"
)

// fakeTokenAuth is a minimal token authenticator for ingest tests.
type fakeTokenAuth struct {
	validTokens map[string]tokenInfo
}

type tokenInfo struct {
	projectID string
	scopes    []string
}

func newFakeTokenAuth() *fakeTokenAuth {
	return &fakeTokenAuth{validTokens: make(map[string]tokenInfo)}
}

func (f *fakeTokenAuth) addToken(plain, projectID string, scopes []string) {
	f.validTokens[plain] = tokenInfo{projectID: projectID, scopes: scopes}
}

func (f *fakeTokenAuth) AuthenticateToken(ctx context.Context, plainToken, requiredScope string) (string, string, error) {
	tok, ok := f.validTokens[plainToken]
	if !ok {
		return "", "", errors.New("unauthorized")
	}
	scopeFound := false
	for _, s := range tok.scopes {
		if s == requiredScope {
			scopeFound = true
			break
		}
	}
	if !scopeFound {
		return "", "", errors.New("forbidden")
	}
	return tok.projectID, "tok-1", nil
}

// fakeRunsService implements RunsServiceInterface for testing.
type fakeRunsService struct {
	runs    []runsEntry
	counter int
}

type runsEntry struct {
	id        string
	projectID string
	traceID   string
}

func newFakeRunsService() *fakeRunsService {
	return &fakeRunsService{}
}

func (f *fakeRunsService) CreateOrGetLiveRunFromTrace(ctx context.Context, projectID, traceID string, attrs map[string]string) (runs.Run, error) {
	f.counter++
	id := "run-" + itoa2(f.counter)
	f.runs = append(f.runs, runsEntry{id: id, projectID: projectID, traceID: traceID})
	return runs.Run{
		ID: id, ProjectID: projectID, Source: "live", Status: "running", TraceID: traceID,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}, nil
}

func (f *fakeRunsService) GetRunByTraceID(ctx context.Context, traceID, projectID string) (runs.Run, error) {
	for _, r := range f.runs {
		if r.traceID == traceID && r.projectID == projectID {
			return runs.Run{ID: r.id, ProjectID: r.projectID, TraceID: r.traceID}, nil
		}
	}
	return runs.Run{}, errors.New("run not found")
}

func (f *fakeRunsService) ListProjectRuns(ctx context.Context, projectID string) ([]runs.RunSummary, error) {
	var result []runs.RunSummary
	for _, r := range f.runs {
		if r.projectID == projectID {
			result = append(result, runs.RunSummary{ID: r.id, ProjectID: r.projectID})
		}
	}
	return result, nil
}

func (f *fakeRunsService) GetRunDetail(ctx context.Context, runID string) (runs.RunDetail, error) {
	for _, r := range f.runs {
		if r.id == runID {
			return runs.RunDetail{
				Run: runs.Run{ID: r.id, ProjectID: r.projectID},
			}, nil
		}
	}
	return runs.RunDetail{}, errors.New("run not found")
}

func (f *fakeRunsService) UpdateRunFromTrace(ctx context.Context, runID, agentName, agentVersion string) (runs.Run, error) {
	return runs.Run{ID: runID, AgentName: agentName, AgentVersion: agentVersion}, nil
}

func itoa2(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}

// ---- fakeTracestore for ingest tests ----
type fakeTracestore struct {
	spans  []ingest.SpanRecord
	events []ingest.SpanEventRecord
}

func newFakeTracestore() *fakeTracestore {
	return &fakeTracestore{}
}

func (f *fakeTracestore) InsertSpans(ctx context.Context, spans []ingest.SpanRecord) error {
	f.spans = append(f.spans, spans...)
	return nil
}

func (f *fakeTracestore) InsertSpanEvents(ctx context.Context, events []ingest.SpanEventRecord) error {
	f.events = append(f.events, events...)
	return nil
}

// ---- Tests ----

func TestIngestHandler_missingToken(t *testing.T) {
	deps := Dependencies{
		TokenAuth:   newFakeTokenAuth(),
		RunsService: newFakeRunsService(),
	}
	router := NewRouter(deps)

	body := "fake body"
	req := httptest.NewRequest(http.MethodPost, "/v1/traces", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-protobuf")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestIngestHandler_wrongScope(t *testing.T) {
	auth := newFakeTokenAuth()
	auth.addToken("ascope_token_no_otlp", "proj-1", []string{"runner:write"})

	deps := Dependencies{
		TokenAuth:   auth,
		RunsService: newFakeRunsService(),
	}
	router := NewRouter(deps)

	body := "fake body"
	req := httptest.NewRequest(http.MethodPost, "/v1/traces", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Authorization", "Bearer ascope_token_no_otlp")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestIngestHandler_invalidBody(t *testing.T) {
	auth := newFakeTokenAuth()
	auth.addToken("ascope_valid_otlp_token", "proj-1", []string{"otlp:write"})

	deps := Dependencies{
		TokenAuth:   auth,
		RunsService: newFakeRunsService(),
	}
	router := NewRouter(deps)

	body := "this is not valid protobuf"
	req := httptest.NewRequest(http.MethodPost, "/v1/traces", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Authorization", "Bearer ascope_valid_otlp_token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Auth passes, protobuf decode fails -> 400
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid body, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestIngestHandler_fullSuccess(t *testing.T) {
	auth := newFakeTokenAuth()
	auth.addToken("ascope_success_token", "proj-1", []string{"otlp:write"})

	ts := newFakeTracestore()

	deps := Dependencies{
		TokenAuth:   auth,
		RunsService: newFakeRunsService(),
		Tracestore:  ts,
	}
	router := NewRouter(deps)

	// Build a minimal valid OTLP protobuf request
	req := buildOTLPRequest([]otlpSpan{
		{
			traceID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			spanID:  "bbbbbbbbbbbbbbbb",
			name:    "test-span",
		},
	})
	body, err := protoMarshalOTLP(req)
	if err != nil {
		t.Fatalf("marshal OTLP: %v", err)
	}

	httpreq := httptest.NewRequest(http.MethodPost, "/v1/traces", strings.NewReader(string(body)))
	httpreq.Header.Set("Content-Type", "application/x-protobuf")
	httpreq.Header.Set("Authorization", "Bearer ascope_success_token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httpreq)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify spans were passed to tracestore with ProjectID and RunID
	if len(ts.spans) != 1 {
		t.Fatalf("got %d spans in tracestore, want 1", len(ts.spans))
	}
	if ts.spans[0].ProjectID != "proj-1" {
		t.Errorf("ProjectID = %q, want proj-1", ts.spans[0].ProjectID)
	}
	if ts.spans[0].RunID == "" {
		t.Error("RunID is empty")
	}
	if ts.spans[0].TraceID != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Errorf("TraceID = %q, want aaaa...", ts.spans[0].TraceID)
	}
}

func TestListProjectRunsHandler(t *testing.T) {
	runsSvc := newFakeRunsService()
	runsSvc.CreateOrGetLiveRunFromTrace(nil, "proj-1", "trace-1", nil)
	runsSvc.CreateOrGetLiveRunFromTrace(nil, "proj-1", "trace-2", nil)

	deps := Dependencies{
		AuthService:     newFakeAuthService(),
		ProjectsService: newFakeProjectsService(),
		RunsService:     runsSvc,
		SessionSigner:   testSigner,
	}
	router := NewRouter(deps)

	req := httptest.NewRequest(http.MethodGet, "/api/projects/proj-1/runs", nil)
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetRunDetailHandler(t *testing.T) {
	runsSvc := newFakeRunsService()
	runsSvc.CreateOrGetLiveRunFromTrace(nil, "proj-1", "trace-1", nil)

	deps := Dependencies{
		AuthService:     newFakeAuthService(),
		ProjectsService: newFakeProjectsService(),
		RunsService:     runsSvc,
		SessionSigner:   testSigner,
	}
	router := NewRouter(deps)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/run-1", nil)
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
