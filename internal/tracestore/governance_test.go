package tracestore

import (
	"context"
	"os"
	"testing"
	"time"

	"AgentScope/internal/anomalies"
	"github.com/google/uuid"
)

func TestGovernanceMigrationCreatesRollupsWithTTL(t *testing.T) {
	store := openGovernanceTestStore(t)
	defer store.Close()
	dropTraceTables(t, store)
	runClickHouseMigration(t, store)

	ctx := context.Background()
	for table, wantTTL := range map[string]string{
		"trace_spans":                    "toIntervalDay(30)",
		"trace_governance_rollup_hourly": "toIntervalDay(180)",
		"trace_governance_rollup_daily":  "toIntervalDay(730)",
	} {
		var ttl string
		if err := store.conn.QueryRow(ctx, `
			SELECT create_table_query
			FROM system.tables
			WHERE database = currentDatabase() AND name = ?
		`, table).Scan(&ttl); err != nil {
			t.Fatalf("read schema for %s: %v", table, err)
		}
		if !containsFold(ttl, wantTTL) {
			t.Fatalf("%s schema does not contain TTL %q: %s", table, wantTTL, ttl)
		}
	}
}

func TestGovernanceMaterializedViewsAggregateAllMetrics(t *testing.T) {
	store := openGovernanceTestStore(t)
	defer store.Close()
	dropTraceTables(t, store)
	runClickHouseMigration(t, store)

	ctx := context.Background()
	projectID := uuid.New()
	runID := uuid.New()
	now := time.Now().UTC().Truncate(time.Second)
	attrs := `{"agentscope.agent.name":"governed-agent","agentscope.agent.version":"v2","agentscope.environment":"prod"}`
	spans := []TraceSpan{
		{
			ProjectID: projectID, RunID: runID, TraceID: "trace-governance", SpanID: "root",
			Name: "agent.run", Kind: "INTERNAL", StatusCode: "OK", StartTime: now,
			EndTime: timePtr(now.Add(250 * time.Millisecond)), DurationMs: int64Ptr(250),
			Attributes: attrs, IngestedAt: now,
		},
		{
			ProjectID: projectID, RunID: runID, TraceID: "trace-governance", SpanID: "llm",
			Name: "llm.call", Kind: "INTERNAL", StatusCode: "OK", StartTime: now,
			EndTime: timePtr(now.Add(100 * time.Millisecond)), DurationMs: int64Ptr(100),
			Attributes: `{"agentscope.agent.name":"governed-agent","agentscope.agent.version":"v2","agentscope.environment":"prod","agentscope.span.kind":"LLM","llm.total_tokens":120,"llm.cost":0.42,"llm.ttft_ms":35}`,
			IngestedAt: now,
		},
		{
			ProjectID: projectID, RunID: runID, TraceID: "trace-governance", SpanID: "tool",
			Name: "tool.call", Kind: "INTERNAL", StatusCode: "ERROR", StartTime: now,
			EndTime: timePtr(now.Add(10 * time.Millisecond)), DurationMs: int64Ptr(10),
			Attributes: `{"agentscope.agent.name":"governed-agent","agentscope.agent.version":"v2","agentscope.environment":"prod","agentscope.span.kind":"TOOL","tool.success":false}`,
			IngestedAt: now,
		},
	}
	if err := store.InsertSpans(ctx, spans); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertIngestHealthEvents(ctx, []IngestHealthEvent{{
		ProjectID: projectID, AgentName: "governed-agent", Outcome: "rejected",
		HTTPStatus: 429, ErrorCode: "quota_exceeded", EventTime: now, IngestedAt: now,
	}}); err != nil {
		t.Fatal(err)
	}

	want := map[string]float64{
		"run_latency_ms":     250,
		"llm_total_tokens":   120,
		"llm_cost":           0.42,
		"llm_ttft_ms":        35,
		"tool_error_rate":    1,
		"ingest_reject_rate": 1,
	}
	for _, table := range []string{"trace_governance_rollup_hourly", "trace_governance_rollup_daily"} {
		rows, err := store.conn.Query(ctx, `
			SELECT metric, sum(value_sum) / sum(sample_count), sum(sample_count)
			FROM `+table+`
			WHERE project_id = ? AND agent_name = ?
			GROUP BY metric
		`, projectID, "governed-agent")
		if err != nil {
			t.Fatalf("query %s: %v", table, err)
		}
		got := map[string]float64{}
		for rows.Next() {
			var metric string
			var value float64
			var count uint64
			if err := rows.Scan(&metric, &value, &count); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			if count != 1 {
				rows.Close()
				t.Fatalf("%s %s sample count = %d, want 1", table, metric, count)
			}
			got[metric] = value
		}
		rows.Close()
		for metric, value := range want {
			if got[metric] != value {
				t.Fatalf("%s metric %s = %v, want %v (all=%v)", table, metric, got[metric], value, got)
			}
		}
	}
}

func TestListSeriesMergesRollupRowsAndPreservesDimensions(t *testing.T) {
	store := openGovernanceTestStore(t)
	defer store.Close()
	dropTraceTables(t, store)
	runClickHouseMigration(t, store)

	ctx := context.Background()
	projectID := uuid.New()
	bucket := time.Now().UTC().Add(-time.Hour).Truncate(time.Hour)
	for _, value := range []float64{100, 200} {
		if err := store.conn.Exec(ctx, `
			INSERT INTO trace_governance_rollup_hourly
				(project_id, agent_name, agent_version, environment, bucket, metric, value_sum, sample_count)
			VALUES (?, 'agent-a', 'v3', 'prod', ?, 'run_latency_ms', ?, 1)
		`, projectID, bucket, value); err != nil {
			t.Fatal(err)
		}
	}

	series, err := store.ListSeries(ctx, bucket.Add(-time.Minute), bucket.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(series) != 1 {
		t.Fatalf("series count = %d, want 1: %+v", len(series), series)
	}
	wantGroup := anomalies.Group{
		ProjectID: projectID.String(), AgentName: "agent-a", AgentVersion: "v3",
		Environment: "prod", Metric: anomalies.MetricRunLatency,
	}
	if series[0].Group != wantGroup {
		t.Fatalf("group = %+v, want %+v", series[0].Group, wantGroup)
	}
	if len(series[0].Points) != 1 || series[0].Points[0].Value != 150 ||
		series[0].Points[0].SampleCount != 2 {
		t.Fatalf("points = %+v", series[0].Points)
	}
}

func openGovernanceTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("AGENTSCOPE_TEST_CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_CLICKHOUSE_DSN not set")
	}
	requireTestDatabase(t, dsn)
	store, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func containsFold(value, fragment string) bool {
	for i := 0; i+len(fragment) <= len(value); i++ {
		if equalFoldASCII(value[i:i+len(fragment)], fragment) {
			return true
		}
	}
	return false
}

func equalFoldASCII(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		a, b := left[i], right[i]
		if a >= 'a' && a <= 'z' {
			a -= 'a' - 'A'
		}
		if b >= 'a' && b <= 'z' {
			b -= 'a' - 'A'
		}
		if a != b {
			return false
		}
	}
	return true
}
