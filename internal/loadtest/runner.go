package loadtest

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	common "go.opentelemetry.io/proto/otlp/common/v1"
	resource "go.opentelemetry.io/proto/otlp/resource/v1"
	trace "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

type Runner struct {
	config Config
	client *http.Client
}

func NewRunner(config Config, client *http.Client) *Runner {
	if client == nil {
		client = &http.Client{}
	}
	if config.HTTPTimeout <= 0 {
		config.HTTPTimeout = 10 * time.Second
	}
	return &Runner{config: config, client: client}
}

func (r *Runner) Run(ctx context.Context) (*Report, error) {
	if err := r.config.Validate(); err != nil {
		return nil, err
	}
	startedAt := time.Now()
	report := NewReport(startedAt, r.config.Seed, len(r.config.Targets))
	report.SetProfile(r.config)
	runCtx, cancel := context.WithCancel(ctx)
	if r.config.Duration > 0 {
		runCtx, cancel = context.WithTimeout(ctx, r.config.Duration)
	}
	defer cancel()

	var sseWG sync.WaitGroup
	if r.config.EnableSSE {
		sseReady := make(chan struct{}, len(r.config.Targets))
		for _, target := range r.config.Targets {
			target := target
			sseWG.Add(1)
			go func() {
				defer sseWG.Done()
				r.observeSSE(runCtx, target, report, sseReady)
			}()
		}
		for range r.config.Targets {
			select {
			case <-sseReady:
			case <-time.After(r.config.HTTPTimeout):
				cancel()
				sseWG.Wait()
				return nil, errors.New("SSE connection readiness timed out")
			}
		}
	}

	var rateTicker *time.Ticker
	var rateTick <-chan time.Time
	if r.config.RequestsPerSecond > 0 {
		interval := time.Second / time.Duration(r.config.RequestsPerSecond)
		if interval <= 0 {
			interval = time.Nanosecond
		}
		rateTicker = time.NewTicker(interval)
		rateTick = rateTicker.C
		defer rateTicker.Stop()
	}

	var next atomic.Int64
	var workers sync.WaitGroup
	for worker := 0; worker < r.config.Concurrency; worker++ {
		workers.Add(1)
		go func(workerID int) {
			defer workers.Done()
			for {
				index := next.Add(1) - 1
				if r.config.Requests > 0 && index >= int64(r.config.Requests) {
					return
				}
				select {
				case <-runCtx.Done():
					return
				default:
				}
				if rateTick != nil {
					select {
					case <-runCtx.Done():
						return
					case <-rateTick:
					}
				}
				target := r.config.Targets[index%int64(len(r.config.Targets))]
				payload, err := BuildOTLPPayload(
					r.config.Seed+int64(workerID),
					target,
					int(index),
					r.config.BatchSize,
				)
				if err != nil {
					report.RecordRequest(0, false, r.config.BatchSize, r.config.BatchSize)
					continue
				}
				begin := time.Now()
				success := r.sendOTLP(runCtx, target, payload)
				dropped := 0
				if !success {
					dropped = r.config.BatchSize
				}
				report.RecordRequest(
					time.Since(begin),
					success,
					r.config.BatchSize,
					dropped,
				)
				if r.config.EnableRuntime {
					report.RecordRuntime(r.sendRuntimeCycle(runCtx, target, int(index)))
				}
			}
		}(worker)
	}
	workers.Wait()
	cancel()
	sseWG.Wait()

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	report.Finalize(time.Now(), r.config.Thresholds)
	return report, nil
}

func (r *Runner) sendOTLP(ctx context.Context, target Target, payload []byte) bool {
	requestCtx, cancel := context.WithTimeout(ctx, r.config.HTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(
		requestCtx,
		http.MethodPost,
		strings.TrimRight(r.config.ServerURL, "/")+"/v1/traces",
		bytes.NewReader(payload),
	)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+target.Token)
	req.Header.Set("Content-Type", "application/x-protobuf")
	response, err := r.client.Do(req)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode >= 200 && response.StatusCode < 300
}

func (r *Runner) sendRuntimeCycle(ctx context.Context, target Target, index int) bool {
	traceID := deterministicBytes(r.config.Seed, target.ProjectID, index, 0, 16)
	startBody := map[string]any{
		"agent_id": target.AgentID, "instance_id": target.InstanceID,
		"input":    fmt.Sprintf("load task %d", index),
		"trace_id": fmt.Sprintf("%x", traceID),
		"metadata": map[string]any{"source": "agentscope-loadtest"},
	}
	var started struct {
		ID string `json:"id"`
	}
	if !r.sendJSON(ctx, target, http.MethodPost, "/api/sdk/runs/start", startBody, &started) ||
		started.ID == "" {
		return false
	}
	stateBody := map[string]any{
		"status": "waiting_tool", "message": "load test",
		"agent_id": target.AgentID, "instance_id": target.InstanceID,
		"trace_id": fmt.Sprintf("%x", traceID),
	}
	if !r.sendJSON(
		ctx,
		target,
		http.MethodPost,
		"/api/sdk/runs/"+started.ID+"/state",
		stateBody,
		nil,
	) {
		return false
	}
	finishBody := map[string]any{
		"output":   "load test complete",
		"agent_id": target.AgentID, "instance_id": target.InstanceID,
		"trace_id": fmt.Sprintf("%x", traceID), "latency_ms": 1,
	}
	return r.sendJSON(
		ctx,
		target,
		http.MethodPost,
		"/api/sdk/runs/"+started.ID+"/finish",
		finishBody,
		nil,
	)
}

func (r *Runner) sendJSON(
	ctx context.Context,
	target Target,
	method string,
	path string,
	body any,
	output any,
) bool {
	data, err := json.Marshal(body)
	if err != nil {
		return false
	}
	requestCtx, cancel := context.WithTimeout(ctx, r.config.HTTPTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(
		requestCtx,
		method,
		strings.TrimRight(r.config.ServerURL, "/")+path,
		bytes.NewReader(data),
	)
	if err != nil {
		return false
	}
	req.Header.Set("Authorization", "Bearer "+target.Token)
	req.Header.Set("Content-Type", "application/json")
	response, err := r.client.Do(req)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, response.Body)
		return false
	}
	if output != nil {
		return json.NewDecoder(response.Body).Decode(output) == nil
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return true
}

func (r *Runner) observeSSE(
	ctx context.Context,
	target Target,
	report *Report,
	ready chan<- struct{},
) {
	connectedAt := time.Now()
	readyOnce := sync.Once{}
	markReady := func() {
		readyOnce.Do(func() { ready <- struct{}{} })
	}
	defer markReady()
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		strings.TrimRight(r.config.ServerURL, "/")+
			"/api/projects/"+target.ProjectID+"/events/stream",
		nil,
	)
	if err != nil {
		report.RecordSSEError()
		return
	}
	req.AddCookie(&http.Cookie{Name: "session", Value: target.SessionCookie})
	if r.config.SSEAfterID != "" {
		req.Header.Set("Last-Event-ID", r.config.SSEAfterID)
	}
	response, err := r.client.Do(req)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			report.RecordSSEError()
		}
		return
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		report.RecordSSEError()
		_, _ = io.Copy(io.Discard, response.Body)
		return
	}
	report.RecordSSEConnection()
	markReady()
	reader := bufio.NewReader(response.Body)
	first := true
	for {
		eventType, occurredAt, err := readSSEEvent(reader)
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(ctx.Err(), context.Canceled) {
				report.RecordSSEError()
			}
			return
		}
		firstLatency := time.Duration(-1)
		if first {
			firstLatency = time.Since(connectedAt)
			first = false
		}
		eventLatency := time.Duration(-1)
		if !occurredAt.IsZero() {
			eventLatency = time.Since(occurredAt)
			if eventLatency < 0 {
				eventLatency = 0
			}
		}
		report.RecordSSEEvent(firstLatency, eventLatency)
		if eventType == "reset" {
			report.RecordSSEReset()
		}
	}
}

func readSSEEvent(reader *bufio.Reader) (string, time.Time, error) {
	var eventType string
	var data strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", time.Time{}, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if eventType == "" && data.Len() == 0 {
				continue
			}
			var envelope struct {
				OccurredAt time.Time `json:"occurred_at"`
			}
			_ = json.Unmarshal([]byte(data.String()), &envelope)
			return eventType, envelope.OccurredAt, nil
		}
		switch {
		case strings.HasPrefix(line, "event:"):
			eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			if data.Len() > 0 {
				data.WriteByte('\n')
			}
			data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
}

func BuildOTLPPayload(seed int64, target Target, requestIndex, batchSize int) ([]byte, error) {
	if batchSize <= 0 {
		return nil, errors.New("batch size must be positive")
	}
	base := time.Now().UTC().Truncate(24 * time.Hour).Add(
		time.Duration(requestIndex%3600) * time.Second,
	)
	spans := make([]*trace.Span, 0, batchSize)
	for index := 0; index < batchSize; index++ {
		traceID := deterministicBytes(seed, target.ProjectID, requestIndex, index, 16)
		spanID := deterministicBytes(seed, target.ProjectID, requestIndex, index, 8)
		runID := deterministicUUID(seed, target.ProjectID, requestIndex, index)
		start := base.Add(time.Duration(index) * time.Millisecond)
		spans = append(spans, &trace.Span{
			TraceId:           traceID,
			SpanId:            spanID,
			Name:              "agent.run",
			Kind:              trace.Span_SPAN_KIND_INTERNAL,
			StartTimeUnixNano: uint64(start.UnixNano()),
			EndTimeUnixNano:   uint64(start.Add(20 * time.Millisecond).UnixNano()),
			Status:            &trace.Status{Code: trace.Status_STATUS_CODE_OK},
			Attributes: []*common.KeyValue{
				stringAttribute("agentscope.run.id", runID),
				stringAttribute("agentscope.agent.name", defaultString(target.AgentName, "load-agent")),
				stringAttribute("agentscope.agent.version", defaultString(target.AgentVersion, "load-v1")),
				stringAttribute("agentscope.environment", defaultString(target.Environment, "load")),
				stringAttribute("agentscope.load.project_id", target.ProjectID),
			},
		})
	}
	message := &trace.TracesData{ResourceSpans: []*trace.ResourceSpans{{
		Resource: &resource.Resource{Attributes: []*common.KeyValue{
			stringAttribute("service.name", "agentscope-loadtest"),
		}},
		ScopeSpans: []*trace.ScopeSpans{{Spans: spans}},
	}}}
	return proto.Marshal(message)
}

func deterministicBytes(seed int64, projectID string, requestIndex, spanIndex, length int) []byte {
	hash := sha256.New()
	var encoded [8]byte
	binary.BigEndian.PutUint64(encoded[:], uint64(seed))
	_, _ = hash.Write(encoded[:])
	_, _ = hash.Write([]byte(projectID))
	binary.BigEndian.PutUint64(encoded[:], uint64(requestIndex))
	_, _ = hash.Write(encoded[:])
	binary.BigEndian.PutUint64(encoded[:], uint64(spanIndex))
	_, _ = hash.Write(encoded[:])
	return append([]byte(nil), hash.Sum(nil)[:length]...)
}

func deterministicUUID(seed int64, projectID string, requestIndex, spanIndex int) string {
	value := deterministicBytes(seed, projectID, requestIndex, spanIndex, 16)
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		value[0:4],
		value[4:6],
		value[6:8],
		value[8:10],
		value[10:16],
	)
}

func stringAttribute(key, value string) *common.KeyValue {
	return &common.KeyValue{
		Key: key,
		Value: &common.AnyValue{
			Value: &common.AnyValue_StringValue{StringValue: value},
		},
	}
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
