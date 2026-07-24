package evals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"AgentScope/internal/evaluators"
)

type RubricTemplate struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type RubricTemplateVersion struct {
	ID          string     `json:"id"`
	TemplateID  string     `json:"template_id"`
	Version     int        `json:"version"`
	Status      string     `json:"status"`
	Rubric      string     `json:"rubric"`
	CreatedBy   string     `json:"created_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
}

type PersistedJudgeVote struct {
	ID              string    `json:"id"`
	ProjectID       string    `json:"project_id"`
	RunID           string    `json:"run_id"`
	MetricName      string    `json:"metric_name"`
	MetricKind      string    `json:"metric_kind"`
	EvaluatorID     string    `json:"evaluator_id,omitempty"`
	RubricVersionID string    `json:"rubric_version_id,omitempty"`
	Provider        string    `json:"provider"`
	Model           string    `json:"model"`
	PromptVersion   string    `json:"prompt_version"`
	Score           *float64  `json:"score,omitempty"`
	Passed          *bool     `json:"passed,omitempty"`
	Reason          string    `json:"reason"`
	RawResponseHash string    `json:"raw_response_hash"`
	LatencyMs       *int64    `json:"latency_ms,omitempty"`
	InputTokens     *int64    `json:"input_tokens,omitempty"`
	OutputTokens    *int64    `json:"output_tokens,omitempty"`
	Cost            *float64  `json:"cost,omitempty"`
	Error           string    `json:"error,omitempty"`
	EssentialFailed bool      `json:"essential_failed"`
	VetoTriggered   bool      `json:"veto_triggered"`
	VetoReason      string    `json:"veto_reason,omitempty"`
	Details         string    `json:"details"`
	CreatedAt       time.Time `json:"created_at"`
}

type JudgeVoteSubmission struct {
	Provider        string   `json:"provider"`
	Model           string   `json:"model"`
	PromptVersion   string   `json:"prompt_version"`
	Score           *float64 `json:"score,omitempty"`
	Passed          *bool    `json:"passed,omitempty"`
	Reason          string   `json:"reason,omitempty"`
	RawResponseHash string   `json:"raw_response_hash,omitempty"`
	LatencyMs       *int64   `json:"latency_ms,omitempty"`
	InputTokens     *int64   `json:"input_tokens,omitempty"`
	OutputTokens    *int64   `json:"output_tokens,omitempty"`
	Cost            *float64 `json:"cost,omitempty"`
	Error           string   `json:"error,omitempty"`
	EssentialFailed bool     `json:"essential_failed,omitempty"`
	VetoTriggered   bool     `json:"veto_triggered,omitempty"`
	VetoReason      string   `json:"veto_reason,omitempty"`
	Details         string   `json:"details,omitempty"`
}

type JudgeVoteBatch struct {
	ProjectID       string                `json:"project_id"`
	RunID           string                `json:"run_id"`
	MetricName      string                `json:"metric_name"`
	MetricKind      string                `json:"metric_kind"`
	EvaluatorID     string                `json:"evaluator_id,omitempty"`
	RubricVersionID string                `json:"rubric_version_id,omitempty"`
	Policy          AggregationPolicy     `json:"aggregation"`
	Votes           []JudgeVoteSubmission `json:"votes"`
}

type PersistedJudgeAggregation struct {
	ID              string           `json:"id"`
	ProjectID       string           `json:"project_id"`
	RunID           string           `json:"run_id"`
	MetricName      string           `json:"metric_name"`
	MetricKind      string           `json:"metric_kind"`
	RubricVersionID string           `json:"rubric_version_id,omitempty"`
	Result          JudgeAggregation `json:"result"`
	Details         string           `json:"details"`
	CreatedAt       time.Time        `json:"created_at"`
}

type JudgeEvaluation struct {
	Votes        []PersistedJudgeVote        `json:"votes"`
	Aggregations []PersistedJudgeAggregation `json:"aggregations"`
}

type CalibrationRun struct {
	ID              string            `json:"id"`
	ProjectID       string            `json:"project_id"`
	RubricVersionID string            `json:"rubric_version_id"`
	Status          string            `json:"status"`
	SampleRunIDs    []string          `json:"sample_run_ids"`
	Report          CalibrationReport `json:"report"`
	CreatedBy       string            `json:"created_by,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	CompletedAt     *time.Time        `json:"completed_at,omitempty"`
}

type GovernanceQuerier interface {
	CreateRubricTemplate(context.Context, string, string, string) (RubricTemplate, error)
	CreateRubricVersion(context.Context, string, string, string, string) (RubricTemplateVersion, error)
	PublishRubricVersion(context.Context, string, string) (RubricTemplateVersion, error)
	ListRubricTemplates(context.Context, string) ([]RubricTemplate, error)
	ListRubricVersions(context.Context, string, string) ([]RubricTemplateVersion, error)
	PersistJudgeAggregation(context.Context, JudgeVoteBatch, JudgeAggregation) (PersistedJudgeAggregation, error)
	ListJudgeEvaluation(context.Context, string, string) (JudgeEvaluation, error)
	BeginCalibration(context.Context, string, string, []string, string) (CalibrationRun, error)
	LoadCalibrationSamples(context.Context, string, string, []string) ([]CalibrationSample, error)
	CompleteCalibration(context.Context, CalibrationRun, CalibrationReport) (CalibrationRun, error)
	FailCalibration(context.Context, CalibrationRun, string) error
	ListCalibrations(context.Context, string) ([]CalibrationRun, error)
}

type GovernanceService struct {
	q GovernanceQuerier
}

func NewGovernanceService(q GovernanceQuerier) *GovernanceService {
	return &GovernanceService{q: q}
}

func (s *GovernanceService) CreateRubricTemplate(ctx context.Context, projectID, name, description string) (RubricTemplate, error) {
	if projectID == "" || name == "" {
		return RubricTemplate{}, errors.New("projectID and name are required")
	}
	return s.q.CreateRubricTemplate(ctx, projectID, name, description)
}

func (s *GovernanceService) CreateRubricVersion(ctx context.Context, projectID, templateID, rubricJSON, createdBy string) (RubricTemplateVersion, error) {
	var rubric evaluators.Rubric
	if err := json.Unmarshal([]byte(rubricJSON), &rubric); err != nil {
		return RubricTemplateVersion{}, fmt.Errorf("invalid rubric JSON: %w", err)
	}
	if err := evaluators.ValidateRubric(rubric); err != nil {
		return RubricTemplateVersion{}, err
	}
	return s.q.CreateRubricVersion(ctx, projectID, templateID, rubricJSON, createdBy)
}

func (s *GovernanceService) PublishRubricVersion(ctx context.Context, projectID, versionID string) (RubricTemplateVersion, error) {
	if projectID == "" || versionID == "" {
		return RubricTemplateVersion{}, errors.New("projectID and versionID are required")
	}
	return s.q.PublishRubricVersion(ctx, projectID, versionID)
}

func (s *GovernanceService) ListRubricTemplates(ctx context.Context, projectID string) ([]RubricTemplate, error) {
	return s.q.ListRubricTemplates(ctx, projectID)
}

func (s *GovernanceService) ListRubricVersions(ctx context.Context, projectID, templateID string) ([]RubricTemplateVersion, error) {
	return s.q.ListRubricVersions(ctx, projectID, templateID)
}

func (s *GovernanceService) SubmitJudgeVotes(ctx context.Context, batch JudgeVoteBatch) (PersistedJudgeAggregation, error) {
	if batch.ProjectID == "" || batch.RunID == "" || batch.MetricName == "" || batch.MetricKind == "" {
		return PersistedJudgeAggregation{}, errors.New("projectID, runID, metricName and metricKind are required")
	}
	if !SupportedMetricKinds[batch.MetricKind] {
		return PersistedJudgeAggregation{}, fmt.Errorf("unsupported metric kind %q", batch.MetricKind)
	}
	votes := make([]JudgeVote, 0, len(batch.Votes))
	for _, vote := range batch.Votes {
		if vote.Error == "" && (vote.Score == nil || vote.Passed == nil) {
			return PersistedJudgeAggregation{}, errors.New("successful judge vote requires score and passed")
		}
		if vote.Error != "" && (vote.Score != nil || vote.Passed != nil) {
			return PersistedJudgeAggregation{}, errors.New("error judge vote cannot include score or passed")
		}
		votes = append(votes, JudgeVote{
			Score:           vote.Score,
			Passed:          vote.Passed,
			Error:           vote.Error,
			EssentialFailed: vote.EssentialFailed,
			VetoTriggered:   vote.VetoTriggered,
			VetoReason:      vote.VetoReason,
		})
	}
	aggregation, err := AggregateJudgeVotes(votes, batch.Policy)
	if err != nil {
		return PersistedJudgeAggregation{}, err
	}
	return s.q.PersistJudgeAggregation(ctx, batch, aggregation)
}

func (s *GovernanceService) ListJudgeEvaluation(ctx context.Context, projectID, runID string) (JudgeEvaluation, error) {
	return s.q.ListJudgeEvaluation(ctx, projectID, runID)
}

func (s *GovernanceService) RunCalibration(
	ctx context.Context,
	projectID, rubricVersionID string,
	sampleRunIDs []string,
	createdBy string,
	passThreshold float64,
) (CalibrationRun, error) {
	if projectID == "" || rubricVersionID == "" || createdBy == "" || len(sampleRunIDs) == 0 {
		return CalibrationRun{}, errors.New("projectID, rubricVersionID, sampleRunIDs and createdBy are required")
	}
	run, err := s.q.BeginCalibration(ctx, projectID, rubricVersionID, sampleRunIDs, createdBy)
	if err != nil {
		return CalibrationRun{}, err
	}
	samples, err := s.q.LoadCalibrationSamples(ctx, projectID, rubricVersionID, sampleRunIDs)
	if err != nil {
		_ = s.q.FailCalibration(ctx, run, err.Error())
		return CalibrationRun{}, err
	}
	report, err := BuildCalibrationReport(samples, passThreshold)
	if err != nil {
		_ = s.q.FailCalibration(ctx, run, err.Error())
		return CalibrationRun{}, err
	}
	return s.q.CompleteCalibration(ctx, run, report)
}

func (s *GovernanceService) ListCalibrations(ctx context.Context, projectID string) ([]CalibrationRun, error) {
	if projectID == "" {
		return nil, errors.New("projectID is required")
	}
	return s.q.ListCalibrations(ctx, projectID)
}
