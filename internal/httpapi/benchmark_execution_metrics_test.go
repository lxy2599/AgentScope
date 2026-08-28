package httpapi

import "testing"

func TestBenchmarkP95LatencyUsesNearestRank(t *testing.T) {
	if got, want := benchmarkP95Latency([]int64{500, 100, 300, 200, 400}), int64(500); got != want {
		t.Fatalf("p95 = %d, want %d", got, want)
	}
	if got, want := benchmarkP95Latency([]int64{42}), int64(42); got != want {
		t.Fatalf("single-sample p95 = %d, want %d", got, want)
	}
	if got := benchmarkP95Latency(nil); got != 0 {
		t.Fatalf("empty p95 = %d, want 0", got)
	}
}
