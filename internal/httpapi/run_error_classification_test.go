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

	"AgentScope/internal/runs"
)

const runSensitiveError = "postgres://admin:secret@db/private Authorization: Bearer run-secret"

type classifiedRunsService struct {
	*fakeRunsService
	getRunErr          error
	getRunDetailErr    error
	getRunByTraceIDErr error
	failRunErr         error
}

func (s *classifiedRunsService) GetRun(ctx context.Context, runID string) (runs.Run, error) {
	if s.getRunErr != nil {
		return runs.Run{}, s.getRunErr
	}
	return s.fakeRunsService.GetRun(ctx, runID)
}

func (s *classifiedRunsService) GetRunDetail(ctx context.Context, runID string) (runs.RunDetail, error) {
	if s.getRunDetailErr != nil {
		return runs.RunDetail{}, s.getRunDetailErr
	}
	return s.fakeRunsService.GetRunDetail(ctx, runID)
}

func (s *classifiedRunsService) GetRunByTraceID(ctx context.Context, traceID, projectID string) (runs.Run, error) {
	if s.getRunByTraceIDErr != nil {
		return runs.Run{}, s.getRunByTraceIDErr
	}
	return s.fakeRunsService.GetRunByTraceID(ctx, traceID, projectID)
}

func (s *classifiedRunsService) FailRunByRunner(ctx context.Context, runID, projectID, errorStr string) error {
	if s.failRunErr != nil {
		return s.failRunErr
	}
	return s.fakeRunsService.FailRunByRunner(ctx, runID, projectID, errorStr)
}

func TestGetRunDetailHandlerClassifiesLookupErrors(t *testing.T) {
	notFoundErr := fmt.Errorf("%s: %w", runSensitiveError, runs.ErrRunNotFound)
	databaseErr := errors.New(runSensitiveError)

	tests := []struct {
		name    string
		err     error
		status  int
		code    string
		message string
	}{
		{
			name:    "wrapped not found",
			err:     notFoundErr,
			status:  http.StatusNotFound,
			code:    "not_found",
			message: "Run not found",
		},
		{
			name:    "database failure",
			err:     databaseErr,
			status:  http.StatusInternalServerError,
			code:    "get_run_failed",
			message: "Failed to get run",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &classifiedRunsService{
				fakeRunsService: newFakeRunsService(),
				getRunErr:       tt.err,
			}
			projectAuth := newFakeProjectAuth()
			projectAuth.allow("user-1", "proj-1")
			deps := Dependencies{
				SessionSigner: testSigner,
				ProjectAuth:   projectAuth,
				RunsService:   service,
			}

			req := httptest.NewRequest(http.MethodGet, "/api/runs/run-1", nil)
			addTestSession(t, req, "user-1")
			rec := httptest.NewRecorder()
			NewRouter(deps).ServeHTTP(rec, req)

			assertRunHTTPError(t, rec, tt.status, tt.code, tt.message)
		})
	}
}

func TestGetRunDetailHandlerClassifiesDetailErrors(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		status  int
		code    string
		message string
	}{
		{
			name:    "wrapped not found",
			err:     fmt.Errorf("%s: %w", runSensitiveError, runs.ErrRunNotFound),
			status:  http.StatusNotFound,
			code:    "not_found",
			message: "Run not found",
		},
		{
			name:    "database failure",
			err:     errors.New(runSensitiveError),
			status:  http.StatusInternalServerError,
			code:    "detail_failed",
			message: "Failed to get run detail",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := newFakeRunsService()
			base.runs = append(base.runs, runsEntry{id: "run-1", projectID: "proj-1", traceID: "trace-1"})
			service := &classifiedRunsService{
				fakeRunsService: base,
				getRunDetailErr: tt.err,
			}
			projectAuth := newFakeProjectAuth()
			projectAuth.allow("user-1", "proj-1")
			deps := Dependencies{
				SessionSigner: testSigner,
				ProjectAuth:   projectAuth,
				RunsService:   service,
			}

			req := httptest.NewRequest(http.MethodGet, "/api/runs/run-1", nil)
			addTestSession(t, req, "user-1")
			rec := httptest.NewRecorder()
			NewRouter(deps).ServeHTTP(rec, req)

			assertRunHTTPError(t, rec, tt.status, tt.code, tt.message)
		})
	}
}

func TestRunnerGetRunByTraceClassifiesErrors(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		status  int
		code    string
		message string
	}{
		{
			name:    "wrapped not found",
			err:     fmt.Errorf("%s: %w", runSensitiveError, runs.ErrRunNotFound),
			status:  http.StatusNotFound,
			code:    "not_found",
			message: "No run found for the given trace ID",
		},
		{
			name:    "database failure",
			err:     errors.New(runSensitiveError),
			status:  http.StatusInternalServerError,
			code:    "get_run_failed",
			message: "Failed to get run",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &classifiedRunsService{
				fakeRunsService:    newFakeRunsService(),
				getRunByTraceIDErr: tt.err,
			}
			rec := performRunnerRunRequest(
				t,
				service,
				http.MethodGet,
				"/runner/runs/by-trace/trace-1",
				"",
			)
			assertRunHTTPError(t, rec, tt.status, tt.code, tt.message)
		})
	}
}

func TestRunnerGetRunDetailClassifiesErrors(t *testing.T) {
	tests := []struct {
		name            string
		getRunErr       error
		getRunDetailErr error
		status          int
		code            string
		message         string
	}{
		{
			name:      "lookup wrapped not found",
			getRunErr: fmt.Errorf("%s: %w", runSensitiveError, runs.ErrRunNotFound),
			status:    http.StatusNotFound,
			code:      "not_found",
			message:   "Run not found",
		},
		{
			name:      "lookup database failure",
			getRunErr: errors.New(runSensitiveError),
			status:    http.StatusInternalServerError,
			code:      "get_run_failed",
			message:   "Failed to get run",
		},
		{
			name:            "detail wrapped not found",
			getRunDetailErr: fmt.Errorf("%s: %w", runSensitiveError, runs.ErrRunNotFound),
			status:          http.StatusNotFound,
			code:            "not_found",
			message:         "Run not found",
		},
		{
			name:            "detail database failure",
			getRunDetailErr: errors.New(runSensitiveError),
			status:          http.StatusInternalServerError,
			code:            "detail_failed",
			message:         "Failed to get run detail",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := newFakeRunsService()
			base.runs = append(base.runs, runsEntry{id: "run-1", projectID: "proj-1", traceID: "trace-1"})
			service := &classifiedRunsService{
				fakeRunsService: base,
				getRunErr:       tt.getRunErr,
				getRunDetailErr: tt.getRunDetailErr,
			}
			rec := performRunnerRunRequest(
				t,
				service,
				http.MethodGet,
				"/runner/runs/run-1/detail",
				"",
			)
			assertRunHTTPError(t, rec, tt.status, tt.code, tt.message)
		})
	}
}

func TestRunnerFailRunClassifiesErrors(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		status  int
		code    string
		message string
	}{
		{
			name:    "wrapped not found",
			err:     fmt.Errorf("%s: %w", runSensitiveError, runs.ErrRunNotFound),
			status:  http.StatusNotFound,
			code:    "not_found",
			message: "Run not found",
		},
		{
			name:    "database failure",
			err:     errors.New(runSensitiveError),
			status:  http.StatusInternalServerError,
			code:    "fail_run_failed",
			message: "Failed to mark run as failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &classifiedRunsService{
				fakeRunsService: newFakeRunsService(),
				failRunErr:      tt.err,
			}
			rec := performRunnerRunRequest(
				t,
				service,
				http.MethodPost,
				"/runner/runs/run-1/fail",
				`{"error":"target failed"}`,
			)
			assertRunHTTPError(t, rec, tt.status, tt.code, tt.message)
		})
	}
}

func performRunnerRunRequest(
	t *testing.T,
	service RunsServiceInterface,
	method string,
	path string,
	body string,
) *httptest.ResponseRecorder {
	t.Helper()

	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("runner-token", "proj-1", []string{"runner:write"})
	deps := Dependencies{
		TokenAuth:   tokenAuth,
		RunsService: service,
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer runner-token")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	NewRouter(deps).ServeHTTP(rec, req)
	return rec
}

func assertRunHTTPError(
	t *testing.T,
	rec *httptest.ResponseRecorder,
	status int,
	code string,
	message string,
) {
	t.Helper()

	if rec.Code != status {
		t.Fatalf("status = %d, want %d: %s", rec.Code, status, rec.Body.String())
	}
	var response map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["code"] != code || response["message"] != message {
		t.Fatalf("response = %#v, want code=%q message=%q", response, code, message)
	}
	for _, sensitive := range []string{"admin:secret", "/private", "Bearer run-secret", "run-secret"} {
		if strings.Contains(rec.Body.String(), sensitive) {
			t.Fatalf("response leaked %q: %s", sensitive, rec.Body.String())
		}
	}
}
