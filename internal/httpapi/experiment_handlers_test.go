package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateExperimentHandler(t *testing.T) {
	fakeES := newFakeExperimentsService()
	deps := Dependencies{
		AuthService:        newFakeAuthService(),
		ExperimentsService: fakeES,
		SessionSigner:      testSigner,
	}
	router := NewRouter(deps)

	signed, _ := testSigner.SignUserID("user-1")

	t.Run("success", func(t *testing.T) {
		body := `{"dataset_id":"ds-1","target_name":"agent","case_ids":["c1","c2"]}`
		req := httptest.NewRequest(http.MethodPost, "/api/projects/proj-1/experiments", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: signed})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("no auth returns 401", func(t *testing.T) {
		body := `{"dataset_id":"ds-1","target_name":"agent","case_ids":["c1"]}`
		req := httptest.NewRequest(http.MethodPost, "/api/projects/proj-1/experiments", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("missing dataset_id returns 400", func(t *testing.T) {
		body := `{"target_name":"agent","case_ids":["c1"]}`
		req := httptest.NewRequest(http.MethodPost, "/api/projects/proj-1/experiments", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: signed})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("missing target_name returns 400", func(t *testing.T) {
		body := `{"dataset_id":"ds-1","case_ids":["c1"]}`
		req := httptest.NewRequest(http.MethodPost, "/api/projects/proj-1/experiments", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: signed})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})

	t.Run("empty case_ids returns 400", func(t *testing.T) {
		body := `{"dataset_id":"ds-1","target_name":"agent","case_ids":[]}`
		req := httptest.NewRequest(http.MethodPost, "/api/projects/proj-1/experiments", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: signed})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})
}

func TestGetExperimentHandler(t *testing.T) {
	fakeES := newFakeExperimentsService()
	deps := Dependencies{
		AuthService:        newFakeAuthService(),
		ExperimentsService: fakeES,
		SessionSigner:      testSigner,
	}
	router := NewRouter(deps)

	signed, _ := testSigner.SignUserID("user-1")

	// Create an experiment first
	exp, _ := fakeES.CreateExperiment(nil, "proj-1", "ds-1", "", "agent", "v1", []string{"c1", "c2"})

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/experiments/"+exp.ID, nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: signed})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("no auth returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/experiments/"+exp.ID, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})
}
