package httpapi

import (
	"encoding/hex"
	"time"

	"google.golang.org/protobuf/proto"

	common "go.opentelemetry.io/proto/otlp/common/v1"
	trace "go.opentelemetry.io/proto/otlp/trace/v1"
)

// otlpSpan is a minimal span description for building OTLP requests in tests.
type otlpSpan struct {
	traceID    string
	spanID     string
	name       string
	attributes []*common.KeyValue
	events     []*trace.Span_Event
}

// buildOTLPRequest creates a TracesData protobuf message with the given spans.
func buildOTLPRequest(spans []otlpSpan) *trace.TracesData {
	now := time.Now()
	td := &trace.TracesData{}
	for _, sp := range spans {
		attrs := sp.attributes
		if attrs == nil {
			attrs = []*common.KeyValue{
				{Key: "agentscope.agent.name", Value: &common.AnyValue{Value: &common.AnyValue_StringValue{StringValue: "test-agent"}}},
			}
		}
		td.ResourceSpans = append(td.ResourceSpans, &trace.ResourceSpans{
			ScopeSpans: []*trace.ScopeSpans{
				{
					Spans: []*trace.Span{
						{
							TraceId:           mustDecodeHex(sp.traceID),
							SpanId:            mustDecodeHex(sp.spanID),
							Name:              sp.name,
							StartTimeUnixNano: uint64(now.UnixNano()),
							EndTimeUnixNano:   uint64(now.Add(time.Second).UnixNano()),
							Attributes:        attrs,
							Events:            sp.events,
						},
					},
				},
			},
		})
	}
	return td
}

// protoMarshalOTLP marshals a TracesData message to protobuf bytes.
func protoMarshalOTLP(msg *trace.TracesData) ([]byte, error) {
	return proto.Marshal(msg)
}

// mustDecodeHex decodes a hex string to bytes, panicking on error.
func mustDecodeHex(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		panic("mustDecodeHex: " + err.Error())
	}
	return b
}
