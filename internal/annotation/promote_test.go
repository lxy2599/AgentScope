package annotation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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
	id        string
	projectID string
	input     string
	traceID   string
	agentName string
	output    string
	errorVal  string
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

func (f *fakeRunProvider) GetRunDetail(ctx context.Context, runID string) (RunDetail, error) {
	r, ok := f.runs[runID]
	if !ok {
		return RunDetail{}, errors.New("run not found")
	}
	return RunDetail{
		ID:            r.id,
		ProjectID:     r.projectID,
		TraceID:       r.traceID,
		AgentName:     r.agentName,
		InputSummary:  r.input,
		OutputSummary: r.output,
		Error:         r.errorVal,
	}, nil
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
	rp.runs["run-1"] = runRecord{id: "run-1", input: `{"query":"hello"}`, traceID: "trace-1", projectID: "proj-1"}

	svc := NewService(q)
	svc.SetPromoteDeps(dc, rp)

	// Create queue and item
	queue, _ := svc.CreateQueue(context.Background(), CreateQueueParams{
		ProjectID: "proj-1", Name: "test",
	})
	item, _ := svc.CreateItem(context.Background(), CreateItemParams{
		QueueID: queue.ID, RunID: "run-1", TraceID: "trace-1",
	})

	// Set corrected output and privacy_reviewed
	item, err := svc.UpdateItem(context.Background(), item.ID, UpdateItemParams{
		CorrectedOutput:  "correct answer",
		ExpectedToolPath: `[{"tool":"search","args":{"q":"test"}}]`,
		Comment:          "fixed",
		PrivacyReviewed:  true,
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
	rp.runs["run-1"] = runRecord{id: "run-1", input: `{"query":"hello"}`, traceID: "trace-1", projectID: "proj-1"}

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
	rp.runs["run-1"] = runRecord{id: "run-1", input: `{"query":"hello"}`, traceID: "trace-1", projectID: "proj-1"}

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

func TestPromoteToDataset_JSONEscape(t *testing.T) {
	q := newFakeQuerier()
	dc := &fakeDatasetCreator{}
	rp := newFakeRunProvider()
	rp.runs["run-1"] = runRecord{id: "run-1", input: `{"query":"hello"}`, traceID: "trace-1", projectID: "proj-1"}

	svc := NewService(q)
	svc.SetPromoteDeps(dc, rp)

	queue, _ := svc.CreateQueue(context.Background(), CreateQueueParams{
		ProjectID: "proj-1", Name: "test",
	})
	item, _ := svc.CreateItem(context.Background(), CreateItemParams{
		QueueID: queue.ID, RunID: "run-1", TraceID: "trace-1",
		Scores: `[{"metric_name":"efficiency","score":1.0}]`,
	})

	// corrected_output containing quotes, newlines, backslashes
	corrected := "He said \"hello\"\nand \\escaped\\"
	svc.UpdateItem(context.Background(), item.ID, UpdateItemParams{
		CorrectedOutput: corrected,
		PrivacyReviewed: true,
	})

	caseID, err := svc.PromoteToDataset(context.Background(), item.ID, "ds-1")
	if err != nil {
		t.Fatalf("PromoteToDataset() error = %v", err)
	}
	if caseID == "" {
		t.Fatal("expected non-empty caseID")
	}

	// Verify dataset case
	if len(dc.cases) != 1 {
		t.Fatalf("expected 1 dataset case, got %d", len(dc.cases))
	}

	// Verify the expected (corrected_output) is passed through correctly
	if dc.cases[0].expected != corrected {
		t.Errorf("expected = %q, want %q", dc.cases[0].expected, corrected)
	}

	// Verify metadata.eval_scores is structured JSON, not escaped string
	var metadata map[string]any
	if err := json.Unmarshal([]byte(dc.cases[0].metadata), &metadata); err != nil {
		t.Fatalf("metadata is not valid JSON: %v", err)
	}
	evalScores, ok := metadata["eval_scores"]
	if !ok {
		t.Fatal("metadata missing eval_scores key")
	}
	switch evalScores.(type) {
	case string:
		t.Errorf("eval_scores is a string (double-escaped), want structured JSON array")
	case []any:
		// This is correct - it's a proper JSON array
	default:
		t.Logf("eval_scores type = %T (may be OK if it's a JSON array)", evalScores)
	}
}

func TestPromoteToDataset_NonJSONInput(t *testing.T) {
	q := newFakeQuerier()
	dc := &fakeDatasetCreator{}
	rp := newFakeRunProvider()
	rp.runs["run-1"] = runRecord{id: "run-1", input: "plain text input", traceID: "trace-1", projectID: "proj-1"}

	svc := NewService(q)
	svc.SetPromoteDeps(dc, rp)

	queue, _ := svc.CreateQueue(context.Background(), CreateQueueParams{
		ProjectID: "proj-1", Name: "test",
	})
	item, _ := svc.CreateItem(context.Background(), CreateItemParams{
		QueueID: queue.ID, RunID: "run-1", TraceID: "trace-1",
	})
	svc.UpdateItem(context.Background(), item.ID, UpdateItemParams{
		CorrectedOutput: "corrected",
		PrivacyReviewed: true,
	})

	_, err := svc.PromoteToDataset(context.Background(), item.ID, "ds-1")
	if err != nil {
		t.Fatalf("PromoteToDataset() error = %v", err)
	}

	if len(dc.cases) != 1 {
		t.Fatalf("expected 1 dataset case, got %d", len(dc.cases))
	}

	// Verify input is preserved (JSON wrapping is handled by promote.go adapter)
	if dc.cases[0].input != "plain text input" {
		t.Errorf("input = %q, want %q", dc.cases[0].input, "plain text input")
	}
}

func TestPromoteToDataset_StructuredMetadata(t *testing.T) {
	q := newFakeQuerier()
	dc := &fakeDatasetCreator{}
	rp := newFakeRunProvider()
	rp.runs["run-1"] = runRecord{id: "run-1", input: `{"query":"hello"}`, traceID: "trace-1", projectID: "proj-1"}

	svc := NewService(q)
	svc.SetPromoteDeps(dc, rp)

	queue, _ := svc.CreateQueue(context.Background(), CreateQueueParams{
		ProjectID: "proj-1", Name: "test",
	})
	item, _ := svc.CreateItem(context.Background(), CreateItemParams{
		QueueID: queue.ID, RunID: "run-1", TraceID: "trace-1",
		Scores: `[{"metric_name":"tool_correctness","score":0.0,"passed":false}]`,
	})
	svc.UpdateItem(context.Background(), item.ID, UpdateItemParams{
		CorrectedOutput: "fixed",
		PrivacyReviewed: true,
	})

	_, err := svc.PromoteToDataset(context.Background(), item.ID, "ds-1")
	if err != nil {
		t.Fatalf("PromoteToDataset() error = %v", err)
	}

	if len(dc.cases) != 1 {
		t.Fatalf("expected 1 dataset case, got %d", len(dc.cases))
	}

	// Verify metadata has the expected structure
	var metadata map[string]any
	if err := json.Unmarshal([]byte(dc.cases[0].metadata), &metadata); err != nil {
		t.Fatalf("metadata is not valid JSON: %v", err)
	}

	// Verify run_id and trace_id
	if metadata["source_run_id"] != "run-1" {
		t.Errorf("metadata.run_id = %v, want run-1", metadata["source_run_id"])
	}
	if metadata["source_trace_id"] != "trace-1" {
		t.Errorf("metadata.trace_id = %v, want trace-1", metadata["source_trace_id"])
	}

	// Verify eval_scores is a structured array, not an escaped string
	scoresVal := metadata["eval_scores"]
	scoresJSON, err := json.Marshal(scoresVal)
	if err != nil {
		t.Fatalf("marshal eval_scores: %v", err)
	}
	// Should contain the original metric_name without being double-escaped
	if !strings.Contains(string(scoresJSON), "tool_correctness") {
		t.Errorf("eval_scores JSON %q should contain raw metric_name 'tool_correctness'", string(scoresJSON))
	}
	// Should NOT have escaped quotes around the array
	if strings.Contains(string(scoresJSON), `\"`) {
		t.Errorf("eval_scores JSON %q should not have double-escaped quotes", string(scoresJSON))
	}
}

func TestPromoteToDataset_RejectDuplicate(t *testing.T) {
	q := newFakeQuerier()
	dc := &fakeDatasetCreator{}
	rp := newFakeRunProvider()
	rp.runs["run-1"] = runRecord{id: "run-1", input: `{"query":"hello"}`, traceID: "trace-1", projectID: "proj-1"}

	svc := NewService(q)
	svc.SetPromoteDeps(dc, rp)

	queue, _ := svc.CreateQueue(context.Background(), CreateQueueParams{
		ProjectID: "proj-1", Name: "test",
	})
	item, _ := svc.CreateItem(context.Background(), CreateItemParams{
		QueueID: queue.ID, RunID: "run-1",
	})
	svc.UpdateItem(context.Background(), item.ID, UpdateItemParams{
		CorrectedOutput: "fixed",
		PrivacyReviewed: true,
	})

	// First promote succeeds
	caseID, err := svc.PromoteToDataset(context.Background(), item.ID, "ds-1")
	if err != nil {
		t.Fatalf("first PromoteToDataset() error = %v", err)
	}
	if caseID == "" {
		t.Fatal("expected non-empty caseID")
	}

	// Second promote returns error
	_, err = svc.PromoteToDataset(context.Background(), item.ID, "ds-1")
	if err == nil {
		t.Fatal("expected error for duplicate promotion")
	}
	if err.Error() != "annotation item already promoted" {
		t.Errorf("error = %q, want 'annotation item already promoted'", err.Error())
	}
}
