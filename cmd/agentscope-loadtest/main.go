package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"AgentScope/internal/loadtest"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("agentscope-loadtest", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var (
		serverURL     = flags.String("server-url", "http://127.0.0.1:8080", "AgentScope server URL")
		targetsPath   = flags.String("targets", "", "JSON target file")
		duration      = flags.Duration("duration", 0, "load duration")
		requests      = flags.Int("requests", 0, "fixed request count")
		concurrency   = flags.Int("concurrency", 8, "concurrent workers")
		batchSize     = flags.Int("batch-size", 32, "spans per OTLP request")
		requestRate   = flags.Int("rate", 0, "global requests per second; zero is unlimited")
		seed          = flags.Int64("seed", 42, "deterministic seed")
		httpTimeout   = flags.Duration("http-timeout", 10*time.Second, "per-request timeout")
		enableSSE     = flags.Bool("sse", false, "observe project SSE streams")
		sseAfterID    = flags.String("sse-after-id", "", "SSE Last-Event-ID recovery cursor")
		enableRuntime = flags.Bool("runtime", false, "drive Runtime start/state/finish")
		workload      = flags.String("workload", string(loadtest.WorkloadRaw), "OTLP workload: raw or llm")
		output        = flags.String("output", "terminal", "terminal or json")
		maxP95        = flags.Duration("max-p95", 0, "maximum allowed P95 latency")
		maxErrorRate  = flags.Float64("max-error-rate", 0, "maximum allowed request error rate")
		maxDropRate   = flags.Float64("max-drop-rate", 0, "maximum allowed dropped span rate")
	)
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *targetsPath == "" {
		fmt.Fprintln(stderr, "--targets is required")
		return 2
	}
	targets, err := loadtest.LoadTargets(*targetsPath)
	if err != nil {
		fmt.Fprintf(stderr, "load targets: %v\n", err)
		return 2
	}
	config := loadtest.Config{
		ServerURL:         *serverURL,
		Targets:           targets,
		Duration:          *duration,
		Requests:          *requests,
		Concurrency:       *concurrency,
		BatchSize:         *batchSize,
		RequestsPerSecond: *requestRate,
		Seed:              *seed,
		HTTPTimeout:       *httpTimeout,
		EnableSSE:         *enableSSE,
		SSEAfterID:        *sseAfterID,
		EnableRuntime:     *enableRuntime,
		Workload:          loadtest.WorkloadProfile(*workload),
		Thresholds: loadtest.Thresholds{
			MaxP95Latency: *maxP95,
			MaxErrorRate:  *maxErrorRate,
			MaxDropRate:   *maxDropRate,
		},
	}
	report, err := loadtest.NewRunner(config, nil).Run(context.Background())
	if err != nil {
		fmt.Fprintf(stderr, "load test failed: %v\n", err)
		return 1
	}
	if *output == "json" {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintf(stderr, "encode report: %v\n", err)
			return 1
		}
	} else {
		printTerminalReport(stdout, report)
	}
	return report.ExitCode()
}

func printTerminalReport(output io.Writer, report *loadtest.Report) {
	fmt.Fprintf(output, "AgentScope Phase 4 load test: passed=%t\n", report.Passed)
	fmt.Fprintf(
		output,
		"projects=%d requests=%d spans=%d errors=%d dropped=%d\n",
		report.Projects,
		report.Requests,
		report.Spans,
		report.Errors,
		report.DroppedSpans,
	)
	fmt.Fprintf(
		output,
		"latency p50=%.2fms p95=%.2fms p99=%.2fms error_rate=%.4f drop_rate=%.4f\n",
		report.Latency.P50MS,
		report.Latency.P95MS,
		report.Latency.P99MS,
		report.ErrorRate,
		report.DropRate,
	)
	fmt.Fprintf(
		output,
		"throughput requests=%.2f/s spans=%.2f/s concurrency=%d batch=%d workload=%s\n",
		report.Throughput.RequestsPerSecond,
		report.Throughput.SpansPerSecond,
		report.Profile.Concurrency,
		report.Profile.BatchSize,
		report.Profile.Workload,
	)
	fmt.Fprintf(
		output,
		"sse connections=%d events=%d resets=%d errors=%d first_event=%.2fms\n",
		report.SSE.Connections,
		report.SSE.Events,
		report.SSE.Resets,
		report.SSE.Errors,
		report.SSE.FirstEventMS,
	)
	for _, violation := range report.Violations {
		fmt.Fprintf(output, "threshold: %s\n", violation)
	}
}
