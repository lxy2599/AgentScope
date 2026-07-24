package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSamplingRulesHandler(t *testing.T) {
	fakeSvc := newFakeSamplingService()
	projectAuth := newFakeProjectAuth()
	projectAuth.allow("user-1", "proj-1")
	projectAuth.allow("user-1", "proj-2")

	deps := Dependencies{
		SessionSigner:   testSigner,
		ProjectAuth:     projectAuth,
		SamplingService: fakeSvc,
	}
	router := NewRouter(deps)

	// Helper to make authenticated requests
	makeReq := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if body != "" {
			req = httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		}
		signed, _ := testSigner.SignUserID("user-1")
		req.AddCookie(&http.Cookie{Name: "session", Value: signed})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	t.Run("create sampling rule", func(t *testing.T) {
		body := `{"name":"test-rule","sample_rate":0.5,"filters":"{\"has_error\":true}"}`
		rec := makeReq("POST", "/api/projects/proj-1/sampling-rules", body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}

		var resp SamplingRule
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp.Name != "test-rule" {
			t.Errorf("name = %q, want %q", resp.Name, "test-rule")
		}
		if resp.SampleRate != 0.5 {
			t.Errorf("sample_rate = %f, want %f", resp.SampleRate, 0.5)
		}
		if !resp.Enabled {
			t.Error("enabled = false, want true")
		}
	})

	t.Run("create with missing name returns 400", func(t *testing.T) {
		body := `{"sample_rate":0.5}`
		rec := makeReq("POST", "/api/projects/proj-1/sampling-rules", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("create with invalid filters returns stable 400", func(t *testing.T) {
		body := `{"name":"invalid-filters","sample_rate":0.5,"filters":"[]"}`
		rec := makeReq("POST", "/api/projects/proj-1/sampling-rules", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
		var response map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
			t.Fatalf("decode error response: %v", err)
		}
		if response["code"] != "invalid_sampling_rule" {
			t.Fatalf("error code = %q, want invalid_sampling_rule", response["code"])
		}
		if response["message"] != "filters must be a valid JSON object" {
			t.Fatalf("error message = %q", response["message"])
		}
	})

	t.Run("create rejects explicit null rule values", func(t *testing.T) {
		tests := []struct {
			name    string
			body    string
			message string
		}{
			{
				name:    "null sample rate",
				body:    `{"name":"null-rate","sample_rate":null}`,
				message: "sample_rate must be between 0.0 and 1.0",
			},
			{
				name:    "null filters",
				body:    `{"name":"null-filters","sample_rate":0.5,"filters":null}`,
				message: "filters must be a valid JSON object",
			},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				rec := makeReq("POST", "/api/projects/proj-1/sampling-rules", tt.body)
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
				}
				var response map[string]string
				json.NewDecoder(rec.Body).Decode(&response)
				if response["code"] != "invalid_sampling_rule" || response["message"] != tt.message {
					t.Fatalf("error response = %#v", response)
				}
			})
		}
	})

	t.Run("create in non-member project returns 403", func(t *testing.T) {
		body := `{"name":"test","sample_rate":0.5}`
		rec := makeReq("POST", "/api/projects/proj-other/sampling-rules", body)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("list rules by project", func(t *testing.T) {
		// Create some rules first
		makeReq("POST", "/api/projects/proj-1/sampling-rules", `{"name":"rule-a","sample_rate":1.0}`)
		makeReq("POST", "/api/projects/proj-1/sampling-rules", `{"name":"rule-b","sample_rate":0.5}`)
		makeReq("POST", "/api/projects/proj-2/sampling-rules", `{"name":"rule-c","sample_rate":0.2}`)

		rec := makeReq("GET", "/api/projects/proj-1/sampling-rules", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var rules []SamplingRule
		if err := json.NewDecoder(rec.Body).Decode(&rules); err != nil {
			t.Fatalf("decode: %v", err)
		}
		// 2 existing + 2 created = 4
		if len(rules) != 3 {
			t.Fatalf("got %d rules, want 3", len(rules))
		}
	})

	t.Run("get rule by ID", func(t *testing.T) {
		// Create a rule and get its ID
		rec := makeReq("POST", "/api/projects/proj-1/sampling-rules", `{"name":"get-me","sample_rate":0.8}`)
		var created SamplingRule
		json.NewDecoder(rec.Body).Decode(&created)

		rec = makeReq("GET", "/api/sampling-rules/"+created.ID, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var got SamplingRule
		json.NewDecoder(rec.Body).Decode(&got)
		if got.ID != created.ID {
			t.Errorf("id = %q, want %q", got.ID, created.ID)
		}
	})

	t.Run("get nonexistent rule returns 404", func(t *testing.T) {
		rec := makeReq("GET", "/api/sampling-rules/nonexistent", "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("update rule enabled", func(t *testing.T) {
		rec := makeReq("POST", "/api/projects/proj-1/sampling-rules", `{"name":"toggle","sample_rate":0.5}`)
		var created SamplingRule
		json.NewDecoder(rec.Body).Decode(&created)

		// Disable
		body := `{"enabled":false}`
		rec = makeReq("PATCH", "/api/sampling-rules/"+created.ID, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var updated SamplingRule
		json.NewDecoder(rec.Body).Decode(&updated)
		if updated.Enabled {
			t.Error("rule still enabled after disabling")
		}
	})

	t.Run("update all mutable fields", func(t *testing.T) {
		rec := makeReq("POST", "/api/projects/proj-1/sampling-rules", `{"name":"before-update","sample_rate":0.25}`)
		var created SamplingRule
		json.NewDecoder(rec.Body).Decode(&created)

		body := `{"name":"after-update","enabled":false,"sample_rate":0.75,"filters":"{\"status\":\"failed\"}"}`
		rec = makeReq("PATCH", "/api/sampling-rules/"+created.ID, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var updated SamplingRule
		if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
			t.Fatalf("decode updated rule: %v", err)
		}
		if updated.Name != "after-update" || updated.Enabled || updated.SampleRate != 0.75 {
			t.Fatalf("updated rule = %#v, want all scalar fields updated", updated)
		}
		if updated.Filters != `{"status":"failed"}` {
			t.Fatalf("updated filters = %q", updated.Filters)
		}
	})

	t.Run("invalid update is rejected before enabled mutation", func(t *testing.T) {
		tests := []struct {
			name    string
			patch   string
			message string
		}{
			{
				name:    "sample rate out of range",
				patch:   `{"enabled":false,"sample_rate":1.5}`,
				message: "sample_rate must be between 0.0 and 1.0",
			},
			{
				name:    "malformed filters",
				patch:   `{"enabled":false,"filters":"{"}`,
				message: "filters must be a valid JSON object",
			},
			{
				name:    "array filters",
				patch:   `{"enabled":false,"filters":"[]"}`,
				message: "filters must be a valid JSON object",
			},
			{
				name:    "scalar filters",
				patch:   `{"enabled":false,"filters":"true"}`,
				message: "filters must be a valid JSON object",
			},
			{
				name:    "null filters",
				patch:   `{"enabled":false,"filters":null}`,
				message: "filters must be a valid JSON object",
			},
			{
				name:    "null sample rate",
				patch:   `{"enabled":false,"sample_rate":null}`,
				message: "sample_rate must be between 0.0 and 1.0",
			},
			{
				name:    "null name",
				patch:   `{"enabled":false,"name":null}`,
				message: "name is required",
			},
			{
				name:    "null enabled",
				patch:   `{"enabled":null}`,
				message: "enabled must be a boolean",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				rec := makeReq("POST", "/api/projects/proj-1/sampling-rules", `{"name":"must-stay-enabled","sample_rate":0.5}`)
				var created SamplingRule
				json.NewDecoder(rec.Body).Decode(&created)

				rec = makeReq("PATCH", "/api/sampling-rules/"+created.ID, tt.patch)
				if rec.Code != http.StatusBadRequest {
					t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
				}
				var response map[string]string
				if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
					t.Fatalf("decode error response: %v", err)
				}
				if response["code"] != "invalid_sampling_rule" || response["message"] != tt.message {
					t.Fatalf("error response = %#v", response)
				}

				rec = makeReq("GET", "/api/sampling-rules/"+created.ID, "")
				var unchanged SamplingRule
				json.NewDecoder(rec.Body).Decode(&unchanged)
				if !unchanged.Enabled || unchanged.SampleRate != 0.5 || unchanged.Filters != "{}" {
					t.Fatalf("invalid patch mutated rule: %#v", unchanged)
				}
			})
		}
	})

	t.Run("empty update returns stable 400", func(t *testing.T) {
		rec := makeReq("POST", "/api/projects/proj-1/sampling-rules", `{"name":"empty-patch","sample_rate":0.5}`)
		var created SamplingRule
		json.NewDecoder(rec.Body).Decode(&created)

		rec = makeReq("PATCH", "/api/sampling-rules/"+created.ID, `{}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
		}
		var response map[string]string
		json.NewDecoder(rec.Body).Decode(&response)
		if response["code"] != "invalid_sampling_rule" || response["message"] != "at least one sampling rule field is required" {
			t.Fatalf("error response = %#v", response)
		}
	})

	t.Run("delete rule", func(t *testing.T) {
		rec := makeReq("POST", "/api/projects/proj-1/sampling-rules", `{"name":"delete-me","sample_rate":0.5}`)
		var created SamplingRule
		json.NewDecoder(rec.Body).Decode(&created)

		rec = makeReq("DELETE", "/api/sampling-rules/"+created.ID, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify deleted
		rec = makeReq("GET", "/api/sampling-rules/"+created.ID, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 after delete, got %d", rec.Code)
		}
	})

	t.Run("unauthenticated request returns 401", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/projects/proj-1/sampling-rules", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})
}
