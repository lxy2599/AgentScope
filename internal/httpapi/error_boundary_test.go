package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"AgentScope/internal/auth"
	"AgentScope/internal/evals"
)

const sensitiveDependencyError = "pq: SELECT secret FROM tokens; open /srv/private/credentials.json: Authorization: Bearer ascope_sensitive_token"

type failingAuthService struct {
	*fakeAuthService
	err error
}

func (f *failingAuthService) Register(context.Context, string, string, string) (string, error) {
	return "", f.err
}

func (f *failingAuthService) Login(context.Context, string, string) (string, string, error) {
	return "", "", f.err
}

type failingRuntimeService struct {
	*fakeRuntimeService
	err error
}

func (f *failingRuntimeService) StartRun(context.Context, string, SDKStartRunRequest) (RuntimeRunResponse, error) {
	return RuntimeRunResponse{}, f.err
}

type failingRunnerService struct {
	*fakeRunnerService
	err error
}

func (f *failingRunnerService) Heartbeat(context.Context, string, string, string) error {
	return f.err
}

type failingEvalsService struct {
	*fakeEvalsService
	err error
}

func (f *failingEvalsService) CreateEvalScore(context.Context, evals.EvalScore) (evals.EvalScore, error) {
	return evals.EvalScore{}, f.err
}

type failingAnnotationService struct {
	*fakeAnnotationService
	err error
}

func (f *failingAnnotationService) CreateItemFromRun(context.Context, string, string, string) (AnnotationItem, error) {
	return AnnotationItem{}, f.err
}

func TestRepresentativeHandlersDoNotLeakDependencyErrors(t *testing.T) {
	injectedErr := errors.New(sensitiveDependencyError)

	t.Run("auth register", func(t *testing.T) {
		deps := Dependencies{
			AuthService: &failingAuthService{
				fakeAuthService: newFakeAuthService(),
				err:             injectedErr,
			},
			SessionSigner: testSigner,
		}
		req := httptest.NewRequest(
			http.MethodPost,
			"/api/auth/register",
			strings.NewReader(`{"email":"safe@example.com","password":"secret"}`),
		)
		rec := httptest.NewRecorder()
		NewRouter(deps).ServeHTTP(rec, req)
		assertSafeHTTPError(t, rec, http.StatusInternalServerError, "registration_failed", "Registration failed")
	})

	t.Run("auth login", func(t *testing.T) {
		deps := Dependencies{
			AuthService: &failingAuthService{
				fakeAuthService: newFakeAuthService(),
				err:             injectedErr,
			},
			SessionSigner: testSigner,
		}
		req := httptest.NewRequest(
			http.MethodPost,
			"/api/auth/login",
			strings.NewReader(`{"email":"safe@example.com","password":"not-a-secret"}`),
		)
		rec := httptest.NewRecorder()
		NewRouter(deps).ServeHTTP(rec, req)
		assertSafeHTTPError(t, rec, http.StatusInternalServerError, "login_failed", "Login failed")
	})

	t.Run("SDK runtime", func(t *testing.T) {
		tokenAuth := newFakeTokenAuth()
		tokenAuth.addToken("ascope_sensitive_token", "proj-1", []string{"agent:runtime"})
		deps := Dependencies{
			TokenAuth: tokenAuth,
			RuntimeService: &failingRuntimeService{
				fakeRuntimeService: &fakeRuntimeService{},
				err:                injectedErr,
			},
		}
		req := httptest.NewRequest(
			http.MethodPost,
			"/api/sdk/runs/start",
			strings.NewReader(`{"agent_id":"agent-1","input":"hello"}`),
		)
		req.Header.Set("Authorization", "Bearer ascope_sensitive_token")
		rec := httptest.NewRecorder()
		NewRouter(deps).ServeHTTP(rec, req)
		assertSafeHTTPError(t, rec, http.StatusInternalServerError, "start_run_failed", "Failed to start run")
	})

	t.Run("runner heartbeat", func(t *testing.T) {
		tokenAuth := newFakeTokenAuth()
		tokenAuth.addToken("ascope_sensitive_token", "proj-1", []string{"runner:write"})
		deps := Dependencies{
			TokenAuth: tokenAuth,
			RunnerService: &failingRunnerService{
				fakeRunnerService: newFakeRunnerService(),
				err:               injectedErr,
			},
		}
		req := httptest.NewRequest(
			http.MethodPost,
			"/runner/jobs/job-1/heartbeat",
			strings.NewReader(`{"runner_name":"runner-1"}`),
		)
		req.Header.Set("Authorization", "Bearer ascope_sensitive_token")
		rec := httptest.NewRecorder()
		NewRouter(deps).ServeHTTP(rec, req)
		assertSafeHTTPError(t, rec, http.StatusInternalServerError, "heartbeat_failed", "Failed to heartbeat job")
	})

	t.Run("eval score", func(t *testing.T) {
		projectAuth := newFakeProjectAuth()
		projectAuth.allow("user-1", "proj-1")
		deps := Dependencies{
			SessionSigner: testSigner,
			ProjectAuth:   projectAuth,
			EvalsService: &failingEvalsService{
				fakeEvalsService: newFakeEvalsService(),
				err:              injectedErr,
			},
		}
		req := httptest.NewRequest(
			http.MethodPost,
			"/api/projects/proj-1/eval-scores",
			strings.NewReader(`{"run_id":"run-1","metric_name":"quality","metric_kind":"final_answer","score":0.9}`),
		)
		addTestSession(t, req, "user-1")
		rec := httptest.NewRecorder()
		NewRouter(deps).ServeHTTP(rec, req)
		assertSafeHTTPError(t, rec, http.StatusInternalServerError, "create_failed", "Failed to create eval score")
	})

	t.Run("annotation item", func(t *testing.T) {
		projectAuth := newFakeProjectAuth()
		projectAuth.allow("user-1", "proj-1")
		runsService := newFakeRunsService()
		run, err := runsService.CreateOrGetLiveRunFromTrace(context.Background(), "proj-1", "trace-1", nil)
		if err != nil {
			t.Fatalf("create fake run: %v", err)
		}
		deps := Dependencies{
			SessionSigner: testSigner,
			ProjectAuth:   projectAuth,
			RunsService:   runsService,
			AnnotationService: &failingAnnotationService{
				fakeAnnotationService: newFakeAnnotationService(),
				err:                   injectedErr,
			},
		}
		req := httptest.NewRequest(
			http.MethodPost,
			"/api/projects/proj-1/runs/"+run.ID+"/annotation-items",
			nil,
		)
		addTestSession(t, req, "user-1")
		rec := httptest.NewRecorder()
		NewRouter(deps).ServeHTTP(rec, req)
		assertSafeHTTPError(t, rec, http.StatusInternalServerError, "create_failed", "Failed to create annotation item")
	})
}

func TestRegisterClassifiedErrorsUseFixedSafeMessages(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		status  int
		code    string
		message string
	}{
		{
			name:    "duplicate email",
			err:     fmt.Errorf("%s: %w", sensitiveDependencyError, auth.ErrEmailAlreadyRegistered),
			status:  http.StatusConflict,
			code:    "email_exists",
			message: "Email already registered",
		},
		{
			name:    "short password",
			err:     fmt.Errorf("%s: %w", sensitiveDependencyError, auth.ErrPasswordTooShort),
			status:  http.StatusBadRequest,
			code:    "registration_failed",
			message: auth.ErrPasswordTooShort.Error(),
		},
		{
			name:    "long password",
			err:     fmt.Errorf("%s: %w", sensitiveDependencyError, auth.ErrPasswordTooLong),
			status:  http.StatusBadRequest,
			code:    "registration_failed",
			message: auth.ErrPasswordTooLong.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deps := Dependencies{
				AuthService: &failingAuthService{
					fakeAuthService: newFakeAuthService(),
					err:             tt.err,
				},
				SessionSigner: testSigner,
			}
			req := httptest.NewRequest(
				http.MethodPost,
				"/api/auth/register",
				strings.NewReader(`{"email":"safe@example.com","password":"secret"}`),
			)
			rec := httptest.NewRecorder()
			NewRouter(deps).ServeHTTP(rec, req)
			assertSafeHTTPError(t, rec, tt.status, tt.code, tt.message)
		})
	}
}

func TestOversizedJSONBodyReturns413(t *testing.T) {
	deps := Dependencies{
		AuthService:   newFakeAuthService(),
		SessionSigner: testSigner,
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/auth/register",
		strings.NewReader(strings.Repeat("x", maxJSONBodySize+1)),
	)
	rec := httptest.NewRecorder()
	NewRouter(deps).ServeHTTP(rec, req)

	assertSafeHTTPError(t, rec, http.StatusRequestEntityTooLarge, "body_too_large", "Request body exceeds maximum size")
}

func TestCreateAnnotationItemOptionalBodyValidation(t *testing.T) {
	projectAuth := newFakeProjectAuth()
	projectAuth.allow("user-1", "proj-1")
	runsService := newFakeRunsService()
	run, err := runsService.CreateOrGetLiveRunFromTrace(context.Background(), "proj-1", "trace-optional", nil)
	if err != nil {
		t.Fatalf("create fake run: %v", err)
	}
	deps := Dependencies{
		SessionSigner:     testSigner,
		ProjectAuth:       projectAuth,
		RunsService:       runsService,
		AnnotationService: newFakeAnnotationService(),
	}
	router := NewRouter(deps)

	t.Run("empty body is allowed", func(t *testing.T) {
		req := httptest.NewRequest(
			http.MethodPost,
			"/api/projects/proj-1/runs/"+run.ID+"/annotation-items",
			nil,
		)
		addTestSession(t, req, "user-1")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("malformed non-empty body is rejected", func(t *testing.T) {
		req := httptest.NewRequest(
			http.MethodPost,
			"/api/projects/proj-1/runs/"+run.ID+"/annotation-items",
			strings.NewReader(`{"queue_id":`),
		)
		addTestSession(t, req, "user-1")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assertSafeHTTPError(t, rec, http.StatusBadRequest, "invalid_request", "Invalid JSON body")
	})

	t.Run("oversized non-empty body is rejected", func(t *testing.T) {
		req := httptest.NewRequest(
			http.MethodPost,
			"/api/projects/proj-1/runs/"+run.ID+"/annotation-items",
			strings.NewReader(strings.Repeat("x", maxJSONBodySize+1)),
		)
		addTestSession(t, req, "user-1")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assertSafeHTTPError(t, rec, http.StatusRequestEntityTooLarge, "body_too_large", "Request body exceeds maximum size")
	})
}

func addTestSession(t *testing.T, req *http.Request, userID string) {
	t.Helper()
	signed, err := testSigner.SignUserID(userID)
	if err != nil {
		t.Fatalf("sign session: %v", err)
	}
	req.AddCookie(&http.Cookie{Name: "session", Value: signed})
}

func assertSafeHTTPError(t *testing.T, rec *httptest.ResponseRecorder, status int, code, message string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d: %s", rec.Code, status, rec.Body.String())
	}
	var response map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if response["code"] != code || response["message"] != message {
		t.Fatalf("error response = %#v, want code=%q message=%q", response, code, message)
	}
	for _, sensitive := range []string{
		"SELECT secret",
		"/srv/private/credentials.json",
		"Bearer ascope_sensitive_token",
		"ascope_sensitive_token",
	} {
		if strings.Contains(rec.Body.String(), sensitive) {
			t.Fatalf("response leaked %q: %s", sensitive, rec.Body.String())
		}
	}
}
