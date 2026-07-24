package ingest

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	common "go.opentelemetry.io/proto/otlp/common/v1"
	trace "go.opentelemetry.io/proto/otlp/trace/v1"
)

// MaxAttributeSize is the maximum permitted size (in bytes) of the JSON
// representation of a span's or event's attributes. Larger payloads are
// truncated and the Truncated flag is set to true.
const MaxAttributeSize = 64 * 1024 // 64 KiB

// ErrTooManySpans is returned when an OTLP request exceeds the configured
// number of spans.
var ErrTooManySpans = errors.New("too many spans")

// SpanRecord is the decoded and normalized span to be written to ClickHouse.
type SpanRecord struct {
	ProjectID     string     `json:"project_id"`
	RunID         string     `json:"run_id"`
	TraceID       string     `json:"trace_id"`
	SpanID        string     `json:"span_id"`
	ParentSpanID  string     `json:"parent_span_id,omitempty"`
	Name          string     `json:"name"`
	Kind          string     `json:"kind"`
	StatusCode    string     `json:"status_code"`
	StatusMessage string     `json:"status_message,omitempty"`
	StartTime     time.Time  `json:"start_time"`
	EndTime       *time.Time `json:"end_time,omitempty"`
	DurationMs    *int64     `json:"duration_ms,omitempty"`
	Attributes    string     `json:"attributes"`
	Truncated     bool       `json:"truncated"`
}

// SpanEventRecord is a decoded span event to be written to ClickHouse.
type SpanEventRecord struct {
	ProjectID  string    `json:"project_id"`
	RunID      string    `json:"run_id"`
	TraceID    string    `json:"trace_id"`
	SpanID     string    `json:"span_id"`
	EventName  string    `json:"event_name"`
	EventTime  time.Time `json:"event_time"`
	Attributes string    `json:"attributes"`
	Truncated  bool      `json:"truncated"`
}

// DecodeOTLPTraceRequest decodes an OTLP HTTP trace request body (protobuf)
// into normalized span and span event records.
// All trace/span IDs are converted to lowercase hex strings.
// maxSpans limits the total number of spans+events; 0 means no limit.
func DecodeOTLPTraceRequest(body []byte, maxSpans int) ([]SpanRecord, []SpanEventRecord, error) {
	var data trace.TracesData
	if err := proto.Unmarshal(body, &data); err != nil {
		return nil, nil, fmt.Errorf("unmarshal OTLP request: %w", err)
	}

	var spans []SpanRecord
	var events []SpanEventRecord

	for _, rs := range data.ResourceSpans {
		if rs == nil {
			continue
		}

		// Extract resource attributes
		resourceAttrs := extractAttributes(rs.Resource.GetAttributes())

		for _, ss := range rs.ScopeSpans {
			if ss == nil {
				continue
			}
			for _, s := range ss.Spans {
				if s == nil {
					continue
				}

				sr := decodeSpan(s, resourceAttrs)
				spans = append(spans, sr)

				for _, e := range s.Events {
					if e == nil {
						continue
					}
					er := decodeSpanEvent(sr, e)
					events = append(events, er)
				}
			}
		}
	}

	// Enforce span count limit
	if maxSpans > 0 && len(spans) > maxSpans {
		return nil, nil, fmt.Errorf("%w: got %d, max %d", ErrTooManySpans, len(spans), maxSpans)
	}

	return spans, events, nil
}

// decodeSpan converts a protobuf Span into a SpanRecord.
func decodeSpan(s *trace.Span, resourceAttrs string) SpanRecord {
	traceID := bytesToHex(s.TraceId)
	spanID := bytesToHex(s.SpanId)
	parentSpanID := bytesToHex(s.ParentSpanId)

	startTime := unixNanoToTime(s.StartTimeUnixNano)
	var endTime *time.Time
	if s.EndTimeUnixNano > 0 {
		t := unixNanoToTime(s.EndTimeUnixNano)
		endTime = &t
	}

	var durationMs *int64
	if s.EndTimeUnixNano > s.StartTimeUnixNano {
		d := int64((s.EndTimeUnixNano - s.StartTimeUnixNano) / 1_000_000)
		durationMs = &d
	}

	kindName := stripEnumPrefix(trace.Span_SpanKind_name[int32(s.Kind)])
	statusCode := stripEnumPrefix(trace.Status_StatusCode_name[int32(s.GetStatus().GetCode())])

	attrs := buildAttrsJSON(s.Attributes, resourceAttrs)
	truncated := len(attrs) > MaxAttributeSize
	if truncated {
		attrs = attrs[:MaxAttributeSize]
	}

	return SpanRecord{
		TraceID:       traceID,
		SpanID:        spanID,
		ParentSpanID:  parentSpanID,
		Name:          s.Name,
		Kind:          kindName,
		StatusCode:    statusCode,
		StatusMessage: s.GetStatus().GetMessage(),
		StartTime:     startTime,
		EndTime:       endTime,
		DurationMs:    durationMs,
		Attributes:    attrs,
		Truncated:     truncated,
	}
}

// decodeSpanEvent converts a protobuf Span_Event into a SpanEventRecord.
func decodeSpanEvent(sr SpanRecord, e *trace.Span_Event) SpanEventRecord {
	attrs := extractAttributes(e.Attributes)
	truncated := len(attrs) > MaxAttributeSize
	if truncated {
		attrs = attrs[:MaxAttributeSize]
	}

	return SpanEventRecord{
		RunID:      sr.RunID,
		TraceID:    sr.TraceID,
		SpanID:     sr.SpanID,
		EventName:  e.Name,
		EventTime:  unixNanoToTime(e.TimeUnixNano),
		Attributes: attrs,
		Truncated:  truncated,
	}
}

// extractAttributes converts a list of OTLP KeyValue attributes into a JSON-like
// key:"value" representation. For simplicity in MVP, it uses a compact format.
func extractAttributes(attrs []*common.KeyValue) string {
	if len(attrs) == 0 {
		return "{}"
	}
	var sb strings.Builder
	sb.WriteByte('{')
	for i, kv := range attrs {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(fmt.Sprintf("%q:", kv.Key))
		sb.WriteString(anyValueToJSON(kv.Value))
	}
	sb.WriteByte('}')
	return sb.String()
}

// buildAttrsJSON merges span attributes with resource attributes.
func buildAttrsJSON(attrs []*common.KeyValue, resourceAttrs string) string {
	spanAttrs := extractAttributes(attrs)
	if resourceAttrs == "{}" {
		return spanAttrs
	}
	// Combine: resource attrs + span attrs
	// Remove closing brace of resource attrs, add comma, add span attrs without opening brace
	if spanAttrs == "{}" {
		return resourceAttrs
	}
	return resourceAttrs[:len(resourceAttrs)-1] + "," + spanAttrs[1:]
}

// anyValueToJSON converts an OTLP AnyValue to a JSON-like string representation.
func anyValueToJSON(v *common.AnyValue) string {
	if v == nil {
		return "null"
	}
	switch val := v.Value.(type) {
	case *common.AnyValue_StringValue:
		return fmt.Sprintf("%q", val.StringValue)
	case *common.AnyValue_IntValue:
		return fmt.Sprintf("%d", val.IntValue)
	case *common.AnyValue_DoubleValue:
		return fmt.Sprintf("%f", val.DoubleValue)
	case *common.AnyValue_BoolValue:
		if val.BoolValue {
			return "true"
		}
		return "false"
	case *common.AnyValue_ArrayValue:
		var elems []string
		for _, e := range val.ArrayValue.Values {
			elems = append(elems, anyValueToJSON(e))
		}
		return "[" + strings.Join(elems, ",") + "]"
	case *common.AnyValue_KvlistValue:
		var pairs []string
		for _, kv := range val.KvlistValue.Values {
			pairs = append(pairs, fmt.Sprintf("%q:%s", kv.Key, anyValueToJSON(kv.Value)))
		}
		return "{" + strings.Join(pairs, ",") + "}"
	default:
		return "null"
	}
}

// bytesToHex converts a byte slice to a lowercase hex string.
// Returns empty string for nil or empty slice.
func bytesToHex(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return hex.EncodeToString(b)
}

// stripEnumPrefix removes the common prefix from OTel enum names.
// E.g. "SPAN_KIND_INTERNAL" -> "INTERNAL", "STATUS_CODE_OK" -> "OK".
func stripEnumPrefix(name string) string {
	idx := strings.LastIndex(name, "_")
	if idx >= 0 {
		return name[idx+1:]
	}
	return name
}

// unixNanoToTime converts Unix nanoseconds to time.Time.
func unixNanoToTime(nano uint64) time.Time {
	sec := int64(nano / 1_000_000_000)
	ns := int64(nano % 1_000_000_000)
	return time.Unix(sec, ns)
}
