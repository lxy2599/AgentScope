package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"AgentScope/internal/auth"
)

func TestMiddleware_requireAuth_valid(t *testing.T) {
	deps := Dependencies{
		SessionSigner: testSigner,
	}

	handler := requireAuth(deps)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			t.Fatal("expected userID in context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	signed, err := testSigner.SignUserID("user-123")
	if err != nil {
		t.Fatalf("SignUserID: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: signed})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestMiddleware_requireAuth_missingCookie(t *testing.T) {
	deps := Dependencies{
		SessionSigner: testSigner,
	}

	handler := requireAuth(deps)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be reached")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestMiddleware_requireAuth_invalidCookie(t *testing.T) {
	deps := Dependencies{
		SessionSigner: testSigner,
	}

	handler := requireAuth(deps)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be reached")
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "invalid-cookie-value"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestMiddleware_requireAuth_rejectsExpiredSession(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	signer := auth.NewSessionSignerWithClock([]byte(testSessionSecret), time.Hour, func() time.Time {
		return now
	})
	signed, err := signer.SignUserID("user-123")
	if err != nil {
		t.Fatalf("SignUserID: %v", err)
	}
	now = now.Add(time.Hour)

	deps := Dependencies{SessionSigner: signer}
	handler := requireAuth(deps)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be reached")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: signed})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestMiddleware_requireAuth_rejectsLegacySession(t *testing.T) {
	deps := Dependencies{SessionSigner: testSigner}
	handler := requireAuth(deps)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be reached")
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "dXNlci0xMjM.invalid-signature"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestSetSessionCookieRoundsSubsecondLifetimeSafely(t *testing.T) {
	before := time.Now().UTC()
	rec := httptest.NewRecorder()
	setSessionCookie(rec, "signed", 500*time.Millisecond, false)

	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookies = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	if cookie.MaxAge != 1 {
		t.Fatalf("MaxAge = %d, want 1", cookie.MaxAge)
	}
	if !cookie.Expires.After(before) {
		t.Fatalf("Expires = %v, want after %v", cookie.Expires, before)
	}
	if cookie.Expires.After(before.Add(2 * time.Second)) {
		t.Fatalf("Expires = %v, unexpectedly far after subsecond TTL", cookie.Expires)
	}
}
