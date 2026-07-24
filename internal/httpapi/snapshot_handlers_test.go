package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"AgentScope/internal/snapshots"
)

type fakeSnapshotService struct {
	snapshot snapshots.Snapshot
	err      error
}

func (f fakeSnapshotService) Get(context.Context, string, string) (snapshots.Snapshot, error) {
	return f.snapshot, f.err
}

func TestGetRunSnapshotReturnsSnapshot(t *testing.T) {
	runsService := newFakeRunsService()
	runsService.runs = append(runsService.runs, runsEntry{id: "run-1", projectID: "project-1"})
	router := NewRouter(Dependencies{
		SessionSigner: testSigner,
		RunsService:   runsService,
		ProjectAuth:   selectiveProjectAuth{},
		SnapshotService: fakeSnapshotService{snapshot: snapshots.Snapshot{
			ID:               "snapshot-1",
			RunID:            "run-1",
			ProjectID:        "project-1",
			SchemaVersion:    snapshots.SchemaVersionV1,
			CanonicalPayload: json.RawMessage(`{"api_key":"[REDACTED]"}`),
			PayloadHash:      strings.Repeat("a", 64),
			CreatedAt:        time.Now(),
		}},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/runs/run-1/snapshot", nil)
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), snapshots.SchemaVersionV1) {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret-value") {
		t.Fatalf("snapshot response leaked secret: %s", rec.Body.String())
	}
}

func TestGetRunSnapshotReportsLegacyRun(t *testing.T) {
	runsService := newFakeRunsService()
	runsService.runs = append(runsService.runs, runsEntry{id: "run-1", projectID: "project-1"})
	router := NewRouter(Dependencies{
		SessionSigner:   testSigner,
		RunsService:     runsService,
		ProjectAuth:     selectiveProjectAuth{},
		SnapshotService: fakeSnapshotService{err: snapshots.ErrUnavailable},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/runs/run-1/snapshot", nil)
	req.AddCookie(signCookie(t, "user-1"))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound ||
		!strings.Contains(rec.Body.String(), "snapshot_unavailable") {
		t.Fatalf("status = %d body = %s", rec.Code, rec.Body.String())
	}
}
