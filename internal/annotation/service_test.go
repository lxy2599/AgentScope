package annotation

import (
	"context"
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
		ID:        id,
		QueueID:   params.QueueID,
		RunID:     params.RunID,
		TraceID:   params.TraceID,
		Status:    "pending",
		Priority:  params.Priority,
		Scores:    params.Scores,
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
