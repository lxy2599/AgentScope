package benchmarkcancel

import (
	"context"
	"errors"
	"testing"
)

type fakeStore struct {
	request Request
	result  Result
	audit   []AuditEvent
	err     error
	calls   int
}

func (f *fakeStore) CancelExperiment(_ context.Context, request Request) (Result, error) {
	f.calls++
	f.request = request
	return f.result, f.err
}

func (f *fakeStore) ListAudit(_ context.Context, _, _ string) ([]AuditEvent, error) {
	return f.audit, f.err
}

func TestServiceCancelsWithNormalizedAuditedReason(t *testing.T) {
	store := &fakeStore{result: Result{ExperimentID: "exp-1", Status: "cancelled", CancelledJobs: 2, AuditEvents: 2}}
	service := NewService(store)

	got, err := service.CancelExperiment(context.Background(), Request{
		ProjectID: " project-1 ", ExperimentID: " exp-1 ", ActorUserID: " admin-1 ", Reason: "  obsolete image digest in immutable snapshot  ",
	})
	if err != nil {
		t.Fatalf("CancelExperiment() error = %v", err)
	}
	if got != store.result {
		t.Fatalf("CancelExperiment() = %#v, want %#v", got, store.result)
	}
	if store.calls != 1 {
		t.Fatalf("store calls = %d, want 1", store.calls)
	}
	if store.request != (Request{ProjectID: "project-1", ExperimentID: "exp-1", ActorUserID: "admin-1", Reason: "obsolete image digest in immutable snapshot"}) {
		t.Fatalf("store request = %#v", store.request)
	}
}

func TestServiceRejectsSecretLikeReasonWithoutCallingStore(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)
	_, err := service.CancelExperiment(context.Background(), Request{
		ProjectID: "project-1", ExperimentID: "exp-1", ActorUserID: "admin-1", Reason: "superseded because api_key=do-not-store",
	})
	if !errors.Is(err, ErrInvalidReason) {
		t.Fatalf("CancelExperiment() error = %v, want ErrInvalidReason", err)
	}
	if store.calls != 0 {
		t.Fatalf("store calls = %d, want 0", store.calls)
	}
}

func TestServiceRejectsEmptyOrOversizeReason(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)
	for _, reason := range []string{"", string(make([]byte, MaxReasonBytes+1))} {
		_, err := service.CancelExperiment(context.Background(), Request{
			ProjectID: "project-1", ExperimentID: "exp-1", ActorUserID: "admin-1", Reason: reason,
		})
		if !errors.Is(err, ErrInvalidReason) {
			t.Fatalf("reason length %d: error = %v, want ErrInvalidReason", len(reason), err)
		}
	}
	if store.calls != 0 {
		t.Fatalf("store calls = %d, want 0", store.calls)
	}
}
