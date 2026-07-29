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

func TestRegisterHandler_success(t *testing.T) {
	const sessionTTL = 2 * time.Hour
	deps := Dependencies{
		AuthService:   newFakeAuthService(),
		SessionSigner: auth.NewSessionSignerWithTTL([]byte(testSessionSecret), sessionTTL),
		SecureCookies: true,
	}
	router := NewRouter(deps)

	before := time.Now()
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
	if sessionCookie.Path != "/" {
		t.Errorf("Path = %q, want /", sessionCookie.Path)
	}
	if !sessionCookie.Secure {
		t.Error("Secure = false, want true")
	}
	if sessionCookie.MaxAge != int(sessionTTL/time.Second) {
		t.Errorf("MaxAge = %d, want %d", sessionCookie.MaxAge, int(sessionTTL/time.Second))
	}
	if sessionCookie.Expires.Before(before.Add(sessionTTL-time.Second)) ||
		sessionCookie.Expires.After(time.Now().Add(sessionTTL+time.Second)) {
		t.Errorf("Expires = %v, want approximately now + %v", sessionCookie.Expires, sessionTTL)
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
	authService := newFakeAuthService()
	authService.Register(context.Background(), "bob@test.com", "secret", "Bob")
	deps := Dependencies{
		AuthService:   authService,
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
	if sessionCookie.Secure {
		t.Error("Secure = true, want false by default")
	}
	if sessionCookie.MaxAge != int(auth.DefaultSessionTTL/time.Second) {
		t.Errorf(
			"MaxAge = %d, want default %d",
			sessionCookie.MaxAge,
			int(auth.DefaultSessionTTL/time.Second),
		)
	}
	if sessionCookie.Expires.IsZero() {
		t.Error("Expires must be set")
	}
}

func TestLoginHandler_wrongPassword(t *testing.T) {
	auth := newFakeAuthService()
	auth.Register(context.Background(), "bob@test.com", "secret", "Bob")
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

func restrictiveHTTPAuthAdmission() *auth.AdmissionController {
	return auth.NewAdmissionController(auth.AdmissionOptions{
		Enabled:              true,
		LoginIP:              auth.RatePolicy{Capacity: 1, Refill: 1, Interval: time.Hour},
		LoginPrincipal:       auth.RatePolicy{},
		RegisterIP:           auth.RatePolicy{Capacity: 1, Refill: 1, Interval: time.Hour},
		RegisterPrincipal:    auth.RatePolicy{},
		BcryptMaxConcurrent:  1,
		MaxTrackedPrincipals: 10,
	})
}

func TestLoginHandlerRateLimitsBeforeAuthenticationService(t *testing.T) {
	authService := newFakeAuthService()
	authService.Register(context.Background(), "rate-limit@test.com", "secret", "Rate Limit")
	router := NewRouter(Dependencies{
		AuthService:   authService,
		AuthAdmission: restrictiveHTTPAuthAdmission(),
		SessionSigner: testSigner,
	})

	body := `{"email":"rate-limit@test.com","password":"wrong"}`
	for attempt := 0; attempt < 2; attempt++ {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(body))
		req.RemoteAddr = "203.0.113.9:4321"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if attempt == 0 {
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("first login status = %d, want 401: %s", rec.Code, rec.Body.String())
			}
			continue
		}
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("second login status = %d, want 429: %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("Retry-After") == "" {
			t.Fatal("rate-limited response omitted Retry-After")
		}
		var response map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
			t.Fatal(err)
		}
		if response["code"] != "auth_rate_limited" {
			t.Fatalf("response = %#v, want auth_rate_limited", response)
		}
	}
}

func TestRegisterHandlerRateLimitsBeforeCreatingSecondUser(t *testing.T) {
	router := NewRouter(Dependencies{
		AuthService:   newFakeAuthService(),
		AuthAdmission: restrictiveHTTPAuthAdmission(),
		SessionSigner: testSigner,
	})

	for attempt, body := range []string{
		`{"email":"first@test.com","password":"correct-horse","display_name":"First"}`,
		`{"email":"second@test.com","password":"correct-horse","display_name":"Second"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(body))
		req.RemoteAddr = "198.51.100.8:4321"
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if attempt == 0 {
			if rec.Code != http.StatusCreated {
				t.Fatalf("first registration status = %d, want 201: %s", rec.Code, rec.Body.String())
			}
			continue
		}
		if rec.Code != http.StatusTooManyRequests {
			t.Fatalf("second registration status = %d, want 429: %s", rec.Code, rec.Body.String())
		}
	}
}

func TestLoginHandlerReturnsBusyWhenBcryptAdmissionIsSaturated(t *testing.T) {
	admissionOptions := auth.AdmissionOptions{
		Enabled:              true,
		LoginIP:              auth.RatePolicy{},
		LoginPrincipal:       auth.RatePolicy{},
		RegisterIP:           auth.RatePolicy{},
		RegisterPrincipal:    auth.RatePolicy{},
		BcryptMaxConcurrent:  1,
		MaxTrackedPrincipals: 10,
	}
	admission := auth.NewAdmissionController(admissionOptions)
	release, outcome, _ := admission.Admit("login", "203.0.113.0/24", "first@test.com")
	if outcome != auth.AdmissionAllowed {
		t.Fatalf("seed admission outcome = %s", outcome)
	}
	defer release()

	router := NewRouter(Dependencies{
		AuthService:   newFakeAuthService(),
		AuthAdmission: admission,
		SessionSigner: testSigner,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"second@test.com","password":"secret"}`))
	req.RemoteAddr = "198.51.100.8:4321"
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("Retry-After = %q, want 1", rec.Header().Get("Retry-After"))
	}
	var response map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response["code"] != "auth_busy" {
		t.Fatalf("response = %#v, want auth_busy", response)
	}
}
