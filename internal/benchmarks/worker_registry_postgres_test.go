package benchmarks

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresWorkerRegistryPersistsLiveWorkersAcrossRestartAndExpiresByTTL(t *testing.T) {
	databaseURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}
	if !strings.Contains(databaseURL, "_test") {
		t.Fatal("AGENTSCOPE_TEST_DATABASE_URL must point to a test database")
	}
	ctx := context.Background()
	pool := openWorkerRegistryTestPool(t, ctx, databaseURL)
	projectID := createWorkerRegistryTestProject(t, ctx, pool)
	// This closure captures the pool variable rather than the initial pool
	// argument, so cleanup uses the replacement connection after the simulated
	// restart below.
	t.Cleanup(func() {
		if pool != nil {
			if _, err := pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, projectID); err != nil {
				t.Errorf("cleanup worker registry test project %s: %v", projectID, err)
			}
			pool.Close()
		}
	})

	now := time.Date(2026, time.August, 10, 9, 30, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	registryBeforeRestart := NewPostgresWorkerRegistry(pool, time.Minute, WithWorkerRegistryClock(clock))

	registered, err := registryBeforeRestart.Register(ctx, WorkerRegistration{
		ProjectID:      projectID,
		Name:           "harbor_primary",
		ExecutionKinds: []string{"harbor", "tau2", "harbor"},
		Capacity:       3,
		Version:        "v-test-1",
	})
	if err != nil {
		t.Fatalf("Register(primary) error = %v", err)
	}
	if got, want := registered.ExecutionKinds, []string{"harbor", "tau2"}; !equalStrings(got, want) {
		t.Fatalf("primary execution kinds = %#v, want %#v", got, want)
	}
	if !registered.LastSeenAt.Equal(now) || !registered.ExpiresAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("primary liveness = %#v, want last_seen=%s expires=%s", registered, now, now.Add(time.Minute))
	}
	if _, err := registryBeforeRestart.Register(ctx, WorkerRegistration{
		ProjectID:      projectID,
		Name:           "function_worker",
		ExecutionKinds: []string{"function"},
		Capacity:       2,
	}); err != nil {
		t.Fatalf("Register(function) error = %v", err)
	}

	availability, err := registryBeforeRestart.Availability(ctx, projectID, "harbor")
	if err != nil {
		t.Fatalf("Availability(before restart) error = %v", err)
	}
	if !availability.Available || availability.State != "ready" || availability.RegisteredWorkers != 2 || availability.MatchingWorkers != 1 || availability.AdvertisedCapacity != 3 {
		t.Fatalf("Availability(before restart) = %#v", availability)
	}

	// A new pool and registry instance model an AgentScope server restart. The
	// only source of registration state is PostgreSQL, not the old registry.
	pool.Close()
	pool = openWorkerRegistryTestPool(t, ctx, databaseURL)
	registryAfterRestart := NewPostgresWorkerRegistry(pool, time.Minute, WithWorkerRegistryClock(clock))
	workers, err := registryAfterRestart.List(ctx, projectID)
	if err != nil {
		t.Fatalf("List(after restart) error = %v", err)
	}
	if len(workers) != 2 || workers[0].Name != "function_worker" || workers[1].Name != "harbor_primary" {
		t.Fatalf("List(after restart) = %#v, want sorted durable workers", workers)
	}
	availability, err = registryAfterRestart.Availability(ctx, projectID, "tau2")
	if err != nil {
		t.Fatalf("Availability(after restart) error = %v", err)
	}
	if !availability.Available || availability.RegisteredWorkers != 2 || availability.MatchingWorkers != 1 || availability.AdvertisedCapacity != 3 {
		t.Fatalf("Availability(after restart) = %#v", availability)
	}

	// Exact expiry is not live, matching the existing in-memory registry's
	// !ExpiresAt.After(now) behavior.
	now = now.Add(time.Minute)
	workers, err = registryAfterRestart.List(ctx, projectID)
	if err != nil {
		t.Fatalf("List(after expiry) error = %v", err)
	}
	if len(workers) != 0 {
		t.Fatalf("List(after expiry) = %#v, want no live workers", workers)
	}
	availability, err = registryAfterRestart.Availability(ctx, projectID, "harbor")
	if err != nil {
		t.Fatalf("Availability(after expiry) error = %v", err)
	}
	if availability.Available || availability.State != "unavailable" || availability.RegisteredWorkers != 0 || availability.MatchingWorkers != 0 || availability.AdvertisedCapacity != 0 {
		t.Fatalf("Availability(after expiry) = %#v", availability)
	}

	// Re-registering the same project/name is an atomic liveness refresh and
	// must replace the advertised capability and capacity rather than duplicate
	// a stale row.
	refreshed, err := registryAfterRestart.Register(ctx, WorkerRegistration{
		ProjectID:      projectID,
		Name:           "harbor_primary",
		ExecutionKinds: []string{"function"},
		Capacity:       7,
		Version:        "v-test-2",
	})
	if err != nil {
		t.Fatalf("Register(refresh) error = %v", err)
	}
	if refreshed.Capacity != 7 || refreshed.Version != "v-test-2" || !refreshed.LastSeenAt.Equal(now) || !refreshed.ExpiresAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("refreshed worker = %#v", refreshed)
	}
	availability, err = registryAfterRestart.Availability(ctx, projectID, "harbor")
	if err != nil {
		t.Fatalf("Availability(harbor after refresh) error = %v", err)
	}
	if availability.Available || availability.MatchingWorkers != 0 {
		t.Fatalf("Availability(harbor after refresh) = %#v", availability)
	}
	availability, err = registryAfterRestart.Availability(ctx, projectID, "function")
	if err != nil {
		t.Fatalf("Availability(function after refresh) error = %v", err)
	}
	if !availability.Available || availability.RegisteredWorkers != 1 || availability.MatchingWorkers != 1 || availability.AdvertisedCapacity != 7 {
		t.Fatalf("Availability(function after refresh) = %#v", availability)
	}
}

func TestPostgresWorkerRegistryReportsMissingStore(t *testing.T) {
	registry := NewPostgresWorkerRegistry(nil, time.Minute)
	if _, err := registry.List(context.Background(), "not-a-project-id"); err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("List(nil registry) error = %v, want not configured", err)
	}
}

func openWorkerRegistryTestPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	var tableName pgtype.Text
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.benchmark_workers')`).Scan(&tableName); err != nil {
		pool.Close()
		t.Fatalf("check benchmark worker registry migration: %v", err)
	}
	if !tableName.Valid {
		pool.Close()
		t.Fatal("benchmark worker registry migration is not applied")
	}
	return pool
}

func createWorkerRegistryTestProject(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	project, err := store.New(pool).CreateProject(ctx, store.CreateProjectParams{
		Name:        "benchmark worker registry persistence test",
		Description: pgtype.Text{String: "durable worker registry test", Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	return project.ID.String()
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
