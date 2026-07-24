package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"AgentScope/internal/evals"
)

func TestCreateExperimentHandler(t *testing.T) {
	fakeDS := newFakeDatasetsService()
	fakeES := newFakeExperimentsService()

	auth := newFakeProjectAuth()
	auth.allow("user-1", "proj-1")

	deps := Dependencies{
		AuthService:        newFakeAuthService(),
		DatasetsService:    fakeDS,
		ExperimentsService: fakeES,
		SessionSigner:      testSigner,
		ProjectAuth:        auth,
	}
	router := NewRouter(deps)

	signed, _ := testSigner.SignUserID("user-1")
	ds, _ := fakeDS.CreateDataset(context.Background(), "proj-1", "test-ds", "")

	t.Run("success", func(t *testing.T) {
		body := `{"dataset_id":"` + ds.ID + `","target_name":"agent","case_ids":["c1","c2"]}`
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

	t.Run("non-member returns 403", func(t *testing.T) {
		otherUser, _ := testSigner.SignUserID("user-2")
		body := `{"dataset_id":"ds-1","target_name":"agent","case_ids":["c1"]}`
		req := httptest.NewRequest(http.MethodPost, "/api/projects/proj-1/experiments", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: otherUser})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})

	t.Run("dataset not in project returns 403", func(t *testing.T) {
		otherDS, _ := fakeDS.CreateDataset(context.Background(), "proj-other", "other-ds", "")
		body := `{"dataset_id":"` + otherDS.ID + `","target_name":"agent","case_ids":["c1"]}`
		req := httptest.NewRequest(http.MethodPost, "/api/projects/proj-1/experiments", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: "session", Value: signed})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
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
	fakeDS := newFakeDatasetsService()
	fakeES := newFakeExperimentsService()

	auth := newFakeProjectAuth()
	auth.allow("user-1", "proj-1")

	deps := Dependencies{
		AuthService:        newFakeAuthService(),
		DatasetsService:    fakeDS,
		ExperimentsService: fakeES,
		SessionSigner:      testSigner,
		ProjectAuth:        auth,
	}
	router := NewRouter(deps)

	signed, _ := testSigner.SignUserID("user-1")
	exp, _ := fakeES.CreateExperiment(context.Background(), "proj-1", "ds-1", "", "agent", "v1", []string{"c1", "c2"})

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

	t.Run("non-member returns 403", func(t *testing.T) {
		otherUser, _ := testSigner.SignUserID("user-2")
		req := httptest.NewRequest(http.MethodGet, "/api/experiments/"+exp.ID, nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: otherUser})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})
}

func TestListExperimentsHandler(t *testing.T) {
	fakeDS := newFakeDatasetsService()
	fakeES := newFakeExperimentsService()

	auth := newFakeProjectAuth()
	auth.allow("user-1", "proj-1")

	deps := Dependencies{
		AuthService:        newFakeAuthService(),
		DatasetsService:    fakeDS,
		ExperimentsService: fakeES,
		SessionSigner:      testSigner,
		ProjectAuth:        auth,
	}
	router := NewRouter(deps)

	signed, _ := testSigner.SignUserID("user-1")

	// Create some experiments for proj-1
	fakeES.CreateExperiment(context.Background(), "proj-1", "ds-1", "", "agent-a", "v1", []string{"c1", "c2"})
	fakeES.CreateExperiment(context.Background(), "proj-1", "ds-1", "", "agent-b", "v1", []string{"c3"})
	// Create an experiment for another project
	fakeES.CreateExperiment(context.Background(), "proj-other", "ds-other", "", "agent-c", "v1", []string{"c4"})

	t.Run("success lists project experiments", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/projects/proj-1/experiments", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: signed})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "agent-a") {
			t.Fatal("expected response to contain agent-a")
		}
		if !strings.Contains(rec.Body.String(), "agent-b") {
			t.Fatal("expected response to contain agent-b")
		}
		if strings.Contains(rec.Body.String(), "agent-c") {
			t.Fatal("response should not contain other project experiments")
		}
	})

	t.Run("no auth returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/projects/proj-1/experiments", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("non-member returns 403", func(t *testing.T) {
		otherUser, _ := testSigner.SignUserID("user-2")
		req := httptest.NewRequest(http.MethodGet, "/api/projects/proj-1/experiments", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: otherUser})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})
}

func TestListExperimentResultsHandler(t *testing.T) {
	fakeDS := newFakeDatasetsService()
	fakeES := newFakeExperimentsService()

	auth := newFakeProjectAuth()
	auth.allow("user-1", "proj-1")

	deps := Dependencies{
		AuthService:        newFakeAuthService(),
		DatasetsService:    fakeDS,
		ExperimentsService: fakeES,
		SessionSigner:      testSigner,
		ProjectAuth:        auth,
	}
	router := NewRouter(deps)

	signed, _ := testSigner.SignUserID("user-1")
	exp, _ := fakeES.CreateExperiment(context.Background(), "proj-1", "ds-1", "", "agent", "v1", []string{"c1"})

	// Add a result with run_id set
	fakeES.results[exp.ID] = []EvalCaseResult{
		{
			ID:            "result-1",
			EvalJobID:     "job-1",
			RunID:         "run-1",
			DatasetCaseID: "case-1",
			Score:         0.95,
			Passed:        true,
			Reason:        "correct",
			CreatedAt:     "2026-07-15T12:00:00Z",
		},
	}

	t.Run("success lists experiment results with run_id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/projects/proj-1/experiments/"+exp.ID+"/results", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: signed})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "result-1") {
			t.Fatal("expected response to contain result-1")
		}
		if !strings.Contains(rec.Body.String(), "dataset_case_id") {
			t.Fatal("expected response to contain dataset_case_id")
		}
		if !strings.Contains(rec.Body.String(), "case-1") {
			t.Fatal("expected response to contain dataset_case_id value case-1")
		}
		if !strings.Contains(rec.Body.String(), "run_id") {
			t.Fatal("expected response to contain run_id field — experiment results must carry a run_id for Trace linkage")
		}
		if !strings.Contains(rec.Body.String(), "run-1") {
			t.Fatal("expected response to contain run_id value run-1")
		}
	})

	t.Run("no auth returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/projects/proj-1/experiments/"+exp.ID+"/results", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %d", rec.Code)
		}
	})

	t.Run("non-member returns 403", func(t *testing.T) {
		otherUser, _ := testSigner.SignUserID("user-2")
		req := httptest.NewRequest(http.MethodGet, "/api/projects/proj-1/experiments/"+exp.ID+"/results", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: otherUser})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", rec.Code)
		}
	})

	t.Run("experiment from different project returns 404", func(t *testing.T) {
		otherExp, _ := fakeES.CreateExperiment(context.Background(), "proj-other", "ds-other", "", "other-agent", "v1", []string{"c1"})
		req := httptest.NewRequest(http.MethodGet, "/api/projects/proj-1/experiments/"+otherExp.ID+"/results", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: signed})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestExperimentReportHandler(t *testing.T) {
	fakeDS := newFakeDatasetsService()
	fakeES := newFakeExperimentsService()
	fakeEvs := newFakeEvalsService()

	auth := newFakeProjectAuth()
	auth.allow("user-1", "proj-1")

	deps := Dependencies{
		AuthService:        newFakeAuthService(),
		DatasetsService:    fakeDS,
		ExperimentsService: fakeES,
		EvalsService:       fakeEvs,
		SessionSigner:      testSigner,
		ProjectAuth:        auth,
	}
	router := NewRouter(deps)

	signed, _ := testSigner.SignUserID("user-1")
	exp, _ := fakeES.CreateExperiment(context.Background(), "proj-1", "ds-1", "", "agent", "v1", []string{"c1", "c2"})

	// Add results with run_ids
	fakeES.results[exp.ID] = []EvalCaseResult{
		{
			ID:            "result-1",
			EvalJobID:     "job-1",
			RunID:         "run-1",
			DatasetCaseID: "case-1",
			Score:         0.85,
			Passed:        true,
			Reason:        "correct",
			CreatedAt:     "2026-07-15T12:00:00Z",
		},
		{
			ID:            "result-2",
			EvalJobID:     "job-2",
			RunID:         "run-2",
			DatasetCaseID: "case-2",
			Score:         0.45,
			Passed:        false,
			Reason:        "incorrect",
			CreatedAt:     "2026-07-15T12:00:00Z",
		},
	}

	// Add eval scores for run-1 with rubric fields
	fakeEvs.CreateEvalScore(context.Background(), evals.EvalScore{
		ProjectID:  "proj-1",
		RunID:      "run-1",
		TraceID:    "trace-1",
		MetricName: "accuracy",
		MetricKind: "final_answer",
		Score:      0.9,
		Passed:     true,
		Reason:     "accurate",
		Details:    `{"weight":0.4,"pass_threshold":0.7,"veto":false,"dimension_scores":{"accuracy":0.9,"relevance":0.85}}`,
	})
	fakeEvs.CreateEvalScore(context.Background(), evals.EvalScore{
		ProjectID:  "proj-1",
		RunID:      "run-1",
		TraceID:    "trace-1",
		MetricName: "completeness",
		MetricKind: "final_answer",
		Score:      0.7,
		Passed:     true,
		Reason:     "complete",
		Details:    `{"weight":0.3,"pass_threshold":0.7,"veto":false}`,
	})
	fakeEvs.CreateEvalScore(context.Background(), evals.EvalScore{
		ProjectID:  "proj-1",
		RunID:      "run-2",
		TraceID:    "trace-2",
		MetricName: "safety",
		MetricKind: "safety",
		Score:      0.2,
		Passed:     false,
		Reason:     "unsafe",
		Details:    `{"weight":0.5,"pass_threshold":0.7,"veto":true,"veto_triggered":true,"veto_reason":"contains harmful content"}`,
	})

	t.Run("report returns results with eval_scores", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/projects/proj-1/experiments/"+exp.ID+"/report", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: signed})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		body := rec.Body.String()
		if !strings.Contains(body, "eval_scores") {
			t.Fatal("report response should contain eval_scores for each result")
		}
		if !strings.Contains(body, "result-1") {
			t.Fatal("report response should contain result-1")
		}
		// Verify eval_scores contain metric details
		if !strings.Contains(body, "weight") {
			t.Fatal("eval_scores details should contain weight")
		}
		if !strings.Contains(body, "pass_threshold") {
			t.Fatal("eval_scores details should contain pass_threshold")
		}
		if !strings.Contains(body, "veto") {
			t.Fatal("eval_scores details should contain veto")
		}
		if !strings.Contains(body, "dimension_scores") {
			t.Fatal("eval_scores details should contain dimension_scores")
		}
		// Verify veto info is present
		if !strings.Contains(body, "veto_triggered") {
			t.Fatal("eval_scores details should contain veto_triggered")
		}
		if !strings.Contains(body, "contains harmful content") {
			t.Fatal("eval_scores details should contain veto_reason")
		}
		// Both results should be present
		if !strings.Contains(body, "run-1") {
			t.Fatal("report response should contain run-1")
		}
		if !strings.Contains(body, "run-2") {
			t.Fatal("report response should contain run-2")
		}
		// Verify accuracy metric score is present
		if !strings.Contains(body, "accuracy") {
			t.Fatal("eval_scores should contain metric_name accuracy")
		}
	})
}
