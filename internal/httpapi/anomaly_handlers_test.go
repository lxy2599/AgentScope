package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"AgentScope/internal/anomalies"
)

type fakeAnomalyService struct {
	projectID string
	records   []anomalies.Record
	calls     int
	detects   int
	detectErr error
}

func (f *fakeAnomalyService) ListProject(
	_ context.Context,
	projectID string,
	_ int,
) ([]anomalies.Record, error) {
	f.calls++
	f.projectID = projectID
	return f.records, nil
}

func (f *fakeAnomalyService) DetectProject(
	_ context.Context,
	projectID string,
) ([]anomalies.Record, error) {
	f.detects++
	f.projectID = projectID
	return f.records, f.detectErr
}

func TestAnomalyHandlerReturnsProjectRecords(t *testing.T) {
	service := &fakeAnomalyService{records: []anomalies.Record{{
		ID: "anomaly-1",
		Group: anomalies.Group{
			ProjectID: "project-1", AgentName: "agent-a", AgentVersion: "v1",
			Environment: "prod", Metric: anomalies.MetricLLMCost,
		},
		Status:      anomalies.StatusInsufficientBaseline,
		WindowStart: time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC),
		WindowEnd:   time.Date(2026, 7, 24, 10, 15, 0, 0, time.UTC),
	}}}
	router := NewRouter(Dependencies{
		SessionSigner:  testSigner,
		ProjectAuth:    selectiveProjectAuth{},
		AnomalyService: service,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/projects/project-1/anomalies?limit=10", nil)
	request.AddCookie(signCookie(t, "member-1"))
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if service.projectID != "project-1" ||
		!strings.Contains(response.Body.String(), `"insufficient_baseline"`) {
		t.Fatalf("service project=%q response=%s", service.projectID, response.Body.String())
	}
}

func TestAnomalyHandlerRejectsNonMemberBeforeQuery(t *testing.T) {
	service := &fakeAnomalyService{}
	router := NewRouter(Dependencies{
		SessionSigner:  testSigner,
		ProjectAuth:    selectiveProjectAuth{err: errors.New("not a member")},
		AnomalyService: service,
	})
	request := httptest.NewRequest(http.MethodGet, "/api/projects/project-2/anomalies", nil)
	request.AddCookie(signCookie(t, "outsider"))
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", response.Code, response.Body.String())
	}
	if service.calls != 0 {
		t.Fatalf("anomaly service called %d times", service.calls)
	}
}

func TestTriggerAnomalyDetectionRequiresProjectAdmin(t *testing.T) {
	service := &fakeAnomalyService{records: []anomalies.Record{{
		ID: "anomaly-1",
		Group: anomalies.Group{
			ProjectID: "project-1",
			Metric:    anomalies.MetricRunLatency,
		},
		Status: anomalies.StatusAnomaly,
	}}}
	router := NewRouter(Dependencies{
		SessionSigner:    testSigner,
		ProjectAuth:      selectiveProjectAuth{},
		ProjectAdminAuth: fakeProjectAdminAuth{},
		AnomalyService:   service,
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/projects/project-1/anomalies/detect",
		nil,
	)
	request.AddCookie(signCookie(t, "admin-1"))
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if service.detects != 1 || service.projectID != "project-1" {
		t.Fatalf("detects=%d project=%q", service.detects, service.projectID)
	}
}

func TestTriggerAnomalyDetectionRejectsNonAdmin(t *testing.T) {
	service := &fakeAnomalyService{}
	router := NewRouter(Dependencies{
		SessionSigner:    testSigner,
		ProjectAuth:      selectiveProjectAuth{},
		ProjectAdminAuth: fakeProjectAdminAuth{err: errors.New("not an admin")},
		AnomalyService:   service,
	})
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/projects/project-1/anomalies/detect",
		nil,
	)
	request.AddCookie(signCookie(t, "member-1"))
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403: %s", response.Code, response.Body.String())
	}
	if service.detects != 0 {
		t.Fatalf("DetectProject() called %d times", service.detects)
	}
}
