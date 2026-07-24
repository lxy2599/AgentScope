package loadtest

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"AgentScope/internal/ingest"
)

func TestBuildOTLPPayloadIsDeterministicAndUsesTargetDimensions(t *testing.T) {
	target := Target{
		ProjectID: "project-a", Token: "secret-token",
		AgentName: "load-agent", AgentVersion: "v2", Environment: "staging",
	}
	first, err := BuildOTLPPayload(42, target, 3, 4)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildOTLPPayload(42, target, 3, 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("same seed did not produce identical payload")
	}
	spans, _, err := ingest.DecodeOTLPTraceRequest(first, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(spans) != 4 {
		t.Fatalf("spans = %d, want 4", len(spans))
	}
	for _, span := range spans {
		if !strings.Contains(span.Attributes, `"agentscope.agent.version":"v2"`) ||
			!strings.Contains(span.Attributes, `"agentscope.environment":"staging"`) {
			t.Fatalf("span dimensions missing: %s", span.Attributes)
		}
	}
}

func TestPercentilesAndThresholdViolations(t *testing.T) {
	startedAt := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	report := NewReport(startedAt, 7, 1)
	report.SetProfile(Config{
		Requests:          6,
		Concurrency:       3,
		BatchSize:         10,
		RequestsPerSecond: 20,
		HTTPTimeout:       2 * time.Second,
	})
	for _, latency := range []time.Duration{
		time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond,
		4 * time.Millisecond, 100 * time.Millisecond,
	} {
		report.RecordRequest(latency, true, 10, 0)
	}
	report.RecordRequest(200*time.Millisecond, false, 10, 10)
	report.Finalize(startedAt.Add(time.Second), Thresholds{
		MaxP95Latency: 50 * time.Millisecond,
		MaxErrorRate:  0.1,
		MaxDropRate:   0.1,
	})

	if report.Latency.P50MS != 3 || report.Latency.P95MS != 200 || report.Latency.P99MS != 200 {
		t.Fatalf("latency = %+v", report.Latency)
	}
	if report.ErrorRate == 0 || report.DropRate == 0 || report.Passed {
		t.Fatalf("report = %+v", report)
	}
	if report.ExitCode() == 0 || len(report.Violations) != 3 {
		t.Fatalf("violations = %v exit=%d", report.Violations, report.ExitCode())
	}
	if report.Throughput.RequestsPerSecond != 6 || report.Throughput.SpansPerSecond != 60 {
		t.Fatalf("throughput = %+v", report.Throughput)
	}
	if report.Profile.Concurrency != 3 || report.Profile.BatchSize != 10 ||
		report.Profile.RequestsPerSecond != 20 || report.Profile.HTTPTimeoutMS != 2000 {
		t.Fatalf("profile = %+v", report.Profile)
	}
}

func TestRuntimeErrorsContributeToErrorThreshold(t *testing.T) {
	report := NewReport(time.Now(), 7, 1)
	report.RecordRequest(time.Millisecond, true, 1, 0)
	report.RecordRuntime(false)
	report.Finalize(time.Now().Add(time.Second), Thresholds{MaxErrorRate: 0.1})

	if report.ErrorRate != 0.5 {
		t.Fatalf("error rate = %v, want 0.5", report.ErrorRate)
	}
	if report.Passed {
		t.Fatal("report passed despite Runtime failure")
	}
}

func TestRunnerSendsConcurrentBatchesAndMeasuresSSE(t *testing.T) {
	var (
		mu             sync.Mutex
		requests       int
		spans          int
		seenAuthTokens = map[string]bool{}
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/traces":
			body, _ := io.ReadAll(r.Body)
			decoded, _, err := ingest.DecodeOTLPTraceRequest(body, 100)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			mu.Lock()
			requests++
			spans += len(decoded)
			seenAuthTokens[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")] = true
			mu.Unlock()
			w.WriteHeader(http.StatusOK)
		case strings.HasSuffix(r.URL.Path, "/events/stream"):
			if cookie, err := r.Cookie("session"); err != nil || cookie.Value != "signed-session" {
				http.Error(w, "missing session", http.StatusUnauthorized)
				return
			}
			if got := r.Header.Get("Last-Event-ID"); got != "1" {
				http.Error(w, "missing recovery cursor", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = fmt.Fprint(w, "event: reset\ndata: {\"reason\":\"cursor_expired\"}\n\n")
			_, _ = fmt.Fprintf(w, "event: run.started\ndata: {\"occurred_at\":%q}\n\n", time.Now().UTC().Format(time.RFC3339Nano))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := Config{
		ServerURL: server.URL,
		Targets: []Target{
			{ProjectID: "project-a", Token: "token-a", SessionCookie: "signed-session", AgentName: "agent-a"},
			{ProjectID: "project-b", Token: "token-b", SessionCookie: "signed-session", AgentName: "agent-b"},
		},
		Concurrency: 4,
		BatchSize:   3,
		Requests:    12,
		Seed:        99,
		EnableSSE:   true,
		SSEAfterID:  "1",
		HTTPTimeout: time.Second,
	}
	report, err := NewRunner(cfg, server.Client()).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 12 || spans != 36 || len(seenAuthTokens) != 2 {
		t.Fatalf("requests=%d spans=%d tokens=%v", requests, spans, seenAuthTokens)
	}
	if report.Requests != 12 || report.Spans != 36 || report.SSE.Connections != 2 ||
		report.SSE.Events < 4 || report.SSE.Resets != 2 || report.SSE.FirstEventMS < 0 {
		t.Fatalf("report = %+v", report)
	}
	if report.Profile.Concurrency != 4 || report.Profile.BatchSize != 3 {
		t.Fatalf("profile = %+v", report.Profile)
	}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"token-a", "token-b", "signed-session"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("report leaked secret %q: %s", secret, encoded)
		}
	}
	if report.SchemaVersion != ReportSchemaVersion {
		t.Fatalf("schema = %q", report.SchemaVersion)
	}
}

func TestSSEParserReadsEventAndOccurrenceTime(t *testing.T) {
	input := "id: 1\nevent: run.started\ndata: {\"occurred_at\":\"2026-07-24T10:00:00Z\"}\n\n"
	event, occurredAt, err := readSSEEvent(bufio.NewReader(strings.NewReader(input)))
	if err != nil {
		t.Fatal(err)
	}
	if event != "run.started" || !occurredAt.Equal(time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("event=%q occurred=%s", event, occurredAt)
	}
}
