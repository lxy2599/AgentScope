package ingest

import (
	"strconv"
	"strings"
	"time"
)

// ---- Structured Call Record Types ----

// LLMCallRecord represents a structured LLM call to be written to ClickHouse.
type LLMCallRecord struct {
	ProjectID       string    `json:"project_id"`
	RunID           string    `json:"run_id"`
	TraceID         string    `json:"trace_id"`
	SpanID          string    `json:"span_id"`
	AgentID         string    `json:"agent_id,omitempty"`
	AgentInstanceID string    `json:"agent_instance_id,omitempty"`
	Provider        string    `json:"provider"`
	Model           string    `json:"model"`
	InputTokens     *int64    `json:"input_tokens,omitempty"`
	OutputTokens    *int64    `json:"output_tokens,omitempty"`
	TotalTokens     *int64    `json:"total_tokens,omitempty"`
	Cost            *float64  `json:"cost,omitempty"`
	InputSummary    string    `json:"input_summary,omitempty"`
	OutputSummary   string    `json:"output_summary,omitempty"`
	ContentCaptured bool      `json:"content_captured"`
	Status          string    `json:"status"`
	Error           string    `json:"error,omitempty"`
	StartTime       time.Time `json:"start_time"`
	DurationMs      *int64    `json:"duration_ms,omitempty"`
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

// ExtractStructuredCalls scans decoded span records and extracts structured
// LLM/tool/retrieval/skill calls from span attributes.
// Detection is based on:
//   - agentscope.span.kind or openinference.span.kind for call type
//   - agentscope.* attributes for structured fields
//
// Raw spans continue to be written to trace_spans as before.
func ExtractStructuredCalls(spans []SpanRecord) ([]LLMCallRecord, []ToolCallRecord, []RetrievalCallRecord, []SkillCallRecord) {
	var llmCalls []LLMCallRecord
	var toolCalls []ToolCallRecord
	var retrievalCalls []RetrievalCallRecord
	var skillCalls []SkillCallRecord

	for _, sp := range spans {
		spanKind := detectCallKind(sp.Attributes)
		if spanKind == "" {
			continue
		}

		agentID := extractAttr(sp.Attributes, "agentscope.agent.id")
		agentInstanceID := extractAttr(sp.Attributes, "agentscope.agent.instance_id")

		switch spanKind {
		case "LLM":
			llm := extractLLMCall(sp, agentID, agentInstanceID)
			llmCalls = append(llmCalls, llm)
		case "TOOL":
			tc := extractToolCall(sp, agentID, agentInstanceID)
			toolCalls = append(toolCalls, tc)
		case "RETRIEVER":
			rc := extractRetrievalCall(sp, agentID, agentInstanceID)
			retrievalCalls = append(retrievalCalls, rc)
		case "SKILL":
			sc := extractSkillCall(sp, agentID, agentInstanceID)
			skillCalls = append(skillCalls, sc)
		}
	}

	return llmCalls, toolCalls, retrievalCalls, skillCalls
}

// detectCallKind determines the structured call kind from span attributes.
// Checks in priority: agentscope.span.kind, openinference.span.kind.
func detectCallKind(attrs string) string {
	kind := extractAttr(attrs, "agentscope.span.kind")
	if kind != "" {
		return kind
	}
	kind = extractAttr(attrs, "openinference.span.kind")
	return kind
}

// extractLLMCall extracts an LLM call record from a span.
func extractLLMCall(sp SpanRecord, agentID, agentInstanceID string) LLMCallRecord {
	attrs := sp.Attributes
	provider := extractAttr(attrs, "llm.provider")
	if provider == "" {
		provider = extractAttr(attrs, "gen_ai.request.model")
	}
	model := extractAttr(attrs, "llm.model")
	if model == "" {
		model = extractAttr(attrs, "gen_ai.response.model")
	}

	inputTokens := parseIntAttr(attrs, "llm.input_tokens")
	outputTokens := parseIntAttr(attrs, "llm.output_tokens")
	totalTokens := parseIntAttr(attrs, "llm.total_tokens")
	if totalTokens == nil && inputTokens != nil && outputTokens != nil {
		t := *inputTokens + *outputTokens
		totalTokens = &t
	}

	cost := parseFloatAttr(attrs, "llm.cost")

	inputSummary := extractAttr(attrs, "llm.input_summary")
	if inputSummary == "" {
		inputSummary = extractAttr(attrs, "llm.prompt_preview")
	}
	outputSummary := extractAttr(attrs, "llm.output_summary")
	if outputSummary == "" {
		outputSummary = extractAttr(attrs, "llm.output_preview")
	}

	contentCaptured := parseBoolAttr(attrs, "agentscope.content_captured")

	status := sp.StatusCode
	errorStr := sp.StatusMessage

	return LLMCallRecord{
		ProjectID:       sp.ProjectID,
		RunID:           sp.RunID,
		TraceID:         sp.TraceID,
		SpanID:          sp.SpanID,
		AgentID:         agentID,
		AgentInstanceID: agentInstanceID,
		Provider:        provider,
		Model:           model,
		InputTokens:     inputTokens,
		OutputTokens:    outputTokens,
		TotalTokens:     totalTokens,
		Cost:            cost,
		InputSummary:    inputSummary,
		OutputSummary:   outputSummary,
		ContentCaptured: contentCaptured,
		Status:          status,
		Error:           errorStr,
		StartTime:       sp.StartTime,
		DurationMs:      sp.DurationMs,
	}
}

// extractToolCall extracts a tool call record from a span.
func extractToolCall(sp SpanRecord, agentID, agentInstanceID string) ToolCallRecord {
	attrs := sp.Attributes
	success := parseBoolAttr(attrs, "tool.success")

	errorStr := extractAttr(attrs, "tool.error")
	if errorStr == "" {
		errorStr = sp.StatusMessage
	}

	return ToolCallRecord{
		ProjectID:        sp.ProjectID,
		RunID:            sp.RunID,
		TraceID:          sp.TraceID,
		SpanID:           sp.SpanID,
		AgentID:          agentID,
		AgentInstanceID:  agentInstanceID,
		ToolName:         extractAttr(attrs, "tool.name"),
		ToolType:         extractAttr(attrs, "tool.type"),
		ArgumentsSummary: extractAttr(attrs, "tool.arguments_summary"),
		ResultSummary:    extractAttr(attrs, "tool.result_summary"),
		Success:          success,
		Error:            errorStr,
		RetryCount:       parseInt64Attr(attrs, "tool.retry_count"),
		SideEffectRisk:   extractAttr(attrs, "tool.side_effect_risk"),
		StartTime:        sp.StartTime,
		DurationMs:       sp.DurationMs,
	}
}

// extractRetrievalCall extracts a retrieval call record from a span.
func extractRetrievalCall(sp SpanRecord, agentID, agentInstanceID string) RetrievalCallRecord {
	attrs := sp.Attributes

	return RetrievalCallRecord{
		ProjectID:       sp.ProjectID,
		RunID:           sp.RunID,
		TraceID:         sp.TraceID,
		SpanID:          sp.SpanID,
		AgentID:         agentID,
		AgentInstanceID: agentInstanceID,
		KnowledgeBase:   extractAttr(attrs, "retrieval.knowledge_base"),
		QueryText:       extractAttr(attrs, "retrieval.query"),
		TopK:            parseIntAttr(attrs, "retrieval.top_k"),
		ReturnedCount:   parseIntAttr(attrs, "retrieval.returned_count"),
		DocumentIDs:     extractStringArray(attrs, "retrieval.document_ids"),
		Scores:          extractFloatArray(attrs, "retrieval.scores"),
		StartTime:       sp.StartTime,
		DurationMs:      sp.DurationMs,
	}
}

// extractSkillCall extracts a skill call record from a span.
func extractSkillCall(sp SpanRecord, agentID, agentInstanceID string) SkillCallRecord {
	attrs := sp.Attributes
	success := parseBoolAttr(attrs, "skill.success")

	errorStr := extractAttr(attrs, "skill.error")
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
		SkillName:       extractAttr(attrs, "skill.name"),
		SkillVersion:    extractAttr(attrs, "skill.version"),
		InputSummary:    extractAttr(attrs, "skill.input_summary"),
		OutputSummary:   extractAttr(attrs, "skill.output_summary"),
		Success:         success,
		Error:           errorStr,
		StartTime:       sp.StartTime,
		DurationMs:      sp.DurationMs,
	}
}

// ---- Attribute parsing helpers ----

// parseIntAttr extracts an int64 attribute from a JSON-like attributes string.
func parseIntAttr(attrs, key string) *int64 {
	v := extractValueByKey(attrs, key)
	if v == "" {
		return nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return nil
	}
	return &n
}

// parseInt64Attr extracts an int64 attribute with default 0.
func parseInt64Attr(attrs, key string) int64 {
	v := extractValueByKey(attrs, key)
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// parseFloatAttr extracts a float64 attribute from a JSON-like attributes string.
func parseFloatAttr(attrs, key string) *float64 {
	v := extractValueByKey(attrs, key)
	if v == "" {
		return nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil
	}
	return &f
}

// parseBoolAttr extracts a bool attribute from a JSON-like attributes string.
func parseBoolAttr(attrs, key string) bool {
	v := extractValueByKey(attrs, key)
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false
	}
	return b
}

// extractStringArray parses a JSON array of strings from an attribute.
func extractStringArray(attrs, key string) []string {
	v := extractValueByKey(attrs, key)
	if v == "" {
		return nil
	}
	// Handle JSON array format: ["a","b","c"]
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "[") || !strings.HasSuffix(v, "]") {
		return nil
	}
	inner := v[1 : len(v)-1]
	if inner == "" {
		return nil
	}
	parts := strings.Split(inner, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, "\"")
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// extractFloatArray parses a JSON array of floats from an attribute.
func extractFloatArray(attrs, key string) []float64 {
	v := extractValueByKey(attrs, key)
	if v == "" {
		return nil
	}
	v = strings.TrimSpace(v)
	if !strings.HasPrefix(v, "[") || !strings.HasSuffix(v, "]") {
		return nil
	}
	inner := v[1 : len(v)-1]
	if inner == "" {
		return nil
	}
	parts := strings.Split(inner, ",")
	result := make([]float64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		f, err := strconv.ParseFloat(p, 64)
		if err != nil {
			continue
		}
		result = append(result, f)
	}
	return result
}

// ---- JSON-like attribute extraction ----

// extractAttr does a simple key:"value" extraction from a JSON-like attribute string.
func extractAttr(attrs, key string) string {
	prefix := `"` + key + `":`
	idx := strings.Index(attrs, prefix)
	if idx < 0 {
		return ""
	}
	start := idx + len(prefix)
	if start >= len(attrs) {
		return ""
	}
	if attrs[start] != '"' {
		return ""
	}
	start++ // skip opening quote
	end := strings.IndexByte(attrs[start:], '"')
	if end < 0 {
		return ""
	}
	return attrs[start : start+end]
}

// extractValueByKey extracts the raw JSON value for a key from a JSON-like attributes string.
// The format is key:"value" or key:123 or key:true for simple values,
// and can be key:["a","b"] or key:[1,2,3] for arrays.
func extractValueByKey(attrs, key string) string {
	prefix := `"` + key + `":`
	idx := strings.Index(attrs, prefix)
	if idx < 0 {
		return ""
	}
	start := idx + len(prefix)
	if start >= len(attrs) {
		return ""
	}

	// Check if value is a JSON string (starts with ")
	if start < len(attrs) && attrs[start] == '"' {
		start++ // skip opening quote
		end := strings.IndexByte(attrs[start:], '"')
		if end < 0 {
			return ""
		}
		return attrs[start : start+end]
	}

	// Check if value is a JSON array (starts with [)
	if start < len(attrs) && attrs[start] == '[' {
		depth := 0
		for i := start; i < len(attrs); i++ {
			switch attrs[i] {
			case '[':
				depth++
			case ']':
				depth--
				if depth == 0 {
					return attrs[start : i+1]
				}
			case '"':
				// Skip string inside array
				i++
				for i < len(attrs) && attrs[i] != '"' {
					if attrs[i] == '\\' {
						i++
					}
					i++
				}
			}
		}
		return ""
	}

	// Numeric or boolean value: read until comma or closing brace
	end := strings.IndexAny(attrs[start:], ",}")
	if end < 0 {
		return attrs[start:]
	}
	return attrs[start : start+end]
}
