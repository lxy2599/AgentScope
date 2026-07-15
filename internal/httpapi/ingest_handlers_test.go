package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	runs      []runsEntry
	counter   int
}

type runsEntry struct {
	id        string
	projectID string
}

func newFakeRunsService() *fakeRunsService {
	return &fakeRunsService{}
}

func (f *fakeRunsService) CreateOrGetLiveRunFromTrace(ctx context.Context, projectID, traceID string, attrs map[string]string) (interface{}, error) {
	f.counter++
	id := "run-" + itoa2(f.counter)
	f.runs = append(f.runs, runsEntry{id: id, projectID: projectID})
	return RunResult{
		ID: id, ProjectID: projectID, Source: "live", Status: "running", TraceID: traceID,
	}, nil
}

func (f *fakeRunsService) ListProjectRuns(ctx context.Context, projectID string) (interface{}, error) {
	var result []interface{}
	for _, r := range f.runs {
		if r.projectID == projectID {
			result = append(result, map[string]string{"id": r.id, "project_id": r.projectID})
		}
	}
	return result, nil
}

func (f *fakeRunsService) GetRunDetail(ctx context.Context, runID string) (interface{}, error) {
	for _, r := range f.runs {
		if r.id == runID {
			return map[string]interface{}{
				"id": r.id, "project_id": r.projectID,
			}, nil
		}
	}
	return nil, errors.New("run not found")
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

// ---- Tests ----

func TestIngestHandler_missingToken(t *testing.T) {
	deps := Dependencies{
		TokenAuth:   newFakeTokenAuth(),
		RunsService: newFakeRunsService(),
	}
	router := NewRouter(deps)

	body := `fake protobuf body`
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
	auth.addToken("ascope_token_without_otlp_scope", "proj-1", []string{"runner:write"})

	deps := Dependencies{
		TokenAuth:   auth,
		RunsService: newFakeRunsService(),
	}
	router := NewRouter(deps)

	body := `fake protobuf body`
	req := httptest.NewRequest(http.MethodPost, "/v1/traces", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Authorization", "Bearer ascope_token_without_otlp_scope")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestIngestHandler_validRequest(t *testing.T) {
	auth := newFakeTokenAuth()
	auth.addToken("ascope_valid_otlp_token", "proj-1", []string{"otlp:write"})

	deps := Dependencies{
		TokenAuth:   auth,
		RunsService: newFakeRunsService(),
	}
	router := NewRouter(deps)

	body := `this is not valid protobuf`
	req := httptest.NewRequest(http.MethodPost, "/v1/traces", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Authorization", "Bearer ascope_valid_otlp_token")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	// Auth passes, but protobuf decode fails -> 400
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid body, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListProjectRunsHandler(t *testing.T) {
	runs := newFakeRunsService()
	runs.CreateOrGetLiveRunFromTrace(nil, "proj-1", "trace-1", nil)
	runs.CreateOrGetLiveRunFromTrace(nil, "proj-1", "trace-2", nil)

	deps := Dependencies{
		AuthService:     newFakeAuthService(),
		ProjectsService: newFakeProjectsService(),
		RunsService:     runs,
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
	runs := newFakeRunsService()
	runs.CreateOrGetLiveRunFromTrace(nil, "proj-1", "trace-1", nil)

	deps := Dependencies{
		AuthService:     newFakeAuthService(),
		ProjectsService: newFakeProjectsService(),
		RunsService:     runs,
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
