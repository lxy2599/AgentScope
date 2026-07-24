package adapter

import (
	"context"
	"os"
	"testing"
	"time"

	"AgentScope/internal/anomalies"
	"AgentScope/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestAnomalyRepositoryUpsertIsIdempotentAndEnqueuesOnce(t *testing.T) {
	databaseURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}
	requireAdapterTestDatabase(t, databaseURL)
	ctx := context.Background()
	pool, err := store.Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	applyAdapterMigration(t, pool, "../../migrations/00012_phase4_operational_anomalies.sql")

	projectID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO projects (id, name) VALUES ($1, $2)`, projectID, "anomaly-test-"+projectID.String()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM projects WHERE id = $1`, projectID)
	})

	repo := NewAnomalyRepository(pool)
	window := time.Now().UTC().Truncate(time.Hour)
	record := anomalies.Record{
		Group: anomalies.Group{
			ProjectID: projectID.String(), AgentName: "agent-a", AgentVersion: "v1",
			Environment: "prod", Metric: anomalies.MetricRunLatency,
		},
		WindowStart: window, WindowEnd: window.Add(15 * time.Minute),
		ObservedValue: 250, BaselineMedian: 100, BaselineMAD: 5,
		DeviationScore: 20, SampleCount: 5, BaselineSampleCount: 30,
		Status: anomalies.StatusAnomaly, LastDetectedAt: window.Add(15 * time.Minute),
	}

	first, err := repo.Upsert(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	record.ObservedValue = 275
	record.LastDetectedAt = record.LastDetectedAt.Add(time.Minute)
	second, err := repo.Upsert(ctx, record)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID == "" || second.ID != first.ID {
		t.Fatalf("ids = %q and %q", first.ID, second.ID)
	}
	if !second.FirstDetectedAt.Equal(first.FirstDetectedAt) ||
		!second.LastDetectedAt.Equal(record.LastDetectedAt) {
		t.Fatalf("timestamps first=%s second=%+v", first.FirstDetectedAt, second)
	}

	list, err := repo.ListProject(ctx, projectID.String(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ObservedValue != 275 {
		t.Fatalf("list = %+v", list)
	}

	var events int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM realtime_outbox
		WHERE project_id = $1 AND event_type = 'operational.anomaly'
	`, projectID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Fatalf("outbox events = %d, want 1", events)
	}
}

func applyAdapterMigration(t *testing.T, pool *pgxpool.Pool, path string) {
	t.Helper()
	sql, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), string(sql)); err != nil {
		t.Fatal(err)
	}
}
