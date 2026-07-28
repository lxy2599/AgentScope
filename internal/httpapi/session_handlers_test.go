package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"AgentScope/internal/auth"
)

func newSessionTestRouter(t *testing.T) (http.Handler, *fakeAuthService, *fakeUserSessions) {
	t.Helper()
	signer := auth.NewSessionSignerWithTTL([]byte(testSessionSecret), time.Hour)
	authService := newFakeAuthService()
	sessions := newFakeUserSessions(signer)
	return NewRouter(Dependencies{
		AuthService:   authService,
		UserSessions:  sessions,
		SessionSigner: signer,
	}), authService, sessions
}

func registerSession(t *testing.T, router http.Handler, email string) *http.Cookie {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"email":"`+email+`","password":"correct-horse","display_name":"Alice"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.27:4555"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register = %d: %s", rec.Code, rec.Body.String())
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "session" {
			return cookie
		}
	}
	t.Fatal("registration did not set a session cookie")
	return nil
}

func sessionRequest(method, path string, cookie *http.Cookie) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(cookie)
	return req
}

func TestServerSessionsLogoutRevokesCookieAndClearsBrowserState(t *testing.T) {
	router, _, _ := newSessionTestRouter(t)
	cookie := registerSession(t, router, "alice-session@test.com")
	if !strings.HasPrefix(cookie.Value, "v2.") {
		t.Fatalf("session cookie = %q, want v2 signed opaque session", cookie.Value)
	}

	list := httptest.NewRecorder()
	router.ServeHTTP(list, sessionRequest(http.MethodGet, "/api/auth/sessions", cookie))
	if list.Code != http.StatusOK {
		t.Fatalf("sessions before logout = %d: %s", list.Code, list.Body.String())
	}
	var sessions []sessionResponse
	if err := json.NewDecoder(list.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	if len(sessions) != 1 || !sessions[0].Current || sessions[0].IPPrefix != "203.0.113.0/24" {
		t.Fatalf("unexpected session response: %#v", sessions)
	}

	logout := httptest.NewRecorder()
	router.ServeHTTP(logout, sessionRequest(http.MethodPost, "/api/auth/logout", cookie))
	if logout.Code != http.StatusNoContent {
		t.Fatalf("logout = %d: %s", logout.Code, logout.Body.String())
	}
	cleared := logout.Result().Cookies()
	if len(cleared) != 1 || cleared[0].Name != "session" || cleared[0].MaxAge >= 0 {
		t.Fatalf("logout did not expire the session cookie: %#v", cleared)
	}

	after := httptest.NewRecorder()
	router.ServeHTTP(after, sessionRequest(http.MethodGet, "/api/auth/sessions", cookie))
	if after.Code != http.StatusUnauthorized {
		t.Fatalf("revoked cookie accepted: %d: %s", after.Code, after.Body.String())
	}
}

func TestServerSessionsDeleteOtherAndLogoutAll(t *testing.T) {
	router, authService, sessions := newSessionTestRouter(t)
	first := registerSession(t, router, "alice-devices@test.com")
	userID, _, err := authService.Login(context.Background(), "alice-devices@test.com", "correct-horse")
	if err != nil {
		t.Fatalf("login fake user: %v", err)
	}
	secondValue, second, err := sessions.Create(context.Background(), userID, "second browser", "")
	if err != nil {
		t.Fatalf("create second session: %v", err)
	}
	secondCookie := &http.Cookie{Name: "session", Value: secondValue}

	deleteOther := httptest.NewRecorder()
	router.ServeHTTP(deleteOther, sessionRequest(http.MethodDelete, "/api/auth/sessions/"+second.ID, first))
	if deleteOther.Code != http.StatusNoContent {
		t.Fatalf("delete other session = %d: %s", deleteOther.Code, deleteOther.Body.String())
	}
	blocked := httptest.NewRecorder()
	router.ServeHTTP(blocked, sessionRequest(http.MethodGet, "/api/auth/sessions", secondCookie))
	if blocked.Code != http.StatusUnauthorized {
		t.Fatalf("deleted session accepted: %d", blocked.Code)
	}

	thirdValue, _, err := sessions.Create(context.Background(), userID, "third browser", "")
	if err != nil {
		t.Fatalf("create third session: %v", err)
	}
	thirdCookie := &http.Cookie{Name: "session", Value: thirdValue}
	all := httptest.NewRecorder()
	router.ServeHTTP(all, sessionRequest(http.MethodPost, "/api/auth/logout-all", first))
	if all.Code != http.StatusNoContent {
		t.Fatalf("logout all = %d: %s", all.Code, all.Body.String())
	}
	for name, candidate := range map[string]*http.Cookie{"current": first, "other": thirdCookie} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, sessionRequest(http.MethodGet, "/api/auth/sessions", candidate))
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s cookie remained valid after logout-all: %d", name, rec.Code)
		}
	}
}
