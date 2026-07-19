package annotation

import (
	"context"
	"errors"
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

// ---- Querier interface ----

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

// ---- Service ----

type Service struct {
	q Querier
}

func NewService(q Querier) *Service {
	return &Service{q: q}
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
