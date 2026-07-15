package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunnerClaimJobHandler(t *testing.T) {
	fakeRunnerSvc := newFakeRunnerService()
	fakeRunnerSvc.addClaimableJob()

	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_testvalidtoken", "proj-1", []string{"runner:write"})

	deps := Dependencies{
		TokenAuth:     tokenAuth,
		RunnerService: fakeRunnerSvc,
	}
	router := NewRouter(deps)

	t.Run("success with bearer token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/runner/jobs/claim?runner_name=test-runner", nil)
		req.Header.Set("Authorization", "Bearer ascope_testvalidtoken")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var body map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}
		// Assert enrichment fields are present in the HTTP response
		if _, ok := body["case_input"]; !ok {
			t.Fatal("expected case_input in claim response")
		}
		if _, ok := body["target_name"]; !ok {
			t.Fatal("expected target_name in claim response")
		}
		if _, ok := body["target_version"]; !ok {
			t.Fatal("expected target_version in claim response")
		}
		if v, ok := body["case_input"].(string); !ok || v == "" {
			t.Fatal("expected non-empty case_input in claim response")
		}
	})

	t.Run("missing token returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/runner/jobs/claim", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("invalid token returns 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/runner/jobs/claim", nil)
		req.Header.Set("Authorization", "Bearer invalid-token")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})
}

func TestRunnerHeartbeatHandler(t *testing.T) {
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_validtoken", "proj-1", []string{"runner:write"})

	fakeRunnerSvc := newFakeRunnerService()
	jobID := fakeRunnerSvc.addClaimableJob()

	deps := Dependencies{
		TokenAuth:     tokenAuth,
		RunnerService: fakeRunnerSvc,
	}
	router := NewRouter(deps)

	t.Run("success", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"runner_name": "test-runner"})
		req := httptest.NewRequest(http.MethodPost, "/runner/jobs/"+jobID+"/heartbeat", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer ascope_validtoken")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("missing token returns 401", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"runner_name": "test-runner"})
		req := httptest.NewRequest(http.MethodPost, "/runner/jobs/"+jobID+"/heartbeat", strings.NewReader(string(body)))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})
}

func TestRunnerSubmitResultHandler(t *testing.T) {
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_validtoken", "proj-1", []string{"runner:write"})

	fakeRunnerSvc := newFakeRunnerService()
	jobID := fakeRunnerSvc.addClaimableJob()

	deps := Dependencies{
		TokenAuth:     tokenAuth,
		RunnerService: fakeRunnerSvc,
	}
	router := NewRouter(deps)

	t.Run("success", func(t *testing.T) {
		body := `{"runner_name":"r1","run_id":"run-1","score":0.95,"passed":true,"reason":"ok","details":{"key":"val"},"actual_output":"hello","latency_ms":100}`
		req := httptest.NewRequest(http.MethodPost, "/runner/jobs/"+jobID+"/results", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer ascope_validtoken")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("missing runner_name returns 400", func(t *testing.T) {
		body := `{"run_id":"run-1","score":0.5,"passed":true}`
		req := httptest.NewRequest(http.MethodPost, "/runner/jobs/"+jobID+"/results", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer ascope_validtoken")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})
}

func TestRunnerFailJobHandler(t *testing.T) {
	tokenAuth := newFakeTokenAuth()
	tokenAuth.addToken("ascope_validtoken", "proj-1", []string{"runner:write"})

	fakeRunnerSvc := newFakeRunnerService()
	jobID := fakeRunnerSvc.addClaimableJob()

	deps := Dependencies{
		TokenAuth:     tokenAuth,
		RunnerService: fakeRunnerSvc,
	}
	router := NewRouter(deps)

	t.Run("success", func(t *testing.T) {
		body := `{"runner_name":"r1","error":"agent crashed"}`
		req := httptest.NewRequest(http.MethodPost, "/runner/jobs/"+jobID+"/fail", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer ascope_validtoken")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("missing error returns 400", func(t *testing.T) {
		body := `{"runner_name":"r1","error":""}`
		req := httptest.NewRequest(http.MethodPost, "/runner/jobs/"+jobID+"/fail", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer ascope_validtoken")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})
}
