package adapter

import (
	"context"
	"errors"
	"os"
	"testing"

	"AgentScope/internal/runs"
	"AgentScope/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestNormalizeRunNotFound(t *testing.T) {
	if err := normalizeRunNotFound(pgx.ErrNoRows); !errors.Is(err, runs.ErrRunNotFound) {
		t.Fatalf("normalizeRunNotFound(pgx.ErrNoRows) = %v, want ErrRunNotFound", err)
	}
	if err := normalizeRunNotFound(errors.Join(errors.New("query failed"), pgx.ErrNoRows)); !errors.Is(err, runs.ErrRunNotFound) {
		t.Fatalf("normalizeRunNotFound(wrapped pgx.ErrNoRows) = %v, want ErrRunNotFound", err)
	}

	databaseErr := errors.New("database unavailable")
	if err := normalizeRunNotFound(databaseErr); !errors.Is(err, databaseErr) {
		t.Fatalf("normalizeRunNotFound(database error) = %v, want original error", err)
	}
}

func TestRunQuerierNormalizesMissingRows(t *testing.T) {
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

	querier := NewRunQuerier(store.New(pool))
	missingRunID := uuid.NewString()
	missingProjectID := uuid.NewString()

	if _, err := querier.GetRunByID(ctx, missingRunID); !errors.Is(err, runs.ErrRunNotFound) {
		t.Fatalf("GetRunByID() error = %v, want ErrRunNotFound", err)
	}
	if _, err := querier.GetRunByTraceID(ctx, "missing-"+uuid.NewString(), missingProjectID); !errors.Is(err, runs.ErrRunNotFound) {
		t.Fatalf("GetRunByTraceID() error = %v, want ErrRunNotFound", err)
	}
	if _, err := querier.FailRunnerRun(ctx, missingRunID, missingProjectID, "failed"); !errors.Is(err, runs.ErrRunNotFound) {
		t.Fatalf("FailRunnerRun() error = %v, want ErrRunNotFound", err)
	}
}
