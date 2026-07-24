package adapter

import (
	"context"
	"encoding/json"

	"AgentScope/internal/annotation"
	"AgentScope/internal/store"
)

// AnnotationQuerier adapts store.Queries to implement annotation.Querier.
type AnnotationQuerier struct {
	q *store.Queries
}

var _ annotation.Querier = (*AnnotationQuerier)(nil)

// NewAnnotationQuerier creates a new AnnotationQuerier.
func NewAnnotationQuerier(q *store.Queries) *AnnotationQuerier {
	return &AnnotationQuerier{q: q}
}

func (a *AnnotationQuerier) CreateQueue(ctx context.Context, params annotation.CreateQueueParams) (annotation.AnnotationQueue, error) {
	storeQueue, err := a.q.CreateAnnotationQueue(ctx, store.CreateAnnotationQueueParams{
		ProjectID:   parsePGUUID(params.ProjectID),
		Name:        params.Name,
		Description: params.Description,
	})
	if err != nil {
		return annotation.AnnotationQueue{}, err
	}
	return annotationQueueToDomain(storeQueue), nil
}

func (a *AnnotationQuerier) ListQueues(ctx context.Context, projectID string) ([]annotation.AnnotationQueue, error) {
	storeQueues, err := a.q.ListAnnotationQueuesByProject(ctx, parsePGUUID(projectID))
	if err != nil {
		return nil, err
	}
	result := make([]annotation.AnnotationQueue, 0, len(storeQueues))
	for _, q := range storeQueues {
		result = append(result, annotationQueueToDomain(q))
	}
	return result, nil
}

func (a *AnnotationQuerier) GetQueue(ctx context.Context, queueID string) (annotation.AnnotationQueue, error) {
	storeQueue, err := a.q.GetAnnotationQueueByID(ctx, parsePGUUID(queueID))
	if err != nil {
		return annotation.AnnotationQueue{}, err
	}
	return annotationQueueToDomain(storeQueue), nil
}

func (a *AnnotationQuerier) DeleteQueue(ctx context.Context, queueID, projectID string) error {
	_, err := a.q.DeleteAnnotationQueue(ctx, store.DeleteAnnotationQueueParams{
		ID:        parsePGUUID(queueID),
		ProjectID: parsePGUUID(projectID),
	})
	return err
}

func (a *AnnotationQuerier) CreateItem(ctx context.Context, params annotation.CreateItemParams) (annotation.AnnotationItem, error) {
	scoresBytes := []byte(params.Scores)
	if scoresBytes == nil {
		scoresBytes = []byte("{}")
	}

	storeItem, err := a.q.CreateAnnotationItem(ctx, store.CreateAnnotationItemParams{
		ID:               parsePGUUID(params.QueueID),
		RunID:            parsePGUUID(params.RunID),
		TraceID:          params.TraceID,
		Status:           "pending",
		Priority:         int32(params.Priority),
		Scores:           scoresBytes,
		CorrectedOutput:  "",
		ExpectedToolPath: nil,
		Comment:          "",
		AgentName:        params.AgentName,
		InputSummary:     params.InputSummary,
		OutputSummary:    params.OutputSummary,
		Error:            params.Error,
		FailureCategory:  params.FailureCategory,
		PrivacyReviewed:  params.PrivacyReviewed,
		ReviewerNote:     params.ReviewerNote,
	})
	if err != nil {
		return annotation.AnnotationItem{}, err
	}
	return annotationItemToDomain(storeItem), nil
}

func (a *AnnotationQuerier) ListItemsByQueue(ctx context.Context, queueID string) ([]annotation.AnnotationItem, error) {
	storeItems, err := a.q.ListAnnotationItemsByQueue(ctx, parsePGUUID(queueID))
	if err != nil {
		return nil, err
	}
	result := make([]annotation.AnnotationItem, 0, len(storeItems))
	for _, item := range storeItems {
		result = append(result, annotationItemToDomain(item))
	}
	return result, nil
}

func (a *AnnotationQuerier) GetItem(ctx context.Context, itemID string) (annotation.AnnotationItem, error) {
	storeItem, err := a.q.GetAnnotationItemByID(ctx, parsePGUUID(itemID))
	if err != nil {
		return annotation.AnnotationItem{}, err
	}
	return annotationItemToDomain(storeItem), nil
}

func (a *AnnotationQuerier) UpdateItem(ctx context.Context, itemID string, params annotation.UpdateItemParams) (annotation.AnnotationItem, error) {
	var toolPath []byte
	if params.ExpectedToolPath != "" {
		toolPath = []byte(params.ExpectedToolPath)
	}

	storeItem, err := a.q.UpdateAnnotationItem(ctx, store.UpdateAnnotationItemParams{
		ID:               parsePGUUID(itemID),
		Status:           params.Status,
		CorrectedOutput:  params.CorrectedOutput,
		ExpectedToolPath: toolPath,
		Comment:          params.Comment,
		FailureCategory:  params.FailureCategory,
		PrivacyReviewed:  params.PrivacyReviewed,
		ReviewerNote:     params.ReviewerNote,
	})
	if err != nil {
		return annotation.AnnotationItem{}, err
	}
	return annotationItemToDomain(storeItem), nil
}

func (a *AnnotationQuerier) UpdateItemStatus(ctx context.Context, itemID, status string) (annotation.AnnotationItem, error) {
	storeItem, err := a.q.UpdateAnnotationItemStatus(ctx, store.UpdateAnnotationItemStatusParams{
		ID:     parsePGUUID(itemID),
		Status: status,
	})
	if err != nil {
		return annotation.AnnotationItem{}, err
	}
	return annotationItemToDomain(storeItem), nil
}

func (a *AnnotationQuerier) ListItemsByProject(ctx context.Context, projectID string) ([]annotation.AnnotationItem, error) {
	storeItems, err := a.q.ListAnnotationItemsByProject(ctx, parsePGUUID(projectID))
	if err != nil {
		return nil, err
	}
	result := make([]annotation.AnnotationItem, 0, len(storeItems))
	for _, item := range storeItems {
		result = append(result, annotationItemToDomain(item))
	}
	return result, nil
}

func (a *AnnotationQuerier) SubmitGold(
	ctx context.Context,
	itemID, projectID, goldScores, annotatorID string,
) (annotation.AnnotationItem, error) {
	item, err := a.q.SubmitAnnotationGold(ctx, store.SubmitAnnotationGoldParams{
		ID: parsePGUUID(itemID), ProjectID: parsePGUUID(projectID),
		GoldScores: []byte(goldScores), AnnotatorID: parsePGUUID(annotatorID),
	})
	if err != nil {
		return annotation.AnnotationItem{}, err
	}
	return annotationItemToDomain(item), nil
}

// ---- Conversion helpers ----

func annotationQueueToDomain(q store.AnnotationQueue) annotation.AnnotationQueue {
	return annotation.AnnotationQueue{
		ID:          UUIDToString(q.ID.Bytes),
		ProjectID:   UUIDToString(q.ProjectID.Bytes),
		Name:        q.Name,
		Description: q.Description,
		CreatedAt:   q.CreatedAt.Time,
		UpdatedAt:   q.UpdatedAt.Time,
	}
}

func annotationItemToDomain(item store.AnnotationItem) annotation.AnnotationItem {
	scores := string(item.Scores)
	if scores == "" {
		scores = "{}"
	}

	toolPath := ""
	if item.ExpectedToolPath != nil {
		toolPath = string(item.ExpectedToolPath)
	}

	return annotation.AnnotationItem{
		ID:               UUIDToString(item.ID.Bytes),
		QueueID:          UUIDToString(item.QueueID.Bytes),
		ProjectID:        UUIDToString(item.ProjectID.Bytes),
		RunID:            UUIDToString(item.RunID.Bytes),
		TraceID:          item.TraceID,
		Status:           item.Status,
		Priority:         int(item.Priority),
		Scores:           scores,
		CorrectedOutput:  item.CorrectedOutput,
		ExpectedToolPath: toolPath,
		Comment:          item.Comment,
		AgentName:        item.AgentName,
		InputSummary:     item.InputSummary,
		OutputSummary:    item.OutputSummary,
		Error:            item.Error,
		FailureCategory:  item.FailureCategory,
		PrivacyReviewed:  item.PrivacyReviewed,
		ReviewerNote:     item.ReviewerNote,
		GoldScores:       string(item.GoldScores),
		IsGold:           item.IsGold,
		AnnotatorID:      optionalUUID(item.AnnotatorID),
		CompletedAt:      pgTimestamptzPtr(item.CompletedAt),
		CreatedAt:        item.CreatedAt.Time,
		UpdatedAt:        item.UpdatedAt.Time,
	}
}

// Ensure JSON conversion works
var _ = json.Marshal
