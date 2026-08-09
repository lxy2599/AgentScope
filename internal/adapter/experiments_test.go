package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestMergeBenchmarkExecutionConfigPreservesDefaults(t *testing.T) {
	merged, err := mergeBenchmarkExecutionConfig(
		[]byte(`{"dataset":"terminal-bench@2.0","jobs_dir":"./jobs","concurrency":1}`),
		`{"agent_setup_timeout_multiplier":3,"concurrency":2}`,
	)
	if err != nil {
		t.Fatalf("mergeBenchmarkExecutionConfig() error = %v", err)
	}
	var values map[string]any
	if err := json.Unmarshal(merged, &values); err != nil {
		t.Fatalf("decode merged config: %v", err)
	}
	if values["dataset"] != "terminal-bench@2.0" || values["jobs_dir"] != "./jobs" || values["concurrency"] != float64(2) || values["agent_setup_timeout_multiplier"] != float64(3) {
		t.Fatalf("merged config = %#v", values)
	}
}

type fakeEvaluatorProjectQuerier struct {
	evaluator store.Evaluator
	err       error
	calls     int
}

type fakeDatasetProjectQuerier struct {
	dataset store.Dataset
	err     error
	calls   int
}

func (f *fakeDatasetProjectQuerier) GetDatasetByID(context.Context, pgtype.UUID) (store.Dataset, error) {
	f.calls++
	return f.dataset, f.err
}

func (f *fakeEvaluatorProjectQuerier) GetEvaluatorByID(context.Context, pgtype.UUID) (store.Evaluator, error) {
	f.calls++
	return f.evaluator, f.err
}

func TestEvaluatorIDForProject(t *testing.T) {
	const (
		projectID      = "11111111-1111-1111-1111-111111111111"
		otherProjectID = "22222222-2222-2222-2222-222222222222"
		evaluatorID    = "33333333-3333-3333-3333-333333333333"
	)

	t.Run("empty evaluator is allowed", func(t *testing.T) {
		q := &fakeEvaluatorProjectQuerier{}
		got, err := evaluatorIDForProject(context.Background(), q, "", projectID)
		if err != nil {
			t.Fatalf("evaluatorIDForProject() error = %v", err)
		}
		if got.Valid {
			t.Fatal("empty evaluator should return an invalid nullable UUID")
		}
		if q.calls != 0 {
			t.Fatalf("GetEvaluatorByID called %d times, want 0", q.calls)
		}
	})

	t.Run("matching project is accepted", func(t *testing.T) {
		q := &fakeEvaluatorProjectQuerier{
			evaluator: store.Evaluator{
				ID:        parsePGUUID(evaluatorID),
				ProjectID: parsePGUUID(projectID),
			},
		}
		got, err := evaluatorIDForProject(context.Background(), q, evaluatorID, projectID)
		if err != nil {
			t.Fatalf("evaluatorIDForProject() error = %v", err)
		}
		if !got.Valid || got.Bytes != parsePGUUID(evaluatorID).Bytes {
			t.Fatalf("evaluatorIDForProject() = %#v, want evaluator %s", got, evaluatorID)
		}
	})

	t.Run("foreign project is rejected", func(t *testing.T) {
		q := &fakeEvaluatorProjectQuerier{
			evaluator: store.Evaluator{
				ID:        parsePGUUID(evaluatorID),
				ProjectID: parsePGUUID(otherProjectID),
			},
		}
		if _, err := evaluatorIDForProject(context.Background(), q, evaluatorID, projectID); err == nil {
			t.Fatal("foreign-project evaluator was accepted")
		}
	})

	t.Run("invalid evaluator id is rejected before querying", func(t *testing.T) {
		q := &fakeEvaluatorProjectQuerier{}
		if _, err := evaluatorIDForProject(context.Background(), q, "not-a-uuid", projectID); err == nil {
			t.Fatal("invalid evaluator ID was accepted")
		}
		if q.calls != 0 {
			t.Fatalf("GetEvaluatorByID called %d times, want 0", q.calls)
		}
	})

	t.Run("lookup failure does not expose evaluator", func(t *testing.T) {
		q := &fakeEvaluatorProjectQuerier{err: errors.New("database detail")}
		if _, err := evaluatorIDForProject(context.Background(), q, evaluatorID, projectID); err == nil {
			t.Fatal("lookup failure was accepted")
		}
	})
}

func TestValidateDatasetProject(t *testing.T) {
	const (
		projectID      = "11111111-1111-1111-1111-111111111111"
		otherProjectID = "22222222-2222-2222-2222-222222222222"
		datasetID      = "44444444-4444-4444-4444-444444444444"
	)

	t.Run("matching project is accepted", func(t *testing.T) {
		q := &fakeDatasetProjectQuerier{
			dataset: store.Dataset{
				ID:        parsePGUUID(datasetID),
				ProjectID: parsePGUUID(projectID),
			},
		}
		if err := validateDatasetProject(context.Background(), q, datasetID, projectID); err != nil {
			t.Fatalf("validateDatasetProject() error = %v", err)
		}
	})

	t.Run("foreign project is rejected", func(t *testing.T) {
		q := &fakeDatasetProjectQuerier{
			dataset: store.Dataset{
				ID:        parsePGUUID(datasetID),
				ProjectID: parsePGUUID(otherProjectID),
			},
		}
		if err := validateDatasetProject(context.Background(), q, datasetID, projectID); err == nil {
			t.Fatal("foreign-project dataset was accepted")
		}
	})

	t.Run("invalid dataset id is rejected before querying", func(t *testing.T) {
		q := &fakeDatasetProjectQuerier{}
		if err := validateDatasetProject(context.Background(), q, "not-a-uuid", projectID); err == nil {
			t.Fatal("invalid dataset ID was accepted")
		}
		if q.calls != 0 {
			t.Fatalf("GetDatasetByID called %d times, want 0", q.calls)
		}
	})
}
