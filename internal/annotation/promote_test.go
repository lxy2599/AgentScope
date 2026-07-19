package annotation

import (
	"context"
	"errors"
	"testing"
)

// ---- Fake Dataset Creator ----

type fakeDatasetCreator struct {
	cases []datasetCaseRecord
}

type datasetCaseRecord struct {
	datasetID string
	input     string
	expected  string
	metadata  string
}

func (f *fakeDatasetCreator) AddCase(ctx context.Context, datasetID string, input, expected, metadata string) (string, error) {
	if datasetID == "" {
		return "", errors.New("datasetID is required")
	}
	f.cases = append(f.cases, datasetCaseRecord{
		datasetID: datasetID,
		input:     input,
		expected:  expected,
		metadata:  metadata,
	})
	return "case-" + itoa2(len(f.cases)), nil
}

// ---- Fake Run Provider ----

type fakeRunProvider struct {
	runs map[string]runRecord
}

type runRecord struct {
	id      string
	input   string
	traceID string
}

func newFakeRunProvider() *fakeRunProvider {
	return &fakeRunProvider{runs: make(map[string]runRecord)}
}

func (f *fakeRunProvider) GetRunInput(ctx context.Context, runID string) (string, string, error) {
	r, ok := f.runs[runID]
	if !ok {
		return "", "", errors.New("run not found")
	}
	return r.input, r.traceID, nil
}

func itoa2(i int) string {
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

// ---- Tests ----

func TestPromoteToDataset_Success(t *testing.T) {
	q := newFakeQuerier()
	dc := &fakeDatasetCreator{}
	rp := newFakeRunProvider()
	rp.runs["run-1"] = runRecord{id: "run-1", input: `{"query":"hello"}`, traceID: "trace-1"}

	svc := NewService(q)
	svc.SetPromoteDeps(dc, rp)

	// Create queue and item
	queue, _ := svc.CreateQueue(context.Background(), CreateQueueParams{
		ProjectID: "proj-1", Name: "test",
	})
	item, _ := svc.CreateItem(context.Background(), CreateItemParams{
		QueueID: queue.ID, RunID: "run-1", TraceID: "trace-1",
	})

	// Set corrected output
	item, err := svc.UpdateItem(context.Background(), item.ID, UpdateItemParams{
		CorrectedOutput:  "correct answer",
		ExpectedToolPath: `[{"tool":"search","args":{"q":"test"}}]`,
		Comment:          "fixed",
	})
	if err != nil {
		t.Fatalf("UpdateItem() error = %v", err)
	}

	// Promote to dataset
	promotedCaseID, err := svc.PromoteToDataset(context.Background(), item.ID, "ds-1")
	if err != nil {
		t.Fatalf("PromoteToDataset() error = %v", err)
	}
	if promotedCaseID == "" {
		t.Fatal("PromoteToDataset() returned empty case ID")
	}

	// Verify dataset case was created
	if len(dc.cases) != 1 {
		t.Fatalf("expected 1 dataset case, got %d", len(dc.cases))
	}
	if dc.cases[0].datasetID != "ds-1" {
		t.Errorf("dataset_id = %q, want %q", dc.cases[0].datasetID, "ds-1")
	}
	if dc.cases[0].input != `{"query":"hello"}` {
		t.Errorf("input = %q", dc.cases[0].input)
	}
	if dc.cases[0].expected != "correct answer" {
		t.Errorf("expected = %q", dc.cases[0].expected)
	}

	// Verify item status changed to promoted
	promotedItem, _ := svc.GetItem(context.Background(), item.ID)
	if promotedItem.Status != "promoted" {
		t.Errorf("item status = %q, want promoted", promotedItem.Status)
	}
}

func TestPromoteToDataset_DuplicatePromotion(t *testing.T) {
	q := newFakeQuerier()
	dc := &fakeDatasetCreator{}
	rp := newFakeRunProvider()
	rp.runs["run-1"] = runRecord{id: "run-1", input: `{"query":"hello"}`, traceID: "trace-1"}

	svc := NewService(q)
	svc.SetPromoteDeps(dc, rp)

	queue, _ := svc.CreateQueue(context.Background(), CreateQueueParams{
		ProjectID: "proj-1", Name: "test",
	})
	item, _ := svc.CreateItem(context.Background(), CreateItemParams{
		QueueID: queue.ID, RunID: "run-1",
	})

	// Mark as promoted first
	svc.UpdateItemStatus(context.Background(), item.ID, "promoted")

	// Try to promote again
	_, err := svc.PromoteToDataset(context.Background(), item.ID, "ds-1")
	if err == nil {
		t.Fatal("PromoteToDataset() expected error for already promoted item")
	}
}

func TestPromoteToDataset_WithoutCorrectedOutput(t *testing.T) {
	q := newFakeQuerier()
	dc := &fakeDatasetCreator{}
	rp := newFakeRunProvider()
	rp.runs["run-1"] = runRecord{id: "run-1", input: `{"query":"hello"}`, traceID: "trace-1"}

	svc := NewService(q)
	svc.SetPromoteDeps(dc, rp)

	queue, _ := svc.CreateQueue(context.Background(), CreateQueueParams{
		ProjectID: "proj-1", Name: "test",
	})
	item, _ := svc.CreateItem(context.Background(), CreateItemParams{
		QueueID: queue.ID, RunID: "run-1",
	})

	// Promote without corrected output
	_, err := svc.PromoteToDataset(context.Background(), item.ID, "ds-1")
	if err == nil {
		t.Fatal("PromoteToDataset() expected error when no corrected output")
	}
}
