package adapter

import (
	"context"

	"AgentScope/internal/annotation"
	"AgentScope/internal/httpapi"
)

// AnnotationServiceAdapter adapts annotation.Service to implement httpapi.AnnotationService.
type AnnotationServiceAdapter struct {
	svc *annotation.Service
}

var _ httpapi.AnnotationService = (*AnnotationServiceAdapter)(nil)

// NewAnnotationServiceAdapter creates a new AnnotationServiceAdapter.
func NewAnnotationServiceAdapter(svc *annotation.Service) *AnnotationServiceAdapter {
	return &AnnotationServiceAdapter{svc: svc}
}

func (a *AnnotationServiceAdapter) CreateQueue(ctx context.Context, projectID, name, description string) (httpapi.AnnotationQueue, error) {
	q, err := a.svc.CreateQueue(ctx, annotation.CreateQueueParams{
		ProjectID:   projectID,
		Name:        name,
		Description: description,
	})
	if err != nil {
		return httpapi.AnnotationQueue{}, err
	}
	return httpapi.AnnotationQueue{
		ID:          q.ID,
		ProjectID:   q.ProjectID,
		Name:        q.Name,
		Description: q.Description,
	}, nil
}

func (a *AnnotationServiceAdapter) ListQueues(ctx context.Context, projectID string) ([]httpapi.AnnotationQueue, error) {
	queues, err := a.svc.ListQueues(ctx, projectID)
	if err != nil {
		return nil, err
	}
	result := make([]httpapi.AnnotationQueue, 0, len(queues))
	for _, q := range queues {
		result = append(result, httpapi.AnnotationQueue{
			ID:          q.ID,
			ProjectID:   q.ProjectID,
			Name:        q.Name,
			Description: q.Description,
		})
	}
	return result, nil
}

func (a *AnnotationServiceAdapter) CreateItem(ctx context.Context, queueID, runID, traceID string, priority int, scores string) (httpapi.AnnotationItem, error) {
	item, err := a.svc.CreateItem(ctx, annotation.CreateItemParams{
		QueueID:  queueID,
		RunID:    runID,
		TraceID:  traceID,
		Priority: priority,
		Scores:   scores,
	})
	if err != nil {
		return httpapi.AnnotationItem{}, err
	}
	return httpapi.AnnotationItem{
		ID:        item.ID,
		QueueID:   item.QueueID,
		ProjectID: item.ProjectID,
		RunID:     item.RunID,
		TraceID:   item.TraceID,
		Status:    item.Status,
		Priority:  item.Priority,
		Scores:    item.Scores,
	}, nil
}

func (a *AnnotationServiceAdapter) ListItemsByQueue(ctx context.Context, queueID string) ([]httpapi.AnnotationItem, error) {
	items, err := a.svc.ListItemsByQueue(ctx, queueID)
	if err != nil {
		return nil, err
	}
	result := make([]httpapi.AnnotationItem, 0, len(items))
	for _, item := range items {
		result = append(result, annotationItemToHTTP(item))
	}
	return result, nil
}

func (a *AnnotationServiceAdapter) GetItem(ctx context.Context, itemID string) (httpapi.AnnotationItem, error) {
	item, err := a.svc.GetItem(ctx, itemID)
	if err != nil {
		return httpapi.AnnotationItem{}, err
	}
	return annotationItemToHTTP(item), nil
}

func (a *AnnotationServiceAdapter) UpdateItem(ctx context.Context, itemID string, status, correctedOutput, expectedToolPath, comment string) (httpapi.AnnotationItem, error) {
	item, err := a.svc.UpdateItem(ctx, itemID, annotation.UpdateItemParams{
		Status:           status,
		CorrectedOutput:  correctedOutput,
		ExpectedToolPath: expectedToolPath,
		Comment:          comment,
	})
	if err != nil {
		return httpapi.AnnotationItem{}, err
	}
	return annotationItemToHTTP(item), nil
}

func (a *AnnotationServiceAdapter) PromoteToDataset(ctx context.Context, itemID, datasetID string) (string, error) {
	return a.svc.PromoteToDataset(ctx, itemID, datasetID)
}

func (a *AnnotationServiceAdapter) UpdateItemStatus(ctx context.Context, itemID, status string) (httpapi.AnnotationItem, error) {
	item, err := a.svc.UpdateItemStatus(ctx, itemID, status)
	if err != nil {
		return httpapi.AnnotationItem{}, err
	}
	return annotationItemToHTTP(item), nil
}

func annotationItemToHTTP(item annotation.AnnotationItem) httpapi.AnnotationItem {
	return httpapi.AnnotationItem{
		ID:               item.ID,
		QueueID:          item.QueueID,
		ProjectID:        item.ProjectID,
		RunID:            item.RunID,
		TraceID:          item.TraceID,
		Status:           item.Status,
		Priority:         item.Priority,
		Scores:           item.Scores,
		CorrectedOutput:  item.CorrectedOutput,
		ExpectedToolPath: item.ExpectedToolPath,
		Comment:          item.Comment,
	}
}
