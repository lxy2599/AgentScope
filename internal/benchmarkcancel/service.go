// Package benchmarkcancel implements the durable cancellation boundary for
// benchmark eval_jobs. Cancellation is intentionally control-plane only: the
// worker owns its private Task Runner network and must cooperatively cancel an
// active sidecar task after it observes that its lease can no longer progress.
package benchmarkcancel

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	// MaxReasonBytes keeps the durable audit record bounded while leaving room
	// for a human explanation of an obsolete or incorrect immutable snapshot.
	MaxReasonBytes = 512
)

var (
	// ErrExperimentNotFound is deliberately project-scoped. Callers must not be
	// able to distinguish a foreign experiment from a nonexistent one.
	ErrExperimentNotFound = errors.New("benchmark experiment not found")
	// ErrInvalidReason never includes the submitted value: cancellation reasons
	// are persisted as audit data and must never become a secret exfiltration
	// vector through an HTTP response or log.
	ErrInvalidReason = errors.New("benchmark cancellation reason is invalid")

	secretLikeReason = regexp.MustCompile(`(?i)(?:api[_-]?key|secret|password|authorization|credential|private[_-]?key|access[_-]?key)\s*[:=]|\bbearer\s+[a-z0-9._-]{8,}|(?:^|\s)(?:sk-[a-z0-9_-]{16,}|akia[0-9a-z]{16}|eyj[a-z0-9_-]{16,}\.[a-z0-9_-]{16,}\.[a-z0-9_-]{16,})(?:\s|$)`)
)

// Request describes one administrator-initiated cancellation request. Reason
// is retained only in the immutable audit rows created for affected jobs.
type Request struct {
	ProjectID    string
	ExperimentID string
	ActorUserID  string
	Reason       string
}

// Result reports the durable state transition. It deliberately does not echo
// Reason, even though the caller supplied it.
type Result struct {
	ExperimentID  string `json:"experiment_id"`
	Status        string `json:"status"`
	CancelledJobs int    `json:"cancelled_jobs"`
	AuditEvents   int    `json:"audit_events"`
}

// AuditEvent is the durable, job-level fact created when a cancellation changes
// a queued or running job. Reasons are accepted only after secret screening.
type AuditEvent struct {
	ID             string `json:"id"`
	ProjectID      string `json:"project_id"`
	ExperimentID   string `json:"experiment_id"`
	EvalJobID      string `json:"eval_job_id"`
	ActorUserID    string `json:"actor_user_id"`
	Reason         string `json:"reason"`
	PreviousStatus string `json:"previous_status"`
	CreatedAt      string `json:"created_at"`
}

// Store performs the project-scoped, transactional state transition and writes
// one audit row for each job that actually moved from queued/running to
// cancelled.
type Store interface {
	CancelExperiment(ctx context.Context, request Request) (Result, error)
	ListAudit(ctx context.Context, projectID, experimentID string) ([]AuditEvent, error)
}

// Service validates the public cancellation contract before delegating the
// atomic persistence work to Store.
type Service struct {
	store Store
}

func NewService(store Store) *Service {
	return &Service{store: store}
}

// CancelExperiment makes cancellation authoritative for all queued or running
// eval_jobs in one benchmark experiment. It is idempotent: a repeated request
// leaves terminal jobs and their original audit rows untouched.
func (s *Service) CancelExperiment(ctx context.Context, request Request) (Result, error) {
	request.ProjectID = strings.TrimSpace(request.ProjectID)
	request.ExperimentID = strings.TrimSpace(request.ExperimentID)
	request.ActorUserID = strings.TrimSpace(request.ActorUserID)
	request.Reason = strings.TrimSpace(request.Reason)
	if request.ProjectID == "" || request.ExperimentID == "" || request.ActorUserID == "" {
		return Result{}, ErrExperimentNotFound
	}
	if !validReason(request.Reason) {
		return Result{}, ErrInvalidReason
	}
	return s.store.CancelExperiment(ctx, request)
}

// ListAudit returns the retained cancellation reasons and pre-cancellation
// states for one project-scoped benchmark experiment.
func (s *Service) ListAudit(ctx context.Context, projectID, experimentID string) ([]AuditEvent, error) {
	projectID = strings.TrimSpace(projectID)
	experimentID = strings.TrimSpace(experimentID)
	if projectID == "" || experimentID == "" {
		return nil, ErrExperimentNotFound
	}
	return s.store.ListAudit(ctx, projectID, experimentID)
}

func validReason(reason string) bool {
	if reason == "" || len(reason) > MaxReasonBytes || !utf8.ValidString(reason) {
		return false
	}
	if secretLikeReason.MatchString(reason) {
		return false
	}
	for _, r := range reason {
		if r < 0x20 && r != '\n' && r != '\t' {
			return false
		}
	}
	return true
}
