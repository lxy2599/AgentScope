package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

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
	if strings.TrimSpace(input) == "" {
		return "", errors.New("input is required")
	}

	// Preserve structured JSON inputs and encode plain text as a JSON string.
	inputJSON := tryParseJSON(input)
	if inputJSON == nil {
		wrapped, err := json.Marshal(input)
		if err != nil {
			return "", err
		}
		inputJSON = wrapped
	}

	// Use json.Marshal to properly escape expected output (handles quotes, newlines, backslashes)
	var expectedJSON json.RawMessage
	if expected != "" {
		escaped, err := json.Marshal(expected)
		if err != nil {
			return "", err
		}
		expectedJSON = escaped
	}

	// metadata is already JSON from the annotation service
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

// tryParseJSON attempts to parse s as JSON. Returns the raw message on success,
// or nil if s is not valid JSON.
func tryParseJSON(s string) json.RawMessage {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil
	}
	return json.RawMessage(s)
}

func (a *AnnotationPromoteAdapter) GetRunInput(ctx context.Context, runID string) (string, string, error) {
	run, err := a.runSvc.GetRun(ctx, runID)
	if err != nil {
		return "", "", err
	}
	input := run.Input
	if input == "" {
		input = run.InputSummary
	}
	return input, run.TraceID, nil
}

func (a *AnnotationPromoteAdapter) GetRunDetail(ctx context.Context, runID string) (annotation.RunDetail, error) {
	run, err := a.runSvc.GetRun(ctx, runID)
	if err != nil {
		return annotation.RunDetail{}, err
	}
	return annotation.RunDetail{
		ID:            run.ID,
		ProjectID:     run.ProjectID,
		TraceID:       run.TraceID,
		AgentName:     run.AgentName,
		InputSummary:  run.Input,
		OutputSummary: run.Output,
		Error:         run.Error,
	}, nil
}
