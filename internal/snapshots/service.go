package snapshots

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

const (
	SchemaVersionV1 = "runtime-contract-v2"
	MaxPayloadBytes = 256 * 1024
	RedactedValue   = "[REDACTED]"
)

var (
	ErrConflict    = errors.New("run snapshot already exists with different content")
	ErrUnavailable = errors.New("run snapshot unavailable")
)

type Prepared struct {
	Payload json.RawMessage
	Hash    string
}

type Snapshot struct {
	ID               string          `json:"id"`
	RunID            string          `json:"run_id"`
	ProjectID        string          `json:"project_id"`
	SchemaVersion    string          `json:"schema_version"`
	CanonicalPayload json.RawMessage `json:"canonical_payload"`
	PayloadHash      string          `json:"payload_hash"`
	CreatedAt        time.Time       `json:"created_at"`
}

type Repository interface {
	Insert(ctx context.Context, snapshot Snapshot) (created Snapshot, inserted bool, err error)
	GetByRun(ctx context.Context, runID, projectID string) (Snapshot, error)
}

type Service struct {
	repository Repository
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

func (s *Service) Create(
	ctx context.Context,
	runID, projectID, schemaVersion string,
	payload json.RawMessage,
) (Snapshot, error) {
	if runID == "" || projectID == "" {
		return Snapshot{}, errors.New("run_id and project_id are required")
	}
	if schemaVersion == "" {
		schemaVersion = SchemaVersionV1
	}
	prepared, err := Canonicalize(payload)
	if err != nil {
		return Snapshot{}, err
	}
	candidate := Snapshot{
		RunID:            runID,
		ProjectID:        projectID,
		SchemaVersion:    schemaVersion,
		CanonicalPayload: prepared.Payload,
		PayloadHash:      prepared.Hash,
	}
	created, inserted, err := s.repository.Insert(ctx, candidate)
	if err != nil {
		return Snapshot{}, err
	}
	if inserted {
		return created, nil
	}
	existing, err := s.repository.GetByRun(ctx, runID, projectID)
	if err != nil {
		return Snapshot{}, err
	}
	if existing.PayloadHash != candidate.PayloadHash ||
		existing.SchemaVersion != candidate.SchemaVersion {
		return Snapshot{}, ErrConflict
	}
	return existing, nil
}

func (s *Service) Get(ctx context.Context, runID, projectID string) (Snapshot, error) {
	snapshot, err := s.repository.GetByRun(ctx, runID, projectID)
	if err != nil {
		return Snapshot{}, err
	}
	return snapshot, nil
}

func Canonicalize(payload json.RawMessage) (Prepared, error) {
	if len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	if len(payload) > MaxPayloadBytes {
		return Prepared{}, fmt.Errorf("snapshot payload exceeds %d bytes", MaxPayloadBytes)
	}

	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return Prepared{}, fmt.Errorf("decode snapshot payload: %w", err)
	}
	if _, ok := value.(map[string]any); !ok {
		return Prepared{}, errors.New("snapshot payload must be a JSON object")
	}

	canonical, err := json.Marshal(redact(value))
	if err != nil {
		return Prepared{}, fmt.Errorf("encode canonical snapshot: %w", err)
	}
	if len(canonical) > MaxPayloadBytes {
		return Prepared{}, fmt.Errorf("canonical snapshot exceeds %d bytes", MaxPayloadBytes)
	}
	sum := sha256.Sum256(canonical)
	return Prepared{
		Payload: canonical,
		Hash:    hex.EncodeToString(sum[:]),
	}, nil
}

func redact(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, item := range typed {
			if isSensitiveKey(key) {
				result[key] = RedactedValue
			} else {
				result[key] = redact(item)
			}
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = redact(item)
		}
		return result
	default:
		return value
	}
}

func isSensitiveKey(key string) bool {
	var normalized strings.Builder
	for _, char := range strings.ToLower(key) {
		if unicode.IsLetter(char) || unicode.IsDigit(char) {
			normalized.WriteRune(char)
		}
	}
	value := normalized.String()
	for _, marker := range []string{
		"password", "passwd", "secret", "token", "cookie",
		"authorization", "apikey", "accesskey", "privatekey",
	} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}
