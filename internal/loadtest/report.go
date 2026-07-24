package loadtest

import (
	"fmt"
	"math"
	"sort"
	"sync"
	"time"
)

const ReportSchemaVersion = "agentscope-load-report-v1"

type LatencySummary struct {
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
	P99MS float64 `json:"p99_ms"`
	MaxMS float64 `json:"max_ms"`
}

type SSESummary struct {
	Connections  int64   `json:"connections"`
	Events       int64   `json:"events"`
	Resets       int64   `json:"resets"`
	Errors       int64   `json:"errors"`
	FirstEventMS float64 `json:"first_event_ms"`
	EventP95MS   float64 `json:"event_p95_ms"`
}

type LoadProfile struct {
	RequestLimit      int   `json:"request_limit"`
	DurationLimitMS   int64 `json:"duration_limit_ms"`
	Concurrency       int   `json:"concurrency"`
	BatchSize         int   `json:"batch_size"`
	RequestsPerSecond int   `json:"requests_per_second"`
	HTTPTimeoutMS     int64 `json:"http_timeout_ms"`
}

type ThroughputSummary struct {
	RequestsPerSecond float64 `json:"requests_per_second"`
	SpansPerSecond    float64 `json:"spans_per_second"`
}

type Report struct {
	SchemaVersion string            `json:"schema_version"`
	StartedAt     time.Time         `json:"started_at"`
	FinishedAt    time.Time         `json:"finished_at"`
	DurationMS    int64             `json:"duration_ms"`
	Seed          int64             `json:"seed"`
	Projects      int               `json:"projects"`
	Profile       LoadProfile       `json:"profile"`
	Requests      int64             `json:"requests"`
	Succeeded     int64             `json:"succeeded"`
	Errors        int64             `json:"errors"`
	Spans         int64             `json:"spans"`
	DroppedSpans  int64             `json:"dropped_spans"`
	RuntimeCalls  int64             `json:"runtime_calls"`
	RuntimeErrors int64             `json:"runtime_errors"`
	ErrorRate     float64           `json:"error_rate"`
	DropRate      float64           `json:"drop_rate"`
	Throughput    ThroughputSummary `json:"throughput"`
	Latency       LatencySummary    `json:"latency"`
	SSE           SSESummary        `json:"sse"`
	Passed        bool              `json:"passed"`
	Violations    []string          `json:"violations"`

	mu                sync.Mutex
	latencies         []time.Duration
	sseFirstEvents    []time.Duration
	sseEventLatencies []time.Duration
}

func NewReport(startedAt time.Time, seed int64, projects int) *Report {
	return &Report{
		SchemaVersion: ReportSchemaVersion,
		StartedAt:     startedAt.UTC(),
		Seed:          seed,
		Projects:      projects,
		Passed:        true,
		Violations:    []string{},
	}
}

func (r *Report) SetProfile(config Config) {
	r.Profile = LoadProfile{
		RequestLimit:      config.Requests,
		DurationLimitMS:   config.Duration.Milliseconds(),
		Concurrency:       config.Concurrency,
		BatchSize:         config.BatchSize,
		RequestsPerSecond: config.RequestsPerSecond,
		HTTPTimeoutMS:     config.HTTPTimeout.Milliseconds(),
	}
}

func (r *Report) RecordRequest(latency time.Duration, success bool, spans, dropped int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Requests++
	r.Spans += int64(spans)
	r.DroppedSpans += int64(dropped)
	r.latencies = append(r.latencies, latency)
	if success {
		r.Succeeded++
	} else {
		r.Errors++
	}
}

func (r *Report) RecordRuntime(success bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.RuntimeCalls++
	if !success {
		r.RuntimeErrors++
	}
}

func (r *Report) RecordSSEConnection() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.SSE.Connections++
}

func (r *Report) RecordSSEEvent(firstEvent, eventLatency time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.SSE.Events++
	if firstEvent >= 0 {
		r.sseFirstEvents = append(r.sseFirstEvents, firstEvent)
	}
	if eventLatency >= 0 {
		r.sseEventLatencies = append(r.sseEventLatencies, eventLatency)
	}
}

func (r *Report) RecordSSEReset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.SSE.Resets++
}

func (r *Report) RecordSSEError() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.SSE.Errors++
}

func (r *Report) Finalize(finishedAt time.Time, thresholds Thresholds) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.FinishedAt = finishedAt.UTC()
	r.DurationMS = finishedAt.Sub(r.StartedAt).Milliseconds()
	r.Latency = summarize(r.latencies)
	operations := r.Requests + r.RuntimeCalls
	if operations > 0 {
		r.ErrorRate = float64(r.Errors+r.RuntimeErrors) / float64(operations)
	}
	if r.Spans > 0 {
		r.DropRate = float64(r.DroppedSpans) / float64(r.Spans)
	}
	elapsedSeconds := finishedAt.Sub(r.StartedAt).Seconds()
	if elapsedSeconds > 0 {
		r.Throughput.RequestsPerSecond = float64(r.Requests) / elapsedSeconds
		r.Throughput.SpansPerSecond = float64(r.Spans) / elapsedSeconds
	}
	if len(r.sseFirstEvents) > 0 {
		r.SSE.FirstEventMS = milliseconds(percentile(r.sseFirstEvents, 0.5))
	}
	if len(r.sseEventLatencies) > 0 {
		r.SSE.EventP95MS = milliseconds(percentile(r.sseEventLatencies, 0.95))
	}
	if thresholds.MaxP95Latency > 0 &&
		r.Latency.P95MS > milliseconds(thresholds.MaxP95Latency) {
		r.Violations = append(r.Violations, fmt.Sprintf(
			"p95 latency %.2fms exceeds %.2fms",
			r.Latency.P95MS,
			milliseconds(thresholds.MaxP95Latency),
		))
	}
	if thresholds.MaxErrorRate > 0 && r.ErrorRate > thresholds.MaxErrorRate {
		r.Violations = append(r.Violations, fmt.Sprintf(
			"error rate %.4f exceeds %.4f",
			r.ErrorRate,
			thresholds.MaxErrorRate,
		))
	}
	if thresholds.MaxDropRate > 0 && r.DropRate > thresholds.MaxDropRate {
		r.Violations = append(r.Violations, fmt.Sprintf(
			"drop rate %.4f exceeds %.4f",
			r.DropRate,
			thresholds.MaxDropRate,
		))
	}
	r.Passed = len(r.Violations) == 0
}

func (r *Report) ExitCode() int {
	if r.Passed {
		return 0
	}
	return 2
}

func summarize(values []time.Duration) LatencySummary {
	if len(values) == 0 {
		return LatencySummary{}
	}
	return LatencySummary{
		P50MS: milliseconds(percentile(values, 0.50)),
		P95MS: milliseconds(percentile(values, 0.95)),
		P99MS: milliseconds(percentile(values, 0.99)),
		MaxMS: milliseconds(percentile(values, 1)),
	}
}

func percentile(values []time.Duration, quantile float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]time.Duration(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	index := int(math.Ceil(quantile*float64(len(ordered)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(ordered) {
		index = len(ordered) - 1
	}
	return ordered[index]
}

func milliseconds(value time.Duration) float64 {
	return float64(value) / float64(time.Millisecond)
}
