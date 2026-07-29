package realtime_test

import (
	"context"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"AgentScope/internal/adapter"
	"AgentScope/internal/realtime"
	"AgentScope/internal/store"
	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func TestPostgresOutboxToRedisStream(t *testing.T) {
	databaseURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AGENTSCOPE_TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || !strings.HasSuffix(strings.TrimPrefix(parsed.Path, "/"), "_test") {
		t.Fatalf("test database name must end with _test: %q", databaseURL)
	}

	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	schema := "realtime_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		adminPool.Close()
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		adminPool.Close()
	})

	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	pool, err := pgxpool.New(ctx, parsed.String())
	if err != nil {
		t.Fatalf("connect PostgreSQL test schema: %v", err)
	}
	defer pool.Close()
	applyMigration(t, pool, "../../migrations/00001_init.sql")
	applyMigration(t, pool, "../../migrations/00008_phase4_realtime.sql")

	projectID := uuid.New()
	userID := uuid.New()
	agentID := uuid.New()
	runID := uuid.New()
	email := "realtime-" + uuid.NewString() + "@test.local"
	fixtures := []struct {
		query string
		args  []any
	}{
		{
			`INSERT INTO users (id, email, password_hash, display_name)
			 VALUES ($1, $2, '\x01', 'Realtime Test')`,
			[]any{userID, email},
		},
		{`INSERT INTO projects (id, name) VALUES ($1, 'Realtime Test')`, []any{projectID}},
		{
			`INSERT INTO project_members (project_id, user_id, role)
			 VALUES ($1, $2, 'owner')`,
			[]any{projectID, userID},
		},
		{
			`INSERT INTO agents (id, project_id, name)
			 VALUES ($1, $2, 'integration-agent')`,
			[]any{agentID, projectID},
		},
		{
			`INSERT INTO runs (id, project_id, source, status, agent_name)
			 VALUES ($1, $2, 'live', 'running', 'integration-agent')`,
			[]any{runID, projectID},
		},
		{
			`INSERT INTO run_state_events (
				project_id, run_id, agent_id, from_status, to_status, message
			 ) VALUES ($1, $2, $3, '', 'running', 'started')`,
			[]any{projectID, runID, agentID},
		},
	}
	for _, fixture := range fixtures {
		if _, err := pool.Exec(ctx, fixture.query, fixture.args...); err != nil {
			t.Fatalf("insert realtime fixture: %v", err)
		}
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM projects WHERE id = $1", projectID)
		_, _ = pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	})

	redisAddr := os.Getenv("AGENTSCOPE_TEST_REDIS_ADDR")
	if redisAddr == "" {
		server := miniredis.RunT(t)
		redisAddr = server.Addr()
	}
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	t.Cleanup(func() {
		_, _ = client.Del(
			context.Background(),
			"agentscope:events:"+projectID.String(),
			"agentscope:events:"+projectID.String()+":published",
		).Result()
		_ = client.Close()
	})
	stream := realtime.NewRedisStream(client, realtime.RedisStreamOptions{
		MaxLen:    100,
		DedupeTTL: time.Hour,
	})
	relay := realtime.NewRelay(
		adapter.NewRealtimeOutbox(store.New(pool)),
		stream,
		realtime.RelayOptions{BatchSize: 100},
	)
	result, err := relay.RunOnce(ctx)
	if err != nil {
		t.Fatalf("RunOnce() error = %v", err)
	}
	if result.Published < 2 {
		t.Fatalf("published = %d, want at least agent.registered and run.started", result.Published)
	}

	events, err := stream.Read(ctx, projectID.String(), "0", 100, 0)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	var foundAgent, foundRun bool
	for _, event := range events {
		switch event.Type {
		case realtime.EventAgentRegistered:
			foundAgent = event.AgentID == agentID.String()
		case realtime.EventRunStarted:
			foundRun = event.RunID == runID.String()
		}
	}
	if !foundAgent || !foundRun {
		t.Fatalf("events missing expected identity: agent=%v run=%v events=%#v", foundAgent, foundRun, events)
	}
}

func applyMigration(t *testing.T, pool *pgxpool.Pool, path string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read migration %s: %v", path, err)
	}
	if _, err := pool.Exec(context.Background(), string(body)); err != nil {
		t.Fatalf("apply migration %s: %v", path, err)
	}
}

func TestPostgresOutboxRetentionAndDurableReader(t *testing.T) {
	databaseURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("AGENTSCOPE_TEST_DATABASE_URL is not set")
	}
	parsed, err := url.Parse(databaseURL)
	if err != nil || !strings.HasSuffix(strings.TrimPrefix(parsed.Path, "/"), "_test") {
		t.Fatalf("test database name must end with _test: %q", databaseURL)
	}

	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	schema := "realtime_retention_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		adminPool.Close()
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		adminPool.Close()
	})

	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	pool, err := pgxpool.New(ctx, parsed.String())
	if err != nil {
		t.Fatalf("connect PostgreSQL test schema: %v", err)
	}
	defer pool.Close()
	applyMigration(t, pool, "../../migrations/00001_init.sql")
	applyMigration(t, pool, "../../migrations/00008_phase4_realtime.sql")
	applyMigration(t, pool, "../../migrations/00020_realtime_outbox_retention.sql")

	var indexName *string
	if err := pool.QueryRow(ctx, "SELECT to_regclass('idx_realtime_outbox_published_retention')::text").Scan(&indexName); err != nil || indexName == nil {
		t.Fatalf("retention index was not created: index=%v err=%v", indexName, err)
	}

	projectID := uuid.New()
	if _, err := pool.Exec(ctx, "INSERT INTO projects (id, name) VALUES ($1, 'Realtime Retention Test')", projectID); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	insert := func(publishedAt *time.Time) int64 {
		t.Helper()
		var id int64
		err := pool.QueryRow(ctx, `
			INSERT INTO realtime_outbox (
				project_id, event_type, payload, payload_redacted, occurred_at, published_at
			) VALUES ($1, 'run.state.changed', '{"status":"running"}', true, now(), $2)
			RETURNING id
		`, projectID, publishedAt).Scan(&id)
		if err != nil {
			t.Fatalf("insert outbox row: %v", err)
		}
		return id
	}
	oldPublishedAt := time.Now().UTC().Add(-2 * time.Hour)
	oldPublishedID1 := insert(&oldPublishedAt)
	oldPublishedID2 := insert(&oldPublishedAt)
	freshPublishedAt := time.Now().UTC()
	freshPublishedID := insert(&freshPublishedAt)
	pendingID := insert(nil)

	outbox := adapter.NewRealtimeOutbox(store.New(pool))
	records, err := outbox.ListAfter(ctx, projectID.String(), 0, 10)
	if err != nil {
		t.Fatalf("ListAfter: %v", err)
	}
	if len(records) != 4 {
		t.Fatalf("ListAfter returned %d rows, want 4", len(records))
	}
	if records[0].ID != oldPublishedID1 || records[1].ID != oldPublishedID2 ||
		records[2].ID != freshPublishedID || records[3].ID != pendingID {
		t.Fatalf("ListAfter IDs = %#v, want ascending durable outbox IDs", []int64{records[0].ID, records[1].ID, records[2].ID, records[3].ID})
	}
	if latest, err := outbox.LatestID(ctx, projectID.String()); err != nil || latest != pendingID {
		t.Fatalf("LatestID = %d, %v; want %d", latest, err, pendingID)
	}
	if oldest, err := outbox.OldestID(ctx, projectID.String()); err != nil || oldest != oldPublishedID1 {
		t.Fatalf("OldestID = %d, %v; want %d", oldest, err, oldPublishedID1)
	}

	cutoff := time.Now().UTC().Add(-time.Hour)
	if deleted, err := outbox.DeletePublishedBefore(ctx, cutoff, 1); err != nil || deleted != 1 {
		t.Fatalf("first bounded cleanup = %d, %v; want 1", deleted, err)
	}
	if deleted, err := outbox.DeletePublishedBefore(ctx, cutoff, 1); err != nil || deleted != 1 {
		t.Fatalf("second bounded cleanup = %d, %v; want 1", deleted, err)
	}
	if deleted, err := outbox.DeletePublishedBefore(ctx, cutoff, 1); err != nil || deleted != 0 {
		t.Fatalf("final cleanup = %d, %v; want 0", deleted, err)
	}

	remaining, err := outbox.ListAfter(ctx, projectID.String(), 0, 10)
	if err != nil {
		t.Fatalf("ListAfter after cleanup: %v", err)
	}
	if len(remaining) != 2 || remaining[0].ID != freshPublishedID || remaining[1].ID != pendingID {
		t.Fatalf("remaining IDs = %#v, want fresh published and pending", remaining)
	}
}
