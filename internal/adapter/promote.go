package adapter

import (
	"context"
	"encoding/json"

	"AgentScope/internal/annotation"
	"AgentScope/internal/datasets"
	"AgentScope/internal/runs"
)

// AnnotationPromoteAdapter adapts datasets.Service and runs.Service to implement
// annotation.DatasetCreator and annotation.RunProvider.
type AnnotationPromoteAdapter struct {
	dsSvc  *datasets.Service
	runSvc *runs.Service
}

var _ annotation.DatasetCreator = (*AnnotationPromoteAdapter)(nil)
var _ annotation.RunProvider = (*AnnotationPromoteAdapter)(nil)

// NewAnnotationPromoteAdapter creates a new adapter for annotation promote.
func NewAnnotationPromoteAdapter(dsSvc *datasets.Service, runSvc *runs.Service) *AnnotationPromoteAdapter {
	return &AnnotationPromoteAdapter{
		dsSvc:  dsSvc,
		runSvc: runSvc,
	}
}

func (a *AnnotationPromoteAdapter) AddCase(ctx context.Context, datasetID, input, expected, metadata string) (string, error) {
	inputJSON := json.RawMessage(input)
	if inputJSON == nil {
		inputJSON = json.RawMessage("{}")
	}
	var expectedJSON json.RawMessage
	if expected != "" {
		expectedJSON = json.RawMessage(`"` + expected + `"`)
	}
	var metadataJSON json.RawMessage
	if metadata != "" {
		metadataJSON = json.RawMessage(metadata)
	}

	dsCase, err := a.dsSvc.AddCase(ctx, datasetID, inputJSON, expectedJSON, metadataJSON)
	if err != nil {
		return "", err
	}
	return dsCase.ID, nil
}

func (a *AnnotationPromoteAdapter) GetRunInput(ctx context.Context, runID string) (string, string, error) {
	run, err := a.runSvc.GetRun(ctx, runID)
	if err != nil {
		return "", "", err
	}
	return run.InputSummary, run.TraceID, nil
}
