package adapter

import (
	"context"
	"encoding/json"

	"AgentScope/internal/datasets"
	"AgentScope/internal/store"
)

// DatasetQuerier implements datasets.Querier using store.Queries.
type DatasetQuerier struct {
	q *store.Queries
}

// compile-time check
var _ datasets.Querier = (*DatasetQuerier)(nil)

// NewDatasetQuerier creates a new DatasetQuerier.
func NewDatasetQuerier(q *store.Queries) *DatasetQuerier {
	return &DatasetQuerier{q: q}
}

func (d *DatasetQuerier) CreateDataset(ctx context.Context, projectID, name, description string) (datasets.Dataset, error) {
	storeDataset, err := d.q.CreateDataset(ctx, store.CreateDatasetParams{
		ProjectID:   parsePGUUID(projectID),
		Name:        name,
		Description: pgText(description),
	})
	if err != nil {
		return datasets.Dataset{}, err
	}
	return datasets.Dataset{
		ID:          uuidToString(storeDataset.ID.Bytes),
		ProjectID:   uuidToString(storeDataset.ProjectID.Bytes),
		Name:        storeDataset.Name,
		Description: strOrZero(storeDataset.Description),
		CreatedAt:   storeDataset.CreatedAt.Time,
	}, nil
}

func (d *DatasetQuerier) AddCase(ctx context.Context, datasetID string, input, expected, metadata json.RawMessage, sortOrder int) (datasets.DatasetCase, error) {
	var inputBytes, expectedBytes, metadataBytes []byte
	if input != nil {
		inputBytes = []byte(input)
	}
	if expected != nil {
		expectedBytes = []byte(expected)
	}
	if metadata != nil {
		metadataBytes = []byte(metadata)
	}

	storeCase, err := d.q.CreateDatasetCase(ctx, store.CreateDatasetCaseParams{
		DatasetID: parsePGUUID(datasetID),
		Input:     inputBytes,
		Expected:  expectedBytes,
		Metadata:  metadataBytes,
		SortOrder: int32(sortOrder),
	})
	if err != nil {
		return datasets.DatasetCase{}, err
	}
	return storeCaseToDomain(storeCase), nil
}

func (d *DatasetQuerier) ListCases(ctx context.Context, datasetID string) ([]datasets.DatasetCase, error) {
	storeCases, err := d.q.ListDatasetCasesByDataset(ctx, parsePGUUID(datasetID))
	if err != nil {
		return nil, err
	}
	result := make([]datasets.DatasetCase, 0, len(storeCases))
	for _, sc := range storeCases {
		result = append(result, storeCaseToDomain(sc))
	}
	return result, nil
}

func (d *DatasetQuerier) GetDataset(ctx context.Context, datasetID string) (datasets.Dataset, error) {
	storeDataset, err := d.q.GetDatasetByID(ctx, parsePGUUID(datasetID))
	if err != nil {
		return datasets.Dataset{}, err
	}
	return datasets.Dataset{
		ID:          uuidToString(storeDataset.ID.Bytes),
		ProjectID:   uuidToString(storeDataset.ProjectID.Bytes),
		Name:        storeDataset.Name,
		Description: strOrZero(storeDataset.Description),
		CreatedAt:   storeDataset.CreatedAt.Time,
	}, nil
}

func (d *DatasetQuerier) ListDatasets(ctx context.Context, projectID string) ([]datasets.Dataset, error) {
	storeDatasets, err := d.q.ListDatasetsByProject(ctx, parsePGUUID(projectID))
	if err != nil {
		return nil, err
	}
	result := make([]datasets.Dataset, 0, len(storeDatasets))
	for _, sd := range storeDatasets {
		result = append(result, datasets.Dataset{
			ID:          uuidToString(sd.ID.Bytes),
			ProjectID:   uuidToString(sd.ProjectID.Bytes),
			Name:        sd.Name,
			Description: strOrZero(sd.Description),
			CreatedAt:   sd.CreatedAt.Time,
		})
	}
	return result, nil
}

func (d *DatasetQuerier) CountCases(ctx context.Context, datasetID string) (int, error) {
	count, err := d.q.CountDatasetCases(ctx, parsePGUUID(datasetID))
	if err != nil {
		return 0, err
	}
	return int(count), nil
}

func storeCaseToDomain(sc store.DatasetCase) datasets.DatasetCase {
	return datasets.DatasetCase{
		ID:        uuidToString(sc.ID.Bytes),
		DatasetID: uuidToString(sc.DatasetID.Bytes),
		Input:     json.RawMessage(sc.Input),
		Expected:  json.RawMessage(sc.Expected),
		Metadata:  json.RawMessage(sc.Metadata),
		SortOrder: int(sc.SortOrder),
		CreatedAt: sc.CreatedAt.Time,
	}
}
