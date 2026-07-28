package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"time"

	"AgentScope/internal/anomalies"
	"AgentScope/internal/auth"
	"AgentScope/internal/evals"
	"AgentScope/internal/experiments"
	"AgentScope/internal/ingest"
	"AgentScope/internal/projects"
	"AgentScope/internal/quotas"
	"AgentScope/internal/realtime"
	"AgentScope/internal/sampling"
	"AgentScope/internal/snapshots"
	"github.com/go-chi/chi/v5"
)

// AuthService defines the auth operations needed by handlers.
type AuthService interface {
	Register(ctx context.Context, email, password, displayName string) (string, error)
	Login(ctx context.Context, email, password string) (string, string, error)
}

// Project is the project type used by ProjectsService.
type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

// ProjectToken is returned by CreateToken.
type ProjectToken struct {
	ID                 string   `json:"id"`
	ProjectID          string   `json:"project_id"`
	TokenPrefix        string   `json:"token_prefix"`
	Scopes             []string `json:"scopes"`
	Description        string   `json:"description,omitempty"`
	CreatedAt          string   `json:"created_at,omitempty"`
	ExpiresAt          string   `json:"expires_at,omitempty"`
	LastUsedAt         string   `json:"last_used_at,omitempty"`
	CreatedBy          string   `json:"created_by,omitempty"`
	RevokedAt          string   `json:"revoked_at,omitempty"`
	RevokedBy          string   `json:"revoked_by,omitempty"`
	RevocationReason   string   `json:"revocation_reason,omitempty"`
	RotatesTokenID     string   `json:"rotates_token_id,omitempty"`
	Environment        string   `json:"environment"`
	TokenType          string   `json:"token_type"`
	LastUsedEndpoint   string   `json:"last_used_endpoint,omitempty"`
	LastUsedAgentID    string   `json:"last_used_agent_id,omitempty"`
	LastUsedInstanceID string   `json:"last_used_instance_id,omitempty"`
	Status             string   `json:"status"`
}
type TokenCreateOptions struct {
	Description string
	Scopes      []string
	ExpiresAt   string
	Environment string
	TokenType   string
}
type TokenAuditEvent struct {
	ID          string         `json:"id"`
	ProjectID   string         `json:"project_id"`
	TokenID     string         `json:"token_id"`
	ActorUserID string         `json:"actor_user_id,omitempty"`
	EventType   string         `json:"event_type"`
	Metadata    map[string]any `json:"metadata"`
	CreatedAt   string         `json:"created_at"`
}
type TokenPolicy struct {
	ProjectID            string `json:"project_id"`
	AllowNonExpiring     bool   `json:"allow_non_expiring"`
	MaxProductionTTLDays *int   `json:"max_production_ttl_days,omitempty"`
	MaxCITTLDays         *int   `json:"max_ci_ttl_days,omitempty"`
	ExpiryWarningDays    []int  `json:"expiry_warning_days"`
	UnusedWarningDays    int    `json:"unused_warning_days"`
	UpdatedBy            string `json:"updated_by,omitempty"`
	UpdatedAt            string `json:"updated_at,omitempty"`
}
type TokenRiskSummary struct {
	ExpiringSoon            int `json:"expiring_soon"`
	Unused                  int `json:"unused"`
	BroadProduction         int `json:"broad_production"`
	UnclassifiedEnvironment int `json:"unclassified_environment"`
}

// ProjectsService defines project and project-token governance operations needed by handlers.
type ProjectsService interface {
	CreateProject(ctx context.Context, userID, name, description string) (Project, error)
	ListProjects(ctx context.Context, userID string) ([]Project, error)
	CreateToken(ctx context.Context, projectID, userID string, options TokenCreateOptions) (plainToken string, token ProjectToken, err error)
	ListTokens(ctx context.Context, projectID, userID string) ([]ProjectToken, error)
	RevokeToken(ctx context.Context, projectID, tokenID, userID, reason string) (ProjectToken, error)
	RotateToken(ctx context.Context, projectID, tokenID, userID string, options TokenCreateOptions) (plainToken string, token ProjectToken, err error)
	ListTokenAudit(ctx context.Context, projectID, tokenID, userID string) ([]TokenAuditEvent, error)
	GetTokenPolicy(ctx context.Context, projectID, userID string) (TokenPolicy, error)
	UpdateTokenPolicy(ctx context.Context, projectID, userID string, policy TokenPolicy) (TokenPolicy, error)
	GetTokenRiskSummary(ctx context.Context, projectID, userID string) (TokenRiskSummary, error)
}

// Dataset is the dataset type used by DatasetsService.
type Dataset struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	CreatedAt   string `json:"created_at"`
}

// DatasetCase is the dataset case type used by DatasetsService.
type DatasetCase struct {
	ID        string          `json:"id"`
	DatasetID string          `json:"dataset_id"`
	Input     json.RawMessage `json:"input"`
	Expected  json.RawMessage `json:"expected,omitempty"`
	Metadata  json.RawMessage `json:"metadata"`
	SortOrder int             `json:"sort_order"`
	CreatedAt string          `json:"created_at"`
}

// DatasetsService defines the dataset operations needed by handlers.
type DatasetsService interface {
	CreateDataset(ctx context.Context, projectID, name, description string) (Dataset, error)
	AddCase(ctx context.Context, datasetID string, input, expected, metadata json.RawMessage) (DatasetCase, error)
	ListCases(ctx context.Context, datasetID string) ([]DatasetCase, error)
	ListDatasets(ctx context.Context, projectID string) ([]Dataset, error)
	GetDataset(ctx context.Context, datasetID string) (Dataset, error)
}

// Experiment is the experiment type used by ExperimentsService.
type Experiment struct {
	ID             string  `json:"id"`
	ProjectID      string  `json:"project_id"`
	DatasetID      string  `json:"dataset_id"`
	EvaluatorID    string  `json:"evaluator_id,omitempty"`
	TargetName     string  `json:"target_name"`
	TargetVersion  string  `json:"target_version,omitempty"`
	Status         string  `json:"status"`
	TotalCases     int     `json:"total_cases"`
	PassedCases    int     `json:"passed_cases"`
	FailedCases    int     `json:"failed_cases"`
	AggregateScore float64 `json:"aggregate_score,omitempty"`
	CreatedAt      string  `json:"created_at"`
	// Harness metadata (optional)
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

// ExperimentDetail provides full experiment information.
type ExperimentDetail struct {
	Experiment
	StatusCounts map[string]int `json:"status_counts"`
}

// EvalCaseResult represents the result of evaluating a single case.
type EvalCaseResult struct {
	ID            string  `json:"id"`
	EvalJobID     string  `json:"eval_job_id"`
	RunID         string  `json:"run_id,omitempty"`
	DatasetCaseID string  `json:"dataset_case_id"`
	CaseInput     string  `json:"case_input,omitempty"`
	CaseExpected  string  `json:"case_expected,omitempty"`
	Score         float64 `json:"score"`
	Passed        bool    `json:"passed"`
	Reason        string  `json:"reason"`
	ActualOutput  string  `json:"actual_output,omitempty"`
	LatencyMs     int64   `json:"latency_ms,omitempty"`
	Cost          float64 `json:"cost,omitempty"`
	CreatedAt     string  `json:"created_at"`
}

// ExperimentsService defines the experiment operations needed by handlers.
type ExperimentsService interface {
	CreateExperiment(ctx context.Context, projectID, datasetID, evaluatorID, targetName, targetVersion string, caseIDs []string) (Experiment, error)
	CreateExperimentWithHarness(ctx context.Context, projectID, datasetID, evaluatorID string, params experiments.CreateExperimentHarnessParams) (Experiment, error)
	GetExperiment(ctx context.Context, experimentID string) (ExperimentDetail, error)
	ListExperiments(ctx context.Context, projectID string) ([]Experiment, error)
	ListExperimentResults(ctx context.Context, experimentID string) ([]EvalCaseResult, error)
	UpdateExperimentHarnessMetadata(ctx context.Context, experimentID, modelProvider, modelName, modelVersion, promptVersion, harnessVersion, datasetVersion string, featureFlags []byte) error
	UpdateFullExperimentHarnessMetadata(ctx context.Context, experimentID, modelProvider, modelName, modelVersion, promptVersion, harnessVersion, datasetVersion, toolsetVersion, skillsetVersion, ragConfigVersion string, featureFlags []byte) error
}

// ---- Experiment Comparison types (Phase 3.6) ----

// ComparisonCaseResult represents a per-case comparison result.
type ComparisonCaseResult struct {
	ID                 string  `json:"id"`
	ComparisonID       string  `json:"comparison_id"`
	DatasetCaseID      string  `json:"dataset_case_id"`
	BaselineRunID      string  `json:"baseline_run_id,omitempty"`
	CandidateRunID     string  `json:"candidate_run_id,omitempty"`
	BaselineScore      float64 `json:"baseline_score"`
	CandidateScore     float64 `json:"candidate_score"`
	BaselinePassed     bool    `json:"baseline_passed"`
	CandidatePassed    bool    `json:"candidate_passed"`
	ScoreDiff          float64 `json:"score_diff"`
	Status             string  `json:"status"`
	BaselineLatencyMs  int64   `json:"baseline_latency_ms"`
	CandidateLatencyMs int64   `json:"candidate_latency_ms"`
	BaselineCost       float64 `json:"baseline_cost"`
	CandidateCost      float64 `json:"candidate_cost"`
	CreatedAt          string  `json:"created_at"`
}

// ComparisonSummary holds aggregate comparison results.
type ComparisonSummary struct {
	TotalCases              int                                             `json:"total_cases"`
	PairedCases             int                                             `json:"paired_cases"`
	BaselinePassRate        float64                                         `json:"baseline_pass_rate"`
	CandidatePassRate       float64                                         `json:"candidate_pass_rate"`
	BaselineAvgScore        float64                                         `json:"baseline_avg_score"`
	CandidateAvgScore       float64                                         `json:"candidate_avg_score"`
	AvgScoreDelta           float64                                         `json:"avg_score_delta"`
	ImprovedCases           int                                             `json:"improved_cases"`
	RegressedCases          int                                             `json:"regressed_cases"`
	UnchangedCases          int                                             `json:"unchanged_cases"`
	IncomparableCases       int                                             `json:"incomparable_cases"`
	BaselineP95LatencyMs    int64                                           `json:"baseline_p95_latency_ms"`
	CandidateP95LatencyMs   int64                                           `json:"candidate_p95_latency_ms"`
	LatencyDeltaPct         float64                                         `json:"latency_delta_pct"`
	BaselineAvgCost         float64                                         `json:"baseline_avg_cost"`
	CandidateAvgCost        float64                                         `json:"candidate_avg_cost"`
	CostDeltaPct            float64                                         `json:"cost_delta_pct"`
	BaselineErrorRate       float64                                         `json:"baseline_error_rate"`
	CandidateErrorRate      float64                                         `json:"candidate_error_rate"`
	SafetyRegressed         bool                                            `json:"safety_regressed"`
	RegressionTriggered     bool                                            `json:"regression_triggered"`
	RegressionReasons       []string                                        `json:"regression_reasons,omitempty"`
	MultiDimMetrics         map[string]experiments.MultiDimMetricComparison `json:"multi_dim_metrics,omitempty"`
	StatisticalSignificance experiments.StatisticalSignificance             `json:"statistical_significance"`
}

// ComparisonDetail is the full comparison response.
type ComparisonDetail struct {
	Comparison
	BaselineExperiment  Experiment             `json:"baseline_experiment"`
	CandidateExperiment Experiment             `json:"candidate_experiment"`
	ComparisonSummary   ComparisonSummary      `json:"comparison_summary"`
	CaseResults         []ComparisonCaseResult `json:"case_results"`
}

// Comparison is the top-level comparison record.
type Comparison struct {
	ID                    string `json:"id"`
	ProjectID             string `json:"project_id"`
	BaselineExperimentID  string `json:"baseline_experiment_id"`
	CandidateExperimentID string `json:"candidate_experiment_id"`
	ComparisonType        string `json:"comparison_type"`
	CreatedBy             string `json:"created_by"`
	Status                string `json:"status"`
	Summary               string `json:"summary"`
	RegressionGates       string `json:"regression_gates"`
	CreatedAt             string `json:"created_at"`
	UpdatedAt             string `json:"updated_at"`
}

// CompareService defines comparison operations needed by handlers.
type CompareService interface {
	CreateComparison(ctx context.Context, projectID, baselineExpID, candidateExpID, createdBy, compType, regressionGatesJSON string) (ComparisonDetail, error)
	GetComparison(ctx context.Context, comparisonID string) (ComparisonDetail, error)
	ListComparisons(ctx context.Context, projectID string) ([]Comparison, error)
}

// EvalsService defines the multi-dimensional evaluation operations needed by handlers.
type EvalsService interface {
	CreateMetricDefinition(ctx context.Context, projectID, name, kind, config string) (evals.MetricDefinition, error)
	UpdateMetricDefinition(ctx context.Context, projectID, definitionID, name, kind, config string) (evals.MetricDefinition, error)
	DeleteMetricDefinition(ctx context.Context, projectID, definitionID string) error
	ListMetricDefinitions(ctx context.Context, projectID string) ([]evals.MetricDefinition, error)
	CreateEvalScore(ctx context.Context, score evals.EvalScore) (evals.EvalScore, error)
	CreateEvalScores(ctx context.Context, scores []evals.EvalScore) ([]evals.EvalScore, error)
	ListEvalScoresByRun(ctx context.Context, projectID, runID string) ([]evals.EvalScore, error)
	ListEvalScoresByRuns(ctx context.Context, projectID string, runIDs []string) ([]evals.EvalScore, error)
	AggregateEvalScoresByExperiment(ctx context.Context, projectID, experimentID string) ([]evals.MetricKindAggregation, error)
}

type EvalGovernanceService interface {
	CreateRubricTemplate(ctx context.Context, projectID, name, description string) (evals.RubricTemplate, error)
	CreateRubricVersion(ctx context.Context, projectID, templateID, rubricJSON, createdBy string) (evals.RubricTemplateVersion, error)
	PublishRubricVersion(ctx context.Context, projectID, versionID string) (evals.RubricTemplateVersion, error)
	ListRubricTemplates(ctx context.Context, projectID string) ([]evals.RubricTemplate, error)
	ListRubricVersions(ctx context.Context, projectID, templateID string) ([]evals.RubricTemplateVersion, error)
	SubmitJudgeVotes(ctx context.Context, batch evals.JudgeVoteBatch) (evals.PersistedJudgeAggregation, error)
	ListJudgeEvaluation(ctx context.Context, projectID, runID string) (evals.JudgeEvaluation, error)
	RunCalibration(ctx context.Context, projectID, rubricVersionID string, sampleRunIDs []string, createdBy string, passThreshold float64) (evals.CalibrationRun, error)
	ListCalibrations(ctx context.Context, projectID string) ([]evals.CalibrationRun, error)
}

// EvaluatorCreator defines evaluator CRUD needed by handlers.
type EvaluatorCreator interface {
	Create(ctx context.Context, projectID, name, evalType, config string) (map[string]interface{}, error)
	Get(ctx context.Context, evaluatorID string) (map[string]interface{}, error)
	List(ctx context.Context, projectID string) ([]map[string]interface{}, error)
}

// SamplingRule is the HTTP DTO for a sampling rule.
type SamplingRule struct {
	ID             string  `json:"id"`
	ProjectID      string  `json:"project_id"`
	AgentID        string  `json:"agent_id,omitempty"`
	Name           string  `json:"name"`
	Enabled        bool    `json:"enabled"`
	SampleRate     float32 `json:"sample_rate"`
	Filters        string  `json:"filters,omitempty"`
	Strategy       string  `json:"strategy"`
	DailyBudget    int     `json:"daily_budget"`
	Stratification string  `json:"stratification"`
}

// SamplingService defines the sampling rule operations needed by handlers.
type SamplingService interface {
	CreateRule(ctx context.Context, params sampling.CreateRuleParams) (sampling.SamplingRule, error)
	ListRules(ctx context.Context, projectID string) ([]sampling.SamplingRule, error)
	GetRule(ctx context.Context, ruleID string) (sampling.SamplingRule, error)
	UpdateRule(ctx context.Context, projectID, ruleID string, params sampling.UpdateRuleParams) (sampling.SamplingRule, error)
	DeleteRule(ctx context.Context, ruleID string) error
}

// AnnotationQueue is the HTTP DTO for an annotation queue.
type AnnotationQueue struct {
	ID          string `json:"id"`
	ProjectID   string `json:"project_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// AnnotationItem is the HTTP DTO for an annotation item.
type AnnotationItem struct {
	ID               string     `json:"id"`
	QueueID          string     `json:"queue_id"`
	ProjectID        string     `json:"project_id"`
	RunID            string     `json:"run_id"`
	TraceID          string     `json:"trace_id,omitempty"`
	Status           string     `json:"status"`
	Priority         int        `json:"priority"`
	Scores           string     `json:"scores,omitempty"`
	CorrectedOutput  string     `json:"corrected_output,omitempty"`
	ExpectedToolPath string     `json:"expected_tool_path,omitempty"`
	Comment          string     `json:"comment,omitempty"`
	AgentName        string     `json:"agent_name,omitempty"`
	InputSummary     string     `json:"input_summary,omitempty"`
	OutputSummary    string     `json:"output_summary,omitempty"`
	Error            string     `json:"error,omitempty"`
	FailureCategory  string     `json:"failure_category,omitempty"`
	PrivacyReviewed  bool       `json:"privacy_reviewed"`
	ReviewerNote     string     `json:"reviewer_note,omitempty"`
	GoldScores       string     `json:"gold_scores,omitempty"`
	IsGold           bool       `json:"is_gold"`
	AnnotatorID      string     `json:"annotator_id,omitempty"`
	CompletedAt      *time.Time `json:"completed_at,omitempty"`
}

// AnnotationService defines the annotation operations needed by handlers.
type AnnotationService interface {
	CreateQueue(ctx context.Context, projectID, name, description string) (AnnotationQueue, error)
	ListQueues(ctx context.Context, projectID string) ([]AnnotationQueue, error)
	GetQueue(ctx context.Context, queueID string) (AnnotationQueue, error)
	CreateItem(ctx context.Context, queueID, runID, traceID string, priority int, scores string) (AnnotationItem, error)
	CreateItemFromRun(ctx context.Context, projectID, runID, queueID string) (AnnotationItem, error)
	ListItemsByQueue(ctx context.Context, queueID string) ([]AnnotationItem, error)
	ListItemsByProject(ctx context.Context, projectID string) ([]AnnotationItem, error)
	GetItem(ctx context.Context, itemID string) (AnnotationItem, error)
	UpdateItem(ctx context.Context, itemID string, status, correctedOutput, expectedToolPath, comment string) (AnnotationItem, error)
	UpdateItemFull(ctx context.Context, itemID string, status, correctedOutput, expectedToolPath, comment, failureCategory string, privacyReviewed bool, reviewerNote string) (AnnotationItem, error)
	UpdateItemStatus(ctx context.Context, itemID, status string) (AnnotationItem, error)
	PromoteToDataset(ctx context.Context, itemID, datasetID string) (string, error)
	SubmitGold(ctx context.Context, itemID, projectID, goldScores, annotatorID string) (AnnotationItem, error)
}

// OnlineEvalDispatcher dispatches online evaluations for completed runs.
type OnlineEvalDispatcher interface {
	// DispatchRun synchronously dispatches online evaluation for a completed run.
	DispatchRun(ctx context.Context, projectID, runID string) error
	// DispatchRunAsync dispatches online evaluation asynchronously using its own
	// background context, so it is not affected by HTTP request cancellation.
	DispatchRunAsync(projectID, runID string)
}

// EvalJob is the HTTP DTO for a runner's view of an evaluation job.
type EvalJob struct {
	ID            string `json:"id"`
	ExperimentID  string `json:"experiment_id"`
	DatasetCaseID string `json:"dataset_case_id"`
	Status        string `json:"status"`
}

// ClaimResponse is returned to the runner after claiming.
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

// EvalScoreItem is a single multi-dimensional score submitted with a result.
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

// SubmitResultRequest is submitted by the runner.
type SubmitResultRequest struct {
	RunID        string          `json:"run_id"`
	Score        float64         `json:"score"`
	Passed       bool            `json:"passed"`
	Reason       string          `json:"reason"`
	Details      json.RawMessage `json:"details,omitempty"`
	ActualOutput string          `json:"actual_output"`
	LatencyMs    int64           `json:"latency_ms"`
	Cost         float64         `json:"cost"`
	EvalScores   []EvalScoreItem `json:"eval_scores,omitempty"`
}

// RunnerService defines the runner operations needed by handlers.
type RunnerService interface {
	Claim(ctx context.Context, projectID, runnerName string) (ClaimResponse, error)
	Heartbeat(ctx context.Context, jobID, projectID, runnerName string) error
	SubmitResult(ctx context.Context, jobID, projectID, runnerName string, req SubmitResultRequest) error
	FailJob(ctx context.Context, jobID, projectID, runnerName, errorMsg string) error
}

// AgentInfo is the HTTP DTO for agent information.
type AgentInfo struct {
	ID          string                 `json:"id"`
	ProjectID   string                 `json:"project_id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Status      string                 `json:"status"`
	Version     int                    `json:"version"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt   string                 `json:"created_at,omitempty"`
	UpdatedAt   string                 `json:"updated_at,omitempty"`
}

// AgentInstanceInfo is the HTTP DTO for an agent instance.
type AgentInstanceInfo struct {
	ID               string                 `json:"id"`
	AgentID          string                 `json:"agent_id"`
	ProjectID        string                 `json:"project_id"`
	InstanceID       string                 `json:"instance_id"`
	InstanceName     string                 `json:"instance_name"`
	Status           string                 `json:"status"`
	ConnectionStatus string                 `json:"connection_status"`
	Hostname         string                 `json:"hostname"`
	PID              int                    `json:"pid"`
	ActiveRunID      string                 `json:"active_run_id,omitempty"`
	Metadata         map[string]interface{} `json:"metadata,omitempty"`
	StartedAt        string                 `json:"started_at"`
	LastHeartbeatAt  string                 `json:"last_heartbeat_at"`
}

type AgentRuntimeSummary struct {
	TotalInstanceCount   int     `json:"total_instance_count"`
	OnlineInstanceCount  int     `json:"online_instance_count"`
	OfflineInstanceCount int     `json:"offline_instance_count"`
	LastHeartbeatAt      *string `json:"last_heartbeat_at,omitempty"`
	ConnectionStatus     string  `json:"connection_status"`
}

// AgentCapabilityInfo is the HTTP DTO for an agent capability.
type AgentCapabilityInfo struct {
	ID          string                 `json:"id"`
	AgentID     string                 `json:"agent_id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt   string                 `json:"created_at"`
}

// AgentDetail combines agent info with instances and capabilities.
type AgentDetail struct {
	AgentInfo
	RuntimeSummary AgentRuntimeSummary   `json:"runtime_summary"`
	Instances      []AgentInstanceInfo   `json:"instances"`
	Capabilities   []AgentCapabilityInfo `json:"capabilities"`
}

// AgentSummaryResponse is the HTTP DTO for agent control center summary.
type AgentSummaryResponse struct {
	ID          string                 `json:"id"`
	ProjectID   string                 `json:"project_id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Status      string                 `json:"status"`
	Version     int                    `json:"version"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt   string                 `json:"created_at,omitempty"`
	UpdatedAt   string                 `json:"updated_at,omitempty"`
	// Runtime contract summary from metadata
	Framework      string `json:"framework,omitempty"`
	Runtime        string `json:"runtime,omitempty"`
	Source         string `json:"source,omitempty"`
	Environment    string `json:"environment,omitempty"`
	AgentVersion   string `json:"agent_version,omitempty"`
	ModelProvider  string `json:"model_provider,omitempty"`
	ModelName      string `json:"model_name,omitempty"`
	HarnessVersion string `json:"harness_version,omitempty"`
	PromptVersion  string `json:"prompt_version,omitempty"`
	// Aggregated metrics
	InstanceCount       int      `json:"instance_count"`
	OnlineInstanceCount int      `json:"online_instance_count"`
	ActiveRunCount      int      `json:"active_run_count"`
	RecentRunCount      int      `json:"recent_run_count"`
	RecentErrorCount    int      `json:"recent_error_count"`
	LastHeartbeatAt     *string  `json:"last_heartbeat_at,omitempty"`
	LastRunAt           *string  `json:"last_run_at,omitempty"`
	P95LatencyMs        *float64 `json:"p95_latency_ms,omitempty"`
	ErrorRate           float64  `json:"error_rate"`
	TotalCost           *float64 `json:"total_cost,omitempty"`
	// Derived online status
	CurrentStatusSummary string `json:"current_status_summary"`
}

// AgentService defines agent operations needed by handlers.
type AgentService interface {
	RegisterAgent(ctx context.Context, projectID, name, description string, metadata map[string]interface{}) (AgentInfo, error)
	ListAgents(ctx context.Context, projectID string) ([]AgentInfo, error)
	GetAgentDetail(ctx context.Context, agentID string) (AgentDetail, error)
	RecordHeartbeat(ctx context.Context, agentID, projectID, instanceID, instanceName, status string, payload map[string]interface{}) error
	SetCapability(ctx context.Context, agentID, name, description string, metadata map[string]interface{}) error
	GetAgentByID(ctx context.Context, agentID string) (AgentInfo, error)
	ListAgentSummaries(ctx context.Context, projectID string) ([]AgentSummaryResponse, error)
}

// SDK Runtime API DTOs

// SDKStartRunRequest is the request body for SDK start run.
type SDKStartRunRequest struct {
	AgentID    string `json:"agent_id"`
	InstanceID string `json:"instance_id"`
	// AgentInstanceID is a deprecated compat alias for InstanceID.
	// New code should use instance_id instead.
	AgentInstanceID string         `json:"agent_instance_id,omitempty"`
	Input           string         `json:"input"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	TraceID         string         `json:"trace_id"`
	Snapshot        map[string]any `json:"snapshot,omitempty"`
}

// GetInstanceID returns the instance_id, falling back to agent_instance_id for backwards compat.
func (s *SDKStartRunRequest) GetInstanceID() string {
	if s.InstanceID != "" {
		return s.InstanceID
	}
	return s.AgentInstanceID
}

// SDKUpdateRunStateRequest is the request body for SDK update run state.
type SDKUpdateRunStateRequest struct {
	Status     string `json:"status"`
	Message    string `json:"message"`
	AgentID    string `json:"agent_id"`
	InstanceID string `json:"instance_id"`
	// AgentInstanceID is a deprecated compat alias for InstanceID.
	AgentInstanceID string         `json:"agent_instance_id,omitempty"`
	Attributes      map[string]any `json:"attributes,omitempty"`
	TraceID         string         `json:"trace_id"`
}

// GetInstanceID returns the instance_id, falling back to agent_instance_id for backwards compat.
func (s *SDKUpdateRunStateRequest) GetInstanceID() string {
	if s.InstanceID != "" {
		return s.InstanceID
	}
	return s.AgentInstanceID
}

// SDKFinishRunRequest is the request body for SDK finish run.
type SDKFinishRunRequest struct {
	Output     string `json:"output"`
	AgentID    string `json:"agent_id"`
	InstanceID string `json:"instance_id"`
	// AgentInstanceID is a deprecated compat alias for InstanceID.
	AgentInstanceID string         `json:"agent_instance_id,omitempty"`
	Metrics         map[string]any `json:"metrics,omitempty"`
	TraceID         string         `json:"trace_id"`
	LatencyMS       int64          `json:"latency_ms"`
}

// GetInstanceID returns the instance_id, falling back to agent_instance_id for backwards compat.
func (s *SDKFinishRunRequest) GetInstanceID() string {
	if s.InstanceID != "" {
		return s.InstanceID
	}
	return s.AgentInstanceID
}

// SDKFailRunRequest is the request body for SDK fail run.
type SDKFailRunRequest struct {
	Error      string `json:"error"`
	AgentID    string `json:"agent_id"`
	InstanceID string `json:"instance_id"`
	// AgentInstanceID is a deprecated compat alias for InstanceID.
	AgentInstanceID string         `json:"agent_instance_id,omitempty"`
	Attributes      map[string]any `json:"attributes,omitempty"`
	TraceID         string         `json:"trace_id"`
	LatencyMS       int64          `json:"latency_ms"`
}

// GetInstanceID returns the instance_id, falling back to agent_instance_id for backwards compat.
func (s *SDKFailRunRequest) GetInstanceID() string {
	if s.InstanceID != "" {
		return s.InstanceID
	}
	return s.AgentInstanceID
}

// RuntimeRunResponse is the response for runtime run operations.
type RuntimeRunResponse struct {
	ID        string `json:"id"`
	ProjectID string `json:"project_id"`
	Status    string `json:"status"`
	Input     string `json:"input,omitempty"`
	Output    string `json:"output,omitempty"`
	Error     string `json:"error,omitempty"`
	TraceID   string `json:"trace_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	AgentName string `json:"agent_name,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// ActiveRunResponse is the response for active run listing.
type ActiveRunResponse struct {
	ID                string  `json:"id"`
	ProjectID         string  `json:"project_id"`
	Status            string  `json:"status"`
	InputSummary      string  `json:"input_summary,omitempty"`
	OutputSummary     string  `json:"output_summary,omitempty"`
	Error             string  `json:"error,omitempty"`
	TraceID           string  `json:"trace_id,omitempty"`
	LatencyMsStr      string  `json:"latency_ms,omitempty"`
	AgentID           string  `json:"agent_id,omitempty"`
	AgentName         string  `json:"agent_name,omitempty"`
	AgentInstanceID   string  `json:"agent_instance_id,omitempty"`
	AgentInstanceName string  `json:"agent_instance_name,omitempty"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
	LastStateEventAt  *string `json:"last_state_event_at,omitempty"`
	LastState         string  `json:"last_state,omitempty"`
}

// RunStateEventResponse is the response for run state event.
type RunStateEventResponse struct {
	ID         string `json:"id"`
	RunID      string `json:"run_id"`
	FromStatus string `json:"from_status,omitempty"`
	ToStatus   string `json:"to_status"`
	Message    string `json:"message,omitempty"`
	CreatedAt  string `json:"created_at"`
}

// RecentRunResponse is the response for recent agent runs.
type RecentRunResponse struct {
	ID           string `json:"id"`
	ProjectID    string `json:"project_id"`
	Status       string `json:"status"`
	InputSummary string `json:"input_summary,omitempty"`
	Error        string `json:"error,omitempty"`
	TraceID      string `json:"trace_id,omitempty"`
	LatencyMs    *int64 `json:"latency_ms,omitempty"`
	AgentName    string `json:"agent_name,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

// Semantic event record types for runtime detail response.
type ThoughtRecord struct {
	ProjectID       string   `json:"project_id"`
	RunID           string   `json:"run_id"`
	TraceID         string   `json:"trace_id"`
	SpanID          string   `json:"span_id"`
	AgentID         string   `json:"agent_id,omitempty"`
	AgentInstanceID string   `json:"agent_instance_id,omitempty"`
	Stage           string   `json:"stage,omitempty"`
	Summary         string   `json:"summary"`
	Content         string   `json:"content,omitempty"`
	Confidence      *float64 `json:"confidence,omitempty"`
	ContentCaptured bool     `json:"content_captured"`
	StartTime       string   `json:"start_time"`
	DurationMs      *int64   `json:"duration_ms,omitempty"`
}

type LLMCallRecord struct {
	ProjectID        string   `json:"project_id"`
	RunID            string   `json:"run_id"`
	TraceID          string   `json:"trace_id"`
	SpanID           string   `json:"span_id"`
	AgentID          string   `json:"agent_id,omitempty"`
	AgentInstanceID  string   `json:"agent_instance_id,omitempty"`
	Provider         string   `json:"provider"`
	Model            string   `json:"model"`
	InputTokens      *int64   `json:"input_tokens,omitempty"`
	OutputTokens     *int64   `json:"output_tokens,omitempty"`
	TotalTokens      *int64   `json:"total_tokens,omitempty"`
	CacheReadTokens  *int64   `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int64   `json:"cache_write_tokens,omitempty"`
	TTFTMs           *int64   `json:"ttft_ms,omitempty"`
	Cost             *float64 `json:"cost,omitempty"`
	EstimatedCost    *float64 `json:"estimated_cost,omitempty"`
	ActualCost       *float64 `json:"actual_cost,omitempty"`
	CostSource       string   `json:"cost_source,omitempty"`
	InputSummary     string   `json:"input_summary,omitempty"`
	OutputSummary    string   `json:"output_summary,omitempty"`
	ContentCaptured  bool     `json:"content_captured"`
	Status           string   `json:"status"`
	Error            string   `json:"error,omitempty"`
	StartTime        string   `json:"start_time"`
	DurationMs       *int64   `json:"duration_ms,omitempty"`
}

type ToolCallRecord struct {
	ProjectID        string `json:"project_id"`
	RunID            string `json:"run_id"`
	TraceID          string `json:"trace_id"`
	SpanID           string `json:"span_id"`
	AgentID          string `json:"agent_id,omitempty"`
	AgentInstanceID  string `json:"agent_instance_id,omitempty"`
	ToolName         string `json:"tool_name"`
	ToolType         string `json:"tool_type,omitempty"`
	ArgumentsSummary string `json:"arguments_summary,omitempty"`
	ResultSummary    string `json:"result_summary,omitempty"`
	Success          bool   `json:"success"`
	Error            string `json:"error,omitempty"`
	RetryCount       int64  `json:"retry_count"`
	SideEffectRisk   string `json:"side_effect_risk,omitempty"`
	StartTime        string `json:"start_time"`
	DurationMs       *int64 `json:"duration_ms,omitempty"`
}

type RetrievalCallRecord struct {
	ProjectID       string    `json:"project_id"`
	RunID           string    `json:"run_id"`
	TraceID         string    `json:"trace_id"`
	SpanID          string    `json:"span_id"`
	AgentID         string    `json:"agent_id,omitempty"`
	AgentInstanceID string    `json:"agent_instance_id,omitempty"`
	KnowledgeBase   string    `json:"knowledge_base"`
	QueryText       string    `json:"query_text"`
	TopK            *int64    `json:"top_k,omitempty"`
	ReturnedCount   *int64    `json:"returned_count,omitempty"`
	DocumentIDs     []string  `json:"document_ids,omitempty"`
	Scores          []float64 `json:"scores,omitempty"`
	StartTime       string    `json:"start_time"`
	DurationMs      *int64    `json:"duration_ms,omitempty"`
}

type SkillCallRecord struct {
	ProjectID       string `json:"project_id"`
	RunID           string `json:"run_id"`
	TraceID         string `json:"trace_id"`
	SpanID          string `json:"span_id"`
	AgentID         string `json:"agent_id,omitempty"`
	AgentInstanceID string `json:"agent_instance_id,omitempty"`
	SkillName       string `json:"skill_name"`
	SkillVersion    string `json:"skill_version,omitempty"`
	InputSummary    string `json:"input_summary,omitempty"`
	OutputSummary   string `json:"output_summary,omitempty"`
	Success         bool   `json:"success"`
	Error           string `json:"error,omitempty"`
	StartTime       string `json:"start_time"`
	DurationMs      *int64 `json:"duration_ms,omitempty"`
}

type FinalAnswerRecord struct {
	ProjectID        string `json:"project_id"`
	RunID            string `json:"run_id"`
	TraceID          string `json:"trace_id"`
	SpanID           string `json:"span_id"`
	AnswerSummary    string `json:"answer_summary"`
	AnswerOutput     string `json:"answer_output,omitempty"`
	StructuredOutput string `json:"structured_output,omitempty"`
	StartTime        string `json:"start_time"`
	DurationMs       *int64 `json:"duration_ms,omitempty"`
}

// RuntimeRunDetailResponse is the response for runtime run detail.
type RuntimeRunDetailResponse struct {
	ID                string                  `json:"id"`
	ProjectID         string                  `json:"project_id"`
	Status            string                  `json:"status"`
	Input             string                  `json:"input,omitempty"`
	Output            string                  `json:"output,omitempty"`
	Error             string                  `json:"error,omitempty"`
	TraceID           string                  `json:"trace_id,omitempty"`
	AgentID           string                  `json:"agent_id,omitempty"`
	AgentName         string                  `json:"agent_name,omitempty"`
	AgentInstanceID   string                  `json:"agent_instance_id,omitempty"`
	AgentInstanceName string                  `json:"agent_instance_name,omitempty"`
	CreatedAt         string                  `json:"created_at"`
	UpdatedAt         string                  `json:"updated_at"`
	Events            []RunStateEventResponse `json:"events"`
	// Semantic events from ClickHouse (best-effort)
	Thoughts       []ThoughtRecord       `json:"thoughts,omitempty"`
	LLMCalls       []LLMCallRecord       `json:"llm_calls,omitempty"`
	ToolCalls      []ToolCallRecord      `json:"tool_calls,omitempty"`
	RetrievalCalls []RetrievalCallRecord `json:"retrieval_calls,omitempty"`
	SkillCalls     []SkillCallRecord     `json:"skill_calls,omitempty"`
	FinalAnswers   []FinalAnswerRecord   `json:"final_answers,omitempty"`
	Spans          []TraceSpanRecord     `json:"spans,omitempty"`
}

// TraceSpanRecord matches the frontend TraceSpan type for runtime detail.
type TraceSpanRecord struct {
	RunID         string `json:"run_id"`
	SpanID        string `json:"span_id"`
	TraceID       string `json:"trace_id"`
	ProjectID     string `json:"project_id"`
	ParentSpanID  string `json:"parent_span_id,omitempty"`
	Name          string `json:"name"`
	Kind          string `json:"kind,omitempty"`
	StatusCode    string `json:"status_code,omitempty"`
	StatusMessage string `json:"status_message,omitempty"`
	StartTime     string `json:"start_time"`
	EndTime       string `json:"end_time,omitempty"`
	DurationMs    *int64 `json:"duration_ms,omitempty"`
	Attributes    string `json:"attributes,omitempty"`
	Truncated     bool   `json:"truncated"`
}
type RuntimeService interface {
	StartRun(ctx context.Context, projectID string, req SDKStartRunRequest) (RuntimeRunResponse, error)
	UpdateRunState(ctx context.Context, projectID, runID string, req SDKUpdateRunStateRequest) (RuntimeRunResponse, error)
	FinishRun(ctx context.Context, projectID, runID string, req SDKFinishRunRequest) (RuntimeRunResponse, error)
	FailRun(ctx context.Context, projectID, runID string, req SDKFailRunRequest) (RuntimeRunResponse, error)
	ListActiveRuns(ctx context.Context, projectID string, limit, offset int) ([]ActiveRunResponse, error)
	GetRuntimeRunDetail(ctx context.Context, projectID, runID string) (RuntimeRunDetailResponse, error)
	ListRecentRunsByAgent(ctx context.Context, projectID, agentName string, limit int) ([]RecentRunResponse, error)
}

// ProjectAuth checks project membership for a given user.
type ProjectAuth interface {
	IsProjectMember(ctx context.Context, userID, projectID string) error
}

// ProjectAdminAuth protects project governance mutations.
type ProjectAdminAuth interface {
	IsProjectAdmin(ctx context.Context, userID, projectID string) error
}

// QuotaService enforces distributed project quotas and serves governance data.
type QuotaService interface {
	Status(ctx context.Context, projectID string) (quotas.Status, error)
	UpdateQuota(ctx context.Context, projectID, updatedBy string, quota quotas.Quota) (quotas.Quota, error)
	CheckIngest(ctx context.Context, projectID string, dimension quotas.Dimension, amount int64) quotas.Decision
	AcquireSSE(ctx context.Context, projectID string) quotas.Decision
	ReleaseSSE(ctx context.Context, projectID string)
	ConsumeDailyBudget(ctx context.Context, projectID string, dimension quotas.Dimension, amount int64) quotas.Decision
}

// AnomalyService serves project-scoped operational anomaly records.
type AnomalyService interface {
	ListProject(ctx context.Context, projectID string, limit int) ([]anomalies.Record, error)
	DetectProject(ctx context.Context, projectID string) ([]anomalies.Record, error)
}

// Dependencies holds the dependencies injected into the router.
type Dependencies struct {
	AuthService        AuthService
	ProjectsService    ProjectsService
	TokenAuth          TokenAuth
	RunsService        RunsServiceInterface
	Tracestore         TracestoreInterface
	SessionSigner      auth.SessionSigner
	DatasetsService    DatasetsService
	ExperimentsService ExperimentsService
	RunnerService      RunnerService
	ProjectAuth        ProjectAuth
	ProjectAdminAuth   ProjectAdminAuth
	AgentService       AgentService
	RuntimeService     RuntimeService
	EvalsService       EvalsService
	EvalGovernance     EvalGovernanceService
	EvaluatorCreator   EvaluatorCreator
	SamplingService    SamplingService
	AnnotationService  AnnotationService
	OnlineEvalDisp     OnlineEvalDispatcher
	CompareService     CompareService
	StaticFS           fs.FS
	RateLimiter        *ingest.ProjectRateLimiter
	QuotaService       QuotaService
	AnomalyService     AnomalyService
	TraceWriter        ingest.TraceBatchWriter
	IngestStats        IngestStatsQuerier
	IngestHealth       ingest.HealthRecorder
	RealtimeOutbox     RealtimeOutboxWriter
	RealtimeSubscriber realtime.Subscriber
	RealtimeHeartbeat  time.Duration
	SnapshotService    SnapshotService
	MaxBodySize        int
	MaxSpans           int
	SecureCookies      bool
}

type SnapshotService interface {
	Get(ctx context.Context, runID, projectID string) (snapshots.Snapshot, error)
}

// ---- Auth handlers ----

type registerRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

func handleRegister(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req registerRequest
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		if req.Email == "" || req.Password == "" {
			writeError(w, http.StatusBadRequest, "missing_fields", "Email and password are required")
			return
		}

		userID, err := deps.AuthService.Register(r.Context(), req.Email, req.Password, req.DisplayName)
		if err != nil {
			switch {
			case errors.Is(err, auth.ErrEmailAlreadyRegistered):
				writeError(w, http.StatusConflict, "email_exists", "Email already registered")
				return
			case errors.Is(err, auth.ErrPasswordEmpty):
				writeError(w, http.StatusBadRequest, "registration_failed", auth.ErrPasswordEmpty.Error())
				return
			case errors.Is(err, auth.ErrPasswordTooShort):
				writeError(w, http.StatusBadRequest, "registration_failed", auth.ErrPasswordTooShort.Error())
				return
			case errors.Is(err, auth.ErrPasswordTooLong):
				writeError(w, http.StatusBadRequest, "registration_failed", auth.ErrPasswordTooLong.Error())
				return
			}
			writeInternalError(w, "registration_failed", "Registration failed", "register user", err)
			return
		}

		signed, err := deps.SessionSigner.SignUserID(userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "session_error", "Failed to create session")
			return
		}

		setSessionCookie(w, signed, deps.SessionSigner.TTL(), deps.SecureCookies)
		writeJSON(w, http.StatusCreated, map[string]string{
			"id":           userID,
			"email":        req.Email,
			"display_name": req.DisplayName,
		})
	}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func handleLogin(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		if !decodeJSONRequest(w, r, &req) {
			return
		}

		userID, displayName, err := deps.AuthService.Login(r.Context(), req.Email, req.Password)
		if err != nil {
			if errors.Is(err, auth.ErrInvalidCredentials) {
				writeError(w, http.StatusUnauthorized, "invalid_credentials", "Invalid email or password")
				return
			}
			writeInternalError(w, "login_failed", "Login failed", "login user", err)
			return
		}

		signed, err := deps.SessionSigner.SignUserID(userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "session_error", "Failed to create session")
			return
		}

		setSessionCookie(w, signed, deps.SessionSigner.TTL(), deps.SecureCookies)
		writeJSON(w, http.StatusOK, map[string]string{
			"id":           userID,
			"email":        req.Email,
			"display_name": displayName,
		})
	}
}

// ---- Project handlers ----

type createProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

func handleCreateProject(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		var req createProjectRequest
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "missing_name", "Project name is required")
			return
		}

		project, err := deps.ProjectsService.CreateProject(r.Context(), userID, req.Name, req.Description)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "create_failed", "Failed to create project")
			return
		}

		writeJSON(w, http.StatusCreated, project)
	}
}

func handleListProjects(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}

		projects, err := deps.ProjectsService.ListProjects(r.Context(), userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", "Failed to list projects")
			return
		}

		writeJSON(w, http.StatusOK, projects)
	}
}

type createTokenRequest struct {
	Description string   `json:"description"`
	Scopes      []string `json:"scopes"`
	ExpiresAt   string   `json:"expires_at"`
	Environment string   `json:"environment"`
	TokenType   string   `json:"token_type"`
}

func tokenOptions(req createTokenRequest) TokenCreateOptions {
	return TokenCreateOptions{Description: req.Description, Scopes: req.Scopes, ExpiresAt: req.ExpiresAt, Environment: req.Environment, TokenType: req.TokenType}
}
func writeTokenGovernanceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, projects.ErrInvalidTokenScopes):
		writeError(w, http.StatusBadRequest, "invalid_scopes", "Token scopes are invalid")
	case errors.Is(err, projects.ErrInvalidTokenOptions):
		writeError(w, http.StatusBadRequest, "invalid_token_options", "Token options are invalid")
	case errors.Is(err, projects.ErrTokenPolicyViolation):
		writeError(w, http.StatusUnprocessableEntity, "token_policy_violation", "Token options violate the project policy")
	case errors.Is(err, projects.ErrTokenNotFound):
		writeError(w, http.StatusNotFound, "token_not_found", "Project Token was not found")
	case errors.Is(err, projects.ErrTokenRevoked):
		writeError(w, http.StatusConflict, "token_revoked", "Project Token has already been revoked")
	case errors.Is(err, projects.ErrTokenExpired):
		writeError(w, http.StatusConflict, "token_expired", "Project Token has expired")
	case errors.Is(err, projects.ErrProjectAdminRequired):
		writeError(w, http.StatusForbidden, "token_governance_forbidden", "Project owner or admin permission is required")
	default:
		writeError(w, http.StatusInternalServerError, "token_governance_failed", "Project Token operation failed")
	}
}
func handleCreateToken(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromCtx(r.Context())
		if userID == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "Not authenticated")
			return
		}
		var req createTokenRequest
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		plain, token, err := deps.ProjectsService.CreateToken(r.Context(), chi.URLParam(r, "projectID"), userID, tokenOptions(req))
		if err != nil {
			writeTokenGovernanceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]interface{}{"plain_token": plain, "token": token})
	}
}
func handleListProjectTokens(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tokens, err := deps.ProjectsService.ListTokens(r.Context(), chi.URLParam(r, "projectID"), userIDFromCtx(r.Context()))
		if err != nil {
			writeTokenGovernanceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, tokens)
	}
}

type revokeTokenRequest struct {
	Reason string `json:"reason"`
}

func handleRevokeProjectToken(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req revokeTokenRequest
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		token, err := deps.ProjectsService.RevokeToken(r.Context(), chi.URLParam(r, "projectID"), chi.URLParam(r, "tokenID"), userIDFromCtx(r.Context()), req.Reason)
		if err != nil {
			writeTokenGovernanceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, token)
	}
}
func handleRotateProjectToken(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createTokenRequest
		if !decodeJSONRequest(w, r, &req) {
			return
		}
		plain, token, err := deps.ProjectsService.RotateToken(r.Context(), chi.URLParam(r, "projectID"), chi.URLParam(r, "tokenID"), userIDFromCtx(r.Context()), tokenOptions(req))
		if err != nil {
			writeTokenGovernanceError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]interface{}{"plain_token": plain, "token": token})
	}
}
func handleListTokenAudit(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		events, err := deps.ProjectsService.ListTokenAudit(r.Context(), chi.URLParam(r, "projectID"), chi.URLParam(r, "tokenID"), userIDFromCtx(r.Context()))
		if err != nil {
			writeTokenGovernanceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, events)
	}
}
func handleGetTokenPolicy(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		policy, err := deps.ProjectsService.GetTokenPolicy(r.Context(), chi.URLParam(r, "projectID"), userIDFromCtx(r.Context()))
		if err != nil {
			writeTokenGovernanceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, policy)
	}
}
func handleUpdateTokenPolicy(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var policy TokenPolicy
		if !decodeJSONRequest(w, r, &policy) {
			return
		}
		updated, err := deps.ProjectsService.UpdateTokenPolicy(r.Context(), chi.URLParam(r, "projectID"), userIDFromCtx(r.Context()), policy)
		if err != nil {
			writeTokenGovernanceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, updated)
	}
}
func handleGetTokenRiskSummary(deps Dependencies) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		summary, err := deps.ProjectsService.GetTokenRiskSummary(r.Context(), chi.URLParam(r, "projectID"), userIDFromCtx(r.Context()))
		if err != nil {
			writeTokenGovernanceError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, summary)
	}
}
