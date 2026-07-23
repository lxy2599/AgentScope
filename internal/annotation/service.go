package annotation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ---- Domain types ----

// AnnotationQueue represents a named queue for collecting annotation items.
type AnnotationQueue struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// AnnotationItem represents a single item in an annotation queue.
type AnnotationItem struct {
	ID               string    `json:"id"`
	QueueID          string    `json:"queue_id"`
	ProjectID        string    `json:"project_id"`
	RunID            string    `json:"run_id"`
	TraceID          string    `json:"trace_id"`
	Status           string    `json:"status"`
	Priority         int       `json:"priority"`
	Scores           string    `json:"scores"`
	CorrectedOutput  string    `json:"corrected_output,omitempty"`
	ExpectedToolPath string    `json:"expected_tool_path,omitempty"`
	Comment          string    `json:"comment,omitempty"`
	AgentName        string    `json:"agent_name,omitempty"`
	InputSummary     string    `json:"input_summary,omitempty"`
	OutputSummary    string    `json:"output_summary,omitempty"`
	Error            string    `json:"error,omitempty"`
	FailureCategory  string    `json:"failure_category,omitempty"`
	PrivacyReviewed  bool      `json:"privacy_reviewed"`
	ReviewerNote     string    `json:"reviewer_note,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// CreateQueueParams contains fields for creating an annotation queue.
type CreateQueueParams struct {
	ProjectID   string
	Name        string
	Description string
}

// CreateItemParams contains fields for creating an annotation item.
type CreateItemParams struct {
	QueueID         string
	RunID           string
	TraceID         string
	Priority        int
	Scores          string
	AgentName       string
	InputSummary    string
	OutputSummary   string
	Error           string
	FailureCategory string
	PrivacyReviewed bool
	ReviewerNote    string
	MissingTraceID  bool
}

// UpdateItemParams contains fields for updating an annotation item.
type UpdateItemParams struct {
	Status           string
	CorrectedOutput  string
	ExpectedToolPath string
	Comment          string
	FailureCategory  string
	PrivacyReviewed  bool
	ReviewerNote     string
}

// CreateItemFromRunParams contains fields for creating an annotation item from a run.
type CreateItemFromRunParams struct {
	ProjectID string
	RunID     string
	QueueID   string // if empty, the default queue will be used
}

// ---- Interfaces ----

// Querier defines the database operations the annotation service needs.
type Querier interface {
	CreateQueue(ctx context.Context, params CreateQueueParams) (AnnotationQueue, error)
	ListQueues(ctx context.Context, projectID string) ([]AnnotationQueue, error)
	GetQueue(ctx context.Context, queueID string) (AnnotationQueue, error)
	DeleteQueue(ctx context.Context, queueID, projectID string) error
	CreateItem(ctx context.Context, params CreateItemParams) (AnnotationItem, error)
	ListItemsByQueue(ctx context.Context, queueID string) ([]AnnotationItem, error)
	GetItem(ctx context.Context, itemID string) (AnnotationItem, error)
	UpdateItem(ctx context.Context, itemID string, params UpdateItemParams) (AnnotationItem, error)
	UpdateItemStatus(ctx context.Context, itemID, status string) (AnnotationItem, error)
	ListItemsByProject(ctx context.Context, projectID string) ([]AnnotationItem, error)
}

// DatasetCreator adds a case to a dataset.
type DatasetCreator interface {
	AddCase(ctx context.Context, datasetID string, input, expected, metadata string) (string, error)
}

// RunDetail holds the fields needed when creating an annotation item from a run.
type RunDetail struct {
	ID            string
	ProjectID     string
	TraceID       string
	AgentName     string
	InputSummary  string
	OutputSummary string
	Error         string
}

// RunProvider provides run data.
type RunProvider interface {
	GetRunInput(ctx context.Context, runID string) (input, traceID string, err error)
	GetRunDetail(ctx context.Context, runID string) (RunDetail, error)
}

// ---- Service ----

// Service provides annotation queue and promote-to-dataset operations.
type Service struct {
	q              Querier
	datasetCreator DatasetCreator
	runProvider    RunProvider
}

// NewService creates a new annotation Service.
func NewService(q Querier) *Service {
	return &Service{q: q}
}

// SetPromoteDeps sets the dependencies needed for promote-to-dataset.
func (s *Service) SetPromoteDeps(dc DatasetCreator, rp RunProvider) {
	s.datasetCreator = dc
	s.runProvider = rp
}

func (s *Service) CreateQueue(ctx context.Context, params CreateQueueParams) (AnnotationQueue, error) {
	if params.ProjectID == "" {
		return AnnotationQueue{}, errors.New("projectID is required")
	}
	if params.Name == "" {
		return AnnotationQueue{}, errors.New("name is required")
	}
	return s.q.CreateQueue(ctx, params)
}

func (s *Service) ListQueues(ctx context.Context, projectID string) ([]AnnotationQueue, error) {
	if projectID == "" {
		return nil, errors.New("projectID is required")
	}
	return s.q.ListQueues(ctx, projectID)
}

func (s *Service) GetQueue(ctx context.Context, queueID string) (AnnotationQueue, error) {
	if queueID == "" {
		return AnnotationQueue{}, errors.New("queueID is required")
	}
	return s.q.GetQueue(ctx, queueID)
}

func (s *Service) DeleteQueue(ctx context.Context, queueID, projectID string) error {
	if queueID == "" {
		return errors.New("queueID is required")
	}
	return s.q.DeleteQueue(ctx, queueID, projectID)
}

// EnsureDefaultQueue returns the "线上评测待标注" queue for the project, creating it
// if no queue with that name exists. This provides a default target for auto-created
// annotation items from online evaluation.
func (s *Service) EnsureDefaultQueue(ctx context.Context, projectID string) (AnnotationQueue, error) {
	if projectID == "" {
		return AnnotationQueue{}, errors.New("projectID is required")
	}
	queues, err := s.q.ListQueues(ctx, projectID)
	if err != nil {
		return AnnotationQueue{}, err
	}
	for _, q := range queues {
		if q.Name == "线上评测待标注" {
			return q, nil
		}
	}
	// Create default queue
	return s.q.CreateQueue(ctx, CreateQueueParams{
		ProjectID:   projectID,
		Name:        "线上评测待标注",
		Description: "自动创建的默认队列，用于收集线上评测产生的待标注数据",
	})
}

func (s *Service) CreateItem(ctx context.Context, params CreateItemParams) (AnnotationItem, error) {
	if params.QueueID == "" {
		return AnnotationItem{}, errors.New("queueID is required")
	}
	if params.RunID == "" {
		return AnnotationItem{}, errors.New("runID is required")
	}
	return s.q.CreateItem(ctx, params)
}

func (s *Service) ListItemsByQueue(ctx context.Context, queueID string) ([]AnnotationItem, error) {
	if queueID == "" {
		return nil, errors.New("queueID is required")
	}
	return s.q.ListItemsByQueue(ctx, queueID)
}

func (s *Service) GetItem(ctx context.Context, itemID string) (AnnotationItem, error) {
	if itemID == "" {
		return AnnotationItem{}, errors.New("itemID is required")
	}
	return s.q.GetItem(ctx, itemID)
}

// CreateItemFromRun creates an annotation item by copying run data.
// It uses the specified queue (or the default queue if queueID is empty).
// If the run lacks a trace_id, it still creates the item; the caller can
// detect the missing trace via item.TraceID == "".
func (s *Service) CreateItemFromRun(ctx context.Context, params CreateItemFromRunParams) (AnnotationItem, error) {
	if params.ProjectID == "" {
		return AnnotationItem{}, errors.New("projectID is required")
	}
	if params.RunID == "" {
		return AnnotationItem{}, errors.New("runID is required")
	}
	if s.runProvider == nil {
		return AnnotationItem{}, errors.New("run provider not configured")
	}

	// Determine target queue
	queueID := params.QueueID
	if queueID == "" {
		q, err := s.EnsureDefaultQueue(ctx, params.ProjectID)
		if err != nil {
			return AnnotationItem{}, fmt.Errorf("ensure default queue: %w", err)
		}
		queueID = q.ID
	}

	// Get run details
	detail, err := s.runProvider.GetRunDetail(ctx, params.RunID)
	if err != nil {
		return AnnotationItem{}, fmt.Errorf("get run detail: %w", err)
	}

	// Verify run belongs to the given project
	if detail.ProjectID != params.ProjectID {
		return AnnotationItem{}, errors.New("run does not belong to the specified project")
	}

	return s.q.CreateItem(ctx, CreateItemParams{
		QueueID:         queueID,
		RunID:           detail.ID,
		TraceID:         detail.TraceID,
		Priority:        0,
		Scores:          "[]",
		AgentName:       detail.AgentName,
		InputSummary:    detail.InputSummary,
		OutputSummary:   detail.OutputSummary,
		Error:           detail.Error,
		PrivacyReviewed: false,
	})
}

func (s *Service) UpdateItem(ctx context.Context, itemID string, params UpdateItemParams) (AnnotationItem, error) {
	if itemID == "" {
		return AnnotationItem{}, errors.New("itemID is required")
	}
	return s.q.UpdateItem(ctx, itemID, params)
}

func (s *Service) UpdateItemStatus(ctx context.Context, itemID, status string) (AnnotationItem, error) {
	if itemID == "" {
		return AnnotationItem{}, errors.New("itemID is required")
	}
	validStatuses := map[string]bool{
		"pending": true, "in_progress": true, "completed": true,
		"skipped": true, "promoted": true,
	}
	if !validStatuses[status] {
		return AnnotationItem{}, errors.New("invalid status: must be pending, in_progress, completed, skipped, or promoted")
	}
	return s.q.UpdateItemStatus(ctx, itemID, status)
}

// PromoteToDataset promotes an annotation item to a dataset case.
// Returns the created dataset case ID.
func (s *Service) PromoteToDataset(ctx context.Context, itemID, datasetID string) (string, error) {
	if itemID == "" {
		return "", errors.New("itemID is required")
	}
	if datasetID == "" {
		return "", errors.New("datasetID is required")
	}
	if s.datasetCreator == nil || s.runProvider == nil {
		return "", errors.New("promote dependencies not configured")
	}

	// Get the annotation item
	item, err := s.q.GetItem(ctx, itemID)
	if err != nil {
		return "", fmt.Errorf("get annotation item: %w", err)
	}

	// Check if already promoted
	if item.Status == "promoted" {
		return "", errors.New("annotation item already promoted")
	}

	// Must have corrected output
	if item.CorrectedOutput == "" {
		return "", errors.New("annotation item has no corrected output; set corrected_output before promoting")
	}

	// Must have privacy review
	if !item.PrivacyReviewed {
		return "", errors.New("privacy review required; set privacy_reviewed=true before promoting")
	}

	// Get run input
	input, _, err := s.runProvider.GetRunInput(ctx, item.RunID)
	if err != nil {
		return "", fmt.Errorf("get run input: %w", err)
	}

	// Build structured metadata
	metadata := map[string]any{
		"source":                    "annotation",
		"source_annotation_item_id": itemID,
		"source_run_id":             item.RunID,
		"source_trace_id":           item.TraceID,
		"source_agent_name":         item.AgentName,
		"failure_category":          item.FailureCategory,
		"privacy_reviewed":          item.PrivacyReviewed,
		"missing_trace_id":          item.TraceID == "",
	}

	// Include expected_tool_path if set
	if item.ExpectedToolPath != "" {
		var toolPath any
		if err := json.Unmarshal([]byte(item.ExpectedToolPath), &toolPath); err == nil {
			metadata["expected_tool_path"] = toolPath
		} else {
			metadata["expected_tool_path"] = item.ExpectedToolPath
		}
	}

	// Include eval scores if available
	if item.Scores != "" && item.Scores != "[]" {
		var parsedScores any
		if err := json.Unmarshal([]byte(item.Scores), &parsedScores); err == nil {
			metadata["eval_scores"] = parsedScores
		}
	}

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("marshal metadata: %w", err)
	}

	// Create dataset case
	caseID, err := s.datasetCreator.AddCase(ctx, datasetID, input, item.CorrectedOutput, string(metadataJSON))
	if err != nil {
		return "", fmt.Errorf("create dataset case: %w", err)
	}

	// Mark item as promoted
	_, err = s.q.UpdateItemStatus(ctx, itemID, "promoted")
	if err != nil {
		return "", fmt.Errorf("update item status: %w", err)
	}

	return caseID, nil
}
