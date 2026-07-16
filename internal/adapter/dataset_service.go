package adapter

import (
	"context"
	"encoding/json"

	"AgentScope/internal/datasets"
	"AgentScope/internal/httpapi"
)

// HTTPDatasetsService implements httpapi.DatasetsService using *datasets.Service.
type HTTPDatasetsService struct {
	svc *datasets.Service
}

// compile-time check
var _ httpapi.DatasetsService = (*HTTPDatasetsService)(nil)

// NewHTTPDatasetsService creates a new HTTPDatasetsService.
func NewHTTPDatasetsService(svc *datasets.Service) *HTTPDatasetsService {
	return &HTTPDatasetsService{svc: svc}
}

func (h *HTTPDatasetsService) CreateDataset(ctx context.Context, projectID, name, description string) (httpapi.Dataset, error) {
	d, err := h.svc.CreateDataset(ctx, projectID, name, description)
	if err != nil {
		return httpapi.Dataset{}, err
	}
	return httpapi.Dataset{
		ID:          d.ID,
		ProjectID:   d.ProjectID,
		Name:        d.Name,
		Description: d.Description,
		CreatedAt:   d.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}, nil
}

func (h *HTTPDatasetsService) AddCase(ctx context.Context, datasetID string, input, expected, metadata json.RawMessage) (httpapi.DatasetCase, error) {
	c, err := h.svc.AddCase(ctx, datasetID, input, expected, metadata)
	if err != nil {
		return httpapi.DatasetCase{}, err
	}
	return httpapi.DatasetCase{
		ID:        c.ID,
		DatasetID: c.DatasetID,
		Input:     c.Input,
		Expected:  c.Expected,
		Metadata:  c.Metadata,
		SortOrder: c.SortOrder,
		CreatedAt: c.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}, nil
}

func (h *HTTPDatasetsService) ListCases(ctx context.Context, datasetID string) ([]httpapi.DatasetCase, error) {
	cases, err := h.svc.ListCases(ctx, datasetID)
	if err != nil {
		return nil, err
	}
	result := make([]httpapi.DatasetCase, 0, len(cases))
	for _, c := range cases {
		result = append(result, httpapi.DatasetCase{
			ID:        c.ID,
			DatasetID: c.DatasetID,
			Input:     c.Input,
			Expected:  c.Expected,
			Metadata:  c.Metadata,
			SortOrder: c.SortOrder,
			CreatedAt: c.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	return result, nil
}

func (h *HTTPDatasetsService) GetDataset(ctx context.Context, datasetID string) (httpapi.Dataset, error) {
	d, err := h.svc.GetDataset(ctx, datasetID)
	if err != nil {
		return httpapi.Dataset{}, err
	}
	return httpapi.Dataset{
		ID:          d.ID,
		ProjectID:   d.ProjectID,
		Name:        d.Name,
		Description: d.Description,
		CreatedAt:   d.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}, nil
}

func (h *HTTPDatasetsService) ListDatasets(ctx context.Context, projectID string) ([]httpapi.Dataset, error) {
	datasets, err := h.svc.ListDatasets(ctx, projectID)
	if err != nil {
		return nil, err
	}
	result := make([]httpapi.Dataset, 0, len(datasets))
	for _, d := range datasets {
		result = append(result, httpapi.Dataset{
			ID:          d.ID,
			ProjectID:   d.ProjectID,
			Name:        d.Name,
			Description: d.Description,
			CreatedAt:   d.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	return result, nil
}
