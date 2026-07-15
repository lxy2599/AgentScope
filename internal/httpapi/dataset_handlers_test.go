package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateDatasetHandler(t *testing.T) {
	fakeDS := newFakeDatasetsService()
	auth := newFakeProjectAuth()
	auth.allow("user-1", "proj-1")

	deps := Dependencies{
		AuthService:     newFakeAuthService(),
		DatasetsService: fakeDS,
		SessionSigner:   testSigner,
		ProjectAuth:     auth,
	}
	router := NewRouter(deps)

	signed, _ := testSigner.SignUserID("user-1")

	t.Run("success", func(t *testing.T) {
		body := `{"name":"test-dataset","description":"a test"}`
		req := httptest.NewRequest(http.MethodPost, "/api/projects/proj-1/datasets", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: signed})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("no auth returns 401", func(t *testing.T) {
		body := `{"name":"test"}`
		req := httptest.NewRequest(http.MethodPost, "/api/projects/proj-1/datasets", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("non-member returns 403", func(t *testing.T) {
		otherUser, _ := testSigner.SignUserID("user-2")
		body := `{"name":"test"}`
		req := httptest.NewRequest(http.MethodPost, "/api/projects/proj-1/datasets", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: otherUser})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})

	t.Run("missing name returns 400", func(t *testing.T) {
		body := `{"name":""}`
		req := httptest.NewRequest(http.MethodPost, "/api/projects/proj-1/datasets", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: signed})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", rec.Code)
		}
	})
}

func TestAddCaseHandler(t *testing.T) {
	fakeDS := newFakeDatasetsService()
	auth := newFakeProjectAuth()
	auth.allow("user-1", "proj-1")

	deps := Dependencies{
		AuthService:     newFakeAuthService(),
		DatasetsService: fakeDS,
		SessionSigner:   testSigner,
		ProjectAuth:     auth,
	}
	router := NewRouter(deps)

	signed, _ := testSigner.SignUserID("user-1")
	ds, _ := fakeDS.CreateDataset(nil, "proj-1", "test", "")

	t.Run("success", func(t *testing.T) {
		body := `{"input":{"q":"hello"},"expected":{"a":"world"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/datasets/"+ds.ID+"/cases", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: signed})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("no auth returns 401", func(t *testing.T) {
		body := `{"input":{"q":"hello"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/datasets/"+ds.ID+"/cases", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("non-member returns 403", func(t *testing.T) {
		otherUser, _ := testSigner.SignUserID("user-2")
		body := `{"input":{"q":"hello"}}`
		req := httptest.NewRequest(http.MethodPost, "/api/datasets/"+ds.ID+"/cases", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: otherUser})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})
}

func TestListCasesHandler(t *testing.T) {
	fakeDS := newFakeDatasetsService()
	auth := newFakeProjectAuth()
	auth.allow("user-1", "proj-1")

	deps := Dependencies{
		AuthService:     newFakeAuthService(),
		DatasetsService: fakeDS,
		SessionSigner:   testSigner,
		ProjectAuth:     auth,
	}
	router := NewRouter(deps)

	signed, _ := testSigner.SignUserID("user-1")
	ds, _ := fakeDS.CreateDataset(nil, "proj-1", "test", "")

	t.Run("success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/datasets/"+ds.ID+"/cases", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: signed})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("no auth returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/datasets/"+ds.ID+"/cases", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("non-member returns 403", func(t *testing.T) {
		otherUser, _ := testSigner.SignUserID("user-2")
		req := httptest.NewRequest(http.MethodGet, "/api/datasets/"+ds.ID+"/cases", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: otherUser})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})
}
