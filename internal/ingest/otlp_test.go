package ingest

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	common "go.opentelemetry.io/proto/otlp/common/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	trace "go.opentelemetry.io/proto/otlp/trace/v1"
)

func TestDecodeOTLPTraceRequest_basic(t *testing.T) {
	now := time.Now()
	startNano := now.UnixNano()
	endNano := now.Add(time.Second).UnixNano()

	req := &trace.TracesData{
		ResourceSpans: []*trace.ResourceSpans{
			{
				Resource: &resource.Resource{
					Attributes: []*common.KeyValue{
						{Key: "service.name", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "test-service"}}},
					},
				},
				ScopeSpans: []*trace.ScopeSpans{
					{
						Spans: []*trace.Span{
							{
								TraceId:           mustDecodeHex("0123456789abcdef0123456789abcdef"),
								SpanId:            mustDecodeHex("abcdef0123456789"),
								ParentSpanId:      mustDecodeHex("0000000000000000"),
								Name:              "test-span",
								Kind:              trace.Span_SPAN_KIND_INTERNAL,
								StartTimeUnixNano: uint64(startNano),
								EndTimeUnixNano:   uint64(endNano),
								Status: &trace.Status{
									Code:    trace.Status_STATUS_CODE_OK,
									Message: "success",
								},
								Attributes: []*common.KeyValue{
									{Key: "agentscope.run.id", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "run-123"}}},
									{Key: "agentscope.agent.name", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "my-agent"}}},
								},
							},
						},
					},
				},
			},
		},
	}

	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	spans, events, err := DecodeOTLPTraceRequest(body)
	if err != nil {
		t.Fatalf("DecodeOTLPTraceRequest() error = %v", err)
	}

	if len(spans) == 0 {
		t.Fatal("expected at least one span record")
	}

	span := spans[0]
	if span.TraceID != "0123456789abcdef0123456789abcdef" {
		t.Errorf("TraceID = %q, want 0123456789abcdef0123456789abcdef", span.TraceID)
	}
	if span.SpanID != "abcdef0123456789" {
		t.Errorf("SpanID = %q, want abcdef0123456789", span.SpanID)
	}
	if span.ParentSpanID != "0000000000000000" {
		t.Errorf("ParentSpanID = %q, want 0000000000000000", span.ParentSpanID)
	}
	if span.Name != "test-span" {
		t.Errorf("Name = %q, want test-span", span.Name)
	}
	if span.Kind != "INTERNAL" {
		t.Errorf("Kind = %q, want INTERNAL", span.Kind)
	}
	if span.StatusCode != "OK" {
		t.Errorf("StatusCode = %q, want OK", span.StatusCode)
	}
	if span.StatusMessage != "success" {
		t.Errorf("StatusMessage = %q, want success", span.StatusMessage)
	}

	// Check attributes extraction
	if !strings.Contains(span.Attributes, "agentscope.run.id") {
		t.Error("expected agentscope.run.id in span attributes")
	}
	if !strings.Contains(span.Attributes, "agentscope.agent.name") {
		t.Error("expected agentscope.agent.name in span attributes")
	}

	// Check events (none in this basic test)
	if len(events) != 0 {
		t.Errorf("got %d events, want 0", len(events))
	}
}

func TestDecodeOTLPTraceRequest_withEvents(t *testing.T) {
	now := time.Now()

	req := &trace.TracesData{
		ResourceSpans: []*trace.ResourceSpans{
			{
				ScopeSpans: []*trace.ScopeSpans{
					{
						Spans: []*trace.Span{
							{
								TraceId: mustDecodeHex("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
								SpanId:  mustDecodeHex("bbbbbbbbbbbbbbbb"),
								Name:    "span-with-events",
								Events: []*trace.Span_Event{
									{
										TimeUnixNano: uint64(now.UnixNano()),
										Name:         "llm.call",
										Attributes: []*common.KeyValue{
											{Key: "llm.model", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "gpt-4"}}},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	_, events, err := DecodeOTLPTraceRequest(body)
	if err != nil {
		t.Fatalf("DecodeOTLPTraceRequest() error = %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	if events[0].EventName != "llm.call" {
		t.Errorf("EventName = %q, want llm.call", events[0].EventName)
	}
	if !strings.Contains(events[0].Attributes, "llm.model") {
		t.Error("expected llm.model in event attributes")
	}
}

func TestDecodeOTLPTraceRequest_truncation(t *testing.T) {
	req := &trace.TracesData{
		ResourceSpans: []*trace.ResourceSpans{
			{
				ScopeSpans: []*trace.ScopeSpans{
					{
						Spans: []*trace.Span{
							{
								TraceId: mustDecodeHex("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
								SpanId:  mustDecodeHex("bbbbbbbbbbbbbbbb"),
								Name:    "big-span",
								Attributes: []*common.KeyValue{
									{
										Key: "large.payload",
										Value: &common.AnyValue{Value: &common.AnyValue_StringValue{
											StringValue: strings.Repeat("X", MaxAttributeSize+100),
										}},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	spans, _, err := DecodeOTLPTraceRequest(body)
	if err != nil {
		t.Fatalf("DecodeOTLPTraceRequest() error = %v", err)
	}

	if !spans[0].Truncated {
		t.Error("expected Truncated=true for oversize attributes")
	}
}

func TestDecodeOTLPTraceRequest_invalidBody(t *testing.T) {
	_, _, err := DecodeOTLPTraceRequest([]byte("not-valid-protobuf"))
	if err == nil {
		t.Fatal("expected error for invalid protobuf")
	}
}

func TestDecodeOTLPTraceRequest_missingTraceID(t *testing.T) {
	req := &trace.TracesData{
		ResourceSpans: []*trace.ResourceSpans{
			{
				ScopeSpans: []*trace.ScopeSpans{
					{
						Spans: []*trace.Span{
							{
								Name: "span-without-trace-id",
							},
						},
					},
				},
			},
		},
	}

	body, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	spans, _, err := DecodeOTLPTraceRequest(body)
	if err != nil {
		t.Fatalf("DecodeOTLPTraceRequest() error = %v", err)
	}

	if len(spans) != 1 {
		t.Fatalf("got %d spans, want 1 (should decode but with empty traceID)", len(spans))
	}
}

func mustDecodeHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}
