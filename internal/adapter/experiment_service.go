package adapter

import (
	"context"

	"AgentScope/internal/experiments"
	"AgentScope/internal/httpapi"
)

// HTTPExperimentsService implements httpapi.ExperimentsService using *experiments.Service.
type HTTPExperimentsService struct {
	svc *experiments.Service
}

// compile-time check
var _ httpapi.ExperimentsService = (*HTTPExperimentsService)(nil)

// NewHTTPExperimentsService creates a new HTTPExperimentsService.
func NewHTTPExperimentsService(svc *experiments.Service) *HTTPExperimentsService {
	return &HTTPExperimentsService{svc: svc}
}

func (h *HTTPExperimentsService) CreateExperiment(ctx context.Context, projectID, datasetID, evaluatorID, targetName, targetVersion string, caseIDs []string) (httpapi.Experiment, error) {
	e, err := h.svc.CreateExperiment(ctx, projectID, datasetID, evaluatorID, targetName, targetVersion, caseIDs)
	if err != nil {
		return httpapi.Experiment{}, err
	}
	return httpapi.Experiment{
		ID:               e.ID,
		ProjectID:        e.ProjectID,
		DatasetID:        e.DatasetID,
		EvaluatorID:      e.EvaluatorID,
		TargetName:       e.TargetName,
		TargetVersion:    e.TargetVersion,
		Status:           e.Status,
		TotalCases:       e.TotalCases,
		PassedCases:      e.PassedCases,
		FailedCases:      e.FailedCases,
		CreatedAt:        e.CreatedAt.Format("2006-01-02T15:04:05Z"),
		ModelProvider:    e.ModelProvider,
		ModelName:        e.ModelName,
		ModelVersion:     e.ModelVersion,
		PromptVersion:    e.PromptVersion,
		HarnessVersion:   e.HarnessVersion,
		DatasetVersion:   e.DatasetVersion,
		ToolsetVersion:   e.ToolsetVersion,
		SkillsetVersion:  e.SkillsetVersion,
		RAGConfigVersion: e.RAGConfigVersion,
		FeatureFlags:     e.FeatureFlags,
	}, nil
}

func (h *HTTPExperimentsService) GetExperiment(ctx context.Context, experimentID string) (httpapi.ExperimentDetail, error) {
	d, err := h.svc.GetExperiment(ctx, experimentID)
	if err != nil {
		return httpapi.ExperimentDetail{}, err
	}
	return httpapi.ExperimentDetail{
		Experiment: httpapi.Experiment{
			ID:               d.ID,
			ProjectID:        d.ProjectID,
			DatasetID:        d.DatasetID,
			EvaluatorID:      d.EvaluatorID,
			TargetName:       d.TargetName,
			TargetVersion:    d.TargetVersion,
			Status:           d.Status,
			TotalCases:       d.TotalCases,
			PassedCases:      d.PassedCases,
			FailedCases:      d.FailedCases,
			CreatedAt:        d.CreatedAt.Format("2006-01-02T15:04:05Z"),
			ModelProvider:    d.ModelProvider,
			ModelName:        d.ModelName,
			ModelVersion:     d.ModelVersion,
			PromptVersion:    d.PromptVersion,
			HarnessVersion:   d.HarnessVersion,
			DatasetVersion:   d.DatasetVersion,
			ToolsetVersion:   d.ToolsetVersion,
			SkillsetVersion:  d.SkillsetVersion,
			RAGConfigVersion: d.RAGConfigVersion,
			FeatureFlags:     d.FeatureFlags,
		},
		StatusCounts: d.StatusCounts,
	}, nil
}

func (h *HTTPExperimentsService) ListExperiments(ctx context.Context, projectID string) ([]httpapi.Experiment, error) {
	exps, err := h.svc.ListExperiments(ctx, projectID)
	if err != nil {
		return nil, err
	}
	result := make([]httpapi.Experiment, 0, len(exps))
	for _, e := range exps {
		result = append(result, httpapi.Experiment{
			ID:               e.ID,
			ProjectID:        e.ProjectID,
			DatasetID:        e.DatasetID,
			EvaluatorID:      e.EvaluatorID,
			TargetName:       e.TargetName,
			TargetVersion:    e.TargetVersion,
			Status:           e.Status,
			TotalCases:       e.TotalCases,
			PassedCases:      e.PassedCases,
			FailedCases:      e.FailedCases,
			CreatedAt:        e.CreatedAt.Format("2006-01-02T15:04:05Z"),
			ModelProvider:    e.ModelProvider,
			ModelName:        e.ModelName,
			ModelVersion:     e.ModelVersion,
			PromptVersion:    e.PromptVersion,
			HarnessVersion:   e.HarnessVersion,
			DatasetVersion:   e.DatasetVersion,
			ToolsetVersion:   e.ToolsetVersion,
			SkillsetVersion:  e.SkillsetVersion,
			RAGConfigVersion: e.RAGConfigVersion,
			FeatureFlags:     e.FeatureFlags,
		})
	}
	return result, nil
}

func (h *HTTPExperimentsService) ListExperimentResults(ctx context.Context, experimentID string) ([]httpapi.EvalCaseResult, error) {
	results, err := h.svc.ListEvalResults(ctx, experimentID)
	if err != nil {
		return nil, err
	}
	httpResults := make([]httpapi.EvalCaseResult, 0, len(results))
	for _, r := range results {
		httpResults = append(httpResults, httpapi.EvalCaseResult{
			ID:            r.ID,
			DatasetCaseID: r.DatasetCaseID,
			EvalJobID:     r.EvalJobID,
			RunID:         r.RunID,
			Score:         r.Score,
			Passed:        r.Passed,
			Reason:        r.Reason,
			ActualOutput:  r.ActualOutput,
			LatencyMs:     r.LatencyMs,
			Cost:          r.Cost,
			CreatedAt:     r.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	return httpResults, nil
}

func (h *HTTPExperimentsService) UpdateExperimentHarnessMetadata(ctx context.Context, experimentID, modelProvider, modelName, modelVersion, promptVersion, harnessVersion, datasetVersion string, featureFlags []byte) error {
	return h.svc.UpdateExperimentHarnessMetadata(ctx, experimentID, modelProvider, modelName, modelVersion, promptVersion, harnessVersion, datasetVersion, featureFlags)
}

func (h *HTTPExperimentsService) UpdateFullExperimentHarnessMetadata(ctx context.Context, experimentID, modelProvider, modelName, modelVersion, promptVersion, harnessVersion, datasetVersion, toolsetVersion, skillsetVersion, ragConfigVersion string, featureFlags []byte) error {
	return h.svc.UpdateFullExperimentHarnessMetadata(ctx, experimentID, modelProvider, modelName, modelVersion, promptVersion, harnessVersion, datasetVersion, toolsetVersion, skillsetVersion, ragConfigVersion, featureFlags)
}

func (h *HTTPExperimentsService) CreateExperimentWithHarness(ctx context.Context, projectID, datasetID, evaluatorID string, params experiments.CreateExperimentHarnessParams) (httpapi.Experiment, error) {
	e, err := h.svc.CreateExperimentWithHarness(ctx, params)
	if err != nil {
		return httpapi.Experiment{}, err
	}
	return httpapi.Experiment{
		ID:               e.ID,
		ProjectID:        e.ProjectID,
		DatasetID:        e.DatasetID,
		EvaluatorID:      e.EvaluatorID,
		TargetName:       e.TargetName,
		TargetVersion:    e.TargetVersion,
		Status:           e.Status,
		TotalCases:       e.TotalCases,
		PassedCases:      e.PassedCases,
		FailedCases:      e.FailedCases,
		CreatedAt:        e.CreatedAt.Format("2006-01-02T15:04:05Z"),
		ModelProvider:    e.ModelProvider,
		ModelName:        e.ModelName,
		ModelVersion:     e.ModelVersion,
		PromptVersion:    e.PromptVersion,
		HarnessVersion:   e.HarnessVersion,
		DatasetVersion:   e.DatasetVersion,
		ToolsetVersion:   e.ToolsetVersion,
		SkillsetVersion:  e.SkillsetVersion,
		RAGConfigVersion: e.RAGConfigVersion,
		FeatureFlags:     e.FeatureFlags,
	}, nil
}
