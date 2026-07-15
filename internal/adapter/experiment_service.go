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
		ID:            e.ID,
		ProjectID:     e.ProjectID,
		DatasetID:     e.DatasetID,
		EvaluatorID:   e.EvaluatorID,
		TargetName:    e.TargetName,
		TargetVersion: e.TargetVersion,
		Status:        e.Status,
		TotalCases:    e.TotalCases,
		PassedCases:   e.PassedCases,
		FailedCases:   e.FailedCases,
		CreatedAt:     e.CreatedAt.Format("2006-01-02T15:04:05Z"),
	}, nil
}

func (h *HTTPExperimentsService) GetExperiment(ctx context.Context, experimentID string) (httpapi.ExperimentDetail, error) {
	d, err := h.svc.GetExperiment(ctx, experimentID)
	if err != nil {
		return httpapi.ExperimentDetail{}, err
	}
	return httpapi.ExperimentDetail{
		Experiment: httpapi.Experiment{
			ID:            d.ID,
			ProjectID:     d.ProjectID,
			DatasetID:     d.DatasetID,
			EvaluatorID:   d.EvaluatorID,
			TargetName:    d.TargetName,
			TargetVersion: d.TargetVersion,
			Status:        d.Status,
			TotalCases:    d.TotalCases,
			PassedCases:   d.PassedCases,
			FailedCases:   d.FailedCases,
			CreatedAt:     d.CreatedAt.Format("2006-01-02T15:04:05Z"),
		},
		StatusCounts: d.StatusCounts,
	}, nil
}
