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
	for _, tbl := range []string{"trace_spans", "trace_span_events", "trace_llm_calls", "trace_tool_calls", "trace_retrieval_calls", "trace_skill_calls"} {
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
	for _, tbl := range []string{"trace_spans", "trace_span_events", "trace_llm_calls", "trace_tool_calls", "trace_retrieval_calls", "trace_skill_calls"} {
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
	if err := store.InsertLLMCalls(context.Background(), nil); err != nil {
		t.Errorf("InsertLLMCalls(nil) error = %v", err)
	}
	if err := store.InsertToolCalls(context.Background(), nil); err != nil {
		t.Errorf("InsertToolCalls(nil) error = %v", err)
	}
	if err := store.InsertRetrievalCalls(context.Background(), nil); err != nil {
		t.Errorf("InsertRetrievalCalls(nil) error = %v", err)
	}
	if err := store.InsertSkillCalls(context.Background(), nil); err != nil {
		t.Errorf("InsertSkillCalls(nil) error = %v", err)
	}
}

func TestInsertAndListLLMCalls(t *testing.T) {
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
	projectID := uuid.New()
	runID := uuid.New()
	agentID := "agent-1"
	agentInstanceID := "inst-1"
	provider := "openai"
	model := "gpt-4"
	inputTokens := int64(50)
	outputTokens := int64(100)
	totalTokens := int64(150)
	cost := 0.002

	calls := []TraceLLMCall{
		{
			ProjectID:       projectID,
			RunID:           runID,
			TraceID:         "trace-llm-1",
			SpanID:          "span-llm-001",
			AgentID:         &agentID,
			AgentInstanceID: &agentInstanceID,
			Provider:        provider,
			Model:           model,
			InputTokens:     &inputTokens,
			OutputTokens:    &outputTokens,
			TotalTokens:     &totalTokens,
			Cost:            &cost,
			InputSummary:    "What is the capital of France?",
			OutputSummary:   "Paris",
			ContentCaptured: false,
			Status:          "OK",
			Error:           nil,
			StartTime:       now,
			DurationMs:      int64Ptr(500),
			IngestedAt:      now,
		},
	}

	if err := store.InsertLLMCalls(ctx, calls); err != nil {
		t.Fatalf("InsertLLMCalls: %v", err)
	}

	got, err := store.ListLLMCallsByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListLLMCallsByRun: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d llm calls, want 1", len(got))
	}
	if got[0].Provider != provider {
		t.Errorf("got provider = %q, want %q", got[0].Provider, provider)
	}
	if got[0].Model != model {
		t.Errorf("got model = %q, want %q", got[0].Model, model)
	}
	if got[0].TotalTokens == nil || *got[0].TotalTokens != totalTokens {
		t.Errorf("got total_tokens = %v, want %d", got[0].TotalTokens, totalTokens)
	}
}

func TestInsertAndListToolCalls(t *testing.T) {
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
	projectID := uuid.New()
	runID := uuid.New()
	risk := "medium"

	calls := []TraceToolCall{
		{
			ProjectID:        projectID,
			RunID:            runID,
			TraceID:          "trace-tool-1",
			SpanID:           "span-tool-001",
			ToolName:         "search_order",
			ToolType:         "api",
			ArgumentsSummary: `{"order_id": "123"}`,
			ResultSummary:    "order found",
			Success:          true,
			Error:            nil,
			RetryCount:       0,
			SideEffectRisk:   &risk,
			StartTime:        now,
			DurationMs:       int64Ptr(200),
			IngestedAt:       now,
		},
	}

	if err := store.InsertToolCalls(ctx, calls); err != nil {
		t.Fatalf("InsertToolCalls: %v", err)
	}

	got, err := store.ListToolCallsByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListToolCallsByRun: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d tool calls, want 1", len(got))
	}
	if got[0].ToolName != "search_order" {
		t.Errorf("got tool_name = %q, want search_order", got[0].ToolName)
	}
	if got[0].Success != true {
		t.Errorf("got success = %v, want true", got[0].Success)
	}
}

func TestInsertAndListRetrievalCalls(t *testing.T) {
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
	projectID := uuid.New()
	runID := uuid.New()
	topK := int64(5)
	returnedCount := int64(3)

	calls := []TraceRetrievalCall{
		{
			ProjectID:      projectID,
			RunID:          runID,
			TraceID:        "trace-ret-1",
			SpanID:         "span-ret-001",
			KnowledgeBase:  "product-knowledge",
			QueryText:      "order return policy",
			TopK:           &topK,
			ReturnedCount:  &returnedCount,
			DocumentIDs:    []string{"doc-1", "doc-2", "doc-3"},
			Scores:         []float64{0.95, 0.87, 0.72},
			StartTime:      now,
			DurationMs:     int64Ptr(150),
			IngestedAt:     now,
		},
	}

	if err := store.InsertRetrievalCalls(ctx, calls); err != nil {
		t.Fatalf("InsertRetrievalCalls: %v", err)
	}

	got, err := store.ListRetrievalCallsByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListRetrievalCallsByRun: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d retrieval calls, want 1", len(got))
	}
	if got[0].KnowledgeBase != "product-knowledge" {
		t.Errorf("got knowledge_base = %q, want product-knowledge", got[0].KnowledgeBase)
	}
	if len(got[0].DocumentIDs) != 3 {
		t.Errorf("got %d document_ids, want 3", len(got[0].DocumentIDs))
	}
}

func TestInsertAndListSkillCalls(t *testing.T) {
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
	projectID := uuid.New()
	runID := uuid.New()

	calls := []TraceSkillCall{
		{
			ProjectID:     projectID,
			RunID:         runID,
			TraceID:       "trace-skill-1",
			SpanID:        "span-skill-001",
			SkillName:     "refund_policy_check",
			SkillVersion:  "1.2.0",
			InputSummary:  "order-456",
			OutputSummary: "eligible for refund",
			Success:       true,
			Error:         nil,
			StartTime:     now,
			DurationMs:    int64Ptr(300),
			IngestedAt:    now,
		},
	}

	if err := store.InsertSkillCalls(ctx, calls); err != nil {
		t.Fatalf("InsertSkillCalls: %v", err)
	}

	got, err := store.ListSkillCallsByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListSkillCallsByRun: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d skill calls, want 1", len(got))
	}
	if got[0].SkillName != "refund_policy_check" {
		t.Errorf("got skill_name = %q, want refund_policy_check", got[0].SkillName)
	}
	if got[0].SkillVersion != "1.2.0" {
		t.Errorf("got skill_version = %q, want 1.2.0", got[0].SkillVersion)
	}
	if got[0].Success != true {
		t.Errorf("got success = %v, want true", got[0].Success)
	}
}

// TestQueryTokenCostSummaryHandlesNullableTokenAndCost verifies that
// QueryTokenCostSummary handles NULL total_tokens / cost correctly.
// When all LLM calls have nil cost, sum(cost) returns NULL in ClickHouse,
// which must be coerced to 0 before scanning into non-nullable Go types.
func TestQueryTokenCostSummaryHandlesNullableTokenAndCost(t *testing.T) {
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
	projectID := uuid.New()
	runID := uuid.New()

	// Insert a single LLM call with TotalTokens set but Cost = nil.
	calls := []TraceLLMCall{
		{
			ProjectID:       projectID,
			RunID:           runID,
			TraceID:         "trace-null-cost-1",
			SpanID:          "span-null-cost-001",
			Provider:        "openai",
			Model:           "gpt-4",
			TotalTokens:     int64Ptr(128),
			InputTokens:     int64Ptr(50),
			OutputTokens:    int64Ptr(78),
			Cost:            nil, // intentionally nil — triggers the bug
			InputSummary:    "hello",
			OutputSummary:   "world",
			ContentCaptured: false,
			Status:          "OK",
			Error:           nil,
			StartTime:       now,
			DurationMs:      int64Ptr(200),
			IngestedAt:      now,
		},
	}
	if err := store.InsertLLMCalls(ctx, calls); err != nil {
		t.Fatalf("InsertLLMCalls: %v", err)
	}

	summary, err := store.QueryTokenCostSummary(ctx, projectID, 15)
	if err != nil {
		t.Fatalf("QueryTokenCostSummary error = %v", err)
	}
	if summary.CallCount != 1 {
		t.Errorf("CallCount = %d, want 1", summary.CallCount)
	}
	if summary.TotalTokens != 128 {
		t.Errorf("TotalTokens = %d, want 128", summary.TotalTokens)
	}
	if summary.TotalCostUSD != 0 {
		t.Errorf("TotalCostUSD = %f, want 0", summary.TotalCostUSD)
	}
}

// TestQueryTokenCostSummaryReturnsZeroForEmptyWindow verifies that
// QueryTokenCostSummary returns zeros (not errors) when the time window
// contains no LLM calls at all.
func TestQueryTokenCostSummaryReturnsZeroForEmptyWindow(t *testing.T) {
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

	projectID := uuid.New()

	// Query with a fresh project that has no data.
	summary, err := store.QueryTokenCostSummary(ctx, projectID, 15)
	if err != nil {
		t.Fatalf("QueryTokenCostSummary error = %v", err)
	}
	if summary.CallCount != 0 {
		t.Errorf("CallCount = %d, want 0", summary.CallCount)
	}
	if summary.TotalTokens != 0 {
		t.Errorf("TotalTokens = %d, want 0", summary.TotalTokens)
	}
	if summary.TotalCostUSD != 0 {
		t.Errorf("TotalCostUSD = %f, want 0", summary.TotalCostUSD)
	}
}

func strPtr(s string) *string        { return &s }
func timePtr(t time.Time) *time.Time { return &t }
func int64Ptr(i int64) *int64        { return &i }
