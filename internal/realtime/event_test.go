package realtime

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEventValidate(t *testing.T) {
	t.Parallel()

	valid := Event{
		ID:              42,
		SchemaVersion:   SchemaVersionV1,
		ProjectID:       "project-1",
		RunID:           "run-1",
		Type:            EventRunStateChanged,
		OccurredAt:      time.Date(2026, 7, 24, 8, 0, 0, 0, time.UTC),
		Payload:         json.RawMessage(`{"status":"thinking"}`),
		PayloadRedacted: true,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got := valid.StreamID(); got != "42-0" {
		t.Fatalf("StreamID() = %q, want 42-0", got)
	}
	valid.StreamCursor = "1234567890-7"
	if got := valid.StreamID(); got != "1234567890-7" {
		t.Fatalf("StreamID() = %q, want Redis cursor", got)
	}

	tests := []struct {
		name   string
		mutate func(*Event)
	}{
		{"missing id", func(e *Event) { e.ID = 0 }},
		{"missing project", func(e *Event) { e.ProjectID = "" }},
		{"missing type", func(e *Event) { e.Type = "" }},
		{"wrong schema", func(e *Event) { e.SchemaVersion = "realtime-event-v2" }},
		{"missing timestamp", func(e *Event) { e.OccurredAt = time.Time{} }},
		{"invalid payload", func(e *Event) { e.Payload = json.RawMessage(`{"bad"`) }},
		{"oversized payload", func(e *Event) {
			e.Payload = json.RawMessage(`{"value":"` + strings.Repeat("x", MaxPayloadBytes) + `"}`)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event := valid
			tt.mutate(&event)
			if err := event.Validate(); err == nil {
				t.Fatal("Validate() error = nil, want validation error")
			}
		})
	}
}

func TestOutboxRecordEventUsesLowSensitivityEnvelope(t *testing.T) {
	t.Parallel()

	record := OutboxRecord{
		ID:              7,
		SchemaVersion:   SchemaVersionV1,
		ProjectID:       "project-1",
		RunID:           "run-1",
		AgentID:         "agent-1",
		EventType:       EventRunStarted,
		Payload:         []byte(`{"input_summary":"redacted"}`),
		OccurredAt:      time.Date(2026, 7, 24, 8, 1, 0, 0, time.UTC),
		PayloadRedacted: true,
	}

	event, err := record.Event()
	if err != nil {
		t.Fatalf("Event() error = %v", err)
	}
	if event.ID != record.ID || event.ProjectID != record.ProjectID || event.RunID != record.RunID {
		t.Fatalf("Event() identity mismatch: %#v", event)
	}
	if !event.PayloadRedacted {
		t.Fatal("PayloadRedacted = false, want true")
	}
}
