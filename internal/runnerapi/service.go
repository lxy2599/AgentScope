package runnerapi

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// DefaultLeaseDuration is the default lease duration for claimed jobs.
const DefaultLeaseDuration = 2 * time.Minute

// ---- Domain types ----

// EvalJob is the runner's view of an evaluation job.
type EvalJob struct {
	ID             string     `json:"id"`
	ExperimentID   string     `json:"experiment_id"`
	DatasetCaseID  string     `json:"dataset_case_id"`
	Status         string     `json:"status"`
	RunnerName     string     `json:"runner_name,omitempty"`
	LeaseExpiresAt *time.Time `json:"lease_expires_at,omitempty"`
	Attempts       int        `json:"attempts"`
	Error          string     `json:"error,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// ClaimResponse is returned to the runner after successfully claiming a job.
type ClaimResponse struct {
	Job           EvalJob `json:"job"`
	Status        string  `json:"status"`
	LeaseDuration string  `json:"lease_duration"`
}

// SubmitResultRequest carries the evaluation result from the runner.
type SubmitResultRequest struct {
	RunID        string  `json:"run_id"`
	Score        float64 `json:"score"`
	Passed       bool    `json:"passed"`
	Reason       string  `json:"reason"`
	Details      []byte  `json:"details,omitempty"`
	ActualOutput string  `json:"actual_output"`
	LatencyMs    int64   `json:"latency_ms"`
	Cost         float64 `json:"cost"`
}

// ---- Querier interface ----

// Querier defines the database operations the runner service needs.
type Querier interface {
	ClaimNextJob(ctx context.Context, projectID, runnerName string, leaseExpiresAt time.Time) (EvalJob, error)
	GetJobByID(ctx context.Context, jobID string) (EvalJob, error)
	HeartbeatJobByProject(ctx context.Context, jobID, runnerName string, leaseExpiresAt time.Time, projectID string) error
	UpdateJobStatusByProject(ctx context.Context, jobID, status, errorMsg, projectID, runnerName string) error
	SubmitResultByProject(ctx context.Context, projectID, runnerName, jobID string, req SubmitResultRequest) error
	UpdateExperimentStatus(ctx context.Context, experimentID, status string, passedCases, failedCases int) error
	GetJobStatusCounts(ctx context.Context, experimentID string) (map[string]int, error)
}

// ---- Service ----

// Service provides runner job operations.
type Service struct {
	q Querier
}

// NewService creates a new runner Service.
func NewService(q Querier) *Service {
	return &Service{q: q}
}

// Claim attempts to claim the next available job for the given project.
func (s *Service) Claim(ctx context.Context, projectID, runnerName string) (ClaimResponse, error) {
	if projectID == "" {
		return ClaimResponse{}, errors.New("projectID is required")
	}
	if runnerName == "" {
		return ClaimResponse{}, errors.New("runnerName is required")
	}

	leaseExpiresAt := time.Now().Add(DefaultLeaseDuration)
	job, err := s.q.ClaimNextJob(ctx, projectID, runnerName, leaseExpiresAt)
	if err != nil {
		return ClaimResponse{}, fmt.Errorf("claim job: %w", err)
	}

	return ClaimResponse{
		Job:           job,
		Status:        job.Status,
		LeaseDuration: DefaultLeaseDuration.String(),
	}, nil
}

// Heartbeat renews the lease on a running job.
func (s *Service) Heartbeat(ctx context.Context, jobID, projectID, runnerName string) error {
	if jobID == "" {
		return errors.New("jobID is required")
	}
	if projectID == "" {
		return errors.New("projectID is required")
	}
	if runnerName == "" {
		return errors.New("runnerName is required")
	}

	leaseExpiresAt := time.Now().Add(DefaultLeaseDuration)
	if err := s.q.HeartbeatJobByProject(ctx, jobID, runnerName, leaseExpiresAt, projectID); err != nil {
		return fmt.Errorf("heartbeat: %w", err)
	}
	return nil
}

// SubmitResult submits a result for a running job and updates the
// experiment status if all jobs are complete.
func (s *Service) SubmitResult(ctx context.Context, jobID, projectID, runnerName string, req SubmitResultRequest) error {
	if jobID == "" {
		return errors.New("jobID is required")
	}
	if projectID == "" {
		return errors.New("projectID is required")
	}
	if runnerName == "" {
		return errors.New("runnerName is required")
	}

	if err := s.q.SubmitResultByProject(ctx, projectID, runnerName, jobID, req); err != nil {
		return fmt.Errorf("submit result: %w", err)
	}
	return nil
}

// FailJob marks a running job as failed and updates the experiment.
func (s *Service) FailJob(ctx context.Context, jobID, projectID, runnerName, errorMsg string) error {
	if jobID == "" {
		return errors.New("jobID is required")
	}
	if projectID == "" {
		return errors.New("projectID is required")
	}
	if runnerName == "" {
		return errors.New("runnerName is required")
	}
	if errorMsg == "" {
		return errors.New("error message is required")
	}

	// Verify the job belongs to this runner
	job, err := s.q.GetJobByID(ctx, jobID)
	if err != nil {
		return fmt.Errorf("get job: %w", err)
	}
	if job.RunnerName != runnerName {
		return errors.New("job not claimed by this runner")
	}
	if job.Status != "running" {
		return errors.New("job is not in running state")
	}

	if err := s.q.UpdateJobStatusByProject(ctx, jobID, "failed", errorMsg, projectID, runnerName); err != nil {
		return fmt.Errorf("update job status: %w", err)
	}

	// Update experiment counts
	if err := s.updateExperimentAfterJob(ctx, job.ExperimentID); err != nil {
		return fmt.Errorf("update experiment: %w", err)
	}

	return nil
}

// updateExperimentAfterJob recomputes experiment counts from eval_jobs
// and marks the experiment as completed if all jobs are finished.
func (s *Service) updateExperimentAfterJob(ctx context.Context, experimentID string) error {
	counts, err := s.q.GetJobStatusCounts(ctx, experimentID)
	if err != nil {
		return fmt.Errorf("get job status counts: %w", err)
	}

	total := 0
	succeeded := counts["succeeded"]
	failed := counts["failed"]
	for _, c := range counts {
		total += c
	}

	// Determine new experiment status
	newStatus := "running"
	queued := counts["queued"]
	running := counts["running"]
	if queued == 0 && running == 0 {
		newStatus = "completed"
	}

	if err := s.q.UpdateExperimentStatus(ctx, experimentID, newStatus, succeeded, failed); err != nil {
		return fmt.Errorf("update experiment status: %w", err)
	}

	return nil
}
