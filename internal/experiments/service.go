package experiments

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// ---- Domain types ----

// Experiment represents an evaluation run against a dataset.
type Experiment struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"project_id"`
	DatasetID       string    `json:"dataset_id"`
	EvaluatorID     string    `json:"evaluator_id,omitempty"`
	EvaluatorConfig string    `json:"evaluator_config,omitempty"`
	TargetName      string    `json:"target_name"`
	TargetVersion   string    `json:"target_version,omitempty"`
	Status          string    `json:"status"`
	TotalCases      int       `json:"total_cases"`
	PassedCases     int       `json:"passed_cases"`
	FailedCases     int       `json:"failed_cases"`
	QualityPassCases   int       `json:"quality_pass_cases"`
	QualityFailedCases int       `json:"quality_failed_cases"`
	AggregateScore  float64   `json:"aggregate_score,omitempty"`
	PassThreshold   float64   `json:"pass_threshold"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// ExperimentDetail provides full experiment information including
// per-status job counts derived from eval_jobs.
type ExperimentDetail struct {
	Experiment
	StatusCounts map[string]int `json:"status_counts"`
}

// EvalJob represents a single evaluation job within an experiment.
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

// EvalCaseResult represents the result of evaluating a single case.
type EvalCaseResult struct {
	ID            string    `json:"id"`
	EvalJobID     string    `json:"eval_job_id"`
	RunID         string    `json:"run_id,omitempty"`
	DatasetCaseID string    `json:"dataset_case_id"`
	Score         float64   `json:"score"`
	Passed        bool      `json:"passed"`
	Reason        string    `json:"reason"`
	Details       string    `json:"details,omitempty"`
	ActualOutput  string    `json:"actual_output,omitempty"`
	LatencyMs     int64     `json:"latency_ms,omitempty"`
	Cost          float64   `json:"cost,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// ---- Querier interface ----

// Querier defines the database operations the experiments service needs.
type Querier interface {
	CreateExperimentWithJobs(ctx context.Context, projectID, datasetID, evaluatorID, targetName, targetVersion string, caseIDs []string) (Experiment, error)
	GetExperiment(ctx context.Context, experimentID string) (Experiment, error)
	CreateEvalJob(ctx context.Context, experimentID, datasetCaseID, status string) (EvalJob, error)
	ListEvalJobs(ctx context.Context, experimentID string) ([]EvalJob, error)
	ListExperiments(ctx context.Context, projectID string) ([]Experiment, error)
	ListEvalResults(ctx context.Context, experimentID string) ([]EvalCaseResult, error)
	GetJobStatusCounts(ctx context.Context, experimentID string) (map[string]int, error)
	UpdateExperimentCounts(ctx context.Context, experimentID string, status string, passedCases, failedCases int) error
	UpdateExperimentQualityCounts(ctx context.Context, experimentID string, qualityPassCases, qualityFailedCases int, aggregateScore float64) error
}

// CaseLister is an interface for listing dataset cases.
type CaseLister interface {
	ListCases(ctx context.Context, datasetID string) ([]interface {
		GetID() string
	}, error)
}

// ---- Service ----

// Service provides experiment operations.
type Service struct {
	q Querier
}

// NewService creates a new experiments Service.
func NewService(q Querier) *Service {
	return &Service{q: q}
}

// CreateExperiment creates an experiment and generates eval jobs for each
// case in the dataset. Returns an error if the dataset has no cases.
func (s *Service) CreateExperiment(ctx context.Context, projectID, datasetID, evaluatorID, targetName, targetVersion string, caseIDs []string) (Experiment, error) {
	if projectID == "" {
		return Experiment{}, errors.New("projectID is required")
	}
	if datasetID == "" {
		return Experiment{}, errors.New("datasetID is required")
	}
	if targetName == "" {
		return Experiment{}, errors.New("targetName is required")
	}
	if len(caseIDs) == 0 {
		return Experiment{}, errors.New("dataset has no cases")
	}

	exp, err := s.q.CreateExperimentWithJobs(ctx, projectID, datasetID, evaluatorID, targetName, targetVersion, caseIDs)
	if err != nil {
		return Experiment{}, fmt.Errorf("create experiment: %w", err)
	}

	return exp, nil
}

// GetExperiment returns the experiment detail with job status counts.
func (s *Service) GetExperiment(ctx context.Context, experimentID string) (ExperimentDetail, error) {
	if experimentID == "" {
		return ExperimentDetail{}, errors.New("experimentID is required")
	}

	exp, err := s.q.GetExperiment(ctx, experimentID)
	if err != nil {
		return ExperimentDetail{}, fmt.Errorf("get experiment: %w", err)
	}

	counts, err := s.q.GetJobStatusCounts(ctx, experimentID)
	if err != nil {
		return ExperimentDetail{}, fmt.Errorf("get job status counts: %w", err)
	}

	// Ensure all expected statuses appear in the map
	for _, s := range []string{"queued", "running", "succeeded", "failed"} {
		if _, ok := counts[s]; !ok {
			counts[s] = 0
		}
	}

	return ExperimentDetail{
		Experiment:   exp,
		StatusCounts: counts,
	}, nil
}

// GetExperimentBasic returns just the experiment without status counts.
func (s *Service) GetExperimentBasic(ctx context.Context, experimentID string) (Experiment, error) {
	if experimentID == "" {
		return Experiment{}, errors.New("experimentID is required")
	}
	return s.q.GetExperiment(ctx, experimentID)
}

// ListExperiments returns all experiments for a project.
func (s *Service) ListExperiments(ctx context.Context, projectID string) ([]Experiment, error) {
	if projectID == "" {
		return nil, errors.New("projectID is required")
	}
	return s.q.ListExperiments(ctx, projectID)
}

// ListEvalResults returns all eval case results for an experiment.
func (s *Service) ListEvalResults(ctx context.Context, experimentID string) ([]EvalCaseResult, error) {
	if experimentID == "" {
		return nil, errors.New("experimentID is required")
	}
	return s.q.ListEvalResults(ctx, experimentID)
}
