package onlinerval

import (
	"context"
	"errors"
	"time"
)

// JobStatus is the durable lifecycle of a single online evaluation attempt set.
type JobStatus string

const (
	JobQueued     JobStatus = "queued"
	JobSampling   JobStatus = "sampling"
	JobRunning    JobStatus = "running"
	JobRetryWait  JobStatus = "retry_wait"
	JobSucceeded  JobStatus = "succeeded"
	JobSkipped    JobStatus = "skipped"
	JobFailed     JobStatus = "failed"
	JobDeadLetter JobStatus = "dead_letter"
)

func (s JobStatus) Terminal() bool {
	switch s {
	case JobSucceeded, JobSkipped, JobFailed, JobDeadLetter:
		return true
	default:
		return false
	}
}

var (
	ErrJobNotFound        = errors.New("online evaluation job not found")
	ErrJobNotRetryable    = errors.New("online evaluation job is not retryable")
	ErrRunProjectMismatch = errors.New("run does not belong to online evaluation job project")
)

// Job is PostgreSQL-authoritative work for one Run/evaluator version pair.
type Job struct {
	ID               string     `json:"id"`
	ProjectID        string     `json:"project_id"`
	RunID            string     `json:"run_id"`
	TraceID          string     `json:"trace_id"`
	EvaluatorVersion string     `json:"evaluator_version"`
	Status           JobStatus  `json:"status"`
	SamplingRuleID   string     `json:"sampling_rule_id,omitempty"`
	SamplingReason   string     `json:"sampling_reason,omitempty"`
	EvaluationSource string     `json:"evaluation_source,omitempty"`
	FallbackReason   string     `json:"fallback_reason,omitempty"`
	AttemptCount     int        `json:"attempt_count"`
	MaxAttempts      int        `json:"max_attempts"`
	AvailableAt      time.Time  `json:"available_at"`
	ClaimedBy        string     `json:"claimed_by,omitempty"`
	ClaimedAt        *time.Time `json:"claimed_at,omitempty"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
	LastError        string     `json:"last_error,omitempty"`
	AnnotationItemID string     `json:"annotation_item_id,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

// Completion is persisted atomically with the score set, terminal job state,
// and (when required) the auto-created annotation item.
type Completion struct {
	Job              Job
	Run              *Run
	Scores           []EvalScore
	EvaluationSource string
	FallbackReason   string
	CreateAnnotation bool
}

// JobRepository owns durable scheduling facts. Redis is deliberately absent:
// it may publish a projection of these changes, never replace them.
type JobRepository interface {
	Enqueue(ctx context.Context, projectID, runID, traceID, evaluatorVersion string) (Job, error)
	Claim(ctx context.Context, workerID string, limit int, now time.Time) ([]Job, error)
	MarkSkipped(ctx context.Context, jobID, samplingRuleID, reason string, at time.Time) error
	Complete(ctx context.Context, completion Completion, at time.Time) (Job, error)
	Retry(ctx context.Context, jobID, workerID, lastError string, availableAt time.Time, deadLetter bool, at time.Time) error
	RecoverStaleClaims(ctx context.Context, claimedBefore, now time.Time) (int64, error)
	Get(ctx context.Context, projectID, jobID string) (Job, error)
	List(ctx context.Context, projectID, runID string, limit int) ([]Job, error)
	RetryManually(ctx context.Context, projectID, jobID string, now time.Time) (Job, error)
}

// JobService exposes durable queue operations to HTTP handlers without exposing
// worker claim/transition internals.
type JobService struct{ repo JobRepository }

func NewJobService(repo JobRepository) *JobService { return &JobService{repo: repo} }
func (s *JobService) Enqueue(ctx context.Context, projectID, runID, traceID string) (Job, error) {
	return s.repo.Enqueue(ctx, projectID, runID, traceID, defaultJobEvaluatorVersion)
}
func (s *JobService) Get(ctx context.Context, projectID, jobID string) (Job, error) {
	return s.repo.Get(ctx, projectID, jobID)
}
func (s *JobService) List(ctx context.Context, projectID, runID string, limit int) ([]Job, error) {
	return s.repo.List(ctx, projectID, runID, limit)
}
func (s *JobService) Retry(ctx context.Context, projectID, jobID string) (Job, error) {
	return s.repo.RetryManually(ctx, projectID, jobID, time.Now().UTC())
}
