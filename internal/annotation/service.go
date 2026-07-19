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
	QueueID  string
	RunID    string
	TraceID  string
	Priority int
	Scores   string
}

// UpdateItemParams contains fields for updating an annotation item.
type UpdateItemParams struct {
	Status           string
	CorrectedOutput  string
	ExpectedToolPath string
	Comment          string
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

// RunProvider provides run input data.
type RunProvider interface {
	GetRunInput(ctx context.Context, runID string) (input, traceID string, err error)
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

	// Get run input
	input, traceID, err := s.runProvider.GetRunInput(ctx, item.RunID)
	if err != nil {
		return "", fmt.Errorf("get run input: %w", err)
	}

	// Build metadata with structured eval_scores
	var evalScoresValue any = item.Scores
	var parsedScores any
	if err := json.Unmarshal([]byte(item.Scores), &parsedScores); err == nil {
		evalScoresValue = parsedScores
	}

	metadata := map[string]any{
		"run_id":      item.RunID,
		"trace_id":    traceID,
		"eval_scores": evalScoresValue,
		"item_id":     itemID,
		"source":      "annotation_promote",
	}
	if item.ExpectedToolPath != "" {
		var toolPath any
		if err := json.Unmarshal([]byte(item.ExpectedToolPath), &toolPath); err == nil {
			metadata["expected_tool_path"] = toolPath
		} else {
			metadata["expected_tool_path"] = item.ExpectedToolPath
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
