package adapter

import (
	"context"
	"os"
	"testing"

	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestAgentSummaryRealAggregation tests the summary SQL against a real database.
// Run with: AGENTSCOPE_TEST_DATABASE_URL=postgres://agentscope:agentscope@localhost:5432/agentscope_test?sslmode=disable
func TestAgentSummaryRealAggregation(t *testing.T) {
	dbURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := store.Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("Open database: %v", err)
	}
	defer pool.Close()

	q := store.New(pool)
	adapterQ := NewAgentQuerier(q)

	// Clean up test data
	cleanup := func() {
		pool.Exec(ctx, "DELETE FROM run_state_events")
		pool.Exec(ctx, "DELETE FROM agent_heartbeats")
		pool.Exec(ctx, "DELETE FROM agent_capabilities")
		pool.Exec(ctx, "DELETE FROM agent_instances")
		pool.Exec(ctx, "DELETE FROM runs")
		pool.Exec(ctx, "DELETE FROM agents")
	}
	cleanup()
	defer cleanup()

	// Create test projects
	for _, pid := range []string{
		"00000000-0000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000002",
	} {
		_, err = pool.Exec(ctx, "INSERT INTO projects (id, name) VALUES ($1::uuid, 'test-project') ON CONFLICT DO NOTHING", pid)
		if err != nil {
			t.Fatalf("Create project %s: %v", pid, err)
		}
	}

	// Create two agents in two different projects
	agent1, err := adapterQ.CreateAgent(ctx, "00000000-0000-0000-0000-000000000001", "test-agent-a", "Agent A", nil)
	if err != nil {
		t.Fatalf("Create agent A: %v", err)
	}
	_, err = adapterQ.CreateAgent(ctx, "00000000-0000-0000-0000-000000000002", "test-agent-b", "Agent B", nil)
	if err != nil {
		t.Fatalf("Create agent B: %v", err)
	}

	// Create instances for agent1
	_, err = adapterQ.CreateAgentInstance(ctx, agent1.ID, agent1.ProjectID, "inst-1", "Instance 1", "online", "host-1", 1001, nil)
	if err != nil {
		t.Fatalf("Create instance: %v", err)
	}

	// For Agent A: create runs with various statuses and latencies
	// Run 1: completed with latency
	err = createTestRun(ctx, pool, agent1.ProjectID, agent1.Name, "completed", 500, nil)
	if err != nil {
		t.Fatalf("Create run 1: %v", err)
	}
	// Run 2: completed with latency
	err = createTestRun(ctx, pool, agent1.ProjectID, agent1.Name, "completed", 1500, nil)
	if err != nil {
		t.Fatalf("Create run 2: %v", err)
	}
	// Run 3: failed (error)
	err = createTestRun(ctx, pool, agent1.ProjectID, agent1.Name, "failed", 200, nil)
	if err != nil {
		t.Fatalf("Create run 3: %v", err)
	}
	// Run 4: running (active)
	err = createTestRun(ctx, pool, agent1.ProjectID, agent1.Name, "running", 100, nil)
	if err != nil {
		t.Fatalf("Create run 4: %v", err)
	}
	// Run 5: completed with NULL latency
	err = createTestRun(ctx, pool, agent1.ProjectID, agent1.Name, "completed", 0, nil)
	if err != nil {
		t.Fatalf("Create run 5: %v", err)
	}

	// Run 6: timeout (recent error)
	err = createTestRun(ctx, pool, agent1.ProjectID, agent1.Name, "timeout", 3000, nil)
	if err != nil {
		t.Fatalf("Create run 6: %v", err)
	}

	t.Run("Agent A summary aggregation", func(t *testing.T) {
		summaries, err := adapterQ.ListAgentSummariesByProject(ctx, agent1.ProjectID)
		if err != nil {
			t.Fatalf("ListAgentSummariesByProject: %v", err)
		}

		if len(summaries) != 1 {
			t.Fatalf("expected 1 summary, got %d", len(summaries))
		}

		s := summaries[0]
		if s.Name != "test-agent-a" {
			t.Errorf("Name = %q, want test-agent-a", s.Name)
		}
		if s.InstanceCount != 1 {
			t.Errorf("InstanceCount = %d, want 1", s.InstanceCount)
		}
		if s.ActiveRunCount < 1 { // at least the "running" run
			t.Errorf("ActiveRunCount = %d, want >= 1", s.ActiveRunCount)
		}
		if s.RecentErrorCount != 2 { // failed + timeout
			t.Errorf("RecentErrorCount = %d, want 2 (failed + timeout)", s.RecentErrorCount)
		}
		if s.ErrorRate <= 0 {
			t.Errorf("ErrorRate = %f, want > 0", s.ErrorRate)
		}
		// p95 should be non-zero - the 1500ms and 3000ms runs should give a reasonable p95
		if s.P95LatencyMs == nil || *s.P95LatencyMs <= 0 {
			t.Errorf("P95LatencyMs = %v, want > 0", s.P95LatencyMs)
		}
	})

	t.Run("Agent B has no data", func(t *testing.T) {
		summaries, err := adapterQ.ListAgentSummariesByProject(ctx, "00000000-0000-0000-0000-000000000002")
		if err != nil {
			t.Fatalf("ListAgentSummariesByProject: %v", err)
		}
		if len(summaries) != 1 {
			t.Fatalf("expected 1 summary, got %d", len(summaries))
		}
		s := summaries[0]
		// No instances or runs: should have 0 counts and nil p95
		if s.InstanceCount != 0 {
			t.Errorf("InstanceCount = %d, want 0", s.InstanceCount)
		}
		if s.ActiveRunCount != 0 {
			t.Errorf("ActiveRunCount = %d, want 0", s.ActiveRunCount)
		}
		if s.RecentErrorCount != 0 {
			t.Errorf("RecentErrorCount = %d, want 0", s.RecentErrorCount)
		}
		if s.P95LatencyMs != nil {
			t.Errorf("P95LatencyMs = %v, want nil (no runs)", s.P95LatencyMs)
		}
		if s.ErrorRate != 0 {
			t.Errorf("ErrorRate = %f, want 0", s.ErrorRate)
		}
	})

	t.Run("Project isolation", func(t *testing.T) {
		// Agent A's project should not include Agent B's runs
		summaries, err := adapterQ.ListAgentSummariesByProject(ctx, agent1.ProjectID)
		if err != nil {
			t.Fatalf("ListAgentSummariesByProject: %v", err)
		}
		for _, s := range summaries {
			if s.Name == "test-agent-b" {
				t.Errorf("Agent B found in project A's summary")
			}
		}
	})
}

func createTestRun(ctx context.Context, pool *pgxpool.Pool, projectID, agentName, status string, latencyMs int64, cost *float64) error {
	var latencyPg *int64
	if latencyMs > 0 {
		latencyPg = &latencyMs
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO runs (project_id, source, status, trace_id, agent_name, input_summary, metadata, latency_ms, cost, started_at)
		VALUES ($1::uuid, 'live', $2, 'trace-'||gen_random_uuid()::text, $3, 'test input', '{}'::jsonb, $4, $5, now())
	`, projectID, status, agentName, latencyPg, cost)
	return err
}

// TestAgentSummaryWithNoRuns tests that agents with instances but no runs don't error.
func TestAgentSummaryWithNoRuns(t *testing.T) {
	dbURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := store.Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("Open database: %v", err)
	}
	defer pool.Close()

	q := store.New(pool)
	adapterQ := NewAgentQuerier(q)

	cleanup := func() {
		pool.Exec(ctx, "DELETE FROM run_state_events")
		pool.Exec(ctx, "DELETE FROM agent_heartbeats")
		pool.Exec(ctx, "DELETE FROM agent_capabilities")
		pool.Exec(ctx, "DELETE FROM agent_instances")
		pool.Exec(ctx, "DELETE FROM runs")
		pool.Exec(ctx, "DELETE FROM agents")
	}
	cleanup()
	defer cleanup()

	agent, err := adapterQ.CreateAgent(ctx, "00000000-0000-0000-0000-000000000001", "no-run-agent", "No runs", nil)
	if err != nil {
		t.Fatalf("Create agent: %v", err)
	}

	// Create instance but no runs
	_, err = adapterQ.CreateAgentInstance(ctx, agent.ID, agent.ProjectID, "inst-1", "Instance 1", "online", "host-1", 1001, nil)
	if err != nil {
		t.Fatalf("Create instance: %v", err)
	}

	summaries, err := adapterQ.ListAgentSummariesByProject(ctx, agent.ProjectID)
	if err != nil {
		t.Fatalf("ListAgentSummariesByProject: %v", err)
	}
	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}

	s := summaries[0]
	if s.InstanceCount != 1 {
		t.Errorf("InstanceCount = %d, want 1", s.InstanceCount)
	}
	if s.ActiveRunCount != 0 {
		t.Errorf("ActiveRunCount = %d, want 0 (no runs)", s.ActiveRunCount)
	}
	if s.RecentErrorCount != 0 {
		t.Errorf("RecentErrorCount = %d, want 0", s.RecentErrorCount)
	}
	if s.P95LatencyMs != nil {
		t.Errorf("P95LatencyMs = %v, want nil", s.P95LatencyMs)
	}
	if s.ErrorRate != 0 {
		t.Errorf("ErrorRate = %f, want 0", s.ErrorRate)
	}
}
