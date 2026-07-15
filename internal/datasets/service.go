package datasets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ---- Domain types ----

// Dataset represents a collection of test cases for evaluation.
type Dataset struct {
	ID          string    `json:"id"`
	ProjectID   string    `json:"project_id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// DatasetCase is a single test case within a dataset.
type DatasetCase struct {
	ID        string          `json:"id"`
	DatasetID string          `json:"dataset_id"`
	Input     json.RawMessage `json:"input"`
	Expected  json.RawMessage `json:"expected,omitempty"`
	Metadata  json.RawMessage `json:"metadata"`
	SortOrder int             `json:"sort_order"`
	CreatedAt time.Time       `json:"created_at"`
}

// ---- Querier interface ----

// Querier defines the database operations the datasets service needs.
type Querier interface {
	CreateDataset(ctx context.Context, projectID, name, description string) (Dataset, error)
	AddCase(ctx context.Context, datasetID string, input, expected, metadata json.RawMessage, sortOrder int) (DatasetCase, error)
	ListCases(ctx context.Context, datasetID string) ([]DatasetCase, error)
	GetDataset(ctx context.Context, datasetID string) (Dataset, error)
	CountCases(ctx context.Context, datasetID string) (int, error)
}

// ---- Service ----

// Service provides dataset operations.
type Service struct {
	q Querier
}

// NewService creates a new datasets Service.
func NewService(q Querier) *Service {
	return &Service{q: q}
}

// CreateDataset creates a new dataset.
func (s *Service) CreateDataset(ctx context.Context, projectID, name, description string) (Dataset, error) {
	if projectID == "" {
		return Dataset{}, errors.New("projectID is required")
	}
	if name == "" {
		return Dataset{}, errors.New("name is required")
	}
	return s.q.CreateDataset(ctx, projectID, name, description)
}

// AddCase adds a test case to a dataset.
func (s *Service) AddCase(ctx context.Context, datasetID string, input, expected, metadata json.RawMessage) (DatasetCase, error) {
	if datasetID == "" {
		return DatasetCase{}, errors.New("datasetID is required")
	}
	if len(input) == 0 {
		return DatasetCase{}, errors.New("input is required")
	}

	// Compute sort order as count of existing cases + 1
	count, err := s.q.CountCases(ctx, datasetID)
	if err != nil {
		return DatasetCase{}, fmt.Errorf("count cases: %w", err)
	}
	sortOrder := count + 1

	if metadata == nil {
		metadata = json.RawMessage("{}")
	}

	return s.q.AddCase(ctx, datasetID, input, expected, metadata, sortOrder)
}

// ListCases returns all cases for a dataset.
func (s *Service) ListCases(ctx context.Context, datasetID string) ([]DatasetCase, error) {
	if datasetID == "" {
		return nil, errors.New("datasetID is required")
	}
	return s.q.ListCases(ctx, datasetID)
}

// GetDataset returns a dataset by ID.
func (s *Service) GetDataset(ctx context.Context, datasetID string) (Dataset, error) {
	if datasetID == "" {
		return Dataset{}, errors.New("datasetID is required")
	}
	return s.q.GetDataset(ctx, datasetID)
}
