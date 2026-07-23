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
	// Harness metadata (optional, for experiment identity/reproducibility)
	ModelProvider    string `json:"model_provider,omitempty"`
	ModelName        string `json:"model_name,omitempty"`
	ModelVersion     string `json:"model_version,omitempty"`
	PromptVersion    string `json:"prompt_version,omitempty"`
	HarnessVersion   string `json:"harness_version,omitempty"`
	DatasetVersion   string `json:"dataset_version,omitempty"`
	ToolsetVersion   string `json:"toolset_version,omitempty"`
	SkillsetVersion  string `json:"skillset_version,omitempty"`
	RAGConfigVersion string `json:"rag_config_version,omitempty"`
	FeatureFlags     string `json:"feature_flags,omitempty"`
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
	UpdateExperimentHarnessMetadata(ctx context.Context, experimentID, modelProvider, modelName, modelVersion, promptVersion, harnessVersion, datasetVersion string, featureFlags []byte) error
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

// UpdateExperimentHarnessMetadata updates harness metadata for an experiment.
func (s *Service) UpdateExperimentHarnessMetadata(ctx context.Context, experimentID, modelProvider, modelName, modelVersion, promptVersion, harnessVersion, datasetVersion string, featureFlags []byte) error {
	if experimentID == "" {
		return errors.New("experimentID is required")
	}
	return s.q.UpdateExperimentHarnessMetadata(ctx, experimentID, modelProvider, modelName, modelVersion, promptVersion, harnessVersion, datasetVersion, featureFlags)
}

// ---- Comparison types ----

// ComparisonType enumerates supported comparison modes.
type ComparisonType string

const (
	ComparisonModelSwap ComparisonType = "model_swap"
	ComparisonAblation  ComparisonType = "ablation"
	ComparisonGeneral   ComparisonType = "general"
)

// RegressionGate defines configurable thresholds for regression detection.
type RegressionGate struct {
	MinPassRateDelta     float64 `json:"min_pass_rate_delta,omitempty"`
	MinScoreDelta        float64 `json:"min_score_delta,omitempty"`
	MaxCostIncreaseRatio float64 `json:"max_cost_increase_ratio,omitempty"`
	MaxP95LatencyIncPct  float64 `json:"max_p95_latency_inc_pct,omitempty"`
	SafetyVetoEnabled    bool    `json:"safety_veto_enabled,omitempty"`
}

// ComparisonSummary holds aggregate results for a comparison.
type ComparisonSummary struct {
	TotalCases          int       `json:"total_cases"`
	PairedCases         int       `json:"paired_cases"`
	BaselinePassRate    float64   `json:"baseline_pass_rate"`
	CandidatePassRate   float64   `json:"candidate_pass_rate"`
	BaselineAvgScore    float64   `json:"baseline_avg_score"`
	CandidateAvgScore   float64   `json:"candidate_avg_score"`
	AvgScoreDelta       float64   `json:"avg_score_delta"`
	ImprovedCases       int       `json:"improved_cases"`
	RegressedCases      int       `json:"regressed_cases"`
	UnchangedCases      int       `json:"unchanged_cases"`
	IncomparableCases   int       `json:"incomparable_cases"`
	BaselineP95LatencyMs int64    `json:"baseline_p95_latency_ms"`
	CandidateP95LatencyMs int64   `json:"candidate_p95_latency_ms"`
	LatencyDeltaPct     float64   `json:"latency_delta_pct"`
	BaselineAvgCost     float64   `json:"baseline_avg_cost"`
	CandidateAvgCost    float64   `json:"candidate_avg_cost"`
	CostDeltaPct        float64   `json:"cost_delta_pct"`
	BaselineErrorRate   float64   `json:"baseline_error_rate"`
	CandidateErrorRate  float64   `json:"candidate_error_rate"`
	SafetyRegressed     bool      `json:"safety_regressed"`
	RegressionTriggered bool      `json:"regression_triggered"`
	RegressionReasons   []string  `json:"regression_reasons,omitempty"`
}

// Comparison represents a baseline vs candidate experiment comparison.
type Comparison struct {
	ID                    string          `json:"id"`
	ProjectID             string          `json:"project_id"`
	BaselineExperimentID  string          `json:"baseline_experiment_id"`
	CandidateExperimentID string          `json:"candidate_experiment_id"`
	ComparisonType        ComparisonType  `json:"comparison_type"`
	CreatedBy             string          `json:"created_by"`
	Status                string          `json:"status"`
	Summary               string          `json:"summary"`
	RegressionGates       string          `json:"regression_gates"`
	CreatedAt             time.Time       `json:"created_at"`
	UpdatedAt             time.Time       `json:"updated_at"`
}

// ComparisonCaseResult stores per-case comparison data.
type ComparisonCaseResult struct {
	ID                string  `json:"id"`
	ComparisonID      string  `json:"comparison_id"`
	DatasetCaseID     string  `json:"dataset_case_id"`
	BaselineRunID     string  `json:"baseline_run_id,omitempty"`
	CandidateRunID    string  `json:"candidate_run_id,omitempty"`
	BaselineScore     float64 `json:"baseline_score"`
	CandidateScore    float64 `json:"candidate_score"`
	BaselinePassed    bool    `json:"baseline_passed"`
	CandidatePassed   bool    `json:"candidate_passed"`
	ScoreDiff         float64 `json:"score_diff"`
	Status            string  `json:"status"`
	BaselineLatencyMs int64   `json:"baseline_latency_ms"`
	CandidateLatencyMs int64  `json:"candidate_latency_ms"`
	BaselineCost      float64 `json:"baseline_cost"`
	CandidateCost     float64 `json:"candidate_cost"`
	Details           string  `json:"details,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

// ComparisonDetail is the full comparison response including experiments and summary.
type ComparisonDetail struct {
	Comparison
	BaselineExperiment  Experiment          `json:"baseline_experiment"`
	CandidateExperiment Experiment          `json:"candidate_experiment"`
	ComparisonSummary   ComparisonSummary   `json:"comparison_summary"`
	CaseResults         []ComparisonCaseResult `json:"case_results"`
}

// CompareQuerier defines comparison-specific database operations.
type CompareQuerier interface {
	CreateExperimentComparison(ctx context.Context, comparison Comparison, caseResults []ComparisonCaseResult) (Comparison, error)
	GetExperimentComparison(ctx context.Context, comparisonID string) (ComparisonDetail, error)
	ListExperimentComparisons(ctx context.Context, projectID string) ([]Comparison, error)
}

// CompareService provides experiment comparison operations.
type CompareService struct {
	q        Querier
	compareQ CompareQuerier
	evalQ    *Service // reuses existing ListEvalResults
}

// NewCompareService creates a new CompareService.
func NewCompareService(q Querier, compareQ CompareQuerier, evalSvc *Service) *CompareService {
	return &CompareService{q: q, compareQ: compareQ, evalQ: evalSvc}
}
