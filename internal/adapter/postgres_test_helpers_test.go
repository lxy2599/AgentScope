package adapter

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func requireAdapterTestDatabase(t *testing.T, databaseURL string) {
	t.Helper()
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("parse AGENTSCOPE_TEST_DATABASE_URL: %v", err)
	}
	databaseName := strings.TrimPrefix(parsed.Path, "/")
	if !strings.HasSuffix(databaseName, "_test") {
		t.Fatalf(
			"AGENTSCOPE_TEST_DATABASE_URL must point to a database ending with _test, got %q",
			databaseName,
		)
	}
}

func registerAdapterFixtureCleanup(
	t *testing.T,
	pool *pgxpool.Pool,
	projectIDs []string,
	userIDs []string,
) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		for _, projectID := range projectIDs {
			if _, err := pool.Exec(ctx, `
				DELETE FROM comparison_case_results
				WHERE baseline_run_id IN (
					SELECT id FROM runs WHERE project_id = $1
				)
				OR candidate_run_id IN (
					SELECT id FROM runs WHERE project_id = $1
				)
			`, projectID); err != nil {
				t.Errorf("cleanup comparison case results for project %s: %v", projectID, err)
			}
			if _, err := pool.Exec(ctx, `
				DELETE FROM eval_case_results
				WHERE run_id IN (
					SELECT id FROM runs WHERE project_id = $1
				)
			`, projectID); err != nil {
				t.Errorf("cleanup eval case results for project %s: %v", projectID, err)
			}
			if _, err := pool.Exec(ctx, "DELETE FROM projects WHERE id = $1", projectID); err != nil {
				t.Errorf("cleanup project %s: %v", projectID, err)
			}
		}
		for _, userID := range userIDs {
			if _, err := pool.Exec(ctx, "DELETE FROM users WHERE id = $1", userID); err != nil {
				t.Errorf("cleanup user %s: %v", userID, err)
			}
		}
	})
}
