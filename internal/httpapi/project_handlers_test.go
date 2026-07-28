package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"AgentScope/internal/projects"
)

type validatingProjectsService struct {
	*fakeProjectsService
}

func (v *validatingProjectsService) CreateToken(ctx context.Context, projectID, userID string, options TokenCreateOptions) (string, ProjectToken, error) {
	if len(options.Scopes) != 1 || options.Scopes[0] != projects.ScopeOTLPWrite {
		return "", ProjectToken{}, projects.ErrInvalidTokenScopes
	}
	return v.fakeProjectsService.CreateToken(ctx, projectID, userID, options)
}

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
	projects.CreateProject(context.Background(), "user-1", "Project A", "")
	projects.CreateProject(context.Background(), "user-1", "Project B", "")

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
	proj, _ := projects.CreateProject(context.Background(), "user-1", "Token Project", "")

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

func TestCreateTokenHandler_invalidScopes(t *testing.T) {
	projectsService := newFakeProjectsService()
	proj, _ := projectsService.CreateProject(context.Background(), "user-1", "Token Project", "")
	deps := Dependencies{
		AuthService:     newFakeAuthService(),
		ProjectsService: &validatingProjectsService{fakeProjectsService: projectsService},
		SessionSigner:   testSigner,
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/projects/"+proj.ID+"/tokens",
		strings.NewReader(`{"description":"bad token","scopes":["admin:write"]}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()
	NewRouter(deps).ServeHTTP(rec, req)

	assertSafeHTTPError(t, rec, http.StatusBadRequest, "invalid_scopes", "Token scopes are invalid")
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

func TestTokenGovernanceHandlersEnforceAdminAndHidePlaintext(t *testing.T) {
	projectsService := newFakeProjectsService()
	project, _ := projectsService.CreateProject(context.Background(), "owner", "Token Project", "")
	projectsService.members[project.ID]["member"] = "member"
	deps := Dependencies{AuthService: newFakeAuthService(), ProjectsService: projectsService, SessionSigner: testSigner}
	router := NewRouter(deps)

	memberCreate := httptest.NewRequest(http.MethodPost, "/api/projects/"+project.ID+"/tokens", strings.NewReader(`{"description":"blocked","scopes":["otlp:write"]}`))
	memberCreate.Header.Set("Content-Type", "application/json")
	memberCreate.AddCookie(signCookie(t, "member"))
	memberRec := httptest.NewRecorder()
	router.ServeHTTP(memberRec, memberCreate)
	assertSafeHTTPError(t, memberRec, http.StatusForbidden, "token_governance_forbidden", "Project owner or admin permission is required")

	ownerCreate := httptest.NewRequest(http.MethodPost, "/api/projects/"+project.ID+"/tokens", strings.NewReader(`{"description":"sdk","scopes":["otlp:write"]}`))
	ownerCreate.Header.Set("Content-Type", "application/json")
	ownerCreate.AddCookie(signCookie(t, "owner"))
	ownerRec := httptest.NewRecorder()
	router.ServeHTTP(ownerRec, ownerCreate)
	if ownerRec.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", ownerRec.Code, ownerRec.Body.String())
	}
	var createResp map[string]any
	if err := json.NewDecoder(ownerRec.Body).Decode(&createResp); err != nil {
		t.Fatal(err)
	}
	plain, _ := createResp["plain_token"].(string)
	if plain == "" {
		t.Fatal("expected one-time plaintext in create response")
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/projects/"+project.ID+"/tokens", nil)
	listReq.AddCookie(signCookie(t, "owner"))
	listRec := httptest.NewRecorder()
	router.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status = %d", listRec.Code)
	}
	if strings.Contains(listRec.Body.String(), plain) {
		t.Fatal("token list leaked plaintext token")
	}
}

func TestTokenRiskSummaryHandler(t *testing.T) {
	projectsService := newFakeProjectsService()
	project, _ := projectsService.CreateProject(context.Background(), "owner", "Token Project", "")
	deps := Dependencies{AuthService: newFakeAuthService(), ProjectsService: projectsService, SessionSigner: testSigner}
	req := httptest.NewRequest(http.MethodGet, "/api/projects/"+project.ID+"/tokens/risk-summary", nil)
	req.AddCookie(signCookie(t, "owner"))
	rec := httptest.NewRecorder()
	NewRouter(deps).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("risk summary status = %d: %s", rec.Code, rec.Body.String())
	}
	var response TokenRiskSummary
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
}
