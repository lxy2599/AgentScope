package adapter

import (
	"context"
	"math"
	"os"
	"testing"

	"AgentScope/internal/evals"
	"AgentScope/internal/store"
	"github.com/google/uuid"
)

func TestEvalQuerierRejectsNonFiniteScoreBeforeDatabase(t *testing.T) {
	querier := &EvalQuerier{}
	for _, score := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		_, err := querier.CreateEvalScore(context.Background(), evals.EvalScore{Score: score})
		if err == nil {
			t.Fatalf("CreateEvalScore() accepted non-finite score %v", score)
		}
	}
}

func TestCreateEvalScoresIsAtomic(t *testing.T) {
	dbURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}
	requireAdapterTestDatabase(t, dbURL)

	ctx := context.Background()
	pool, err := store.Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	projectID := uuid.NewString()
	runID := uuid.NewString()
	registerAdapterFixtureCleanup(t, pool, []string{projectID}, nil)

	if _, err := pool.Exec(ctx, `
		INSERT INTO projects (id, name)
		VALUES ($1, $2)
	`, projectID, "eval-batch-"+uuid.NewString()); err != nil {
		t.Fatalf("create project fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO runs (id, project_id, source, status, agent_name)
		VALUES ($1, $2, 'eval', 'completed', 'eval-batch-test')
	`, runID, projectID); err != nil {
		t.Fatalf("create run fixture: %v", err)
	}

	querier := NewEvalQuerier(store.New(pool)).WithPool(pool)

	t.Run("later failure rolls back earlier insert", func(t *testing.T) {
		metricPrefix := "eval-batch-rollback-" + uuid.NewString()
		scores := []evals.EvalScore{
			validAdapterEvalScore(projectID, runID, metricPrefix+"-first"),
			validAdapterEvalScore(projectID, uuid.NewString(), metricPrefix+"-second"),
		}

		if _, err := querier.CreateEvalScores(ctx, scores); err == nil {
			t.Fatal("CreateEvalScores() succeeded with a nonexistent second run")
		}

		var count int
		if err := pool.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM eval_scores
			WHERE project_id = $1 AND metric_name LIKE $2
		`, projectID, metricPrefix+"%").Scan(&count); err != nil {
			t.Fatalf("count rollback rows: %v", err)
		}
		if count != 0 {
			t.Fatalf("persisted rows after rollback = %d, want 0", count)
		}
	})

	t.Run("valid batch commits all inserts", func(t *testing.T) {
		metricPrefix := "eval-batch-commit-" + uuid.NewString()
		scores := []evals.EvalScore{
			validAdapterEvalScore(projectID, runID, metricPrefix+"-first"),
			validAdapterEvalScore(projectID, runID, metricPrefix+"-second"),
		}

		results, err := querier.CreateEvalScores(ctx, scores)
		if err != nil {
			t.Fatalf("CreateEvalScores() error = %v", err)
		}
		if len(results) != 2 {
			t.Fatalf("CreateEvalScores() results = %d, want 2", len(results))
		}

		var count int
		if err := pool.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM eval_scores
			WHERE project_id = $1 AND metric_name LIKE $2
		`, projectID, metricPrefix+"%").Scan(&count); err != nil {
			t.Fatalf("count committed rows: %v", err)
		}
		if count != 2 {
			t.Fatalf("persisted rows after commit = %d, want 2", count)
		}
	})
}

func validAdapterEvalScore(projectID, runID, metricName string) evals.EvalScore {
	return evals.EvalScore{
		ProjectID:  projectID,
		RunID:      runID,
		MetricName: metricName,
		MetricKind: "final_answer",
		Score:      0.75,
		Passed:     true,
		Details:    "{}",
	}
}
