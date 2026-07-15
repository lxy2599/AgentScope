package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
