package ingest

import (
	"strings"
	"testing"

	common "go.opentelemetry.io/proto/otlp/common/v1"
	trace "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
	"time"
)

func int64Ptr(i int64) *int64 { return &i }

func TestExtractStructuredCalls_LLM(t *testing.T) {
	now := time.Now()
	attrs := `{"agentscope.span.kind":"LLM","llm.provider":"openai","llm.model":"gpt-4","llm.input_tokens":50,"llm.output_tokens":100,"llm.total_tokens":150,"llm.cost":0.002,"llm.input_summary":"What is the capital?","llm.output_summary":"Paris","agentscope.agent.id":"agent-1","agentscope.agent.instance_id":"inst-1"}`

	spans := []SpanRecord{{
		ProjectID:  "proj-1",
		RunID:      "run-1",
		TraceID:    "trace-1",
		SpanID:     "span-llm-001",
		Name:       "llm.call",
		Kind:       "INTERNAL",
		StatusCode: "OK",
		StartTime:  now,
		DurationMs: int64Ptr(500),
		Attributes: attrs,
	}}

	llmCalls, toolCalls, retrievalCalls, skillCalls := ExtractStructuredCalls(spans)

	if len(llmCalls) != 1 {
		t.Fatalf("got %d LLM calls, want 1", len(llmCalls))
	}
	if len(toolCalls) != 0 {
		t.Errorf("got %d tool calls, want 0", len(toolCalls))
	}
	if len(retrievalCalls) != 0 {
		t.Errorf("got %d retrieval calls, want 0", len(retrievalCalls))
	}
	if len(skillCalls) != 0 {
		t.Errorf("got %d skill calls, want 0", len(skillCalls))
	}

	c := llmCalls[0]
	if c.Provider != "openai" {
		t.Errorf("Provider = %q, want openai", c.Provider)
	}
	if c.Model != "gpt-4" {
		t.Errorf("Model = %q, want gpt-4", c.Model)
	}
	if c.InputTokens == nil || *c.InputTokens != 50 {
		t.Errorf("InputTokens = %v, want 50", c.InputTokens)
	}
	if c.OutputTokens == nil || *c.OutputTokens != 100 {
		t.Errorf("OutputTokens = %v, want 100", c.OutputTokens)
	}
	if c.TotalTokens == nil || *c.TotalTokens != 150 {
		t.Errorf("TotalTokens = %v, want 150", c.TotalTokens)
	}
	if c.Cost == nil || *c.Cost != 0.002 {
		t.Errorf("Cost = %v, want 0.002", c.Cost)
	}
	if c.InputSummary != "What is the capital?" {
		t.Errorf("InputSummary = %q", c.InputSummary)
	}
	if c.OutputSummary != "Paris" {
		t.Errorf("OutputSummary = %q", c.OutputSummary)
	}
	if c.Status != "OK" {
		t.Errorf("Status = %q", c.Status)
	}
	if c.AgentID != "agent-1" {
		t.Errorf("AgentID = %q", c.AgentID)
	}
	if c.AgentInstanceID != "inst-1" {
		t.Errorf("AgentInstanceID = %q", c.AgentInstanceID)
	}
}

func TestExtractStructuredCalls_Tool(t *testing.T) {
	now := time.Now()
	attrs := `{"agentscope.span.kind":"TOOL","tool.name":"search_order","tool.type":"api","tool.arguments_summary":"{\"order_id\":\"123\"}","tool.result_summary":"order found","tool.success":true,"tool.retry_count":0,"tool.side_effect_risk":"low"}`

	spans := []SpanRecord{{
		ProjectID:  "proj-1",
		RunID:      "run-1",
		TraceID:    "trace-1",
		SpanID:     "span-tool-001",
		Name:       "tool.call",
		Attributes: attrs,
		StartTime:  now,
		DurationMs: int64Ptr(200),
	}}

	llmCalls, toolCalls, retrievalCalls, skillCalls := ExtractStructuredCalls(spans)

	if len(toolCalls) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(toolCalls))
	}
	if len(llmCalls) != 0 {
		t.Errorf("got %d LLM calls, want 0", len(llmCalls))
	}
	if len(retrievalCalls) != 0 {
		t.Errorf("got %d retrieval calls, want 0", len(retrievalCalls))
	}
	if len(skillCalls) != 0 {
		t.Errorf("got %d skill calls, want 0", len(skillCalls))
	}

	c := toolCalls[0]
	if c.ToolName != "search_order" {
		t.Errorf("ToolName = %q, want search_order", c.ToolName)
	}
	if c.ToolType != "api" {
		t.Errorf("ToolType = %q, want api", c.ToolType)
	}
	if !c.Success {
		t.Error("Success should be true")
	}
	if c.RetryCount != 0 {
		t.Errorf("RetryCount = %d, want 0", c.RetryCount)
	}
	if c.SideEffectRisk != "low" {
		t.Errorf("SideEffectRisk = %q", c.SideEffectRisk)
	}
}

func TestExtractStructuredCalls_Retrieval(t *testing.T) {
	now := time.Now()
	attrs := `{"agentscope.span.kind":"RETRIEVER","retrieval.knowledge_base":"product-knowledge","retrieval.query":"order return policy","retrieval.top_k":5,"retrieval.returned_count":3,"retrieval.document_ids":["doc-1","doc-2","doc-3"],"retrieval.scores":[0.95,0.87,0.72]}`

	spans := []SpanRecord{{
		ProjectID:  "proj-1",
		RunID:      "run-1",
		TraceID:    "trace-1",
		SpanID:     "span-ret-001",
		Name:       "retrieval.call",
		Attributes: attrs,
		StartTime:  now,
		DurationMs: int64Ptr(150),
	}}

	_, _, retrievalCalls, _ := ExtractStructuredCalls(spans)

	if len(retrievalCalls) != 1 {
		t.Fatalf("got %d retrieval calls, want 1", len(retrievalCalls))
	}

	c := retrievalCalls[0]
	if c.KnowledgeBase != "product-knowledge" {
		t.Errorf("KnowledgeBase = %q", c.KnowledgeBase)
	}
	if c.QueryText != "order return policy" {
		t.Errorf("QueryText = %q", c.QueryText)
	}
	if c.TopK == nil || *c.TopK != 5 {
		t.Errorf("TopK = %v", c.TopK)
	}
	if c.ReturnedCount == nil || *c.ReturnedCount != 3 {
		t.Errorf("ReturnedCount = %v", c.ReturnedCount)
	}
	if len(c.DocumentIDs) != 3 || c.DocumentIDs[0] != "doc-1" {
		t.Errorf("DocumentIDs = %v", c.DocumentIDs)
	}
	if len(c.Scores) != 3 || c.Scores[0] != 0.95 {
		t.Errorf("Scores = %v", c.Scores)
	}
}

func TestExtractStructuredCalls_Skill(t *testing.T) {
	now := time.Now()
	attrs := `{"agentscope.span.kind":"SKILL","skill.name":"refund_policy_check","skill.version":"1.2.0","skill.input_summary":"order-456","skill.output_summary":"eligible for refund","skill.success":true,"agentscope.agent.id":"agent-1"}`

	spans := []SpanRecord{{
		ProjectID:  "proj-1",
		RunID:      "run-1",
		TraceID:    "trace-1",
		SpanID:     "span-skill-001",
		Name:       "skill.call",
		Attributes: attrs,
		StartTime:  now,
		DurationMs: int64Ptr(300),
	}}

	_, _, _, skillCalls := ExtractStructuredCalls(spans)

	if len(skillCalls) != 1 {
		t.Fatalf("got %d skill calls, want 1", len(skillCalls))
	}

	c := skillCalls[0]
	if c.SkillName != "refund_policy_check" {
		t.Errorf("SkillName = %q", c.SkillName)
	}
	if c.SkillVersion != "1.2.0" {
		t.Errorf("SkillVersion = %q", c.SkillVersion)
	}
	if !c.Success {
		t.Error("Success should be true")
	}
	if c.AgentID != "agent-1" {
		t.Errorf("AgentID = %q", c.AgentID)
	}
}

func TestExtractStructuredCalls_openinferenceKind(t *testing.T) {
	now := time.Now()
	attrs := `{"openinference.span.kind":"LLM","llm.provider":"anthropic","llm.model":"claude-3"}`

	spans := []SpanRecord{{
		ProjectID:  "proj-1",
		RunID:      "run-1",
		TraceID:    "trace-1",
		SpanID:     "span-llm-002",
		Name:       "llm.call",
		Attributes: attrs,
		StartTime:  now,
	}}

	llmCalls, _, _, _ := ExtractStructuredCalls(spans)

	if len(llmCalls) != 1 {
		t.Fatalf("got %d LLM calls, want 1", len(llmCalls))
	}
	if llmCalls[0].Provider != "anthropic" {
		t.Errorf("Provider = %q, want anthropic", llmCalls[0].Provider)
	}
	if llmCalls[0].Model != "claude-3" {
		t.Errorf("Model = %q, want claude-3", llmCalls[0].Model)
	}
}

func TestExtractStructuredCalls_noKind(t *testing.T) {
	attrs := `{"some.key":"value"}`

	spans := []SpanRecord{{
		Attributes: attrs,
	}}

	llmCalls, toolCalls, retrievalCalls, skillCalls := ExtractStructuredCalls(spans)

	if len(llmCalls) != 0 {
		t.Errorf("got %d LLM calls, want 0", len(llmCalls))
	}
	if len(toolCalls) != 0 {
		t.Errorf("got %d tool calls, want 0", len(toolCalls))
	}
	if len(retrievalCalls) != 0 {
		t.Errorf("got %d retrieval calls, want 0", len(retrievalCalls))
	}
	if len(skillCalls) != 0 {
		t.Errorf("got %d skill calls, want 0", len(skillCalls))
	}
}

func TestExtractStructuredCalls_IgnoresRegularSpans(t *testing.T) {
	attrs := `{"agentscope.span.kind":"AGENT","some.key":"value"}`

	spans := []SpanRecord{{
		Attributes: attrs,
	}}

	llmCalls, toolCalls, retrievalCalls, skillCalls := ExtractStructuredCalls(spans)

	if len(llmCalls)+len(toolCalls)+len(retrievalCalls)+len(skillCalls) != 0 {
		t.Error("AGENT kind spans should not produce structured calls")
	}
}

func TestExtractStructuredCalls_LLMNoProviderFallback(t *testing.T) {
	now := time.Now()
	// Test that gen_ai.request.model fallback works
	attrs := `{"agentscope.span.kind":"LLM","gen_ai.request.model":"gemini-pro","gen_ai.response.model":"gemini-pro"}`

	spans := []SpanRecord{{
		Attributes: attrs,
		StartTime:  now,
	}}

	llmCalls, _, _, _ := ExtractStructuredCalls(spans)
	if len(llmCalls) != 1 {
		t.Fatalf("got %d LLM calls, want 1", len(llmCalls))
	}
	if llmCalls[0].Provider != "gemini-pro" {
		t.Errorf("Provider = %q, want gemini-pro (fallback from gen_ai.request.model)", llmCalls[0].Provider)
	}
	if llmCalls[0].Model != "gemini-pro" {
		t.Errorf("Model = %q, want gemini-pro", llmCalls[0].Model)
	}
}

func TestExtractStructuredCalls_OTLPArrayValueRoundTrip(t *testing.T) {
	now := time.Now()
	attrs := []*common.KeyValue{
		{Key: "agentscope.span.kind", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "RETRIEVER"}}},
		{Key: "retrieval.knowledge_base", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "product-knowledge"}}},
		{Key: "retrieval.query", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "order return policy"}}},
		{Key: "retrieval.document_ids", Value: &common.AnyValue{Value: &common.AnyValue_ArrayValue{
			ArrayValue: &common.ArrayValue{
				Values: []*common.AnyValue{
					{Value: &common.AnyValue_StringValue{StringValue: "doc-1"}},
					{Value: &common.AnyValue_StringValue{StringValue: "doc-2"}},
					{Value: &common.AnyValue_StringValue{StringValue: "doc-3"}},
				},
			},
		}}},
		{Key: "retrieval.scores", Value: &common.AnyValue{Value: &common.AnyValue_ArrayValue{
			ArrayValue: &common.ArrayValue{
				Values: []*common.AnyValue{
					{Value: &common.AnyValue_DoubleValue{DoubleValue: 0.95}},
					{Value: &common.AnyValue_DoubleValue{DoubleValue: 0.87}},
					{Value: &common.AnyValue_DoubleValue{DoubleValue: 0.72}},
				},
			},
		}}},
	}

	req := &trace.TracesData{
		ResourceSpans: []*trace.ResourceSpans{
			{
				ScopeSpans: []*trace.ScopeSpans{
					{
						Spans: []*trace.Span{
							{
								TraceId:           mustDecodeHex("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
								SpanId:            mustDecodeHex("bbbbbbbbbbbbbbbb"),
								Name:              "retrieval.call",
								Kind:              trace.Span_SPAN_KIND_INTERNAL,
								StartTimeUnixNano: uint64(now.UnixNano()),
								Status:            &trace.Status{Code: trace.Status_STATUS_CODE_OK},
								Attributes:        attrs,
							},
						},
					},
				},
			},
		},
	}

	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal OTLP: %v", err)
	}

	spans, _, err := DecodeOTLPTraceRequest(body)
	if err != nil {
		t.Fatalf("DecodeOTLPTraceRequest: %v", err)
	}

	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1", len(spans))
	}

	if !strings.Contains(spans[0].Attributes, `"doc-1"`) {
		t.Errorf("attributes should contain doc-1")
	}
	if !strings.Contains(spans[0].Attributes, `0.95`) {
		t.Errorf("attributes should contain 0.95")
	}

	spans[0].ProjectID = "proj-1"
	spans[0].RunID = "run-1"

	_, _, retrievalCalls, _ := ExtractStructuredCalls(spans)
	if len(retrievalCalls) != 1 {
		t.Fatalf("got %d retrieval calls, want 1", len(retrievalCalls))
	}

	c := retrievalCalls[0]
	if len(c.DocumentIDs) != 3 {
		t.Errorf("got %d document_ids, want 3", len(c.DocumentIDs))
	}
	if c.DocumentIDs[0] != "doc-1" || c.DocumentIDs[1] != "doc-2" || c.DocumentIDs[2] != "doc-3" {
		t.Errorf("document_ids = %v, want [doc-1 doc-2 doc-3]", c.DocumentIDs)
	}
	if len(c.Scores) != 3 {
		t.Errorf("got %d scores, want 3", len(c.Scores))
	}
	if c.Scores[0] != 0.95 || c.Scores[1] != 0.87 || c.Scores[2] != 0.72 {
		t.Errorf("scores = %v, want [0.95 0.87 0.72]", c.Scores)
	}
}
