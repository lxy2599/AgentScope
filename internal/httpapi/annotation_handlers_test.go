package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAnnotationHandlers covers CRUD and permission checks for
// annotation queues, items, and promote-to-dataset.
func TestAnnotationHandlers(t *testing.T) {
	fakeSvc := newFakeAnnotationService()
	fakeDs := newFakeDatasetsService()
	fakeRuns := newFakeRunsService()
	projectAuth := newFakeProjectAuth()
	projectAuth.allow("user-1", "proj-1")
	projectAuth.allow("user-1", "proj-2")

	deps := Dependencies{
		SessionSigner:     testSigner,
		ProjectAuth:       projectAuth,
		AnnotationService: fakeSvc,
		DatasetsService:   fakeDs,
		RunsService:       fakeRuns,
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

	// Helper to make requests as a different user (non-member of proj-1)
	makeReqAs := func(userID, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		if body != "" {
			req = httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		}
		signed, _ := testSigner.SignUserID(userID)
		req.AddCookie(&http.Cookie{Name: "session", Value: signed})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// Create a queue in proj-1 for testing
	rec := makeReq("POST", "/api/projects/proj-1/annotation-queues", `{"name":"test-queue","description":"for testing"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create queue: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var testQueue AnnotationQueue
	if err := json.NewDecoder(rec.Body).Decode(&testQueue); err != nil {
		t.Fatalf("decode queue: %v", err)
	}

	// Create an item in the queue
	rec = makeReq("POST", "/api/projects/proj-1/annotation-queues", `{"name":"item-queue"}`)
	var itemQueue AnnotationQueue
	json.NewDecoder(rec.Body).Decode(&itemQueue)

	// Note: create annotation items through the fake service directly
	item, _ := fakeSvc.CreateItem(nil, itemQueue.ID, "run-1", "trace-1", 1, "[]")
	_ = item

	t.Run("list items by queue - member", func(t *testing.T) {
		rec := makeReq("GET", "/api/annotation-queues/"+itemQueue.ID+"/items", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var items []AnnotationItem
		if err := json.NewDecoder(rec.Body).Decode(&items); err != nil {
			t.Fatalf("decode: %v", err)
		}
	})

	t.Run("list items by queue - non-member returns 403", func(t *testing.T) {
		rec := makeReqAs("user-other", "GET", "/api/annotation-queues/"+itemQueue.ID+"/items", "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("update item - member succeeds", func(t *testing.T) {
		body := `{"corrected_output":"fixed answer","comment":"looks good"}`
		rec := makeReq("PATCH", "/api/annotation-items/"+item.ID, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var updated AnnotationItem
		if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if updated.CorrectedOutput != "fixed answer" {
			t.Errorf("corrected_output = %q, want %q", updated.CorrectedOutput, "fixed answer")
		}
	})

	t.Run("PATCH only status preserves existing fields", func(t *testing.T) {
		// Create an item with corrected_output, comment, and expected_tool_path set
		itemWithContent, _ := fakeSvc.CreateItem(nil, itemQueue.ID, "run-preserve", "trace-preserve", 1, "[]")
		fakeSvc.UpdateItem(nil, itemWithContent.ID,
			"pending", "original_output", `["original_tool"]`, "original_comment")

		// PATCH only status — other fields should NOT be cleared
		body := `{"status":"in_progress"}`
		rec := makeReq("PATCH", "/api/annotation-items/"+itemWithContent.ID, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var updated AnnotationItem
		if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if updated.Status != "in_progress" {
			t.Errorf("status = %q, want %q", updated.Status, "in_progress")
		}
		if updated.CorrectedOutput != "original_output" {
			t.Errorf("corrected_output was cleared; got %q, want %q", updated.CorrectedOutput, "original_output")
		}
		if updated.ExpectedToolPath != `["original_tool"]` {
			t.Errorf("expected_tool_path was cleared; got %q, want %q", updated.ExpectedToolPath, `["original_tool"]`)
		}
		if updated.Comment != "original_comment" {
			t.Errorf("comment was cleared; got %q, want %q", updated.Comment, "original_comment")
		}
	})

	t.Run("PATCH with empty corrected_output clears it", func(t *testing.T) {
		// Create an item with corrected_output set
		itemWithOutput, _ := fakeSvc.CreateItem(nil, itemQueue.ID, "run-clear", "trace-clear", 1, "[]")
		fakeSvc.UpdateItem(nil, itemWithOutput.ID,
			"pending", "to_be_cleared", "", "")

		// PATCH with corrected_output = "" — should clear it
		body := `{"corrected_output":""}`
		rec := makeReq("PATCH", "/api/annotation-items/"+itemWithOutput.ID, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var updated AnnotationItem
		if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if updated.CorrectedOutput != "" {
			t.Errorf("corrected_output = %q, want empty string (should be cleared)", updated.CorrectedOutput)
		}
	})

	t.Run("PATCH with expected_tool_path empty clears it", func(t *testing.T) {
		itemWithPath, _ := fakeSvc.CreateItem(nil, itemQueue.ID, "run-pathclear", "trace-pathclear", 1, "[]")
		fakeSvc.UpdateItem(nil, itemWithPath.ID,
			"pending", "", `["tool1","tool2"]`, "")

		body := `{"expected_tool_path":""}`
		rec := makeReq("PATCH", "/api/annotation-items/"+itemWithPath.ID, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var updated AnnotationItem
		if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if updated.ExpectedToolPath != "" {
			t.Errorf("expected_tool_path = %q, want empty (should be cleared)", updated.ExpectedToolPath)
		}
	})

	t.Run("update item - non-member returns 403", func(t *testing.T) {
		// Create an item in proj-2
		rec2 := makeReq("POST", "/api/projects/proj-2/annotation-queues", `{"name":"other-queue"}`)
		var otherQueue AnnotationQueue
		json.NewDecoder(rec2.Body).Decode(&otherQueue)
		otherItem, _ := fakeSvc.CreateItem(nil, otherQueue.ID, "run-2", "trace-2", 1, "[]")

		// user-1 is member of proj-2, this test uses user-other who is not
		body := `{"corrected_output":"hack"}`
		rec := makeReqAs("user-other", "PATCH", "/api/annotation-items/"+otherItem.ID, body)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("promote - member succeeds", func(t *testing.T) {
		// Create a dataset in proj-1
		ds, _ := fakeDs.CreateDataset(nil, "proj-1", "test-ds", "")

		// Create an item with corrected output
		item2, _ := fakeSvc.CreateItem(nil, itemQueue.ID, "run-3", "trace-3", 1, "[]")
		fakeSvc.UpdateItem(nil, item2.ID, "", "correct answer", "", "")

		body := `{"dataset_id":"` + ds.ID + `"}`
		rec := makeReq("POST", "/api/annotation-items/"+item2.ID+"/promote-to-dataset", body)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		var resp map[string]string
		json.NewDecoder(rec.Body).Decode(&resp)
		if resp["case_id"] == "" {
			t.Error("expected case_id in response")
		}
	})

	t.Run("promote - non-member returns 403", func(t *testing.T) {
		item3, _ := fakeSvc.CreateItem(nil, itemQueue.ID, "run-4", "trace-4", 1, "[]")
		fakeSvc.UpdateItem(nil, item3.ID, "", "good answer", "", "")

		ds, _ := fakeDs.CreateDataset(nil, "proj-2", "other-ds", "")

		// user-1 is member of proj-2, but test user-other is not a member of proj-1
		body := `{"dataset_id":"` + ds.ID + `"}`
		rec := makeReqAs("user-other", "POST", "/api/annotation-items/"+item3.ID+"/promote-to-dataset", body)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("promote - dataset project mismatch returns 403", func(t *testing.T) {
		// Create an item in proj-1
		item4, _ := fakeSvc.CreateItem(nil, itemQueue.ID, "run-5", "trace-5", 1, "[]")
		fakeSvc.UpdateItem(nil, item4.ID, "", "good answer", "", "")

		// Dataset is in proj-2 (different project)
		ds, _ := fakeDs.CreateDataset(nil, "proj-2", "mismatch-ds", "")

		body := `{"dataset_id":"` + ds.ID + `"}`
		rec := makeReq("POST", "/api/annotation-items/"+item4.ID+"/promote-to-dataset", body)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// TestCreateAnnotationItemFromRun covers the POST /api/projects/{projectID}/runs/{runID}/annotation-items endpoint.
func TestCreateAnnotationItemFromRun(t *testing.T) {
	fakeSvc := newFakeAnnotationService()
	fakeDs := newFakeDatasetsService()
	fakeRuns := newFakeRunsService()
	projectAuth := newFakeProjectAuth()
	projectAuth.allow("user-1", "proj-1")

	deps := Dependencies{
		SessionSigner:     testSigner,
		ProjectAuth:       projectAuth,
		AnnotationService: fakeSvc,
		DatasetsService:   fakeDs,
		RunsService:       fakeRuns,
	}
	router := NewRouter(deps)

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

	// Create a run in proj-1 via the fake runs service
	run, _ := fakeRuns.CreateOrGetLiveRunFromTrace(nil, "proj-1", "trace-abc", nil)

	t.Run("success - creates annotation item from run", func(t *testing.T) {
		// First create a queue
		rec := makeReq("POST", "/api/projects/proj-1/annotation-queues", `{"name":"test-queue"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create queue: expected 201, got %d", rec.Code)
		}

		// Create annotation item from run
		rec = makeReq("POST", "/api/projects/proj-1/runs/" + run.ID + "/annotation-items", `{}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
		}
		var item AnnotationItem
		if err := json.NewDecoder(rec.Body).Decode(&item); err != nil {
			t.Fatalf("decode item: %v", err)
		}
		if item.RunID != run.ID {
			t.Errorf("run_id = %q, want %q", item.RunID, run.ID)
		}
		if item.Status != "pending" {
			t.Errorf("status = %q, want pending", item.Status)
		}
	})

	t.Run("rejects run from different project", func(t *testing.T) {
		// Create a run in proj-2
		otherRun, _ := fakeRuns.CreateOrGetLiveRunFromTrace(nil, "proj-2", "trace-xyz", nil)

		// Try to create annotation item in proj-1 with a run from proj-2
		rec := makeReq("POST", "/api/projects/proj-1/runs/" + otherRun.ID + "/annotation-items", `{}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("rejects nonexistent run", func(t *testing.T) {
		rec := makeReq("POST", "/api/projects/proj-1/runs/nonexistent-run/annotation-items", `{}`)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}
