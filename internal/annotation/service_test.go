package annotation

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// ---- Fake Querier ----

type fakeQuerier struct {
	queues map[string]AnnotationQueue
	items  map[string]AnnotationItem
	qSeq   int
	iSeq   int
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{
		queues: make(map[string]AnnotationQueue),
		items:  make(map[string]AnnotationItem),
	}
}

func (f *fakeQuerier) CreateQueue(ctx context.Context, params CreateQueueParams) (AnnotationQueue, error) {
	f.qSeq++
	id := "queue-" + itoa(f.qSeq)
	q := AnnotationQueue{
		ID:          id,
		ProjectID:   params.ProjectID,
		Name:        params.Name,
		Description: params.Description,
	}
	f.queues[id] = q
	return q, nil
}

func (f *fakeQuerier) ListQueues(ctx context.Context, projectID string) ([]AnnotationQueue, error) {
	var result []AnnotationQueue
	for _, q := range f.queues {
		if q.ProjectID == projectID {
			result = append(result, q)
		}
	}
	return result, nil
}

func (f *fakeQuerier) GetQueue(ctx context.Context, queueID string) (AnnotationQueue, error) {
	q, ok := f.queues[queueID]
	if !ok {
		return AnnotationQueue{}, errors.New("queue not found")
	}
	return q, nil
}

func (f *fakeQuerier) DeleteQueue(ctx context.Context, queueID, projectID string) error {
	q, ok := f.queues[queueID]
	if !ok {
		return errors.New("queue not found")
	}
	if q.ProjectID != projectID {
		return errors.New("not found in project")
	}
	delete(f.queues, queueID)
	return nil
}

func (f *fakeQuerier) CreateItem(ctx context.Context, params CreateItemParams) (AnnotationItem, error) {
	f.iSeq++
	id := "item-" + itoa(f.iSeq)
	item := AnnotationItem{
		ID:              id,
		QueueID:         params.QueueID,
		RunID:           params.RunID,
		TraceID:         params.TraceID,
		Status:          "pending",
		Priority:        params.Priority,
		Scores:          params.Scores,
		AgentName:       params.AgentName,
		InputSummary:    params.InputSummary,
		OutputSummary:   params.OutputSummary,
		Error:           params.Error,
		FailureCategory: params.FailureCategory,
		PrivacyReviewed: params.PrivacyReviewed,
		ReviewerNote:    params.ReviewerNote,
	}
	// Look up queue for project_id
	for _, q := range f.queues {
		if q.ID == params.QueueID {
			item.ProjectID = q.ProjectID
		}
	}
	f.items[id] = item
	return item, nil
}

func (f *fakeQuerier) ListItemsByQueue(ctx context.Context, queueID string) ([]AnnotationItem, error) {
	var result []AnnotationItem
	for _, item := range f.items {
		if item.QueueID == queueID {
			result = append(result, item)
		}
	}
	return result, nil
}

func (f *fakeQuerier) GetItem(ctx context.Context, itemID string) (AnnotationItem, error) {
	item, ok := f.items[itemID]
	if !ok {
		return AnnotationItem{}, errors.New("item not found")
	}
	return item, nil
}

func (f *fakeQuerier) UpdateItem(ctx context.Context, itemID string, params UpdateItemParams) (AnnotationItem, error) {
	item, ok := f.items[itemID]
	if !ok {
		return AnnotationItem{}, errors.New("item not found")
	}
	if params.Status != "" {
		item.Status = params.Status
	}
	if params.CorrectedOutput != "" {
		item.CorrectedOutput = params.CorrectedOutput
	}
	if params.ExpectedToolPath != "" {
		item.ExpectedToolPath = params.ExpectedToolPath
	}
	if params.Comment != "" {
		item.Comment = params.Comment
	}
	if params.FailureCategory != "" {
		item.FailureCategory = params.FailureCategory
	}
	item.PrivacyReviewed = params.PrivacyReviewed
	if params.ReviewerNote != "" {
		item.ReviewerNote = params.ReviewerNote
	}
	f.items[itemID] = item
	return item, nil
}

func (f *fakeQuerier) UpdateItemStatus(ctx context.Context, itemID, status string) (AnnotationItem, error) {
	item, ok := f.items[itemID]
	if !ok {
		return AnnotationItem{}, errors.New("item not found")
	}
	item.Status = status
	f.items[itemID] = item
	return item, nil
}

func (f *fakeQuerier) ListItemsByProject(ctx context.Context, projectID string) ([]AnnotationItem, error) {
	var result []AnnotationItem
	for _, item := range f.items {
		if item.ProjectID == projectID {
			result = append(result, item)
		}
	}
	return result, nil
}

func (f *fakeQuerier) SubmitGold(
	ctx context.Context,
	itemID, projectID, goldScores, annotatorID string,
) (AnnotationItem, error) {
	item, ok := f.items[itemID]
	if !ok || item.ProjectID != projectID {
		return AnnotationItem{}, errors.New("item not found")
	}
	item.GoldScores = goldScores
	item.IsGold = true
	item.AnnotatorID = annotatorID
	item.Status = "completed"
	f.items[itemID] = item
	return item, nil
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

// ---- Tests ----

func TestCreateQueue(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	queue, err := svc.CreateQueue(context.Background(), CreateQueueParams{
		ProjectID:   "proj-1",
		Name:        "my-queue",
		Description: "test queue",
	})
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}
	if queue.Name != "my-queue" {
		t.Errorf("name = %q, want %q", queue.Name, "my-queue")
	}
	if queue.ProjectID != "proj-1" {
		t.Errorf("project_id = %q, want %q", queue.ProjectID, "proj-1")
	}
}

func TestCreateQueue_Validation(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	_, err := svc.CreateQueue(context.Background(), CreateQueueParams{Name: "test"})
	if err == nil {
		t.Fatal("expected error for missing projectID")
	}

	_, err = svc.CreateQueue(context.Background(), CreateQueueParams{ProjectID: "proj-1"})
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestListQueues(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	svc.CreateQueue(context.Background(), CreateQueueParams{ProjectID: "proj-1", Name: "q1"})
	svc.CreateQueue(context.Background(), CreateQueueParams{ProjectID: "proj-1", Name: "q2"})

	queues, err := svc.ListQueues(context.Background(), "proj-1")
	if err != nil {
		t.Fatalf("ListQueues() error = %v", err)
	}
	if len(queues) != 2 {
		t.Fatalf("got %d queues, want 2", len(queues))
	}
}

func TestCreateItem(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	// Create queue first
	queue, _ := svc.CreateQueue(context.Background(), CreateQueueParams{
		ProjectID: "proj-1", Name: "test",
	})

	item, err := svc.CreateItem(context.Background(), CreateItemParams{
		QueueID: queue.ID,
		RunID:   "run-1",
	})
	if err != nil {
		t.Fatalf("CreateItem() error = %v", err)
	}
	if item.QueueID != queue.ID {
		t.Errorf("queue_id = %q, want %q", item.QueueID, queue.ID)
	}
	if item.RunID != "run-1" {
		t.Errorf("run_id = %q, want %q", item.RunID, "run-1")
	}
	if item.Status != "pending" {
		t.Errorf("status = %q, want pending", item.Status)
	}
}

func TestCreateItem_Validation(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	_, err := svc.CreateItem(context.Background(), CreateItemParams{RunID: "run-1"})
	if err == nil {
		t.Fatal("expected error for missing queueID")
	}

	_, err = svc.CreateItem(context.Background(), CreateItemParams{QueueID: "queue-1"})
	if err == nil {
		t.Fatal("expected error for missing runID")
	}
}

func TestUpdateItem(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	queue, _ := svc.CreateQueue(context.Background(), CreateQueueParams{
		ProjectID: "proj-1", Name: "test",
	})
	item, _ := svc.CreateItem(context.Background(), CreateItemParams{
		QueueID: queue.ID, RunID: "run-1",
	})

	updated, err := svc.UpdateItem(context.Background(), item.ID, UpdateItemParams{
		Status:          "completed",
		CorrectedOutput: "correct answer",
		Comment:         "looks good",
	})
	if err != nil {
		t.Fatalf("UpdateItem() error = %v", err)
	}
	if updated.Status != "completed" {
		t.Errorf("status = %q, want completed", updated.Status)
	}
	if updated.CorrectedOutput != "correct answer" {
		t.Errorf("corrected_output = %q", updated.CorrectedOutput)
	}
}

func TestUpdateItem_InvalidStatus(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	queue, _ := svc.CreateQueue(context.Background(), CreateQueueParams{
		ProjectID: "proj-1", Name: "test",
	})
	item, _ := svc.CreateItem(context.Background(), CreateItemParams{
		QueueID: queue.ID, RunID: "run-1",
	})

	_, err := svc.UpdateItemStatus(context.Background(), item.ID, "invalid")
	if err == nil {
		t.Fatal("expected error for invalid status")
	}
}

func TestSubmitGoldTracksAnnotator(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)
	queue, _ := svc.CreateQueue(context.Background(), CreateQueueParams{ProjectID: "proj-1", Name: "gold"})
	item, _ := svc.CreateItem(context.Background(), CreateItemParams{QueueID: queue.ID, RunID: "run-1"})

	got, err := svc.SubmitGold(context.Background(), item.ID, "proj-1", `{"quality":0.9,"safety":1}`, "user-1")
	if err != nil {
		t.Fatalf("SubmitGold: %v", err)
	}
	if !got.IsGold || got.AnnotatorID != "user-1" || got.Status != "completed" {
		t.Fatalf("gold audit fields missing: %+v", got)
	}
	if _, err := svc.SubmitGold(context.Background(), item.ID, "proj-1", `{"quality":2}`, "user-1"); err == nil {
		t.Fatal("expected invalid score error")
	}
}

func TestListItemsByQueue(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	q1, _ := svc.CreateQueue(context.Background(), CreateQueueParams{ProjectID: "proj-1", Name: "q1"})
	q2, _ := svc.CreateQueue(context.Background(), CreateQueueParams{ProjectID: "proj-1", Name: "q2"})

	svc.CreateItem(context.Background(), CreateItemParams{QueueID: q1.ID, RunID: "run-1"})
	svc.CreateItem(context.Background(), CreateItemParams{QueueID: q1.ID, RunID: "run-2"})
	svc.CreateItem(context.Background(), CreateItemParams{QueueID: q2.ID, RunID: "run-3"})

	items, err := svc.ListItemsByQueue(context.Background(), q1.ID)
	if err != nil {
		t.Fatalf("ListItemsByQueue() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2", len(items))
	}
}

func TestStatusFlow(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	queue, _ := svc.CreateQueue(context.Background(), CreateQueueParams{
		ProjectID: "proj-1", Name: "test",
	})
	item, _ := svc.CreateItem(context.Background(), CreateItemParams{
		QueueID: queue.ID, RunID: "run-1",
	})

	// pending -> in_progress -> completed -> promoted
	item, _ = svc.UpdateItemStatus(context.Background(), item.ID, "in_progress")
	if item.Status != "in_progress" {
		t.Errorf("status = %q, want in_progress", item.Status)
	}

	item, _ = svc.UpdateItemStatus(context.Background(), item.ID, "completed")
	if item.Status != "completed" {
		t.Errorf("status = %q, want completed", item.Status)
	}

	item, _ = svc.UpdateItemStatus(context.Background(), item.ID, "promoted")
	if item.Status != "promoted" {
		t.Errorf("status = %q, want promoted", item.Status)
	}
}

func TestEnsureDefaultQueue(t *testing.T) {
	q := newFakeQuerier()
	svc := NewService(q)

	t.Run("creates_default_if_not_exists", func(t *testing.T) {
		queue, err := svc.EnsureDefaultQueue(context.Background(), "proj-1")
		if err != nil {
			t.Fatalf("EnsureDefaultQueue() error = %v", err)
		}
		if queue.Name != "线上评测待标注" {
			t.Errorf("name = %q, want 线上评测待标注", queue.Name)
		}
		if queue.ProjectID != "proj-1" {
			t.Errorf("projectID = %q, want proj-1", queue.ProjectID)
		}
	})

	t.Run("reuses_existing_default", func(t *testing.T) {
		queue2, err := svc.EnsureDefaultQueue(context.Background(), "proj-1")
		if err != nil {
			t.Fatalf("EnsureDefaultQueue() second call error = %v", err)
		}
		if queue2.Name != "线上评测待标注" {
			t.Errorf("name = %q, want 线上评测待标注", queue2.Name)
		}

		queues, _ := svc.ListQueues(context.Background(), "proj-1")
		count := 0
		for _, q := range queues {
			if q.Name == "线上评测待标注" {
				count++
			}
		}
		if count != 1 {
			t.Errorf("expected exactly 1 default queue, got %d", count)
		}
	})

	t.Run("validation", func(t *testing.T) {
		_, err := svc.EnsureDefaultQueue(context.Background(), "")
		if err == nil {
			t.Fatal("expected error for empty projectID")
		}
	})
}

// ---- Tests for Trace → Annotation → Dataset closed loop ----

func TestCreateItemFromRun_CopiesRunData(t *testing.T) {
	q := newFakeQuerier()
	rp := newFakeRunProvider()
	rp.runs["run-1"] = runRecord{
		id: "run-1", projectID: "proj-1", input: "user query", traceID: "trace-1",
		agentName: "test-agent", output: "agent response", errorVal: "",
	}

	svc := NewService(q)
	svc.SetPromoteDeps(&fakeDatasetCreator{}, rp)

	// Ensure default queue exists
	queue, _ := svc.EnsureDefaultQueue(context.Background(), "proj-1")

	item, err := svc.CreateItemFromRun(context.Background(), CreateItemFromRunParams{
		ProjectID: "proj-1",
		RunID:     "run-1",
		QueueID:   queue.ID,
	})
	if err != nil {
		t.Fatalf("CreateItemFromRun() error = %v", err)
	}

	if item.RunID != "run-1" {
		t.Errorf("RunID = %q, want run-1", item.RunID)
	}
	if item.TraceID != "trace-1" {
		t.Errorf("TraceID = %q, want trace-1", item.TraceID)
	}
	if item.AgentName != "test-agent" {
		t.Errorf("AgentName = %q, want test-agent", item.AgentName)
	}
	if item.InputSummary != "user query" {
		t.Errorf("InputSummary = %q, want user query", item.InputSummary)
	}
	if item.OutputSummary != "agent response" {
		t.Errorf("OutputSummary = %q, want agent response", item.OutputSummary)
	}
}

func TestCreateItemFromRun_MissingTraceID(t *testing.T) {
	q := newFakeQuerier()
	rp := newFakeRunProvider()
	rp.runs["run-1"] = runRecord{
		id: "run-1", projectID: "proj-1", input: "user query", traceID: "",
		agentName: "test-agent", output: "agent response", errorVal: "",
	}

	svc := NewService(q)
	svc.SetPromoteDeps(&fakeDatasetCreator{}, rp)

	queue, _ := svc.EnsureDefaultQueue(context.Background(), "proj-1")

	item, err := svc.CreateItemFromRun(context.Background(), CreateItemFromRunParams{
		ProjectID: "proj-1",
		RunID:     "run-1",
		QueueID:   queue.ID,
	})
	if err != nil {
		t.Fatalf("CreateItemFromRun() error = %v", err)
	}

	// TraceID should be empty
	if item.TraceID != "" {
		t.Errorf("TraceID = %q, want empty for missing trace_id", item.TraceID)
	}

	// InputSummary should NOT be polluted with [no trace id] prefix
	if item.InputSummary != "user query" {
		t.Errorf("InputSummary = %q, want 'user query' (no [no trace id] prefix)", item.InputSummary)
	}
}

func TestCreateItemFromRun_RejectsQueueFromAnotherProject(t *testing.T) {
	q := newFakeQuerier()
	rp := newFakeRunProvider()
	rp.runs["run-1"] = runRecord{
		id: "run-1", projectID: "proj-1", input: "private input",
		traceID: "trace-1", agentName: "test-agent", output: "private output",
	}

	svc := NewService(q)
	svc.SetPromoteDeps(&fakeDatasetCreator{}, rp)
	foreignQueue, err := svc.CreateQueue(
		context.Background(),
		CreateQueueParams{ProjectID: "proj-2", Name: "foreign"},
	)
	if err != nil {
		t.Fatalf("CreateQueue() error = %v", err)
	}

	_, err = svc.CreateItemFromRun(
		context.Background(),
		CreateItemFromRunParams{
			ProjectID: "proj-1",
			RunID:     "run-1",
			QueueID:   foreignQueue.ID,
		},
	)
	if err == nil {
		t.Fatal("expected cross-project queue to be rejected")
	}
	if len(q.items) != 0 {
		t.Fatalf("cross-project request created %d annotation items", len(q.items))
	}
}

func TestCreateItemFromRun_InvalidRunDoesNotCreateDefaultQueue(t *testing.T) {
	q := newFakeQuerier()
	rp := newFakeRunProvider()
	svc := NewService(q)
	svc.SetPromoteDeps(&fakeDatasetCreator{}, rp)

	_, err := svc.CreateItemFromRun(
		context.Background(),
		CreateItemFromRunParams{
			ProjectID: "proj-1",
			RunID:     "missing-run",
		},
	)
	if err == nil {
		t.Fatal("expected missing run to be rejected")
	}
	if len(q.queues) != 0 {
		t.Fatalf("invalid run created %d annotation queues", len(q.queues))
	}
}

func TestPromoteToDataset_MissingTraceID_Metadata(t *testing.T) {
	q := newFakeQuerier()
	dc := &fakeDatasetCreator{}
	rp := newFakeRunProvider()
	rp.runs["run-1"] = runRecord{
		id: "run-1", projectID: "proj-1", input: `{"query":"hello"}`,
		traceID: "", agentName: "test-agent",
	}

	svc := NewService(q)
	svc.SetPromoteDeps(dc, rp)

	queue, _ := svc.EnsureDefaultQueue(context.Background(), "proj-1")

	// Create item with RunID and empty TraceID
	item, _ := svc.CreateItem(context.Background(), CreateItemParams{
		QueueID: queue.ID, RunID: "run-1",
	})

	// Set corrected output and privacy reviewed
	svc.UpdateItem(context.Background(), item.ID, UpdateItemParams{
		CorrectedOutput: "correct answer",
		PrivacyReviewed: true,
	})

	// Promote to dataset
	caseID, err := svc.PromoteToDataset(context.Background(), item.ID, "ds-1")
	if err != nil {
		t.Fatalf("PromoteToDataset() error = %v", err)
	}
	if caseID == "" {
		t.Fatal("expected non-empty caseID")
	}

	// Verify metadata contains missing_trace_id=true
	if len(dc.cases) != 1 {
		t.Fatalf("expected 1 dataset case, got %d", len(dc.cases))
	}

	var metadata map[string]any
	if err := json.Unmarshal([]byte(dc.cases[0].metadata), &metadata); err != nil {
		t.Fatalf("metadata is not valid JSON: %v", err)
	}

	if metadata["missing_trace_id"] != true {
		t.Errorf("metadata.missing_trace_id = %v, want true", metadata["missing_trace_id"])
	}
	if metadata["source_trace_id"] != "" {
		t.Errorf("metadata.source_trace_id = %q, want empty string", metadata["source_trace_id"])
	}
}

func TestCreateItemFromRun_UsesDefaultQueue(t *testing.T) {
	q := newFakeQuerier()
	rp := newFakeRunProvider()
	rp.runs["run-1"] = runRecord{
		id: "run-1", projectID: "proj-1", input: "input", traceID: "trace-1",
		agentName: "agent", output: "output", errorVal: "",
	}

	svc := NewService(q)
	svc.SetPromoteDeps(&fakeDatasetCreator{}, rp)

	// CreateItemFromRun without specifying queueID should auto-create default queue
	item, err := svc.CreateItemFromRun(context.Background(), CreateItemFromRunParams{
		ProjectID: "proj-1",
		RunID:     "run-1",
	})
	if err != nil {
		t.Fatalf("CreateItemFromRun() error = %v", err)
	}

	// Item must be in default queue
	queues, _ := svc.ListQueues(context.Background(), "proj-1")
	found := false
	for _, qq := range queues {
		if qq.Name == "线上评测待标注" && qq.ID == item.QueueID {
			found = true
			break
		}
	}
	if !found {
		t.Error("item should be in default queue when no queueID specified")
	}
	if item.AgentName != "agent" {
		t.Errorf("AgentName = %q, want agent", item.AgentName)
	}
}

func TestPromoteToDataset_PrivacyReviewRequired(t *testing.T) {
	q := newFakeQuerier()
	dc := &fakeDatasetCreator{}
	rp := newFakeRunProvider()
	rp.runs["run-1"] = runRecord{id: "run-1", projectID: "proj-1", input: `{"query":"hello"}`, traceID: "trace-1"}

	svc := NewService(q)
	svc.SetPromoteDeps(dc, rp)

	queue, _ := svc.CreateQueue(context.Background(), CreateQueueParams{
		ProjectID: "proj-1", Name: "test",
	})

	// Create item with RunID
	item, _ := svc.CreateItem(context.Background(), CreateItemParams{
		QueueID: queue.ID,
		RunID:   "run-1",
	})

	// Set corrected output but NOT privacy_reviewed
	svc.UpdateItem(context.Background(), item.ID, UpdateItemParams{
		CorrectedOutput: "correct answer",
	})

	// Promote should fail because privacy_reviewed is false
	_, err := svc.PromoteToDataset(context.Background(), item.ID, "ds-1")
	if err == nil {
		t.Fatal("PromoteToDataset() expected error when privacy_reviewed is false")
	}
	if err.Error() != "privacy review required; set privacy_reviewed=true before promoting" {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestPromoteToDataset_PromoteMetadata(t *testing.T) {
	q := newFakeQuerier()
	dc := &fakeDatasetCreator{}
	rp := newFakeRunProvider()
	rp.runs["run-1"] = runRecord{
		id: "run-1", projectID: "proj-1", input: `{"query":"hello"}`,
		traceID: "trace-1", agentName: "test-agent",
	}

	svc := NewService(q)
	svc.SetPromoteDeps(dc, rp)

	queue, _ := svc.CreateQueue(context.Background(), CreateQueueParams{
		ProjectID: "proj-1", Name: "test",
	})

	// Create item with full run data
	item, _ := svc.CreateItem(context.Background(), CreateItemParams{
		QueueID:         queue.ID,
		RunID:           "run-1",
		TraceID:         "trace-1",
		AgentName:       "test-agent",
		InputSummary:    `{"query":"hello"}`,
		OutputSummary:   "agent response",
		FailureCategory: "hallucination",
		PrivacyReviewed: true,
	})

	// Set corrected output (required)
	svc.UpdateItem(context.Background(), item.ID, UpdateItemParams{
		CorrectedOutput:  "corrected answer",
		ExpectedToolPath: `[{"tool":"search","args":{"q":"test"}}]`,
		PrivacyReviewed:  true,
	})

	// Promote to dataset
	caseID, err := svc.PromoteToDataset(context.Background(), item.ID, "ds-1")
	if err != nil {
		t.Fatalf("PromoteToDataset() error = %v", err)
	}
	if caseID == "" {
		t.Fatal("expected non-empty caseID")
	}

	// Verify dataset case metadata contains expected source fields
	if len(dc.cases) != 1 {
		t.Fatalf("expected 1 dataset case, got %d", len(dc.cases))
	}

	var metadata map[string]any
	if err := jsonUnmarshal([]byte(dc.cases[0].metadata), &metadata); err != nil {
		t.Fatalf("metadata is not valid JSON: %v", err)
	}

	// Verify each expected field
	if metadata["source"] != "annotation" {
		t.Errorf("metadata.source = %v, want 'annotation'", metadata["source"])
	}
	if metadata["source_annotation_item_id"] != item.ID {
		t.Errorf("metadata.source_annotation_item_id = %v, want %s", metadata["source_annotation_item_id"], item.ID)
	}
	if metadata["source_run_id"] != "run-1" {
		t.Errorf("metadata.source_run_id = %v, want run-1", metadata["source_run_id"])
	}
	if metadata["source_trace_id"] != "trace-1" {
		t.Errorf("metadata.source_trace_id = %v, want trace-1", metadata["source_trace_id"])
	}
	if metadata["source_agent_name"] != "test-agent" {
		t.Errorf("metadata.source_agent_name = %v, want test-agent", metadata["source_agent_name"])
	}
	if metadata["failure_category"] != "hallucination" {
		t.Errorf("metadata.failure_category = %v, want hallucination", metadata["failure_category"])
	}
	if metadata["privacy_reviewed"] != true {
		t.Errorf("metadata.privacy_reviewed = %v, want true", metadata["privacy_reviewed"])
	}
}

// jsonUnmarshal is a test helper that uses encoding/json.
func jsonUnmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
