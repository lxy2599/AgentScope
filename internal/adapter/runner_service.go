package adapter

import (
	"context"

	"AgentScope/internal/httpapi"
	"AgentScope/internal/runnerapi"
)

// HTTPRunnerService implements httpapi.RunnerService using *runnerapi.Service.
type HTTPRunnerService struct {
	svc *runnerapi.Service
}

// compile-time check
var _ httpapi.RunnerService = (*HTTPRunnerService)(nil)

// NewHTTPRunnerService creates a new HTTPRunnerService.
func NewHTTPRunnerService(svc *runnerapi.Service) *HTTPRunnerService {
	return &HTTPRunnerService{svc: svc}
}

func (h *HTTPRunnerService) Claim(ctx context.Context, projectID, runnerName string) (httpapi.ClaimResponse, error) {
	resp, err := h.svc.Claim(ctx, projectID, runnerName)
	if err != nil {
		return httpapi.ClaimResponse{}, err
	}
	return httpapi.ClaimResponse{
		Job: httpapi.EvalJob{
			ID:            resp.Job.ID,
			ExperimentID:  resp.Job.ExperimentID,
			DatasetCaseID: resp.Job.DatasetCaseID,
			Status:        resp.Job.Status,
		},
		Status:        resp.Status,
		LeaseDuration: resp.LeaseDuration,
		CaseInput:     resp.CaseInput,
		CaseExpected:  resp.CaseExpected,
		CaseMetadata:  resp.CaseMetadata,
		TargetName:    resp.TargetName,
		TargetVersion: resp.TargetVersion,
		EvalConfig:    resp.EvalConfig,
	}, nil
}

func (h *HTTPRunnerService) Heartbeat(ctx context.Context, jobID, projectID, runnerName string) error {
	return h.svc.Heartbeat(ctx, jobID, projectID, runnerName)
}

func (h *HTTPRunnerService) SubmitResult(ctx context.Context, jobID, projectID, runnerName string, req httpapi.SubmitResultRequest) error {
	svcReq := runnerapi.SubmitResultRequest{
		RunID:        req.RunID,
		Score:        req.Score,
		Passed:       req.Passed,
		Reason:       req.Reason,
		Details:      []byte(req.Details),
		ActualOutput: req.ActualOutput,
		LatencyMs:    req.LatencyMs,
		Cost:         req.Cost,
	}

	if len(req.EvalScores) > 0 {
		svcReq.EvalScores = make([]runnerapi.EvalScoreItem, 0, len(req.EvalScores))
		for _, es := range req.EvalScores {
			svcReq.EvalScores = append(svcReq.EvalScores, runnerapi.EvalScoreItem{
				MetricName: es.MetricName,
				MetricKind: es.MetricKind,
				Score:      es.Score,
				Passed:     es.Passed,
				Reason:     es.Reason,
				Details:    es.Details,
				TraceID:    es.TraceID,
				SpanID:     es.SpanID,
			})
		}
	}

	return h.svc.SubmitResult(ctx, jobID, projectID, runnerName, svcReq)
}

func (h *HTTPRunnerService) FailJob(ctx context.Context, jobID, projectID, runnerName, errorMsg string) error {
	return h.svc.FailJob(ctx, jobID, projectID, runnerName, errorMsg)
}
