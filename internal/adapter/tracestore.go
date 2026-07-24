package adapter

import (
	"context"
	"time"

	"AgentScope/internal/ingest"
	"AgentScope/internal/tracestore"
)

// HTTPTracestore implements httpapi.TracestoreInterface using tracestore.Store.
// It adds ProjectID, RunID (parsed to uuid.UUID) and IngestedAt timestamp.
type HTTPTracestore struct {
	ts *tracestore.Store
}

// compile-time check
var _ interface {
	InsertSpans(ctx context.Context, spans []ingest.SpanRecord) error
	InsertSpanEvents(ctx context.Context, events []ingest.SpanEventRecord) error
	InsertLLMCalls(ctx context.Context, calls []ingest.LLMCallRecord) error
	InsertToolCalls(ctx context.Context, calls []ingest.ToolCallRecord) error
	InsertRetrievalCalls(ctx context.Context, calls []ingest.RetrievalCallRecord) error
	InsertSkillCalls(ctx context.Context, calls []ingest.SkillCallRecord) error
	InsertThoughts(ctx context.Context, thoughts []ingest.ThoughtRecord) error
	InsertFinalAnswers(ctx context.Context, answers []ingest.FinalAnswerRecord) error
} = (*HTTPTracestore)(nil)

// NewHTTPTracestore creates a new HTTPTracestore.
func NewHTTPTracestore(ts *tracestore.Store) *HTTPTracestore {
	return &HTTPTracestore{ts: ts}
}

func (h *HTTPTracestore) InsertSpans(ctx context.Context, spans []ingest.SpanRecord) error {
	ts := make([]tracestore.TraceSpan, 0, len(spans))
	for _, sp := range spans {
		projectID := goUUID(sp.ProjectID)
		runID := goUUID(sp.RunID)

		var parentSpanID *string
		if sp.ParentSpanID != "" {
			parentSpanID = &sp.ParentSpanID
		}
		var endTime *time.Time
		if sp.EndTime != nil {
			endTime = sp.EndTime
		}
		var statusMsg *string
		if sp.StatusMessage != "" {
			statusMsg = &sp.StatusMessage
		}

		ts = append(ts, tracestore.TraceSpan{
			ProjectID:     projectID,
			RunID:         runID,
			TraceID:       sp.TraceID,
			SpanID:        sp.SpanID,
			ParentSpanID:  parentSpanID,
			Name:          sp.Name,
			Kind:          sp.Kind,
			StatusCode:    sp.StatusCode,
			StatusMessage: statusMsg,
			StartTime:     sp.StartTime,
			EndTime:       endTime,
			DurationMs:    sp.DurationMs,
			Attributes:    sp.Attributes,
			Truncated:     sp.Truncated,
			IngestedAt:    time.Now(),
		})
	}
	return h.ts.InsertSpans(ctx, ts)
}

func (h *HTTPTracestore) InsertSpanEvents(ctx context.Context, events []ingest.SpanEventRecord) error {
	ts := make([]tracestore.TraceSpanEvent, 0, len(events))
	for _, ev := range events {
		ts = append(ts, tracestore.TraceSpanEvent{
			ProjectID:  goUUID(ev.ProjectID),
			RunID:      goUUID(ev.RunID),
			TraceID:    ev.TraceID,
			SpanID:     ev.SpanID,
			EventName:  ev.EventName,
			EventTime:  ev.EventTime,
			Attributes: ev.Attributes,
			Truncated:  ev.Truncated,
			IngestedAt: time.Now(),
		})
	}
	return h.ts.InsertSpanEvents(ctx, ts)
}

func (h *HTTPTracestore) InsertLLMCalls(ctx context.Context, calls []ingest.LLMCallRecord) error {
	ts := make([]tracestore.TraceLLMCall, 0, len(calls))
	for _, c := range calls {
		var agentID, agentInstanceID *string
		if c.AgentID != "" {
			agentID = &c.AgentID
		}
		if c.AgentInstanceID != "" {
			agentInstanceID = &c.AgentInstanceID
		}
		var errStr *string
		if c.Error != "" {
			errStr = &c.Error
		}
		ts = append(ts, tracestore.TraceLLMCall{
			ProjectID:        goUUID(c.ProjectID),
			RunID:            goUUID(c.RunID),
			TraceID:          c.TraceID,
			SpanID:           c.SpanID,
			AgentID:          agentID,
			AgentInstanceID:  agentInstanceID,
			Provider:         c.Provider,
			Model:            c.Model,
			InputTokens:      c.InputTokens,
			OutputTokens:     c.OutputTokens,
			TotalTokens:      c.TotalTokens,
			CacheReadTokens:  c.CacheReadTokens,
			CacheWriteTokens: c.CacheWriteTokens,
			TTFTMs:           c.TTFTMs,
			Cost:             c.Cost,
			EstimatedCost:    c.EstimatedCost,
			ActualCost:       c.ActualCost,
			CostSource:       c.CostSource,
			InputSummary:     c.InputSummary,
			OutputSummary:    c.OutputSummary,
			ContentCaptured:  c.ContentCaptured,
			Status:           c.Status,
			Error:            errStr,
			StartTime:        c.StartTime,
			DurationMs:       c.DurationMs,
			IngestedAt:       time.Now(),
		})
	}
	return h.ts.InsertLLMCalls(ctx, ts)
}

func (h *HTTPTracestore) InsertToolCalls(ctx context.Context, calls []ingest.ToolCallRecord) error {
	ts := make([]tracestore.TraceToolCall, 0, len(calls))
	for _, c := range calls {
		var agentID, agentInstanceID *string
		if c.AgentID != "" {
			agentID = &c.AgentID
		}
		if c.AgentInstanceID != "" {
			agentInstanceID = &c.AgentInstanceID
		}
		var errStr *string
		if c.Error != "" {
			errStr = &c.Error
		}
		var risk *string
		if c.SideEffectRisk != "" {
			risk = &c.SideEffectRisk
		}
		ts = append(ts, tracestore.TraceToolCall{
			ProjectID:        goUUID(c.ProjectID),
			RunID:            goUUID(c.RunID),
			TraceID:          c.TraceID,
			SpanID:           c.SpanID,
			AgentID:          agentID,
			AgentInstanceID:  agentInstanceID,
			ToolName:         c.ToolName,
			ToolType:         c.ToolType,
			ArgumentsSummary: c.ArgumentsSummary,
			ResultSummary:    c.ResultSummary,
			Success:          c.Success,
			Error:            errStr,
			RetryCount:       c.RetryCount,
			SideEffectRisk:   risk,
			StartTime:        c.StartTime,
			DurationMs:       c.DurationMs,
			IngestedAt:       time.Now(),
		})
	}
	return h.ts.InsertToolCalls(ctx, ts)
}

func (h *HTTPTracestore) InsertRetrievalCalls(ctx context.Context, calls []ingest.RetrievalCallRecord) error {
	ts := make([]tracestore.TraceRetrievalCall, 0, len(calls))
	for _, c := range calls {
		var agentID, agentInstanceID *string
		if c.AgentID != "" {
			agentID = &c.AgentID
		}
		if c.AgentInstanceID != "" {
			agentInstanceID = &c.AgentInstanceID
		}
		ts = append(ts, tracestore.TraceRetrievalCall{
			ProjectID:       goUUID(c.ProjectID),
			RunID:           goUUID(c.RunID),
			TraceID:         c.TraceID,
			SpanID:          c.SpanID,
			AgentID:         agentID,
			AgentInstanceID: agentInstanceID,
			KnowledgeBase:   c.KnowledgeBase,
			QueryText:       c.QueryText,
			TopK:            c.TopK,
			ReturnedCount:   c.ReturnedCount,
			DocumentIDs:     c.DocumentIDs,
			Scores:          c.Scores,
			StartTime:       c.StartTime,
			DurationMs:      c.DurationMs,
			IngestedAt:      time.Now(),
		})
	}
	return h.ts.InsertRetrievalCalls(ctx, ts)
}

func (h *HTTPTracestore) InsertSkillCalls(ctx context.Context, calls []ingest.SkillCallRecord) error {
	ts := make([]tracestore.TraceSkillCall, 0, len(calls))
	for _, c := range calls {
		var agentID, agentInstanceID *string
		if c.AgentID != "" {
			agentID = &c.AgentID
		}
		if c.AgentInstanceID != "" {
			agentInstanceID = &c.AgentInstanceID
		}
		var errStr *string
		if c.Error != "" {
			errStr = &c.Error
		}
		ts = append(ts, tracestore.TraceSkillCall{
			ProjectID:       goUUID(c.ProjectID),
			RunID:           goUUID(c.RunID),
			TraceID:         c.TraceID,
			SpanID:          c.SpanID,
			AgentID:         agentID,
			AgentInstanceID: agentInstanceID,
			SkillName:       c.SkillName,
			SkillVersion:    c.SkillVersion,
			InputSummary:    c.InputSummary,
			OutputSummary:   c.OutputSummary,
			Success:         c.Success,
			Error:           errStr,
			StartTime:       c.StartTime,
			DurationMs:      c.DurationMs,
			IngestedAt:      time.Now(),
		})
	}
	return h.ts.InsertSkillCalls(ctx, ts)
}

func (h *HTTPTracestore) InsertThoughts(ctx context.Context, thoughts []ingest.ThoughtRecord) error {
	ts := make([]tracestore.TraceThought, 0, len(thoughts))
	for _, th := range thoughts {
		ts = append(ts, tracestore.TraceThought{
			ProjectID:       goUUID(th.ProjectID),
			RunID:           goUUID(th.RunID),
			TraceID:         th.TraceID,
			SpanID:          th.SpanID,
			AgentID:         th.AgentID,
			AgentInstanceID: th.AgentInstanceID,
			Stage:           th.Stage,
			Summary:         th.Summary,
			Content:         th.Content,
			Confidence:      th.Confidence,
			ContentCaptured: th.ContentCaptured,
			StartTime:       th.StartTime,
			DurationMs:      th.DurationMs,
			IngestedAt:      time.Now(),
		})
	}
	return h.ts.InsertThoughts(ctx, ts)
}

func (h *HTTPTracestore) InsertFinalAnswers(ctx context.Context, answers []ingest.FinalAnswerRecord) error {
	ts := make([]tracestore.TraceFinalAnswer, 0, len(answers))
	for _, fa := range answers {
		ts = append(ts, tracestore.TraceFinalAnswer{
			ProjectID:        goUUID(fa.ProjectID),
			RunID:            goUUID(fa.RunID),
			TraceID:          fa.TraceID,
			SpanID:           fa.SpanID,
			AnswerSummary:    fa.AnswerSummary,
			AnswerOutput:     fa.AnswerOutput,
			StructuredOutput: fa.StructuredOutput,
			StartTime:        fa.StartTime,
			DurationMs:       fa.DurationMs,
			IngestedAt:       time.Now(),
		})
	}
	return h.ts.InsertFinalAnswers(ctx, ts)
}
