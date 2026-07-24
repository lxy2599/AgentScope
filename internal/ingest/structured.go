package ingest

import (
	"time"
)

// ---- Structured Call Record Types ----

// LLMCallRecord represents a structured LLM call to be written to ClickHouse.
type LLMCallRecord struct {
	ProjectID        string    `json:"project_id"`
	RunID            string    `json:"run_id"`
	TraceID          string    `json:"trace_id"`
	SpanID           string    `json:"span_id"`
	AgentID          string    `json:"agent_id,omitempty"`
	AgentInstanceID  string    `json:"agent_instance_id,omitempty"`
	Provider         string    `json:"provider"`
	Model            string    `json:"model"`
	InputTokens      *int64    `json:"input_tokens,omitempty"`
	OutputTokens     *int64    `json:"output_tokens,omitempty"`
	TotalTokens      *int64    `json:"total_tokens,omitempty"`
	CacheReadTokens  *int64    `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens *int64    `json:"cache_write_tokens,omitempty"`
	TTFTMs           *int64    `json:"ttft_ms,omitempty"`
	Cost             *float64  `json:"cost,omitempty"`
	EstimatedCost    *float64  `json:"estimated_cost,omitempty"`
	ActualCost       *float64  `json:"actual_cost,omitempty"`
	CostSource       string    `json:"cost_source,omitempty"`
	InputSummary     string    `json:"input_summary,omitempty"`
	OutputSummary    string    `json:"output_summary,omitempty"`
	ContentCaptured  bool      `json:"content_captured"`
	Status           string    `json:"status"`
	Error            string    `json:"error,omitempty"`
	StartTime        time.Time `json:"start_time"`
	DurationMs       *int64    `json:"duration_ms,omitempty"`
}

// ToolCallRecord represents a structured tool call to be written to ClickHouse.
type ToolCallRecord struct {
	ProjectID        string    `json:"project_id"`
	RunID            string    `json:"run_id"`
	TraceID          string    `json:"trace_id"`
	SpanID           string    `json:"span_id"`
	AgentID          string    `json:"agent_id,omitempty"`
	AgentInstanceID  string    `json:"agent_instance_id,omitempty"`
	ToolName         string    `json:"tool_name"`
	ToolType         string    `json:"tool_type,omitempty"`
	ArgumentsSummary string    `json:"arguments_summary,omitempty"`
	ResultSummary    string    `json:"result_summary,omitempty"`
	Success          bool      `json:"success"`
	Error            string    `json:"error,omitempty"`
	RetryCount       int64     `json:"retry_count"`
	SideEffectRisk   string    `json:"side_effect_risk,omitempty"`
	StartTime        time.Time `json:"start_time"`
	DurationMs       *int64    `json:"duration_ms,omitempty"`
}

// RetrievalCallRecord represents a structured knowledge base retrieval call.
type RetrievalCallRecord struct {
	ProjectID       string    `json:"project_id"`
	RunID           string    `json:"run_id"`
	TraceID         string    `json:"trace_id"`
	SpanID          string    `json:"span_id"`
	AgentID         string    `json:"agent_id,omitempty"`
	AgentInstanceID string    `json:"agent_instance_id,omitempty"`
	KnowledgeBase   string    `json:"knowledge_base"`
	QueryText       string    `json:"query_text"`
	TopK            *int64    `json:"top_k,omitempty"`
	ReturnedCount   *int64    `json:"returned_count,omitempty"`
	DocumentIDs     []string  `json:"document_ids,omitempty"`
	Scores          []float64 `json:"scores,omitempty"`
	StartTime       time.Time `json:"start_time"`
	DurationMs      *int64    `json:"duration_ms,omitempty"`
}

// SkillCallRecord represents a structured skill invocation.
type SkillCallRecord struct {
	ProjectID       string    `json:"project_id"`
	RunID           string    `json:"run_id"`
	TraceID         string    `json:"trace_id"`
	SpanID          string    `json:"span_id"`
	AgentID         string    `json:"agent_id,omitempty"`
	AgentInstanceID string    `json:"agent_instance_id,omitempty"`
	SkillName       string    `json:"skill_name"`
	SkillVersion    string    `json:"skill_version,omitempty"`
	InputSummary    string    `json:"input_summary,omitempty"`
	OutputSummary   string    `json:"output_summary,omitempty"`
	Success         bool      `json:"success"`
	Error           string    `json:"error,omitempty"`
	StartTime       time.Time `json:"start_time"`
	DurationMs      *int64    `json:"duration_ms,omitempty"`
}

// ThoughtRecord represents a structured agent thought extracted from a THOUGHT span.
type ThoughtRecord struct {
	ProjectID       string    `json:"project_id"`
	RunID           string    `json:"run_id"`
	TraceID         string    `json:"trace_id"`
	SpanID          string    `json:"span_id"`
	AgentID         string    `json:"agent_id,omitempty"`
	AgentInstanceID string    `json:"agent_instance_id,omitempty"`
	Stage           string    `json:"stage,omitempty"`
	Summary         string    `json:"summary"`
	Content         string    `json:"content,omitempty"`
	Confidence      *float64  `json:"confidence,omitempty"`
	ContentCaptured bool      `json:"content_captured"`
	StartTime       time.Time `json:"start_time"`
	DurationMs      *int64    `json:"duration_ms,omitempty"`
}

// FinalAnswerRecord represents a structured agent final answer extracted from a FINAL_ANSWER span.
type FinalAnswerRecord struct {
	ProjectID        string    `json:"project_id"`
	RunID            string    `json:"run_id"`
	TraceID          string    `json:"trace_id"`
	SpanID           string    `json:"span_id"`
	AnswerSummary    string    `json:"answer_summary"`
	AnswerOutput     string    `json:"answer_output,omitempty"`
	StructuredOutput string    `json:"structured_output,omitempty"`
	StartTime        time.Time `json:"start_time"`
	DurationMs       *int64    `json:"duration_ms,omitempty"`
}

// SDKExportMetrics contains content-free Python SDK exporter health values.
type SDKExportMetrics struct {
	QueueDepth      *int64
	QueueCapacity   *int64
	SpansDropped    *int64
	ExportRetries   *int64
	ExportLatencyMs *float64
}

// ExtractSDKExportMetrics returns the latest metrics span in an OTLP batch.
func ExtractSDKExportMetrics(spans []SpanRecord) *SDKExportMetrics {
	var result *SDKExportMetrics
	for _, sp := range spans {
		attrs := MustParseAttributes(sp.Attributes)
		if sp.Name != "agentscope.sdk.export_metrics" &&
			AttrString(attrs, "agentscope.span.kind") != "SDK_EXPORT_METRICS" {
			continue
		}
		result = &SDKExportMetrics{
			QueueDepth:      AttrInt(attrs, "agentscope.sdk.export.queue_depth"),
			QueueCapacity:   AttrInt(attrs, "agentscope.sdk.export.queue_capacity"),
			SpansDropped:    AttrInt(attrs, "agentscope.sdk.export.spans_dropped"),
			ExportRetries:   AttrInt(attrs, "agentscope.sdk.export.retries"),
			ExportLatencyMs: AttrFloat(attrs, "agentscope.sdk.export.last_export_latency_ms"),
		}
	}
	return result
}

// ---- Extraction ----

// ExtractStructuredCalls scans decoded span records and extracts structured
// LLM/tool/retrieval/skill/thought/final-answer calls from span attributes.
// Uses the centralized JSON parser (attrs.go) instead of fragile string scanning.
//
// Raw spans continue to be written to trace_spans as before.
func ExtractStructuredCalls(spans []SpanRecord) ([]LLMCallRecord, []ToolCallRecord, []RetrievalCallRecord, []SkillCallRecord, []ThoughtRecord, []FinalAnswerRecord) {
	var llmCalls []LLMCallRecord
	var toolCalls []ToolCallRecord
	var retrievalCalls []RetrievalCallRecord
	var skillCalls []SkillCallRecord
	var thoughts []ThoughtRecord
	var finalAnswers []FinalAnswerRecord

	for _, sp := range spans {
		attrs := MustParseAttributes(sp.Attributes)
		spanKind := detectCallKind(attrs)
		if spanKind == "" {
			continue
		}

		agentID := AttrString(attrs, "agentscope.agent.id")
		agentInstanceID := AttrString(attrs, "agentscope.agent.instance_id")

		switch spanKind {
		case "LLM":
			llm := extractLLMCall(sp, attrs, agentID, agentInstanceID)
			llmCalls = append(llmCalls, llm)
		case "TOOL":
			tc := extractToolCall(sp, attrs, agentID, agentInstanceID)
			toolCalls = append(toolCalls, tc)
		case "RETRIEVER":
			rc := extractRetrievalCall(sp, attrs, agentID, agentInstanceID)
			retrievalCalls = append(retrievalCalls, rc)
		case "SKILL":
			sc := extractSkillCall(sp, attrs, agentID, agentInstanceID)
			skillCalls = append(skillCalls, sc)
		case "THOUGHT":
			tr := extractThought(sp, attrs, agentID, agentInstanceID)
			thoughts = append(thoughts, tr)
		case "FINAL_ANSWER":
			fa := extractFinalAnswer(sp, attrs)
			finalAnswers = append(finalAnswers, fa)
		}
	}

	return llmCalls, toolCalls, retrievalCalls, skillCalls, thoughts, finalAnswers
}

// detectCallKind determines the structured call kind from span attributes.
// Checks in priority: agentscope.span.kind, openinference.span.kind.
func detectCallKind(attrs map[string]any) string {
	if kind := AttrString(attrs, "agentscope.span.kind"); kind != "" {
		return kind
	}
	return AttrString(attrs, "openinference.span.kind")
}

// extractLLMCall extracts an LLM call record from a span.
func extractLLMCall(sp SpanRecord, attrs map[string]any, agentID, agentInstanceID string) LLMCallRecord {
	provider := AttrString(attrs, "llm.provider")
	if provider == "" {
		provider = AttrString(attrs, "gen_ai.request.model")
	}
	model := AttrString(attrs, "llm.model")
	if model == "" {
		model = AttrString(attrs, "gen_ai.response.model")
	}

	inputTokens := AttrInt(attrs, "llm.input_tokens")
	outputTokens := AttrInt(attrs, "llm.output_tokens")
	totalTokens := AttrInt(attrs, "llm.total_tokens")
	if inputTokens != nil && outputTokens != nil {
		t := *inputTokens + *outputTokens
		if totalTokens == nil || *totalTokens < t {
			totalTokens = &t
		}
	}

	cost := AttrFloat(attrs, "llm.cost")
	estimatedCost := AttrFloat(attrs, "llm.estimated_cost")
	actualCost := AttrFloat(attrs, "llm.actual_cost")
	costSource := AttrString(attrs, "llm.cost_source")
	if actualCost != nil {
		cost = actualCost
		costSource = "actual"
	} else if cost == nil && estimatedCost != nil {
		cost = estimatedCost
		costSource = "estimated"
	}

	inputSummary := AttrString(attrs, "llm.input_summary")
	if inputSummary == "" {
		inputSummary = AttrString(attrs, "llm.prompt_preview")
	}
	outputSummary := AttrString(attrs, "llm.output_summary")
	if outputSummary == "" {
		outputSummary = AttrString(attrs, "llm.output_preview")
	}

	contentCaptured := AttrBool(attrs, "agentscope.content_captured")
	durationMs := sp.DurationMs
	if explicitDuration := AttrInt(attrs, "llm.duration_ms"); explicitDuration != nil {
		durationMs = explicitDuration
	}

	return LLMCallRecord{
		ProjectID:        sp.ProjectID,
		RunID:            sp.RunID,
		TraceID:          sp.TraceID,
		SpanID:           sp.SpanID,
		AgentID:          agentID,
		AgentInstanceID:  agentInstanceID,
		Provider:         provider,
		Model:            model,
		InputTokens:      inputTokens,
		OutputTokens:     outputTokens,
		TotalTokens:      totalTokens,
		CacheReadTokens:  AttrInt(attrs, "llm.cache_read_tokens"),
		CacheWriteTokens: AttrInt(attrs, "llm.cache_write_tokens"),
		TTFTMs:           AttrInt(attrs, "llm.ttft_ms"),
		Cost:             cost,
		EstimatedCost:    estimatedCost,
		ActualCost:       actualCost,
		CostSource:       costSource,
		InputSummary:     inputSummary,
		OutputSummary:    outputSummary,
		ContentCaptured:  contentCaptured,
		Status:           sp.StatusCode,
		Error:            sp.StatusMessage,
		StartTime:        sp.StartTime,
		DurationMs:       durationMs,
	}
}

// extractToolCall extracts a tool call record from a span.
func extractToolCall(sp SpanRecord, attrs map[string]any, agentID, agentInstanceID string) ToolCallRecord {
	success := AttrBool(attrs, "tool.success")

	errorStr := AttrString(attrs, "tool.error")
	if errorStr == "" {
		errorStr = sp.StatusMessage
	}

	durationMs := sp.DurationMs
	if explicitDuration := AttrInt(attrs, "tool.duration_ms"); explicitDuration != nil {
		durationMs = explicitDuration
	}

	return ToolCallRecord{
		ProjectID:        sp.ProjectID,
		RunID:            sp.RunID,
		TraceID:          sp.TraceID,
		SpanID:           sp.SpanID,
		AgentID:          agentID,
		AgentInstanceID:  agentInstanceID,
		ToolName:         AttrString(attrs, "tool.name"),
		ToolType:         AttrString(attrs, "tool.type"),
		ArgumentsSummary: AttrString(attrs, "tool.arguments_summary"),
		ResultSummary:    AttrString(attrs, "tool.result_summary"),
		Success:          success,
		Error:            errorStr,
		RetryCount:       AttrInt64(attrs, "tool.retry_count"),
		SideEffectRisk:   AttrString(attrs, "tool.side_effect_risk"),
		StartTime:        sp.StartTime,
		DurationMs:       durationMs,
	}
}

// extractRetrievalCall extracts a retrieval call record from a span.
func extractRetrievalCall(sp SpanRecord, attrs map[string]any, agentID, agentInstanceID string) RetrievalCallRecord {
	return RetrievalCallRecord{
		ProjectID:       sp.ProjectID,
		RunID:           sp.RunID,
		TraceID:         sp.TraceID,
		SpanID:          sp.SpanID,
		AgentID:         agentID,
		AgentInstanceID: agentInstanceID,
		KnowledgeBase:   AttrString(attrs, "retrieval.knowledge_base"),
		QueryText:       AttrString(attrs, "retrieval.query"),
		TopK:            AttrInt(attrs, "retrieval.top_k"),
		ReturnedCount:   AttrInt(attrs, "retrieval.returned_count"),
		DocumentIDs:     AttrStringArray(attrs, "retrieval.document_ids"),
		Scores:          AttrFloatArray(attrs, "retrieval.scores"),
		StartTime:       sp.StartTime,
		DurationMs:      sp.DurationMs,
	}
}

// extractSkillCall extracts a skill call record from a span.
func extractSkillCall(sp SpanRecord, attrs map[string]any, agentID, agentInstanceID string) SkillCallRecord {
	success := AttrBool(attrs, "skill.success")

	errorStr := AttrString(attrs, "skill.error")
	if errorStr == "" {
		errorStr = sp.StatusMessage
	}

	return SkillCallRecord{
		ProjectID:       sp.ProjectID,
		RunID:           sp.RunID,
		TraceID:         sp.TraceID,
		SpanID:          sp.SpanID,
		AgentID:         agentID,
		AgentInstanceID: agentInstanceID,
		SkillName:       AttrString(attrs, "skill.name"),
		SkillVersion:    AttrString(attrs, "skill.version"),
		InputSummary:    AttrString(attrs, "skill.input_summary"),
		OutputSummary:   AttrString(attrs, "skill.output_summary"),
		Success:         success,
		Error:           errorStr,
		StartTime:       sp.StartTime,
		DurationMs:      sp.DurationMs,
	}
}

// extractThought extracts a thought record from a THOUGHT span.
func extractThought(sp SpanRecord, attrs map[string]any, agentID, agentInstanceID string) ThoughtRecord {
	confidence := AttrFloat(attrs, "thought.confidence")

	return ThoughtRecord{
		ProjectID:       sp.ProjectID,
		RunID:           sp.RunID,
		TraceID:         sp.TraceID,
		SpanID:          sp.SpanID,
		AgentID:         agentID,
		AgentInstanceID: agentInstanceID,
		Stage:           AttrString(attrs, "thought.stage"),
		Summary:         AttrString(attrs, "thought.summary"),
		Content:         AttrString(attrs, "thought.content"),
		Confidence:      confidence,
		ContentCaptured: AttrBool(attrs, "agentscope.content_captured"),
		StartTime:       sp.StartTime,
		DurationMs:      sp.DurationMs,
	}
}

// extractFinalAnswer extracts a final answer record from a FINAL_ANSWER span.
func extractFinalAnswer(sp SpanRecord, attrs map[string]any) FinalAnswerRecord {
	return FinalAnswerRecord{
		ProjectID:        sp.ProjectID,
		RunID:            sp.RunID,
		TraceID:          sp.TraceID,
		SpanID:           sp.SpanID,
		AnswerSummary:    AttrString(attrs, "answer.summary"),
		AnswerOutput:     AttrString(attrs, "answer.output"),
		StructuredOutput: AttrString(attrs, "answer.structured_output"),
		StartTime:        sp.StartTime,
		DurationMs:       sp.DurationMs,
	}
}
