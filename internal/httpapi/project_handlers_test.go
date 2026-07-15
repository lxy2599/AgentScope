package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// signCookie signs a userID for use in tests.
func signCookie(t *testing.T, userID string) *http.Cookie {
	t.Helper()
	signed, err := testSigner.SignUserID(userID)
	if err != nil {
		t.Fatalf("SignUserID: %v", err)
	}
	return &http.Cookie{Name: "session", Value: signed}
}

func TestCreateProjectHandler_success(t *testing.T) {
	deps := Dependencies{
		AuthService:     newFakeAuthService(),
		ProjectsService: newFakeProjectsService(),
		SessionSigner:   testSigner,
	}
	router := NewRouter(deps)

	body := `{"name":"My Project","description":"test project"}`
	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["name"] != "My Project" {
		t.Errorf("name = %v, want My Project", resp["name"])
	}
	if resp["id"] == nil || resp["id"] == "" {
		t.Error("expected non-empty project id")
	}
}

func TestCreateProjectHandler_unauthorized(t *testing.T) {
	deps := Dependencies{
		AuthService:     newFakeAuthService(),
		ProjectsService: newFakeProjectsService(),
		SessionSigner:   testSigner,
	}
	router := NewRouter(deps)

	body := `{"name":"Hacker Project"}`
	req := httptest.NewRequest(http.MethodPost, "/api/projects", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	// No session cookie
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestListProjectsHandler_success(t *testing.T) {
	projects := newFakeProjectsService()
	projects.CreateProject(nil, "user-1", "Project A", "")
	projects.CreateProject(nil, "user-1", "Project B", "")

	deps := Dependencies{
		AuthService:     newFakeAuthService(),
		ProjectsService: projects,
		SessionSigner:   testSigner,
	}
	router := NewRouter(deps)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp) != 2 {
		t.Fatalf("got %d projects, want 2", len(resp))
	}
}

func TestCreateTokenHandler_success(t *testing.T) {
	projects := newFakeProjectsService()
	proj, _ := projects.CreateProject(nil, "user-1", "Token Project", "")

	deps := Dependencies{
		AuthService:     newFakeAuthService(),
		ProjectsService: projects,
		SessionSigner:   testSigner,
	}
	router := NewRouter(deps)

	body := `{"description":"dev token","scopes":["otlp:write"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/projects/"+proj.ID+"/tokens", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["plain_token"] == nil || resp["plain_token"] == "" {
		t.Error("expected non-empty plain_token")
	}
	pt, _ := resp["plain_token"].(string)
	if !strings.HasPrefix(pt, "ascope_") {
		t.Errorf("plain_token prefix = %q, want ascope_", pt[:7])
	}
}

func TestListProjectsHandler_unauthorized(t *testing.T) {
	deps := Dependencies{
		AuthService:     newFakeAuthService(),
		ProjectsService: newFakeProjectsService(),
		SessionSigner:   testSigner,
	}
	router := NewRouter(deps)

	req := httptest.NewRequest(http.MethodGet, "/api/projects", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}
