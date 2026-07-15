package runnerapi

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeQuerier implements Querier for testing.
type fakeQuerier struct {
	jobs        map[string]EvalJob
	experiments map[string]struct {
		status       string
		passedCases  int
		failedCases  int
	}
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{
		jobs:        make(map[string]EvalJob),
		experiments: make(map[string]struct {
			status       string
			passedCases  int
			failedCases  int
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

func (f *fakeQuerier) UpdateJobStatusByProject(ctx context.Context, jobID, status, errorMsg, projectID string) error {
	job, ok := f.jobs[jobID]
	if !ok {
		return errors.New("job not found")
	}
	job.Status = status
	job.Error = errorMsg
	f.jobs[jobID] = job
	return nil
}

func (f *fakeQuerier) CreateResult(ctx context.Context, jobID, runID string, score float64, passed bool, reason string, details []byte, actualOutput string, latencyMs int64, cost float64) error {
	return nil
}

func (f *fakeQuerier) GetJobExperimentID(ctx context.Context, jobID string) (string, error) {
	job, ok := f.jobs[jobID]
	if !ok {
		return "", errors.New("job not found")
	}
	return job.ExperimentID, nil
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

	fq.addJob(EvalJob{
		ID:           "job-1",
		ExperimentID: "exp-1",
		Status:       "running",
		RunnerName:   "runner-1",
	})

	t.Run("submit result success", func(t *testing.T) {
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

		job, _ := fq.GetJobByID(context.Background(), "job-1")
		if job.Status != "succeeded" {
			t.Fatalf("expected status succeeded, got %s", job.Status)
		}
	})

	t.Run("wrong runner cannot submit", func(t *testing.T) {
		fq.addJob(EvalJob{
			ID:           "job-2",
			ExperimentID: "exp-2",
			Status:       "running",
			RunnerName:   "runner-1",
		})
		err := svc.SubmitResult(context.Background(), "job-2", "proj-1", "runner-2", SubmitResultRequest{
			RunID:  "run-2",
			Score:  0.5,
			Passed: true,
		})
		if err == nil {
			t.Fatal("expected error for wrong runner")
		}
	})

	t.Run("cannot submit to non-running job", func(t *testing.T) {
		fq.addJob(EvalJob{
			ID:           "job-3",
			ExperimentID: "exp-3",
			Status:       "succeeded",
			RunnerName:   "runner-1",
		})
		err := svc.SubmitResult(context.Background(), "job-3", "proj-1", "runner-1", SubmitResultRequest{
			RunID:  "run-3",
			Score:  1.0,
			Passed: true,
		})
		if err == nil {
			t.Fatal("expected error for non-running job")
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
