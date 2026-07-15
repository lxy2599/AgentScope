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

func (h *HTTPRunnerService) Claim(ctx context.Context, projectID, runnerName string) (httpapi.ClaimedJob, error) {
	resp, err := h.svc.Claim(ctx, projectID, runnerName)
	if err != nil {
		return httpapi.ClaimedJob{}, err
	}
	return httpapi.ClaimedJob{
		JobID:        resp.Job.ID,
		Status:       resp.Status,
		LeaseDuration: resp.LeaseDuration,
	}, nil
}

func (h *HTTPRunnerService) Heartbeat(ctx context.Context, jobID, runnerName string) error {
	return h.svc.Heartbeat(ctx, jobID, runnerName)
}

func (h *HTTPRunnerService) SubmitResult(ctx context.Context, jobID, runnerName string, req httpapi.SubmitResultRequest) error {
	return h.svc.SubmitResult(ctx, jobID, runnerName, runnerapi.SubmitResultRequest{
		RunID:        req.RunID,
		Score:        req.Score,
		Passed:       req.Passed,
		Reason:       req.Reason,
		ActualOutput: req.ActualOutput,
		LatencyMs:    req.LatencyMs,
		Cost:         req.Cost,
	})
}

func (h *HTTPRunnerService) FailJob(ctx context.Context, jobID, runnerName, errorMsg string) error {
	return h.svc.FailJob(ctx, jobID, runnerName, errorMsg)
}
