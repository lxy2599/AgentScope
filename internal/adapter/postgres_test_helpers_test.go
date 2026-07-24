package adapter

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

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
