package adapter

import (
	"context"
	"strconv"
	"time"

	"AgentScope/internal/httpapi"
	"AgentScope/internal/runs"
)

// HTTPRuntimeService implements httpapi.RuntimeService using *runs.RuntimeService.
type HTTPRuntimeService struct {
	svc *runs.RuntimeService
}

var _ httpapi.RuntimeService = (*HTTPRuntimeService)(nil)

// NewHTTPRuntimeService creates a new HTTPRuntimeService adapter.
func NewHTTPRuntimeService(svc *runs.RuntimeService) *HTTPRuntimeService {
	return &HTTPRuntimeService{svc: svc}
}

func (h *HTTPRuntimeService) StartRun(ctx context.Context, projectID string, req httpapi.SDKStartRunRequest) (httpapi.RuntimeRunResponse, error) {
	result, err := h.svc.StartRun(ctx, runs.StartRunInput{
		ProjectID:       projectID,
		AgentID:         req.AgentID,
		AgentInstanceID: req.AgentInstanceID,
		Input:           req.Input,
		Metadata:        req.Metadata,
		TraceID:         req.TraceID,
	})
	if err != nil {
		return httpapi.RuntimeRunResponse{}, err
	}
	return runtimeRunToResponse(*result), nil
}

func (h *HTTPRuntimeService) UpdateRunState(ctx context.Context, projectID, runID string, req httpapi.SDKUpdateRunStateRequest) (httpapi.RuntimeRunResponse, error) {
	result, err := h.svc.UpdateRunState(ctx, runs.UpdateRunStateInput{
		ProjectID:  projectID,
		RunID:      runID,
		Status:     req.Status,
		Message:    req.Message,
		AgentID:    req.AgentID,
		Attributes: req.Attributes,
		TraceID:    req.TraceID,
	})
	if err != nil {
		return httpapi.RuntimeRunResponse{}, err
	}
	return runtimeRunToResponse(*result), nil
}

func (h *HTTPRuntimeService) FinishRun(ctx context.Context, projectID, runID string, req httpapi.SDKFinishRunRequest) (httpapi.RuntimeRunResponse, error) {
	result, err := h.svc.FinishRun(ctx, runs.FinishRunInput{
		ProjectID: projectID,
		RunID:     runID,
		Output:    req.Output,
		TraceID:   req.TraceID,
		LatencyMS: req.LatencyMS,
	})
	if err != nil {
		return httpapi.RuntimeRunResponse{}, err
	}
	return runtimeRunToResponse(*result), nil
}

func (h *HTTPRuntimeService) FailRun(ctx context.Context, projectID, runID string, req httpapi.SDKFailRunRequest) (httpapi.RuntimeRunResponse, error) {
	result, err := h.svc.FailRun(ctx, runs.FailRunInput{
		ProjectID:  projectID,
		RunID:      runID,
		Error:      req.Error,
		Attributes: req.Attributes,
		TraceID:    req.TraceID,
		LatencyMS:  req.LatencyMS,
	})
	if err != nil {
		return httpapi.RuntimeRunResponse{}, err
	}
	return runtimeRunToResponse(*result), nil
}

func (h *HTTPRuntimeService) ListActiveRuns(ctx context.Context, projectID string, limit, offset int) ([]httpapi.ActiveRunResponse, error) {
	runList, err := h.svc.ListActiveRuns(ctx, projectID, limit, offset)
	if err != nil {
		return nil, err
	}
	result := make([]httpapi.ActiveRunResponse, 0, len(runList))
	for _, r := range runList {
		result = append(result, activeRunToResponse(r))
	}
	return result, nil
}

func (h *HTTPRuntimeService) GetRuntimeRunDetail(ctx context.Context, projectID, runID string) (httpapi.RuntimeRunDetailResponse, error) {
	detail, err := h.svc.GetRuntimeRunDetail(ctx, runID, projectID)
	if err != nil {
		return httpapi.RuntimeRunDetailResponse{}, err
	}
	return runtimeDetailToResponse(*detail), nil
}

// ---- Converters ----

func runtimeRunToResponse(r runs.RuntimeRun) httpapi.RuntimeRunResponse {
	return httpapi.RuntimeRunResponse{
		ID:        r.ID,
		ProjectID: r.ProjectID,
		Status:    r.Status,
		Input:     r.Input,
		Output:    r.Output,
		Error:     r.Error,
		TraceID:   r.TraceID,
		AgentID:   r.AgentID,
		AgentName: r.AgentName,
		CreatedAt: r.CreatedAt.Format(time.RFC3339),
		UpdatedAt: r.UpdatedAt.Format(time.RFC3339),
	}
}

func activeRunToResponse(r runs.ActiveRun) httpapi.ActiveRunResponse {
	resp := httpapi.ActiveRunResponse{
		ID:                r.ID,
		ProjectID:         r.ProjectID,
		Status:            r.Status,
		InputSummary:      r.InputSummary,
		OutputSummary:     r.OutputSummary,
		Error:             r.Error,
		TraceID:           r.TraceID,
		AgentID:           r.AgentID,
		AgentName:         r.AgentName,
		AgentInstanceID:   r.AgentInstanceID,
		AgentInstanceName: r.AgentInstanceName,
		CreatedAt:         r.CreatedAt.Format(time.RFC3339),
		UpdatedAt:         r.UpdatedAt.Format(time.RFC3339),
		LastState:         r.LastState,
	}
	if r.LastStateEventAt != nil {
		s := r.LastStateEventAt.Format(time.RFC3339)
		resp.LastStateEventAt = &s
	}
	if r.LatencyMs != nil {
		resp.LatencyMsStr = strconv.FormatInt(*r.LatencyMs, 10) + "ms"
	}
	return resp
}

func runtimeDetailToResponse(d runs.RuntimeRunDetail) httpapi.RuntimeRunDetailResponse {
	base := runtimeRunToResponse(d.RuntimeRun)
	resp := httpapi.RuntimeRunDetailResponse{
		ID:                base.ID,
		ProjectID:         base.ProjectID,
		Status:            base.Status,
		Input:             base.Input,
		Output:            base.Output,
		Error:             base.Error,
		TraceID:           base.TraceID,
		AgentID:           base.AgentID,
		AgentName:         base.AgentName,
		AgentInstanceID:   d.AgentInstanceID,
		AgentInstanceName: d.AgentInstanceName,
		CreatedAt:         base.CreatedAt,
		UpdatedAt:         base.UpdatedAt,
		Events:            make([]httpapi.RunStateEventResponse, 0, len(d.Events)),
	}
	for _, evt := range d.Events {
		resp.Events = append(resp.Events, httpapi.RunStateEventResponse{
			ID:         evt.ID,
			RunID:      evt.RunID,
			FromStatus: evt.FromStatus,
			ToStatus:   evt.ToStatus,
			Message:    evt.Message,
			CreatedAt:  evt.CreatedAt.Format(time.RFC3339),
		})
	}
	return resp
}
