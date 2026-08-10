package benchmarks

import (
	"context"
	"os"
	"strings"
	"testing"

	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresCatalogStorePersistsImmutableProjectScopedCatalogAcrossRestart(t *testing.T) {
	databaseURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}
	if !strings.Contains(databaseURL, "_test") {
		t.Fatal("AGENTSCOPE_TEST_DATABASE_URL must point to a test database")
	}
	ctx := context.Background()
	pool := openCatalogTestPool(t, ctx, databaseURL)
	projectID := createCatalogTestProject(t, ctx, pool)
	defer func() {
		if pool != nil {
			pool.Close()
		}
	}()

	storeBeforeRestart := NewPostgresCatalogStore(pool)
	first := validCatalog()
	first.Revision = "git:catalog-persistence-a"
	if _, err := storeBeforeRestart.SyncCatalog(ctx, projectID, first); err != nil {
		t.Fatalf("SyncCatalog(first) error = %v", err)
	}

	// The same immutable revision may be re-synced idempotently, but cannot be
	// silently rewritten with a new payload.
	if _, err := storeBeforeRestart.SyncCatalog(ctx, projectID, first); err != nil {
		t.Fatalf("SyncCatalog(first again) error = %v", err)
	}
	conflicting := validCatalog()
	conflicting.Revision = first.Revision
	conflicting.Profiles[0].Config = []byte(`{"model":"openai/example","max_tokens":4096}`)
	if _, err := storeBeforeRestart.SyncCatalog(ctx, projectID, conflicting); err == nil || !strings.Contains(err.Error(), "immutable payload") {
		t.Fatalf("SyncCatalog(conflicting revision) error = %v, want immutable conflict", err)
	}
	current, err := storeBeforeRestart.ReadCatalog(ctx, projectID)
	if err != nil {
		t.Fatalf("ReadCatalog(before head advance) error = %v", err)
	}
	if current.Revision != first.Revision || string(current.Profiles[0].Config) != string(first.Profiles[0].Config) {
		t.Fatalf("current catalog after conflict = %#v", current)
	}

	second := validCatalog()
	second.Revision = "git:catalog-persistence-b"
	second.Profiles[0].Config = []byte(`{"model":"openai/example","max_tokens":4096}`)
	if _, err := storeBeforeRestart.SyncCatalog(ctx, projectID, second); err != nil {
		t.Fatalf("SyncCatalog(second) error = %v", err)
	}

	secretBearing := validCatalog()
	secretBearing.Revision = "git:catalog-secret-rejected"
	secretBearing.Profiles[0].Config = []byte(`{"api_key":"do-not-persist"}`)
	if _, err := storeBeforeRestart.SyncCatalog(ctx, projectID, secretBearing); err == nil {
		t.Fatal("SyncCatalog(secret-bearing) succeeded")
	}
	var persistedSecretRevisionCount int
	projectUUID, err := catalogProjectUUID(projectID)
	if err != nil {
		t.Fatalf("catalogProjectUUID() error = %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT count(*)
		FROM benchmark_catalog_revisions
		WHERE project_id = $1
		  AND revision = $2`, projectUUID, secretBearing.Revision).Scan(&persistedSecretRevisionCount); err != nil {
		t.Fatalf("count rejected secret revision: %v", err)
	}
	if persistedSecretRevisionCount != 0 {
		t.Fatalf("secret-bearing revision was persisted: count=%d", persistedSecretRevisionCount)
	}

	// Recreate the pool and store to model a server restart rather than merely
	// reading from the first store object's in-memory state.
	pool.Close()
	pool = openCatalogTestPool(t, ctx, databaseURL)
	storeAfterRestart := NewPostgresCatalogStore(pool)
	loaded, err := storeAfterRestart.ReadCatalog(ctx, projectID)
	if err != nil {
		t.Fatalf("ReadCatalog(after restart) error = %v", err)
	}
	if loaded.Revision != second.Revision || len(loaded.Artifacts) != 1 || len(loaded.Profiles) != 3 || len(loaded.RunTemplates) != 1 {
		t.Fatalf("loaded durable catalog = %#v", loaded)
	}
	if string(loaded.Profiles[0].Config) != string(second.Profiles[0].Config) {
		t.Fatalf("loaded profile payload = %s, want %s", loaded.Profiles[0].Config, second.Profiles[0].Config)
	}

	otherProjectID := createCatalogTestProject(t, ctx, pool)
	other, err := storeAfterRestart.ReadCatalog(ctx, otherProjectID)
	if err != nil {
		t.Fatalf("ReadCatalog(other project) error = %v", err)
	}
	if other.Revision != "unconfigured" || len(other.Artifacts) != 0 || len(other.Profiles) != 0 || len(other.RunTemplates) != 0 {
		t.Fatalf("other project catalog = %#v", other)
	}
	var revisionCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM benchmark_catalog_revisions WHERE project_id = $1`, projectUUID).Scan(&revisionCount); err != nil {
		t.Fatalf("count immutable revisions: %v", err)
	}
	if revisionCount != 2 {
		t.Fatalf("immutable revision count = %d, want 2", revisionCount)
	}
	for _, id := range []string{projectID, otherProjectID} {
		if _, err := pool.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id); err != nil {
			t.Errorf("cleanup catalog test project %s: %v", id, err)
		}
	}
}

func openCatalogTestPool(t *testing.T, ctx context.Context, databaseURL string) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(pool.Close)
	var tableName pgtype.Text
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.benchmark_catalog_revisions')`).Scan(&tableName); err != nil {
		pool.Close()
		t.Fatalf("check benchmark catalog migration: %v", err)
	}
	if !tableName.Valid {
		pool.Close()
		t.Fatal("benchmark catalog migration is not applied")
	}
	return pool
}

func createCatalogTestProject(t *testing.T, ctx context.Context, pool *pgxpool.Pool) string {
	t.Helper()
	project, err := store.New(pool).CreateProject(ctx, store.CreateProjectParams{
		Name:        "benchmark catalog persistence test",
		Description: pgtype.Text{String: "durable catalog test", Valid: true},
	})
	if err != nil {
		t.Fatalf("CreateProject() error = %v", err)
	}
	return project.ID.String()
}
