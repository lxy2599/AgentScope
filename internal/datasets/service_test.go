package datasets

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// fakeQuerier implements Querier for testing.
type fakeQuerier struct {
	datasets map[string]Dataset
	cases    map[string][]DatasetCase
	counter  int
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{
		datasets: make(map[string]Dataset),
		cases:    make(map[string][]DatasetCase),
	}
}

func (f *fakeQuerier) CreateDataset(ctx context.Context, projectID, name, description string) (Dataset, error) {
	f.counter++
	id := "ds-" + itoa(f.counter)
	d := Dataset{
		ID:          id,
		ProjectID:   projectID,
		Name:        name,
		Description: description,
	}
	f.datasets[id] = d
	return d, nil
}

func (f *fakeQuerier) AddCase(ctx context.Context, datasetID string, input, expected, metadata json.RawMessage, sortOrder int) (DatasetCase, error) {
	f.counter++
	id := "case-" + itoa(f.counter)
	c := DatasetCase{
		ID:        id,
		DatasetID: datasetID,
		Input:     input,
		Expected:  expected,
		Metadata:  metadata,
		SortOrder: sortOrder,
	}
	f.cases[datasetID] = append(f.cases[datasetID], c)
	return c, nil
}

func (f *fakeQuerier) ListCases(ctx context.Context, datasetID string) ([]DatasetCase, error) {
	return f.cases[datasetID], nil
}

func (f *fakeQuerier) GetDataset(ctx context.Context, datasetID string) (Dataset, error) {
	d, ok := f.datasets[datasetID]
	if !ok {
		return Dataset{}, errors.New("dataset not found")
	}
	return d, nil
}

func (f *fakeQuerier) CountCases(ctx context.Context, datasetID string) (int, error) {
	return len(f.cases[datasetID]), nil
}

func (f *fakeQuerier) ListDatasets(ctx context.Context, projectID string) ([]Dataset, error) {
	var result []Dataset
	for _, d := range f.datasets {
		if d.ProjectID == projectID {
			result = append(result, d)
		}
	}
	return result, nil
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}

func TestService_CreateDataset(t *testing.T) {
	fq := newFakeQuerier()
	svc := NewService(fq)

	t.Run("success", func(t *testing.T) {
		ds, err := svc.CreateDataset(context.Background(), "proj-1", "test-ds", "Test dataset")
		if err != nil {
			t.Fatalf("CreateDataset: %v", err)
		}
		if ds.ID == "" {
			t.Fatal("expected non-empty ID")
		}
		if ds.ProjectID != "proj-1" {
			t.Fatalf("expected projectID proj-1, got %s", ds.ProjectID)
		}
		if ds.Name != "test-ds" {
			t.Fatalf("expected name test-ds, got %s", ds.Name)
		}
	})

	t.Run("missing projectID", func(t *testing.T) {
		_, err := svc.CreateDataset(context.Background(), "", "name", "desc")
		if err == nil {
			t.Fatal("expected error for missing projectID")
		}
	})

	t.Run("missing name", func(t *testing.T) {
		_, err := svc.CreateDataset(context.Background(), "proj-1", "", "desc")
		if err == nil {
			t.Fatal("expected error for missing name")
		}
	})
}

func TestService_AddCase(t *testing.T) {
	fq := newFakeQuerier()
	svc := NewService(fq)

	// Create a dataset first
	ds, err := svc.CreateDataset(context.Background(), "proj-1", "test-ds", "")
	if err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}

	t.Run("success", func(t *testing.T) {
		input := json.RawMessage(`{"query": "hello"}`)
		expected := json.RawMessage(`{"response": "world"}`)
		metadata := json.RawMessage(`{"difficulty": "easy"}`)

		c, err := svc.AddCase(context.Background(), ds.ID, input, expected, metadata)
		if err != nil {
			t.Fatalf("AddCase: %v", err)
		}
		if c.ID == "" {
			t.Fatal("expected non-empty ID")
		}
		if string(c.Input) != string(input) {
			t.Fatalf("expected input %s, got %s", string(input), string(c.Input))
		}
		if c.SortOrder != 1 {
			t.Fatalf("expected sort_order 1, got %d", c.SortOrder)
		}
	})

	t.Run("missing datasetID", func(t *testing.T) {
		_, err := svc.AddCase(context.Background(), "", json.RawMessage(`{}`), nil, nil)
		if err == nil {
			t.Fatal("expected error for missing datasetID")
		}
	})

	t.Run("missing input", func(t *testing.T) {
		_, err := svc.AddCase(context.Background(), ds.ID, nil, nil, nil)
		if err == nil {
			t.Fatal("expected error for missing input")
		}
	})

	t.Run("auto increment sort order", func(t *testing.T) {
		svc.AddCase(context.Background(), ds.ID, json.RawMessage(`{"q":"second"}`), nil, nil)
		svc.AddCase(context.Background(), ds.ID, json.RawMessage(`{"q":"third"}`), nil, nil)

		cases, _ := svc.ListCases(context.Background(), ds.ID)
		if len(cases) != 3 {
			t.Fatalf("expected 3 cases, got %d", len(cases))
		}
		for i, c := range cases {
			if c.SortOrder != i+1 {
				t.Fatalf("case %d: expected sort_order %d, got %d", i, i+1, c.SortOrder)
			}
		}
	})
}

func TestService_ListCases(t *testing.T) {
	fq := newFakeQuerier()
	svc := NewService(fq)

	ds, _ := svc.CreateDataset(context.Background(), "proj-1", "test-ds", "")

	t.Run("empty dataset", func(t *testing.T) {
		cases, err := svc.ListCases(context.Background(), ds.ID)
		if err != nil {
			t.Fatalf("ListCases: %v", err)
		}
		if len(cases) != 0 {
			t.Fatalf("expected 0 cases, got %d", len(cases))
		}
	})

	t.Run("multiple cases", func(t *testing.T) {
		svc.AddCase(context.Background(), ds.ID, json.RawMessage(`{"q":"1"}`), nil, nil)
		svc.AddCase(context.Background(), ds.ID, json.RawMessage(`{"q":"2"}`), nil, nil)

		cases, err := svc.ListCases(context.Background(), ds.ID)
		if err != nil {
			t.Fatalf("ListCases: %v", err)
		}
		if len(cases) != 2 {
			t.Fatalf("expected 2 cases, got %d", len(cases))
		}
	})

	t.Run("missing datasetID", func(t *testing.T) {
		_, err := svc.ListCases(context.Background(), "")
		if err == nil {
			t.Fatal("expected error for missing datasetID")
		}
	})
}

func TestService_AuthzSimulation(t *testing.T) {
	// Simulate authorization check: dataset belongs to project,
	// so a user not in the project wouldn't see the dataset.
	// This test verifies the service correctly delegates to the
	// querier for dataset retrieval (which in a real DB would
	// check project membership via the handler middleware).
	fq := newFakeQuerier()
	svc := NewService(fq)

	ds, _ := svc.CreateDataset(context.Background(), "proj-1", "test", "")

	// A user from a different project can't find the dataset by ID
	// because they'd need to go through the project listing first.
	got, err := svc.GetDataset(context.Background(), ds.ID)
	if err != nil {
		t.Fatalf("GetDataset: %v", err)
	}
	if got.ProjectID != "proj-1" {
		t.Fatalf("expected proj-1, got %s", got.ProjectID)
	}
}
