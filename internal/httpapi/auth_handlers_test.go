package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterHandler_success(t *testing.T) {
	deps := Dependencies{
		AuthService:    newFakeAuthService(),
		SessionSigner:  testSigner,
	}
	router := NewRouter(deps)

	body := `{"email":"alice@test.com","password":"correct-horse","display_name":"Alice"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// Check cookie is set
	cookies := rec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie")
	}
	if sessionCookie.HttpOnly != true {
		t.Error("expected HttpOnly cookie")
	}
	if sessionCookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("expected SameSite=Lax, got %v", sessionCookie.SameSite)
	}

	// Check response body
	var resp map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["id"] == nil || resp["id"] == "" {
		t.Error("expected non-empty user id in response")
	}
	if resp["email"] != "alice@test.com" {
		t.Errorf("email = %v, want alice@test.com", resp["email"])
	}
}

func TestLoginHandler_success(t *testing.T) {
	auth := newFakeAuthService()
	auth.Register(nil, "bob@test.com", "secret", "Bob")
	deps := Dependencies{
		AuthService:   auth,
		SessionSigner: testSigner,
	}
	router := NewRouter(deps)

	body := `{"email":"bob@test.com","password":"secret"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	cookies := rec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == "session" {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie")
	}
}

func TestLoginHandler_wrongPassword(t *testing.T) {
	auth := newFakeAuthService()
	auth.Register(nil, "bob@test.com", "secret", "Bob")
	deps := Dependencies{
		AuthService:   auth,
		SessionSigner: testSigner,
	}
	router := NewRouter(deps)

	body := `{"email":"bob@test.com","password":"wrong-password"}`
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rec.Code, rec.Body.String())
	}
}
