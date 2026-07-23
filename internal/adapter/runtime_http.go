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
		ProjectID:  projectID,
		AgentID:    req.AgentID,
		InstanceID: req.GetInstanceID(),
		Input:      req.Input,
		Metadata:   req.Metadata,
		TraceID:    req.TraceID,
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
		InstanceID: req.GetInstanceID(),
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
		ProjectID:  projectID,
		RunID:      runID,
		Output:     req.Output,
		AgentID:    req.AgentID,
		InstanceID: req.GetInstanceID(),
		TraceID:    req.TraceID,
		LatencyMS:  req.LatencyMS,
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
		AgentID:    req.AgentID,
		InstanceID: req.GetInstanceID(),
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

func (h *HTTPRuntimeService) ListRecentRunsByAgent(ctx context.Context, projectID, agentName string, limit int) ([]httpapi.RecentRunResponse, error) {
	entries, err := h.svc.ListRecentRunsByAgent(ctx, projectID, agentName, limit)
	if err != nil {
		return nil, err
	}
	result := make([]httpapi.RecentRunResponse, 0, len(entries))
	for _, e := range entries {
		result = append(result, recentRunToResponse(e))
	}
	return result, nil
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
	// Convert semantic events
	resp.Thoughts = convThoughts(d.Thoughts)
	resp.LLMCalls = convLLMCalls(d.LLMCalls)
	resp.ToolCalls = convToolCalls(d.ToolCalls)
	resp.RetrievalCalls = convRetrievalCalls(d.RetrievalCalls)
	resp.SkillCalls = convSkillCalls(d.SkillCalls)
	resp.FinalAnswers = convFinalAnswers(d.FinalAnswers)
		resp.Spans = convSpans(d.Spans)
	return resp
}

func convThoughts(src []runs.ThoughtRecord) []httpapi.ThoughtRecord {
	if src == nil {
		return nil
	}
	out := make([]httpapi.ThoughtRecord, 0, len(src))
	for _, s := range src {
		out = append(out, httpapi.ThoughtRecord{
			ProjectID:       s.ProjectID,
			RunID:           s.RunID,
			TraceID:         s.TraceID,
			SpanID:          s.SpanID,
			AgentID:         s.AgentID,
			AgentInstanceID: s.AgentInstanceID,
			Stage:           s.Stage,
			Summary:         s.Summary,
			Content:         s.Content,
			Confidence:      s.Confidence,
			ContentCaptured: s.ContentCaptured,
			StartTime:       s.StartTime.Format(time.RFC3339),
			DurationMs:      s.DurationMs,
		})
	}
	return out
}

func convLLMCalls(src []runs.LLMCallRecord) []httpapi.LLMCallRecord {
	if src == nil {
		return nil
	}
	out := make([]httpapi.LLMCallRecord, 0, len(src))
	for _, s := range src {
		out = append(out, httpapi.LLMCallRecord{
			ProjectID:       s.ProjectID,
			RunID:           s.RunID,
			TraceID:         s.TraceID,
			SpanID:          s.SpanID,
			AgentID:         s.AgentID,
			AgentInstanceID: s.AgentInstanceID,
			Provider:        s.Provider,
			Model:           s.Model,
			InputTokens:     s.InputTokens,
			OutputTokens:    s.OutputTokens,
			TotalTokens:     s.TotalTokens,
			Cost:            s.Cost,
			InputSummary:    s.InputSummary,
			OutputSummary:   s.OutputSummary,
			ContentCaptured: s.ContentCaptured,
			Status:          s.Status,
			Error:           s.Error,
			StartTime:       s.StartTime.Format(time.RFC3339),
			DurationMs:      s.DurationMs,
		})
	}
	return out
}

func convToolCalls(src []runs.ToolCallRecord) []httpapi.ToolCallRecord {
	if src == nil {
		return nil
	}
	out := make([]httpapi.ToolCallRecord, 0, len(src))
	for _, s := range src {
		out = append(out, httpapi.ToolCallRecord{
			ProjectID:        s.ProjectID,
			RunID:            s.RunID,
			TraceID:          s.TraceID,
			SpanID:           s.SpanID,
			AgentID:          s.AgentID,
			AgentInstanceID:  s.AgentInstanceID,
			ToolName:         s.ToolName,
			ToolType:         s.ToolType,
			ArgumentsSummary: s.ArgumentsSummary,
			ResultSummary:    s.ResultSummary,
			Success:          s.Success,
			Error:            s.Error,
			RetryCount:       s.RetryCount,
			SideEffectRisk:   s.SideEffectRisk,
			StartTime:        s.StartTime.Format(time.RFC3339),
			DurationMs:       s.DurationMs,
		})
	}
	return out
}

func convRetrievalCalls(src []runs.RetrievalCallRecord) []httpapi.RetrievalCallRecord {
	if src == nil {
		return nil
	}
	out := make([]httpapi.RetrievalCallRecord, 0, len(src))
	for _, s := range src {
		out = append(out, httpapi.RetrievalCallRecord{
			ProjectID:       s.ProjectID,
			RunID:           s.RunID,
			TraceID:         s.TraceID,
			SpanID:          s.SpanID,
			AgentID:         s.AgentID,
			AgentInstanceID: s.AgentInstanceID,
			KnowledgeBase:   s.KnowledgeBase,
			QueryText:       s.QueryText,
			TopK:            s.TopK,
			ReturnedCount:   s.ReturnedCount,
			DocumentIDs:     s.DocumentIDs,
			Scores:          s.Scores,
			StartTime:       s.StartTime.Format(time.RFC3339),
			DurationMs:      s.DurationMs,
		})
	}
	return out
}

func convSkillCalls(src []runs.SkillCallRecord) []httpapi.SkillCallRecord {
	if src == nil {
		return nil
	}
	out := make([]httpapi.SkillCallRecord, 0, len(src))
	for _, s := range src {
		out = append(out, httpapi.SkillCallRecord{
			ProjectID:       s.ProjectID,
			RunID:           s.RunID,
			TraceID:         s.TraceID,
			SpanID:          s.SpanID,
			AgentID:         s.AgentID,
			AgentInstanceID: s.AgentInstanceID,
			SkillName:       s.SkillName,
			SkillVersion:    s.SkillVersion,
			InputSummary:    s.InputSummary,
			OutputSummary:   s.OutputSummary,
			Success:         s.Success,
			Error:           s.Error,
			StartTime:       s.StartTime.Format(time.RFC3339),
			DurationMs:      s.DurationMs,
		})
	}
	return out
}

func convFinalAnswers(src []runs.FinalAnswerRecord) []httpapi.FinalAnswerRecord {
	if src == nil {
		return nil
	}
	out := make([]httpapi.FinalAnswerRecord, 0, len(src))
	for _, s := range src {
		out = append(out, httpapi.FinalAnswerRecord{
			ProjectID:       s.ProjectID,
			RunID:           s.RunID,
			TraceID:         s.TraceID,
			SpanID:          s.SpanID,
			AnswerSummary:   s.AnswerSummary,
			AnswerOutput:    s.AnswerOutput,
			StructuredOutput: s.StructuredOutput,
			StartTime:       s.StartTime.Format(time.RFC3339),
			DurationMs:      s.DurationMs,
		})
	}
	return out
}

func convSpans(src []runs.SpanRecord) []httpapi.TraceSpanRecord {
	if src == nil {
		return nil
	}
	out := make([]httpapi.TraceSpanRecord, 0, len(src))
	for _, s := range src {
		var endTime string
		if s.EndTime != nil {
			endTime = s.EndTime.Format(time.RFC3339)
		}
		out = append(out, httpapi.TraceSpanRecord{
			RunID:         s.RunID,
			SpanID:        s.SpanID,
			TraceID:       s.TraceID,
			ProjectID:     s.ProjectID,
			ParentSpanID:  s.ParentSpanID,
			Name:          s.Name,
			Kind:          s.Kind,
			StatusCode:    s.StatusCode,
			StatusMessage: s.StatusMsg,
			StartTime:     s.StartTime.Format(time.RFC3339),
			EndTime:       endTime,
			DurationMs:    s.DurationMs,
			Attributes:    s.Attributes,
			Truncated:     s.Truncated,
		})
	}
	return out
}

func recentRunToResponse(r runs.RecentRunEntry) httpapi.RecentRunResponse {
	return httpapi.RecentRunResponse{
		ID:           r.ID,
		ProjectID:    r.ProjectID,
		Status:       r.Status,
		InputSummary: r.InputSummary,
		Error:        r.Error,
		TraceID:      r.TraceID,
		LatencyMs:    r.LatencyMs,
		AgentName:    r.AgentName,
		CreatedAt:    r.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    r.UpdatedAt.Format(time.RFC3339),
	}
}