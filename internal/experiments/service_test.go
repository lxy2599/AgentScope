package experiments

import (
	"context"
	"errors"
	"testing"
)

// fakeQuerier implements Querier for testing.
type fakeQuerier struct {
	experiments map[string]Experiment
	jobs        map[string][]EvalJob
	results     map[string][]EvalCaseResult
	counter     int
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{
		experiments: make(map[string]Experiment),
		jobs:        make(map[string][]EvalJob),
		results:     make(map[string][]EvalCaseResult),
	}
}

func (f *fakeQuerier) CreateExperiment(ctx context.Context, projectID, datasetID, evaluatorID, targetName, targetVersion string, totalCases int) (Experiment, error) {
	f.counter++
	id := "exp-" + itoa(f.counter)
	e := Experiment{
		ID:          id,
		ProjectID:   projectID,
		DatasetID:   datasetID,
		EvaluatorID: evaluatorID,
		TargetName:  targetName,
		TargetVersion: targetVersion,
		Status:      "pending",
		TotalCases:  totalCases,
	}
	f.experiments[id] = e
	return e, nil
}

func (f *fakeQuerier) GetExperiment(ctx context.Context, experimentID string) (Experiment, error) {
	e, ok := f.experiments[experimentID]
	if !ok {
		return Experiment{}, errors.New("experiment not found")
	}
	return e, nil
}

func (f *fakeQuerier) CreateEvalJob(ctx context.Context, experimentID, datasetCaseID, status string) (EvalJob, error) {
	f.counter++
	id := "job-" + itoa(f.counter)
	j := EvalJob{
		ID:            id,
		ExperimentID:  experimentID,
		DatasetCaseID: datasetCaseID,
		Status:        status,
		Attempts:      0,
	}
	f.jobs[experimentID] = append(f.jobs[experimentID], j)
	return j, nil
}

func (f *fakeQuerier) ListEvalJobs(ctx context.Context, experimentID string) ([]EvalJob, error) {
	return f.jobs[experimentID], nil
}

func (f *fakeQuerier) GetJobStatusCounts(ctx context.Context, experimentID string) (map[string]int, error) {
	jobs := f.jobs[experimentID]
	counts := make(map[string]int)
	for _, j := range jobs {
		counts[j.Status]++
	}
	return counts, nil
}

func (f *fakeQuerier) UpdateExperimentCounts(ctx context.Context, experimentID, status string, passedCases, failedCases int) error {
	e, ok := f.experiments[experimentID]
	if !ok {
		return errors.New("experiment not found")
	}
	e.Status = status
	e.PassedCases = passedCases
	e.FailedCases = failedCases
	f.experiments[experimentID] = e
	return nil
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}

func TestService_CreateExperiment(t *testing.T) {
	fq := newFakeQuerier()
	svc := NewService(fq)

	t.Run("success with cases", func(t *testing.T) {
		caseIDs := []string{"case-1", "case-2", "case-3"}
		exp, err := svc.CreateExperiment(
			context.Background(),
			"proj-1", "ds-1", "",
			"test-agent", "v1.0", caseIDs,
		)
		if err != nil {
			t.Fatalf("CreateExperiment: %v", err)
		}
		if exp.ID == "" {
			t.Fatal("expected non-empty ID")
		}
		if exp.ProjectID != "proj-1" {
			t.Fatalf("expected proj-1, got %s", exp.ProjectID)
		}
		if exp.Status != "pending" {
			t.Fatalf("expected status pending, got %s", exp.Status)
		}
		if exp.TotalCases != 3 {
			t.Fatalf("expected 3 total cases, got %d", exp.TotalCases)
		}

		// Verify jobs were created
		jobs, _ := fq.ListEvalJobs(context.Background(), exp.ID)
		if len(jobs) != 3 {
			t.Fatalf("expected 3 eval jobs, got %d", len(jobs))
		}
		for _, j := range jobs {
			if j.Status != "queued" {
				t.Fatalf("expected job status queued, got %s", j.Status)
			}
		}
	})

	t.Run("missing projectID", func(t *testing.T) {
		_, err := svc.CreateExperiment(context.Background(), "", "ds-1", "", "agent", "", []string{"c1"})
		if err == nil {
			t.Fatal("expected error for missing projectID")
		}
	})

	t.Run("missing datasetID", func(t *testing.T) {
		_, err := svc.CreateExperiment(context.Background(), "proj-1", "", "", "agent", "", []string{"c1"})
		if err == nil {
			t.Fatal("expected error for missing datasetID")
		}
	})

	t.Run("missing target name", func(t *testing.T) {
		_, err := svc.CreateExperiment(context.Background(), "proj-1", "ds-1", "", "", "", []string{"c1"})
		if err == nil {
			t.Fatal("expected error for missing target name")
		}
	})

	t.Run("empty case list", func(t *testing.T) {
		_, err := svc.CreateExperiment(context.Background(), "proj-1", "ds-1", "", "agent", "", []string{})
		if err == nil {
			t.Fatal("expected error for empty case list")
		}
	})
}

func TestService_GetExperiment(t *testing.T) {
	fq := newFakeQuerier()
	svc := NewService(fq)

	exp, _ := svc.CreateExperiment(
		context.Background(),
		"proj-1", "ds-1", "",
		"agent", "v1", []string{"c1", "c2"},
	)

	t.Run("returns experiment with status counts", func(t *testing.T) {
		detail, err := svc.GetExperiment(context.Background(), exp.ID)
		if err != nil {
			t.Fatalf("GetExperiment: %v", err)
		}
		if detail.ID != exp.ID {
			t.Fatalf("expected ID %s, got %s", exp.ID, detail.ID)
		}
		if detail.TotalCases != 2 {
			t.Fatalf("expected 2 total cases, got %d", detail.TotalCases)
		}
		// All jobs should be queued
		if detail.StatusCounts["queued"] != 2 {
			t.Fatalf("expected 2 queued, got %d", detail.StatusCounts["queued"])
		}
		if detail.StatusCounts["running"] != 0 {
			t.Fatalf("expected 0 running, got %d", detail.StatusCounts["running"])
		}
		if detail.StatusCounts["succeeded"] != 0 {
			t.Fatalf("expected 0 succeeded, got %d", detail.StatusCounts["succeeded"])
		}
		if detail.StatusCounts["failed"] != 0 {
			t.Fatalf("expected 0 failed, got %d", detail.StatusCounts["failed"])
		}
	})

	t.Run("missing experimentID", func(t *testing.T) {
		_, err := svc.GetExperiment(context.Background(), "")
		if err == nil {
			t.Fatal("expected error for missing experimentID")
		}
	})
}

func TestService_CreateExperiment_EmptyDataset(t *testing.T) {
	fq := newFakeQuerier()
	svc := NewService(fq)

	// Empty case list should fail
	_, err := svc.CreateExperiment(
		context.Background(),
		"proj-1", "ds-1", "",
		"agent", "", []string{},
	)
	if err == nil {
		t.Fatal("expected error for empty case list")
	}
}

func (f *fakeQuerier) CreateExperimentWithJobs(ctx context.Context, projectID, datasetID, evaluatorID, targetName, targetVersion string, caseIDs []string) (Experiment, error) {
	exp, err := f.CreateExperiment(ctx, projectID, datasetID, evaluatorID, targetName, targetVersion, len(caseIDs))
	if err != nil {
		return Experiment{}, err
	}
	for _, caseID := range caseIDs {
		if _, err := f.CreateEvalJob(ctx, exp.ID, caseID, "queued"); err != nil {
			return Experiment{}, err
		}
	}
	return exp, nil
}

func (f *fakeQuerier) ListExperiments(ctx context.Context, projectID string) ([]Experiment, error) {
	var result []Experiment
	for _, e := range f.experiments {
		if e.ProjectID == projectID {
			result = append(result, e)
		}
	}
	return result, nil
}

func (f *fakeQuerier) ListEvalResults(ctx context.Context, experimentID string) ([]EvalCaseResult, error) {
	return f.results[experimentID], nil
}

func (f *fakeQuerier) UpdateExperimentQualityCounts(ctx context.Context, experimentID string, qualityPassCases, qualityFailedCases int, aggregateScore float64) error {
	return nil
}
