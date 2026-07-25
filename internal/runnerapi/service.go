package runnerapi

import (
	"context"
	"encoding/json"
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
// EnrichedFields contains the case input, target info, and evaluator config
// needed for runner execution.
type ClaimResponse struct {
	Job           EvalJob         `json:"job"`
	Status        string          `json:"status"`
	LeaseDuration string          `json:"lease_duration"`
	CaseInput     string          `json:"case_input"`
	CaseExpected  *string         `json:"case_expected,omitempty"`
	CaseMetadata  json.RawMessage `json:"case_metadata,omitempty"`
	TargetName    string          `json:"target_name"`
	TargetVersion string          `json:"target_version"`
	EvalConfig    json.RawMessage `json:"eval_config,omitempty"`
}

// DatasetCaseData holds the case input/expected/metadata from the dataset case.
type DatasetCaseData struct {
	Input    string          `json:"input"`
	Expected *string         `json:"expected,omitempty"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// EvalScoreItem represents a single multi-dimensional evaluation score
// that can be submitted alongside the main result.
type EvalScoreItem struct {
	MetricName string  `json:"metric_name"`
	MetricKind string  `json:"metric_kind"`
	Score      float64 `json:"score"`
	Passed     bool    `json:"passed"`
	Reason     string  `json:"reason"`
	Details    []byte  `json:"details,omitempty"`
	TraceID    string  `json:"trace_id,omitempty"`
	SpanID     string  `json:"span_id,omitempty"`
}

// SubmitResultRequest carries the evaluation result from the runner.
type SubmitResultRequest struct {
	RunID        string          `json:"run_id"`
	Score        float64         `json:"score"`
	Passed       bool            `json:"passed"`
	Reason       string          `json:"reason"`
	Details      []byte          `json:"details,omitempty"`
	ActualOutput string          `json:"actual_output"`
	LatencyMs    int64           `json:"latency_ms"`
	Cost         float64         `json:"cost"`
	EvalScores   []EvalScoreItem `json:"eval_scores,omitempty"`
}

// ---- Querier interface ----

// Querier defines the database operations the runner service needs.
type Querier interface {
	ClaimNextJob(ctx context.Context, projectID, runnerName string, leaseExpiresAt time.Time) (EvalJob, error)
	GetJobByID(ctx context.Context, jobID string) (EvalJob, error)
	HeartbeatJobByProject(ctx context.Context, jobID, runnerName string, leaseExpiresAt time.Time, projectID string) error
	UpdateJobStatusByProject(ctx context.Context, jobID, status, errorMsg, projectID, runnerName string) error
	SubmitResultByProject(ctx context.Context, projectID, runnerName, jobID string, req SubmitResultRequest) error
	RefreshExperimentSummary(ctx context.Context, experimentID string) error
	// Enrichment queries for runner execution context
	GetExperimentTargetVersion(ctx context.Context, experimentID string) (targetName string, targetVersion string, evaluatorID string, err error)
	GetDatasetCaseData(ctx context.Context, caseID string) (DatasetCaseData, error)
	GetEvaluatorConfig(ctx context.Context, evaluatorID, projectID string) (config json.RawMessage, err error)
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

// Claim attempts to claim the next available job for the given project
// and enriches the response with case input, target info, and evaluator config.
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

	resp := ClaimResponse{
		Job:           job,
		Status:        job.Status,
		LeaseDuration: DefaultLeaseDuration.String(),
	}

	// Enrich with case data
	caseData, err := s.q.GetDatasetCaseData(ctx, job.DatasetCaseID)
	if err != nil {
		return ClaimResponse{}, fmt.Errorf("get dataset case data: %w", err)
	}
	resp.CaseInput = caseData.Input
	resp.CaseExpected = caseData.Expected
	resp.CaseMetadata = caseData.Metadata

	// Enrich with experiment target info
	targetName, targetVersion, evaluatorID, err := s.q.GetExperimentTargetVersion(ctx, job.ExperimentID)
	if err != nil {
		return ClaimResponse{}, fmt.Errorf("get experiment target version: %w", err)
	}
	resp.TargetName = targetName
	resp.TargetVersion = targetVersion

	// Enrich with evaluator config if configured
	if evaluatorID != "" {
		config, err := s.q.GetEvaluatorConfig(ctx, evaluatorID, projectID)
		if err != nil {
			return ClaimResponse{}, fmt.Errorf("get evaluator config: %w", err)
		}
		config, err = withEvaluatorID(config, evaluatorID)
		if err != nil {
			return ClaimResponse{}, fmt.Errorf("enrich evaluator config: %w", err)
		}
		resp.EvalConfig = config
	}

	return resp, nil
}

func withEvaluatorID(config json.RawMessage, evaluatorID string) (json.RawMessage, error) {
	values := make(map[string]json.RawMessage)
	if err := json.Unmarshal(config, &values); err != nil {
		return nil, fmt.Errorf("decode evaluator config: %w", err)
	}
	encodedID, err := json.Marshal(evaluatorID)
	if err != nil {
		return nil, fmt.Errorf("encode evaluator id: %w", err)
	}
	values["evaluator_id"] = encodedID
	enriched, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("encode evaluator config: %w", err)
	}
	return enriched, nil
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

	// Recompute execution completion and quality counts after the failed job.
	if err := s.q.RefreshExperimentSummary(ctx, job.ExperimentID); err != nil {
		return fmt.Errorf("update experiment: %w", err)
	}

	return nil
}
