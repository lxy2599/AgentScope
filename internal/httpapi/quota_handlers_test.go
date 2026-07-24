package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"AgentScope/internal/evals"
	"AgentScope/internal/quotas"
)

type fakeQuotaService struct {
	status       quotas.Status
	updated      quotas.Quota
	updatedBy    string
	updateErr    error
	ingest       map[quotas.Dimension]quotas.Decision
	sse          quotas.Decision
	releaseCount int
	daily        map[quotas.Dimension]quotas.Decision
	dailyAmounts map[quotas.Dimension]int64
}

func (f *fakeQuotaService) Status(context.Context, string) (quotas.Status, error) {
	return f.status, nil
}

func (f *fakeQuotaService) UpdateQuota(
	_ context.Context,
	_ string,
	updatedBy string,
	quota quotas.Quota,
) (quotas.Quota, error) {
	f.updated = quota
	f.updatedBy = updatedBy
	return quota, f.updateErr
}

func (f *fakeQuotaService) CheckIngest(
	_ context.Context,
	_ string,
	dimension quotas.Dimension,
	_ int64,
) quotas.Decision {
	if decision, ok := f.ingest[dimension]; ok {
		return decision
	}
	return quotas.Decision{Allowed: true, Dimension: dimension, Limit: 100, Remaining: 99}
}

func (f *fakeQuotaService) AcquireSSE(context.Context, string) quotas.Decision {
	return f.sse
}

func (f *fakeQuotaService) ReleaseSSE(context.Context, string) {
	f.releaseCount++
}

func (f *fakeQuotaService) ConsumeDailyBudget(
	_ context.Context,
	_ string,
	dimension quotas.Dimension,
	amount int64,
) quotas.Decision {
	if f.dailyAmounts == nil {
		f.dailyAmounts = make(map[quotas.Dimension]int64)
	}
	f.dailyAmounts[dimension] += amount
	if decision, ok := f.daily[dimension]; ok {
		return decision
	}
	return quotas.Decision{Allowed: true}
}

type fakeProjectAdminAuth struct {
	err error
}

func (f fakeProjectAdminAuth) IsProjectAdmin(context.Context, string, string) error {
	return f.err
}

func TestQuotaHandlerAllowsMemberRead(t *testing.T) {
	service := &fakeQuotaService{status: quotas.Status{
		Quota:  quotas.DefaultQuota("project-1"),
		Usage:  map[quotas.Dimension]int64{quotas.DimensionSSEConnections: 2},
		Events: []quotas.Event{},
	}}
	router := NewRouter(Dependencies{
		SessionSigner:    testSigner,
		ProjectAuth:      selectiveProjectAuth{},
		ProjectAdminAuth: fakeProjectAdminAuth{},
		QuotaService:     service,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/projects/project-1/quotas", nil)
	request.AddCookie(signCookie(t, "member-1"))
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"sse_connections":2`) {
		t.Fatalf("response = %s", response.Body.String())
	}
}

func TestQuotaHandlerRejectsMemberUpdate(t *testing.T) {
	router := NewRouter(Dependencies{
		SessionSigner:    testSigner,
		ProjectAuth:      selectiveProjectAuth{},
		ProjectAdminAuth: fakeProjectAdminAuth{err: errors.New("admin required")},
		QuotaService:     &fakeQuotaService{},
	})
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/projects/project-1/quotas",
		strings.NewReader(`{"ingest_requests_per_minute":10}`),
	)
	request.AddCookie(signCookie(t, "member-1"))
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", response.Code, response.Body.String())
	}
}

func TestQuotaHandlerAdminUpdatesAllLimits(t *testing.T) {
	service := &fakeQuotaService{}
	router := NewRouter(Dependencies{
		SessionSigner:    testSigner,
		ProjectAuth:      selectiveProjectAuth{},
		ProjectAdminAuth: fakeProjectAdminAuth{},
		QuotaService:     service,
	})
	body, _ := json.Marshal(quotas.Quota{
		IngestRequestsPerMinute: 10,
		IngestSpansPerMinute:    20,
		IngestBytesPerMinute:    30,
		SSEConnections:          4,
		TokensPerDay:            50,
		CostMicrosPerDay:        60,
	})
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/projects/project-1/quotas",
		strings.NewReader(string(body)),
	)
	request.AddCookie(signCookie(t, "owner-1"))
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", response.Code, response.Body.String())
	}
	if service.updatedBy != "owner-1" || service.updated.ProjectID != "project-1" {
		t.Fatalf("update = %+v by %q", service.updated, service.updatedBy)
	}
}

func TestConsumeJudgeBudgetsRejectsHardCostLimit(t *testing.T) {
	input, output := int64(12), int64(8)
	cost := 0.25
	service := &fakeQuotaService{daily: map[quotas.Dimension]quotas.Decision{
		quotas.DimensionTokensDaily: {
			Allowed: true, Dimension: quotas.DimensionTokensDaily,
		},
		quotas.DimensionCostMicrosDaily: {
			Allowed: false, Dimension: quotas.DimensionCostMicrosDaily,
			Limit: 100_000, Remaining: 0,
		},
	}}
	request := submitJudgeVotesRequest{Votes: []evals.JudgeVoteSubmission{{
		InputTokens: &input, OutputTokens: &output, Cost: &cost,
	}}}
	response := httptest.NewRecorder()

	if consumeJudgeBudgets(response, httptest.NewRequest(http.MethodPost, "/", nil), service, "project-1", request) {
		t.Fatal("consumeJudgeBudgets returned true after hard cost rejection")
	}
	if response.Code != http.StatusPaymentRequired {
		t.Fatalf("status = %d, want 402: %s", response.Code, response.Body.String())
	}
	if service.dailyAmounts[quotas.DimensionTokensDaily] != 20 ||
		service.dailyAmounts[quotas.DimensionCostMicrosDaily] != 250_000 {
		t.Fatalf("daily amounts = %#v", service.dailyAmounts)
	}
}
