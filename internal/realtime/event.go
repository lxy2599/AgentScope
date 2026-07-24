package realtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

const (
	SchemaVersionV1 = "realtime-event-v1"
	MaxPayloadBytes = 16 * 1024

	EventAgentRegistered     = "agent.registered"
	EventAgentHeartbeat      = "agent.heartbeat"
	EventAgentStatusChanged  = "agent.status.changed"
	EventRunStarted          = "run.started"
	EventRunStateChanged     = "run.state.changed"
	EventRunSemanticAppended = "run.semantic.appended"
	EventRunCompleted        = "run.completed"
	EventRunFailed           = "run.failed"
	EventIngestAccepted      = "ingest.accepted"
	EventIngestRejected      = "ingest.rejected"
	EventEvalCompleted       = "eval.completed"
	EventQuotaWarning        = "quota.warning"
	EventQuotaExceeded       = "quota.exceeded"
	EventOperationalAnomaly  = "operational.anomaly"
)

// Event is the low-sensitivity envelope transported through Redis and SSE.
type Event struct {
	ID              int64           `json:"id"`
	StreamCursor    string          `json:"-"`
	SchemaVersion   string          `json:"schema_version"`
	ProjectID       string          `json:"project_id"`
	RunID           string          `json:"run_id,omitempty"`
	AgentID         string          `json:"agent_id,omitempty"`
	AgentInstanceID string          `json:"agent_instance_id,omitempty"`
	Type            string          `json:"type"`
	OccurredAt      time.Time       `json:"occurred_at"`
	Payload         json.RawMessage `json:"payload"`
	PayloadRedacted bool            `json:"payload_redacted"`
}

func (e Event) Validate() error {
	switch {
	case e.ID <= 0:
		return errors.New("event id must be positive")
	case e.SchemaVersion != SchemaVersionV1:
		return fmt.Errorf("unsupported schema version %q", e.SchemaVersion)
	case e.ProjectID == "":
		return errors.New("project id is required")
	case e.Type == "":
		return errors.New("event type is required")
	case e.OccurredAt.IsZero():
		return errors.New("occurred_at is required")
	case len(e.Payload) > MaxPayloadBytes:
		return fmt.Errorf("event payload exceeds %d bytes", MaxPayloadBytes)
	case len(e.Payload) == 0 || !json.Valid(e.Payload):
		return errors.New("event payload must be valid JSON")
	default:
		return nil
	}
}

func (e Event) StreamID() string {
	if e.StreamCursor != "" {
		return e.StreamCursor
	}
	return strconv.FormatInt(e.ID, 10) + "-0"
}

// OutboxRecord is the storage-neutral representation claimed by Relay.
type OutboxRecord struct {
	ID              int64
	SchemaVersion   string
	ProjectID       string
	RunID           string
	AgentID         string
	AgentInstanceID string
	EventType       string
	Payload         []byte
	OccurredAt      time.Time
	PayloadRedacted bool
}

func (r OutboxRecord) Event() (Event, error) {
	event := Event{
		ID:              r.ID,
		SchemaVersion:   r.SchemaVersion,
		ProjectID:       r.ProjectID,
		RunID:           r.RunID,
		AgentID:         r.AgentID,
		AgentInstanceID: r.AgentInstanceID,
		Type:            r.EventType,
		OccurredAt:      r.OccurredAt,
		Payload:         json.RawMessage(r.Payload),
		PayloadRedacted: r.PayloadRedacted,
	}
	if err := event.Validate(); err != nil {
		return Event{}, err
	}
	return event, nil
}
