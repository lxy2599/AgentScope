package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"AgentScope/internal/sampling"
	"AgentScope/internal/store"
	"github.com/google/uuid"
)

func TestSamplingRuleUpdateIsAtomicAndProjectScoped(t *testing.T) {
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
	foreignProjectID := uuid.NewString()
	registerAdapterFixtureCleanup(t, pool, []string{projectID, foreignProjectID}, nil)

	if _, err := pool.Exec(ctx, `
		INSERT INTO projects (id, name)
		VALUES ($1, 'sampling-update-owner'), ($2, 'sampling-update-foreign')
	`, projectID, foreignProjectID); err != nil {
		t.Fatalf("create projects: %v", err)
	}

	querier := NewSamplingQuerier(store.New(pool))
	svc := sampling.NewService(querier)
	created, err := svc.CreateRule(ctx, sampling.CreateRuleParams{
		ProjectID:  projectID,
		Name:       "before-update",
		SampleRate: 0.25,
	})
	if err != nil {
		t.Fatalf("CreateRule() error = %v", err)
	}
	if created.Filters != "{}" {
		t.Fatalf("created filters = %q, want {}", created.Filters)
	}

	name := "after-update"
	enabled := false
	sampleRate := float32(0.75)
	filters := `{"status":"failed"}`
	updated, err := svc.UpdateRule(ctx, projectID, created.ID, sampling.UpdateRuleParams{
		Name:       &name,
		Enabled:    &enabled,
		SampleRate: &sampleRate,
		Filters:    &filters,
	})
	if err != nil {
		t.Fatalf("UpdateRule() error = %v", err)
	}
	if updated.Name != name || updated.Enabled || updated.SampleRate != sampleRate {
		t.Fatalf("UpdateRule() = %#v, want all fields persisted", updated)
	}
	var persistedFilters map[string]any
	if err := json.Unmarshal([]byte(updated.Filters), &persistedFilters); err != nil {
		t.Fatalf("decode persisted filters: %v", err)
	}
	if persistedFilters["status"] != "failed" {
		t.Fatalf("persisted filters = %#v, want failed status", persistedFilters)
	}

	enabled = true
	partial, err := svc.UpdateRule(ctx, projectID, created.ID, sampling.UpdateRuleParams{Enabled: &enabled})
	if err != nil {
		t.Fatalf("partial UpdateRule() error = %v", err)
	}
	if !partial.Enabled || partial.Name != name || partial.SampleRate != sampleRate || partial.Filters != updated.Filters {
		t.Fatalf("partial UpdateRule() changed omitted fields: %#v", partial)
	}

	invalidFilters := `[]`
	enabled = false
	_, err = querier.UpdateSamplingRuleByProject(ctx, projectID, created.ID, sampling.UpdateRuleParams{
		Enabled: &enabled,
		Filters: &invalidFilters,
	})
	if !errors.Is(err, sampling.ErrInvalidFilters) {
		t.Fatalf("invalid UpdateRule() error = %v, want invalid filters", err)
	}

	foreignName := "foreign-mutation"
	_, err = svc.UpdateRule(ctx, foreignProjectID, created.ID, sampling.UpdateRuleParams{Name: &foreignName})
	if !errors.Is(err, sampling.ErrRuleNotFound) {
		t.Fatalf("foreign UpdateRule() error = %v, want not found", err)
	}

	got, err := querier.GetSamplingRuleByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetSamplingRuleByID() error = %v", err)
	}
	if !got.Enabled || got.Name != name || got.SampleRate != sampleRate || got.Filters != updated.Filters {
		t.Fatalf("rejected update mutated persisted rule: %#v", got)
	}
}
