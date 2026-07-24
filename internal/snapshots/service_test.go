package snapshots

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

type memoryRepository struct {
	snapshot Snapshot
	inserted bool
}

func (m *memoryRepository) Insert(_ context.Context, snapshot Snapshot) (Snapshot, bool, error) {
	if m.inserted {
		return Snapshot{}, false, nil
	}
	m.inserted = true
	snapshot.ID = "snapshot-1"
	m.snapshot = snapshot
	return snapshot, true, nil
}

func (m *memoryRepository) GetByRun(_ context.Context, runID, projectID string) (Snapshot, error) {
	if !m.inserted || m.snapshot.RunID != runID || m.snapshot.ProjectID != projectID {
		return Snapshot{}, ErrUnavailable
	}
	return m.snapshot, nil
}

func TestCanonicalizeIsStableAcrossKeyOrder(t *testing.T) {
	t.Parallel()

	first, err := Canonicalize(json.RawMessage(`{"model":{"temperature":0.2,"name":"glm"},"agent":"demo"}`))
	if err != nil {
		t.Fatalf("Canonicalize(first) error = %v", err)
	}
	second, err := Canonicalize(json.RawMessage(`{"agent":"demo","model":{"name":"glm","temperature":0.2}}`))
	if err != nil {
		t.Fatalf("Canonicalize(second) error = %v", err)
	}
	if first.Hash != second.Hash || string(first.Payload) != string(second.Payload) {
		t.Fatalf("canonical snapshots differ:\n%s\n%s", first.Payload, second.Payload)
	}
}

func TestCanonicalizeRedactsSecretsRecursively(t *testing.T) {
	t.Parallel()

	result, err := Canonicalize(json.RawMessage(`{
		"api_key":"secret-1",
		"nested":{"Authorization":"Bearer secret-2","cookie":"sid=secret-3"},
		"safe":"visible"
	}`))
	if err != nil {
		t.Fatalf("Canonicalize() error = %v", err)
	}
	payload := string(result.Payload)
	for _, secret := range []string{"secret-1", "secret-2", "secret-3"} {
		if strings.Contains(payload, secret) {
			t.Fatalf("canonical payload leaked %q: %s", secret, payload)
		}
	}
	if !strings.Contains(payload, `"safe":"visible"`) ||
		strings.Count(payload, `"[REDACTED]"`) != 3 {
		t.Fatalf("canonical payload = %s", payload)
	}
}

func TestCanonicalizeRejectsOversizedPayload(t *testing.T) {
	t.Parallel()

	_, err := Canonicalize(json.RawMessage(`{"value":"` + strings.Repeat("x", MaxPayloadBytes) + `"}`))
	if err == nil {
		t.Fatal("Canonicalize() error = nil, want payload limit error")
	}
}

func TestServiceCreateIsIdempotentForSameHashAndConflictsForDifferentHash(t *testing.T) {
	t.Parallel()

	repository := &memoryRepository{}
	service := NewService(repository)
	first, err := service.Create(
		context.Background(), "run-1", "project-1", SchemaVersionV1,
		json.RawMessage(`{"model":"glm"}`),
	)
	if err != nil {
		t.Fatalf("first Create() error = %v", err)
	}
	second, err := service.Create(
		context.Background(), "run-1", "project-1", SchemaVersionV1,
		json.RawMessage(`{"model":"glm"}`),
	)
	if err != nil {
		t.Fatalf("idempotent Create() error = %v", err)
	}
	if first.ID != second.ID || first.PayloadHash != second.PayloadHash {
		t.Fatalf("idempotent snapshot changed: %#v %#v", first, second)
	}

	_, err = service.Create(
		context.Background(), "run-1", "project-1", SchemaVersionV1,
		json.RawMessage(`{"model":"deepseek"}`),
	)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("different Create() error = %v, want ErrConflict", err)
	}
}
