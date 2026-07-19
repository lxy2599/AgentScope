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
	common "go.opentelemetry.io/proto/otlp/common/v1"
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

func (f *fakeRunsService) GetRun(ctx context.Context, runID string) (runs.Run, error) {
	for _, r := range f.runs {
		if r.id == runID {
			return runs.Run{ID: r.id, ProjectID: r.projectID}, nil
		}
	}
	return runs.Run{}, errors.New("run not found")
}

func (f *fakeRunsService) UpdateRunFromTrace(ctx context.Context, runID, agentName, agentVersion string) (runs.Run, error) {
	return runs.Run{ID: runID, AgentName: agentName, AgentVersion: agentVersion}, nil
}

func (f *fakeRunsService) FailRunByRunner(ctx context.Context, runID, projectID, errorStr string) error {
	return nil
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
	spans          []ingest.SpanRecord
	events         []ingest.SpanEventRecord
	llmCalls       []ingest.LLMCallRecord
	toolCalls      []ingest.ToolCallRecord
	retrievalCalls []ingest.RetrievalCallRecord
	skillCalls     []ingest.SkillCallRecord

	insertLLMError       error
	insertToolError      error
	insertRetrievalError error
	insertSkillError     error
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

func (f *fakeTracestore) InsertLLMCalls(ctx context.Context, calls []ingest.LLMCallRecord) error {
	if f.insertLLMError != nil {
		return f.insertLLMError
	}
	f.llmCalls = append(f.llmCalls, calls...)
	return nil
}

func (f *fakeTracestore) InsertToolCalls(ctx context.Context, calls []ingest.ToolCallRecord) error {
	if f.insertToolError != nil {
		return f.insertToolError
	}
	f.toolCalls = append(f.toolCalls, calls...)
	return nil
}

func (f *fakeTracestore) InsertRetrievalCalls(ctx context.Context, calls []ingest.RetrievalCallRecord) error {
	if f.insertRetrievalError != nil {
		return f.insertRetrievalError
	}
	f.retrievalCalls = append(f.retrievalCalls, calls...)
	return nil
}

func (f *fakeTracestore) InsertSkillCalls(ctx context.Context, calls []ingest.SkillCallRecord) error {
	if f.insertSkillError != nil {
		return f.insertSkillError
	}
	f.skillCalls = append(f.skillCalls, calls...)
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

	auth := newFakeProjectAuth()
	auth.allow("user-1", "proj-1")

	deps := Dependencies{
		AuthService:     newFakeAuthService(),
		ProjectsService: newFakeProjectsService(),
		RunsService:     runsSvc,
		SessionSigner:   testSigner,
		ProjectAuth:     auth,
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

func TestListProjectRunsHandler_nonMember(t *testing.T) {
	runsSvc := newFakeRunsService()
	runsSvc.CreateOrGetLiveRunFromTrace(nil, "proj-1", "trace-1", nil)

	auth := newFakeProjectAuth()
	auth.allow("user-1", "proj-2") // user-1 is member of proj-2, not proj-1

	deps := Dependencies{
		AuthService:     newFakeAuthService(),
		ProjectsService: newFakeProjectsService(),
		RunsService:     runsSvc,
		SessionSigner:   testSigner,
		ProjectAuth:     auth,
	}
	router := NewRouter(deps)

	req := httptest.NewRequest(http.MethodGet, "/api/projects/proj-1/runs", nil)
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetRunDetailHandler(t *testing.T) {
	runsSvc := newFakeRunsService()
	runsSvc.CreateOrGetLiveRunFromTrace(nil, "proj-1", "trace-1", nil)

	auth := newFakeProjectAuth()
	auth.allow("user-1", "proj-1")

	deps := Dependencies{
		AuthService:     newFakeAuthService(),
		ProjectsService: newFakeProjectsService(),
		RunsService:     runsSvc,
		SessionSigner:   testSigner,
		ProjectAuth:     auth,
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

func TestGetRunDetailHandler_nonMember(t *testing.T) {
	runsSvc := newFakeRunsService()
	runsSvc.CreateOrGetLiveRunFromTrace(nil, "proj-1", "trace-1", nil)

	auth := newFakeProjectAuth()
	auth.allow("user-1", "proj-2") // user-1 is member of proj-2, not proj-1

	deps := Dependencies{
		AuthService:     newFakeAuthService(),
		ProjectsService: newFakeProjectsService(),
		RunsService:     runsSvc,
		SessionSigner:   testSigner,
		ProjectAuth:     auth,
	}
	router := NewRouter(deps)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/run-1", nil)
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
}

// ---- Structured call ingestion tests ----

func TestIngestHandler_structuredCallsSuccess(t *testing.T) {
	auth := newFakeTokenAuth()
	auth.addToken("ascope_struct_token", "proj-1", []string{"otlp:write"})

	ts := newFakeTracestore()

	deps := Dependencies{
		TokenAuth:   auth,
		RunsService: newFakeRunsService(),
		Tracestore:  ts,
	}
	router := NewRouter(deps)

	// Build spans with agentscope.span.kind for each structured call type
	attrs := func(kind string, extra ...*common.KeyValue) []*common.KeyValue {
		base := []*common.KeyValue{
			{Key: "agentscope.span.kind", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: kind}}},
			{Key: "agentscope.agent.id", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "agent-1"}}},
		}
		return append(base, extra...)
	}

	spans := []otlpSpan{
		{
			traceID: "11111111111111111111111111111111",
			spanID:  "aaaaaaaaaaaa0001",
			name:    "llm.call",
			attributes: attrs("LLM",
				&common.KeyValue{Key: "llm.provider", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "openai"}}},
				&common.KeyValue{Key: "llm.model", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "gpt-4"}}},
				&common.KeyValue{Key: "llm.input_tokens", Value: &common.AnyValue{Value: &common.AnyValue_IntValue{IntValue: 50}}},
				&common.KeyValue{Key: "llm.output_tokens", Value: &common.AnyValue{Value: &common.AnyValue_IntValue{IntValue: 100}}},
			),
		},
		{
			traceID: "11111111111111111111111111111111",
			spanID:  "aaaaaaaaaaaa0002",
			name:    "tool.call",
			attributes: attrs("TOOL",
				&common.KeyValue{Key: "tool.name", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "search"}}},
				&common.KeyValue{Key: "tool.type", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "api"}}},
				&common.KeyValue{Key: "tool.success", Value: &common.AnyValue{Value: &common.AnyValue_BoolValue{BoolValue: true}}},
			),
		},
		{
			traceID: "11111111111111111111111111111111",
			spanID:  "aaaaaaaaaaaa0003",
			name:    "retrieval.call",
			attributes: attrs("RETRIEVER",
				&common.KeyValue{Key: "retrieval.knowledge_base", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "docs"}}},
				&common.KeyValue{Key: "retrieval.query", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "how to?"}}},
			),
		},
		{
			traceID: "11111111111111111111111111111111",
			spanID:  "aaaaaaaaaaaa0004",
			name:    "skill.call",
			attributes: attrs("SKILL",
				&common.KeyValue{Key: "skill.name", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "refund"}}},
				&common.KeyValue{Key: "skill.success", Value: &common.AnyValue{Value: &common.AnyValue_BoolValue{BoolValue: true}}},
			),
		},
	}

	req := buildOTLPRequest(spans)
	body, err := protoMarshalOTLP(req)
	if err != nil {
		t.Fatalf("marshal OTLP: %v", err)
	}

	httpreq := httptest.NewRequest(http.MethodPost, "/v1/traces", strings.NewReader(string(body)))
	httpreq.Header.Set("Content-Type", "application/x-protobuf")
	httpreq.Header.Set("Authorization", "Bearer ascope_struct_token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httpreq)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify raw spans count (4 spans + potentially agent spans)
	if len(ts.spans) < 4 {
		t.Fatalf("got %d raw spans, want at least 4", len(ts.spans))
	}

	// Verify structured calls
	if len(ts.llmCalls) != 1 {
		t.Errorf("got %d llm calls, want 1", len(ts.llmCalls))
	} else if ts.llmCalls[0].Provider != "openai" {
		t.Errorf("LLM provider = %q, want openai", ts.llmCalls[0].Provider)
	}

	if len(ts.toolCalls) != 1 {
		t.Errorf("got %d tool calls, want 1", len(ts.toolCalls))
	} else if ts.toolCalls[0].ToolName != "search" {
		t.Errorf("tool name = %q, want search", ts.toolCalls[0].ToolName)
	}

	if len(ts.retrievalCalls) != 1 {
		t.Errorf("got %d retrieval calls, want 1", len(ts.retrievalCalls))
	} else if ts.retrievalCalls[0].KnowledgeBase != "docs" {
		t.Errorf("retrieval knowledge_base = %q, want docs", ts.retrievalCalls[0].KnowledgeBase)
	}

	if len(ts.skillCalls) != 1 {
		t.Errorf("got %d skill calls, want 1", len(ts.skillCalls))
	} else if ts.skillCalls[0].SkillName != "refund" {
		t.Errorf("skill name = %q, want refund", ts.skillCalls[0].SkillName)
	}
}

func TestIngestHandler_structuredCallWriteFails(t *testing.T) {
	auth := newFakeTokenAuth()
	auth.addToken("ascope_fail_token", "proj-1", []string{"otlp:write"})

	ts := newFakeTracestore()
	ts.insertLLMError = errors.New("llm insert failed")

	deps := Dependencies{
		TokenAuth:   auth,
		RunsService: newFakeRunsService(),
		Tracestore:  ts,
	}
	router := NewRouter(deps)

	// Single LLM span
	spans := []otlpSpan{
		{
			traceID: "22222222222222222222222222222222",
			spanID:  "bbbbbbbbbbbb0001",
			name:    "llm.call",
			attributes: []*common.KeyValue{
				{Key: "agentscope.span.kind", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "LLM"}}},
				{Key: "llm.provider", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "openai"}}},
			},
		},
	}

	req := buildOTLPRequest(spans)
	body, err := protoMarshalOTLP(req)
	if err != nil {
		t.Fatalf("marshal OTLP: %v", err)
	}

	httpreq := httptest.NewRequest(http.MethodPost, "/v1/traces", strings.NewReader(string(body)))
	httpreq.Header.Set("Content-Type", "application/x-protobuf")
	httpreq.Header.Set("Authorization", "Bearer ascope_fail_token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httpreq)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify response contains storage_failed
	if !strings.Contains(rec.Body.String(), "storage_failed") {
		t.Errorf("expected storage_failed in response, got: %s", rec.Body.String())
	}
}
