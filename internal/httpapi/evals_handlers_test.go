package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"AgentScope/internal/evals"
)

type recordingBatchEvalsService struct {
	*fakeEvalsService
	batchCalls int
	received   []evals.EvalScore
}

func (f *recordingBatchEvalsService) CreateEvalScores(
	ctx context.Context,
	scores []evals.EvalScore,
) ([]evals.EvalScore, error) {
	f.batchCalls++
	f.received = append([]evals.EvalScore(nil), scores...)
	return f.fakeEvalsService.CreateEvalScores(ctx, scores)
}

func TestBatchCreateEvalScoresRejectsInvalidRequestsBeforeService(t *testing.T) {
	tests := []struct {
		name    string
		body    func(t *testing.T) string
		code    string
		message string
	}{
		{
			name: "empty batch",
			body: func(*testing.T) string {
				return `{"scores":[]}`
			},
			code:    "no_scores",
			message: "At least one score is required",
		},
		{
			name: "oversized batch",
			body: func(t *testing.T) string {
				t.Helper()
				scores := make([]createEvalScoreRequest, evals.MaxEvalScoreBatchSize+1)
				for i := range scores {
					scores[i] = validCreateEvalScoreRequest()
				}
				payload, err := json.Marshal(map[string]any{"scores": scores})
				if err != nil {
					t.Fatalf("marshal request: %v", err)
				}
				return string(payload)
			},
			code:    "too_many_scores",
			message: "A maximum of 500 scores is allowed",
		},
		{
			name: "later item missing required field",
			body: func(t *testing.T) string {
				t.Helper()
				valid := validCreateEvalScoreRequest()
				invalid := validCreateEvalScoreRequest()
				invalid.MetricName = ""
				payload, err := json.Marshal(map[string]any{
					"scores": []createEvalScoreRequest{valid, invalid},
				})
				if err != nil {
					t.Fatalf("marshal request: %v", err)
				}
				return string(payload)
			},
			code:    "invalid_score",
			message: "Each score must have valid required fields, metric kind, and score",
		},
		{
			name: "unsupported metric kind",
			body: func(t *testing.T) string {
				t.Helper()
				invalid := validCreateEvalScoreRequest()
				invalid.MetricKind = "unknown"
				payload, err := json.Marshal(map[string]any{
					"scores": []createEvalScoreRequest{invalid},
				})
				if err != nil {
					t.Fatalf("marshal request: %v", err)
				}
				return string(payload)
			},
			code:    "invalid_score",
			message: "Each score must have valid required fields, metric kind, and score",
		},
		{
			name: "score out of range",
			body: func(t *testing.T) string {
				t.Helper()
				invalid := validCreateEvalScoreRequest()
				invalid.Score = 1.1
				payload, err := json.Marshal(map[string]any{
					"scores": []createEvalScoreRequest{invalid},
				})
				if err != nil {
					t.Fatalf("marshal request: %v", err)
				}
				return string(payload)
			},
			code:    "invalid_score",
			message: "Each score must have valid required fields, metric kind, and score",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &recordingBatchEvalsService{fakeEvalsService: newFakeEvalsService()}
			router := newEvalScoresTestRouter(service)
			req := httptest.NewRequest(
				http.MethodPost,
				"/api/projects/proj-1/eval-scores/batch",
				strings.NewReader(tt.body(t)),
			)
			addTestSession(t, req, "user-1")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			assertSafeHTTPError(t, rec, http.StatusBadRequest, tt.code, tt.message)
			if service.batchCalls != 0 {
				t.Fatalf("CreateEvalScores() calls = %d, want 0", service.batchCalls)
			}
		})
	}
}

func TestBatchCreateEvalScoresCallsServiceOnce(t *testing.T) {
	service := &recordingBatchEvalsService{fakeEvalsService: newFakeEvalsService()}
	router := newEvalScoresTestRouter(service)
	first := validCreateEvalScoreRequest()
	second := validCreateEvalScoreRequest()
	second.RunID = "run-2"
	second.MetricName = "safety"
	second.MetricKind = "safety"
	payload, err := json.Marshal(map[string]any{
		"scores": []createEvalScoreRequest{first, second},
	})
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/projects/proj-1/eval-scores/batch",
		strings.NewReader(string(payload)),
	)
	addTestSession(t, req, "user-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", rec.Code, rec.Body.String())
	}
	if service.batchCalls != 1 {
		t.Fatalf("CreateEvalScores() calls = %d, want 1", service.batchCalls)
	}
	if len(service.received) != 2 {
		t.Fatalf("received scores = %d, want 2", len(service.received))
	}
}

func TestCreateEvalScoreRejectsInvalidScoreBeforeService(t *testing.T) {
	service := &recordingBatchEvalsService{fakeEvalsService: newFakeEvalsService()}
	router := newEvalScoresTestRouter(service)
	invalid := validCreateEvalScoreRequest()
	invalid.Score = -0.1
	payload, err := json.Marshal(invalid)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/projects/proj-1/eval-scores",
		strings.NewReader(string(payload)),
	)
	addTestSession(t, req, "user-1")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assertSafeHTTPError(
		t,
		rec,
		http.StatusBadRequest,
		"invalid_score",
		"Score fields, metric kind, and score must be valid",
	)
	if len(service.scores) != 0 {
		t.Fatalf("service persisted %d scores, want 0", len(service.scores))
	}
}

func newEvalScoresTestRouter(service EvalsService) http.Handler {
	projectAuth := newFakeProjectAuth()
	projectAuth.allow("user-1", "proj-1")
	return NewRouter(Dependencies{
		SessionSigner: testSigner,
		ProjectAuth:   projectAuth,
		EvalsService:  service,
	})
}

func validCreateEvalScoreRequest() createEvalScoreRequest {
	return createEvalScoreRequest{
		RunID:      "run-1",
		MetricName: "quality",
		MetricKind: "final_answer",
		Score:      0.9,
		Passed:     true,
	}
}
