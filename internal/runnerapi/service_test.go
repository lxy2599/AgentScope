package runnerapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// fakeQuerier implements Querier for testing.
type fakeQuerier struct {
	jobs        map[string]EvalJob
	resultCount int
	evaluatorID string
	experiments map[string]struct {
		status      string
		passedCases int
		failedCases int
	}
	enrichmentErrCase string // when set, enrichment queries return this error
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{
		jobs: make(map[string]EvalJob),
		experiments: make(map[string]struct {
			status      string
			passedCases int
			failedCases int
		}),
	}
}

func (f *fakeQuerier) addJob(job EvalJob) {
	f.jobs[job.ID] = job
}

func (f *fakeQuerier) ClaimNextJob(ctx context.Context, projectID, runnerName string, leaseExpiresAt time.Time) (EvalJob, error) {
	for _, job := range f.jobs {
		if job.Status == "queued" {
			job.Status = "running"
			job.RunnerName = runnerName
			job.LeaseExpiresAt = &leaseExpiresAt
			job.Attempts++
			f.jobs[job.ID] = job
			return job, nil
		}
		if job.Status == "running" && job.LeaseExpiresAt != nil && job.LeaseExpiresAt.Before(time.Now()) {
			job.RunnerName = runnerName
			job.LeaseExpiresAt = &leaseExpiresAt
			job.Attempts++
			f.jobs[job.ID] = job
			return job, nil
		}
	}
	return EvalJob{}, errors.New("no claimable jobs")
}

func (f *fakeQuerier) GetJobByID(ctx context.Context, jobID string) (EvalJob, error) {
	job, ok := f.jobs[jobID]
	if !ok {
		return EvalJob{}, errors.New("job not found")
	}
	return job, nil
}

func (f *fakeQuerier) UpdateJobStatus(ctx context.Context, jobID, status, errorMsg string) error {
	job, ok := f.jobs[jobID]
	if !ok {
		return errors.New("job not found")
	}
	job.Status = status
	job.Error = errorMsg
	f.jobs[jobID] = job
	return nil
}

func (f *fakeQuerier) HeartbeatJobByProject(ctx context.Context, jobID, runnerName string, leaseExpiresAt time.Time, projectID string) error {
	job, ok := f.jobs[jobID]
	if !ok {
		return errors.New("job not found")
	}
	if job.RunnerName != runnerName {
		return errors.New("wrong runner")
	}
	if job.Status != "running" {
		return errors.New("job not running")
	}
	job.LeaseExpiresAt = &leaseExpiresAt
	f.jobs[jobID] = job
	return nil
}

func (f *fakeQuerier) UpdateJobStatusByProject(ctx context.Context, jobID, status, errorMsg, projectID, runnerName string) error {
	job, ok := f.jobs[jobID]
	if !ok {
		return errors.New("job not found")
	}
	if job.RunnerName != "" && job.RunnerName != runnerName {
		return errors.New("runner mismatch")
	}
	if job.Status != "running" {
		return errors.New("job not in running state")
	}
	job.Status = status
	job.Error = errorMsg
	f.jobs[jobID] = job
	return nil
}

func (f *fakeQuerier) SubmitResultByProject(ctx context.Context, projectID, runnerName, jobID string, req SubmitResultRequest) error {
	job, ok := f.jobs[jobID]
	if !ok {
		return errors.New("job not found or project mismatch")
	}
	// Simulate project boundary check by requiring the experiment entry to exist
	if _, hasExp := f.experiments[job.ExperimentID]; !hasExp && projectID != "" {
		return errors.New("job not found or project mismatch")
	}
	if job.RunnerName != runnerName {
		return errors.New("job not claimed by this runner")
	}
	if job.Status != "running" {
		return errors.New("job is not in running state")
	}

	// Track that a result was inserted
	f.resultCount++

	// Update job status
	job.Status = "succeeded"
	f.jobs[jobID] = job

	// Update experiment counts
	exp := f.experiments[job.ExperimentID]
	counts := map[string]int{}
	for _, j := range f.jobs {
		if j.ExperimentID == job.ExperimentID {
			counts[j.Status]++
		}
	}
	queued := counts["queued"]
	running := counts["running"]
	if queued == 0 && running == 0 {
		exp.status = "completed"
	} else {
		exp.status = "running"
	}
	exp.passedCases = counts["succeeded"]
	exp.failedCases = counts["failed"]
	f.experiments[job.ExperimentID] = exp

	return nil
}

func (f *fakeQuerier) UpdateExperimentStatus(ctx context.Context, experimentID, status string, passedCases, failedCases int) error {
	exp := f.experiments[experimentID]
	exp.status = status
	exp.passedCases = passedCases
	exp.failedCases = failedCases
	f.experiments[experimentID] = exp
	return nil
}

func (f *fakeQuerier) GetJobStatusCounts(ctx context.Context, experimentID string) (map[string]int, error) {
	counts := make(map[string]int)
	for _, job := range f.jobs {
		if job.ExperimentID == experimentID {
			counts[job.Status]++
		}
	}
	return counts, nil
}

func (f *fakeQuerier) GetExperimentTargetVersion(ctx context.Context, experimentID string) (string, string, string, error) {
	if f.enrichmentErrCase == "experiment" {
		return "", "", "", errors.New("experiment not found")
	}
	return "support-agent", "1.0", f.evaluatorID, nil
}

func (f *fakeQuerier) GetDatasetCaseData(ctx context.Context, caseID string) (DatasetCaseData, error) {
	if f.enrichmentErrCase == "case" {
		return DatasetCaseData{}, errors.New("dataset case not found")
	}
	return DatasetCaseData{
		Input:    "test input",
		Expected: strPtr("expected output"),
	}, nil
}

func (f *fakeQuerier) GetEvaluatorConfig(ctx context.Context, evaluatorID string) (json.RawMessage, error) {
	if f.enrichmentErrCase == "eval_config" {
		return nil, errors.New("evaluator config not found")
	}
	return json.RawMessage(`{"type":"exact"}`), nil
}

func strPtr(s string) *string {
	return &s
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

func TestService_Claim(t *testing.T) {
	fq := newFakeQuerier()
	svc := NewService(fq)

	fq.addJob(EvalJob{
		ID:            "job-1",
		ExperimentID:  "exp-1",
		DatasetCaseID: "case-1",
		Status:        "queued",
	})

	t.Run("successfully claim queued job", func(t *testing.T) {
		resp, err := svc.Claim(context.Background(), "proj-1", "runner-1")
		if err != nil {
			t.Fatalf("Claim: %v", err)
		}
		if resp.Job.ID != "job-1" {
			t.Fatalf("expected job-1, got %s", resp.Job.ID)
		}
		if resp.Job.Status != "running" {
			t.Fatalf("expected status running, got %s", resp.Job.Status)
		}
		if resp.Job.RunnerName != "runner-1" {
			t.Fatalf("expected runner runner-1, got %s", resp.Job.RunnerName)
		}
		if resp.Job.Attempts != 1 {
			t.Fatalf("expected 1 attempt, got %d", resp.Job.Attempts)
		}
	})

	t.Run("cannot claim already running job", func(t *testing.T) {
		_, err := svc.Claim(context.Background(), "proj-1", "runner-2")
		if err == nil {
			t.Fatal("expected error claiming already claimed job")
		}
	})

	t.Run("missing projectID", func(t *testing.T) {
		_, err := svc.Claim(context.Background(), "", "runner-1")
		if err == nil {
			t.Fatal("expected error for missing projectID")
		}
	})

	t.Run("missing runnerName", func(t *testing.T) {
		_, err := svc.Claim(context.Background(), "proj-1", "")
		if err == nil {
			t.Fatal("expected error for missing runnerName")
		}
	})
}

func TestService_Claim_ExpiredLease(t *testing.T) {
	fq := newFakeQuerier()
	svc := NewService(fq)

	expired := time.Now().Add(-10 * time.Minute)
	fq.addJob(EvalJob{
		ID:             "job-expired",
		ExperimentID:   "exp-2",
		DatasetCaseID:  "case-2",
		Status:         "running",
		RunnerName:     "old-runner",
		LeaseExpiresAt: &expired,
	})

	resp, err := svc.Claim(context.Background(), "proj-2", "new-runner")
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	if resp.Job.RunnerName != "new-runner" {
		t.Fatalf("expected new-runner, got %s", resp.Job.RunnerName)
	}
	if resp.Job.Attempts != 1 {
		t.Fatalf("expected 1 attempt (first from expired), got %d", resp.Job.Attempts)
	}
}

func TestService_Claim_EnrichmentErrors(t *testing.T) {
	t.Run("dataset case lookup failure returns error", func(t *testing.T) {
		errFq := newFakeQuerier()
		errFq.enrichmentErrCase = "case"
		errSvc := NewService(errFq)
		errFq.addJob(EvalJob{
			ID:            "job-err",
			ExperimentID:  "exp-1",
			DatasetCaseID: "case-missing",
			Status:        "queued",
		})
		_, err := errSvc.Claim(context.Background(), "proj-1", "runner-1")
		if err == nil {
			t.Fatal("expected error when dataset case lookup fails")
		}
	})

	t.Run("experiment lookup failure returns error", func(t *testing.T) {
		errFq := newFakeQuerier()
		errFq.enrichmentErrCase = "experiment"
		errSvc := NewService(errFq)
		errFq.addJob(EvalJob{
			ID:            "job-err2",
			ExperimentID:  "exp-missing",
			DatasetCaseID: "case-1",
			Status:        "queued",
		})
		_, err := errSvc.Claim(context.Background(), "proj-1", "runner-1")
		if err == nil {
			t.Fatal("expected error when experiment lookup fails")
		}
	})

	t.Run("enrichment success includes all fields", func(t *testing.T) {
		fq := newFakeQuerier()
		fq.evaluatorID = "evaluator-1"
		svc := NewService(fq)
		fq.addJob(EvalJob{
			ID:            "job-enrich",
			ExperimentID:  "exp-1",
			DatasetCaseID: "case-1",
			Status:        "queued",
		})
		resp, err := svc.Claim(context.Background(), "proj-1", "runner-1")
		if err != nil {
			t.Fatalf("expected claim success, got error: %v", err)
		}
		if resp.CaseInput != "test input" {
			t.Fatalf("expected case_input 'test input', got %q", resp.CaseInput)
		}
		if resp.CaseExpected == nil || *resp.CaseExpected != "expected output" {
			t.Fatalf("expected case_expected 'expected output', got %v", resp.CaseExpected)
		}
		if resp.TargetName != "support-agent" {
			t.Fatalf("expected target_name 'support-agent', got %q", resp.TargetName)
		}
		if resp.TargetVersion != "1.0" {
			t.Fatalf("expected target_version '1.0', got %q", resp.TargetVersion)
		}
		var evalConfig map[string]any
		if err := json.Unmarshal(resp.EvalConfig, &evalConfig); err != nil {
			t.Fatalf("unmarshal eval config: %v", err)
		}
		if evalConfig["evaluator_id"] != "evaluator-1" {
			t.Fatalf("expected evaluator_id evaluator-1, got %v", evalConfig["evaluator_id"])
		}
	})
}

func TestService_Heartbeat(t *testing.T) {
	fq := newFakeQuerier()
	svc := NewService(fq)

	fq.addJob(EvalJob{
		ID:           "job-1",
		ExperimentID: "exp-1",
		Status:       "running",
		RunnerName:   "runner-1",
	})

	t.Run("heartbeat success", func(t *testing.T) {
		err := svc.Heartbeat(context.Background(), "job-1", "proj-1", "runner-1")
		if err != nil {
			t.Fatalf("Heartbeat: %v", err)
		}
	})

	t.Run("wrong runner cannot heartbeat", func(t *testing.T) {
		err := svc.Heartbeat(context.Background(), "job-1", "proj-1", "runner-2")
		if err == nil {
			t.Fatal("expected error for wrong runner")
		}
	})

	t.Run("missing jobID", func(t *testing.T) {
		err := svc.Heartbeat(context.Background(), "", "proj-1", "runner-1")
		if err == nil {
			t.Fatal("expected error for missing jobID")
		}
	})

	t.Run("missing projectID", func(t *testing.T) {
		err := svc.Heartbeat(context.Background(), "job-1", "", "runner-1")
		if err == nil {
			t.Fatal("expected error for missing projectID")
		}
	})

	t.Run("missing runnerName", func(t *testing.T) {
		err := svc.Heartbeat(context.Background(), "job-1", "proj-1", "")
		if err == nil {
			t.Fatal("expected error for missing runnerName")
		}
	})
}

func TestService_SubmitResult(t *testing.T) {
	fq := newFakeQuerier()
	svc := NewService(fq)

	// Create experiment entry for project boundary tracking
	fq.experiments["exp-1"] = struct {
		status      string
		passedCases int
		failedCases int
	}{status: "running"}

	fq.addJob(EvalJob{
		ID:           "job-1",
		ExperimentID: "exp-1",
		Status:       "running",
		RunnerName:   "runner-1",
	})

	t.Run("submit result success", func(t *testing.T) {
		prevCount := fq.resultCount
		err := svc.SubmitResult(context.Background(), "job-1", "proj-1", "runner-1", SubmitResultRequest{
			RunID:        "run-1",
			Score:        0.95,
			Passed:       true,
			Reason:       "correct answer",
			ActualOutput: "hello world",
			LatencyMs:    1500,
		})
		if err != nil {
			t.Fatalf("SubmitResult: %v", err)
		}

		// Verify result was tracked
		if fq.resultCount != prevCount+1 {
			t.Fatalf("expected result count %d, got %d", prevCount+1, fq.resultCount)
		}

		job, _ := fq.GetJobByID(context.Background(), "job-1")
		if job.Status != "succeeded" {
			t.Fatalf("expected status succeeded, got %s", job.Status)
		}

		// Verify experiment was updated
		exp := fq.experiments["exp-1"]
		if exp.status != "completed" {
			t.Fatalf("unexpected experiment status: got %s", exp.status)
		}
		if exp.passedCases != 1 {
			t.Fatalf("expected 1 passed case, got %d", exp.passedCases)
		}
	})

	t.Run("wrong runner cannot submit", func(t *testing.T) {
		fq.experiments["exp-2"] = struct {
			status      string
			passedCases int
			failedCases int
		}{status: "running"}
		fq.addJob(EvalJob{
			ID:           "job-2",
			ExperimentID: "exp-2",
			Status:       "running",
			RunnerName:   "runner-1",
		})
		prevCount := fq.resultCount
		err := svc.SubmitResult(context.Background(), "job-2", "proj-2", "runner-2", SubmitResultRequest{
			RunID:  "run-2",
			Score:  0.5,
			Passed: true,
		})
		if err == nil {
			t.Fatal("expected error for wrong runner")
		}
		// Verify no result was inserted
		if fq.resultCount != prevCount {
			t.Fatalf("expected no result inserted on error, but count changed from %d to %d", prevCount, fq.resultCount)
		}
	})

	t.Run("cannot submit to non-running job", func(t *testing.T) {
		fq.experiments["exp-3"] = struct {
			status      string
			passedCases int
			failedCases int
		}{status: "succeeded"}
		fq.addJob(EvalJob{
			ID:           "job-3",
			ExperimentID: "exp-3",
			Status:       "succeeded",
			RunnerName:   "runner-1",
		})
		prevCount := fq.resultCount
		err := svc.SubmitResult(context.Background(), "job-3", "proj-3", "runner-1", SubmitResultRequest{
			RunID:  "run-3",
			Score:  1.0,
			Passed: true,
		})
		if err == nil {
			t.Fatal("expected error for non-running job")
		}
		// Verify no result was inserted
		if fq.resultCount != prevCount {
			t.Fatalf("expected no result inserted on error, but count changed from %d to %d", prevCount, fq.resultCount)
		}
	})

	t.Run("missing projectID", func(t *testing.T) {
		err := svc.SubmitResult(context.Background(), "job-1", "", "runner-1", SubmitResultRequest{
			RunID:  "run-x",
			Score:  1.0,
			Passed: true,
		})
		if err == nil {
			t.Fatal("expected error for missing projectID")
		}
	})
	t.Run("project mismatch returns error and no result inserted", func(t *testing.T) {
		// Add a job whose experiment has no entry in the experiment map,
		// simulating a project boundary failure
		fq.addJob(EvalJob{
			ID:           "job-foreign",
			ExperimentID: "exp-foreign",
			Status:       "running",
			RunnerName:   "runner-1",
		})
		prevCount := fq.resultCount
		err := svc.SubmitResult(context.Background(), "job-foreign", "proj-foreign", "runner-1", SubmitResultRequest{
			RunID:  "run-foreign",
			Score:  0.8,
			Passed: true,
		})
		if err == nil {
			t.Fatal("expected error for project mismatch")
		}
		// Verify no result was inserted
		if fq.resultCount != prevCount {
			t.Fatalf("expected no result inserted on project mismatch, but count changed from %d to %d", prevCount, fq.resultCount)
		}
		// Verify job status unchanged
		job, _ := fq.GetJobByID(context.Background(), "job-foreign")
		if job.Status != "running" {
			t.Fatalf("expected job status running after failed submit, got %s", job.Status)
		}
	})
}

func TestService_FailJob(t *testing.T) {
	fq := newFakeQuerier()
	svc := NewService(fq)

	fq.addJob(EvalJob{
		ID:           "job-1",
		ExperimentID: "exp-1",
		Status:       "running",
		RunnerName:   "runner-1",
	})

	t.Run("fail job success", func(t *testing.T) {
		err := svc.FailJob(context.Background(), "job-1", "proj-1", "runner-1", "agent crashed")
		if err != nil {
			t.Fatalf("FailJob: %v", err)
		}

		job, _ := fq.GetJobByID(context.Background(), "job-1")
		if job.Status != "failed" {
			t.Fatalf("expected status failed, got %s", job.Status)
		}
		if job.Error != "agent crashed" {
			t.Fatalf("expected error 'agent crashed', got '%s'", job.Error)
		}
	})

	t.Run("wrong runner cannot fail", func(t *testing.T) {
		fq.addJob(EvalJob{
			ID:           "job-2",
			ExperimentID: "exp-2",
			Status:       "running",
			RunnerName:   "runner-1",
		})
		err := svc.FailJob(context.Background(), "job-2", "proj-2", "runner-2", "error")
		if err == nil {
			t.Fatal("expected error for wrong runner")
		}
	})

	t.Run("missing error message", func(t *testing.T) {
		err := svc.FailJob(context.Background(), "job-1", "proj-1", "runner-1", "")
		if err == nil {
			t.Fatal("expected error for empty error message")
		}
	})

	t.Run("missing projectID", func(t *testing.T) {
		err := svc.FailJob(context.Background(), "job-1", "", "runner-1", "error")
		if err == nil {
			t.Fatal("expected error for missing projectID")
		}
	})

	t.Run("missing jobID", func(t *testing.T) {
		err := svc.FailJob(context.Background(), "", "proj-1", "runner-1", "error")
		if err == nil {
			t.Fatal("expected error for missing jobID")
		}
	})
}
