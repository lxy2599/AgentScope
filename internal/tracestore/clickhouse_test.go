package tracestore

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// requireTestDatabase fails the test if the database name in dsn
// does not end with "_test". This prevents accidental destructive operations.
func requireTestDatabase(t *testing.T, dsn string) string {
	t.Helper()
	parts := strings.SplitN(strings.TrimPrefix(dsn, "clickhouse://"), "/", 2)
	if len(parts) < 2 {
		t.Fatalf("cannot extract database from DSN: %q", dsn)
	}
	dbPart := strings.SplitN(parts[1], "?", 2)[0]
	if !strings.HasSuffix(dbPart, "_test") {
		t.Fatalf("DSN must point to a database ending with '_test'; got %q", dbPart)
	}
	return dbPart
}

// runClickHouseMigration executes the ClickHouse migration SQL.
func runClickHouseMigration(t *testing.T, store *Store) {
	t.Helper()
	b, err := os.ReadFile("../../clickhouse/migrations/00001_trace.sql")
	if err != nil {
		t.Fatalf("read ClickHouse migration file: %v", err)
	}
	ctx := context.Background()
	statements := strings.Split(string(b), ";")
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if err := store.conn.Exec(ctx, stmt); err != nil {
			if !strings.Contains(err.Error(), "already exists") {
				t.Fatalf("execute migration: %v\nStatement: %s", err, stmt)
			}
		}
	}
}

// dropTraceTables drops trace tables from the current database.
func dropTraceTables(t *testing.T, store *Store) {
	t.Helper()
	ctx := context.Background()
	for _, tbl := range []string{"trace_spans", "trace_span_events"} {
		_ = store.conn.Exec(ctx, "DROP TABLE IF EXISTS "+tbl)
	}
}

// currentDatabase returns the current database name from ClickHouse.
func currentDatabase(t *testing.T, store *Store) string {
	t.Helper()
	ctx := context.Background()
	row := store.conn.QueryRow(ctx, "SELECT currentDatabase()")
	var db string
	if err := row.Scan(&db); err != nil {
		t.Fatalf("get currentDatabase: %v", err)
	}
	return db
}

func TestClickHouseOpen(t *testing.T) {
	dsn := os.Getenv("AGENTSCOPE_TEST_CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_CLICKHOUSE_DSN not set")
	}

	ctx := context.Background()
	store, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()
}

func TestMigrationCreatesTablesInCorrectDatabase(t *testing.T) {
	dsn := os.Getenv("AGENTSCOPE_TEST_CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_CLICKHOUSE_DSN not set")
	}

	expectedDB := requireTestDatabase(t, dsn)

	ctx := context.Background()
	store, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	// Verify the connection is using the expected database
	gotDB := currentDatabase(t, store)
	if gotDB != expectedDB {
		t.Fatalf("currentDatabase() = %q, want %q", gotDB, expectedDB)
	}

	// Clean any trace tables from previous runs
	dropTraceTables(t, store)

	// Run migration
	runClickHouseMigration(t, store)

	// Verify tables exist in the current (test) database, not hardcoded elsewhere
	for _, tbl := range []string{"trace_spans", "trace_span_events"} {
		row := store.conn.QueryRow(ctx,
			"SELECT count() FROM system.tables WHERE database = currentDatabase() AND name = $1", tbl)
		var count uint64
		if err := row.Scan(&count); err != nil {
			t.Fatalf("check system.tables for %s: %v", tbl, err)
		}
		if count == 0 {
			t.Errorf("table %q not found in database %q (migration may use hardcoded database name)", tbl, expectedDB)
		}
	}
}

func TestInsertAndListSpans(t *testing.T) {
	dsn := os.Getenv("AGENTSCOPE_TEST_CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_CLICKHOUSE_DSN not set")
	}

	requireTestDatabase(t, dsn)

	ctx := context.Background()
	store, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	dropTraceTables(t, store)
	runClickHouseMigration(t, store)

	now := time.Now().UTC()
	ingestedAt := time.Now().UTC()
	projectID := uuid.New()
	runID := uuid.New()
	traceID := "test-trace-1"

	spans := []TraceSpan{
		{
			ProjectID:     projectID,
			RunID:         runID,
			TraceID:       traceID,
			SpanID:        "span-001",
			ParentSpanID:  nil,
			Name:          "root-span",
			Kind:          "INTERNAL",
			StatusCode:    "OK",
			StatusMessage: strPtr("success"),
			StartTime:     now,
			EndTime:       timePtr(now.Add(100 * time.Millisecond)),
			DurationMs:    int64Ptr(100),
			Attributes:    `{"key":"value"}`,
			Truncated:     false,
			IngestedAt:    ingestedAt,
		},
		{
			ProjectID:     projectID,
			RunID:         runID,
			TraceID:       traceID,
			SpanID:        "span-002",
			ParentSpanID:  strPtr("span-001"),
			Name:          "child-span",
			Kind:          "CLIENT",
			StatusCode:    "OK",
			StatusMessage: nil,
			StartTime:     now.Add(10 * time.Millisecond),
			EndTime:       timePtr(now.Add(80 * time.Millisecond)),
			DurationMs:    int64Ptr(70),
			Attributes:    `{}`,
			Truncated:     false,
			IngestedAt:    ingestedAt,
		},
	}

	if err := store.InsertSpans(ctx, spans); err != nil {
		t.Fatalf("InsertSpans: %v", err)
	}

	got, err := store.ListSpansByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListSpansByRun: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d spans, want 2", len(got))
	}
	if got[0].SpanID != "span-001" {
		t.Errorf("got first span ID = %q, want span-001", got[0].SpanID)
	}
	if got[1].ParentSpanID == nil || *got[1].ParentSpanID != "span-001" {
		t.Errorf("got second span parent = %v, want span-001", got[1].ParentSpanID)
	}
}

func TestInsertAndListSpanEvents(t *testing.T) {
	dsn := os.Getenv("AGENTSCOPE_TEST_CLICKHOUSE_DSN")
	if dsn == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_CLICKHOUSE_DSN not set")
	}

	requireTestDatabase(t, dsn)

	ctx := context.Background()
	store, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer store.Close()

	dropTraceTables(t, store)
	runClickHouseMigration(t, store)

	now := time.Now().UTC()
	ingestedAt := time.Now().UTC()
	projectID := uuid.New()
	runID := uuid.New()

	events := []TraceSpanEvent{
		{
			ProjectID:  projectID,
			RunID:      runID,
			TraceID:    "trace-events-1",
			SpanID:     "span-001",
			EventName:  "llm.request",
			EventTime:  now,
			Attributes: `{"model":"gpt-4"}`,
			Truncated:  false,
			IngestedAt: ingestedAt,
		},
		{
			ProjectID:  projectID,
			RunID:      runID,
			TraceID:    "trace-events-1",
			SpanID:     "span-001",
			EventName:  "llm.response",
			EventTime:  now.Add(200 * time.Millisecond),
			Attributes: `{"tokens":100}`,
			Truncated:  false,
			IngestedAt: ingestedAt,
		},
	}

	if err := store.InsertSpanEvents(ctx, events); err != nil {
		t.Fatalf("InsertSpanEvents: %v", err)
	}

	got, err := store.ListSpanEventsByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListSpanEventsByRun: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}
	if got[0].EventName != "llm.request" {
		t.Errorf("got first event name = %q, want llm.request", got[0].EventName)
	}
	if got[1].EventName != "llm.response" {
		t.Errorf("got second event name = %q, want llm.response", got[1].EventName)
	}
}

func TestEmptyInsert(t *testing.T) {
	store := &Store{}
	if err := store.InsertSpans(context.Background(), nil); err != nil {
		t.Errorf("InsertSpans(nil) error = %v", err)
	}
	if err := store.InsertSpanEvents(context.Background(), nil); err != nil {
		t.Errorf("InsertSpanEvents(nil) error = %v", err)
	}
	if err := store.InsertSpans(context.Background(), []TraceSpan{}); err != nil {
		t.Errorf("InsertSpans(empty) error = %v", err)
	}
	if err := store.InsertSpanEvents(context.Background(), []TraceSpanEvent{}); err != nil {
		t.Errorf("InsertSpanEvents(empty) error = %v", err)
	}
}

func strPtr(s string) *string        { return &s }
func timePtr(t time.Time) *time.Time { return &t }
func int64Ptr(i int64) *int64        { return &i }
