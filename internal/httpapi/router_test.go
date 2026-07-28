package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestHealthzReturns200(t *testing.T) {
	router := NewRouter(Dependencies{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200, got %d", rec.Code)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %q", ct)
	}

	var body map[string]bool
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	ok, exists := body["ok"]
	if !exists {
		t.Fatal("response body missing 'ok' field")
	}
	if !ok {
		t.Fatal("expected ok=true")
	}
}

func TestStaticServingWithNoStaticFS(t *testing.T) {
	router := NewRouter(Dependencies{})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 without StaticFS, got %d", rec.Code)
	}
}

func TestStaticServesIndex(t *testing.T) {
	memFS := fstest.MapFS{
		"index.html": {Data: []byte("<html><body>AgentScope</body></html>")},
	}

	router := NewRouter(Dependencies{StaticFS: memFS})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestStaticSPAFallback(t *testing.T) {
	memFS := fstest.MapFS{
		"index.html": {Data: []byte("<html><body>SPA</body></html>")},
	}

	router := NewRouter(Dependencies{StaticFS: memFS})

	// Unknown path should fallback to index.html (SPA behavior)
	req := httptest.NewRequest(http.MethodGet, "/runs/some-uuid", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (SPA fallback), got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/unknown-path", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for /api/* path, got %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for /healthz, got %d", rec.Code)
	}
}

func TestStaticAPIpathsNotServed(t *testing.T) {
	memFS := fstest.MapFS{
		"index.html": {Data: []byte("index")},
	}

	router := NewRouter(Dependencies{StaticFS: memFS})

	tests := []struct {
		path       string
		wantNot404 bool
	}{
		{"/api/unknown", false},
		{"/v1/traces", false},
		{"/runner/jobs/claim", true}, // returns 401 not 404 (needs auth)
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if tt.wantNot404 && rec.Code == http.StatusNotFound {
			t.Fatalf("expected non-404 for %s, got %d", tt.path, rec.Code)
		}
		if !tt.wantNot404 && rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for %s, got %d", tt.path, rec.Code)
		}
	}
}

func TestStaticAssetsAreNotRewrittenToSPAIndex(t *testing.T) {
	memFS := fstest.MapFS{
		"index.html":      {Data: []byte("<html>index</html>")},
		"assets/app-1.js": {Data: []byte("export const app = true;")},
	}
	router := NewRouter(Dependencies{StaticFS: memFS})
	req := httptest.NewRequest(http.MethodGet, "/assets/app-1.js", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("asset status = %d, want 200", rec.Code)
	}
	if got := rec.Body.String(); got != "export const app = true;" {
		t.Fatalf("asset body = %q, want JS asset content", got)
	}
}
