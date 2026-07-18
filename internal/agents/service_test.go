package agents_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"AgentScope/internal/adapter"
	"AgentScope/internal/agents"
	"AgentScope/internal/store"
)

// This file contains integration tests for the agents service.
// Run with AGENTSCOPE_TEST_DATABASE_URL set.
//
// TDD: tests written BEFORE the production code.

type testFixture struct {
	svc     *agents.Service
	pool    *pgxpool.Pool
	projID  string
	cleanup func()
}

func setupAgentTest(t *testing.T) testFixture {
	t.Helper()
	dbURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}
	if !strings.HasSuffix(dbURL, "_test") && !strings.Contains(dbURL, "_test?") {
		t.Fatal("AGENTSCOPE_TEST_DATABASE_URL must point to a database ending with '_test'")
	}

	ctx := context.Background()
	pool, err := store.Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}

	dropAllTables(t, pool)
	runMigration(t, pool)

	q := store.New(pool)
	user, err := q.CreateUser(ctx, store.CreateUserParams{
		Email:        "agent-svc-test@test.com",
		PasswordHash: []byte("hash"),
		DisplayName:  "Agent Svc Tester",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	proj, err := q.CreateProject(ctx, store.CreateProjectParams{
		Name: "Agent Svc Test Project",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	_, err = q.AddProjectMember(ctx, store.AddProjectMemberParams{
		ProjectID: proj.ID,
		UserID:    user.ID,
		Role:      "owner",
	})
	if err != nil {
		t.Fatalf("AddProjectMember: %v", err)
	}

	projID := adapter.UUIDToString(proj.ID.Bytes)

	agentQuerier := adapter.NewAgentQuerier(q)
	svc := agents.NewService(agentQuerier)

	return testFixture{
		svc:    svc,
		pool:   pool,
		projID: projID,
		cleanup: func() {
			pool.Close()
		},
	}
}

// ---- Tests ----

func TestRegisterAgent(t *testing.T) {
	f := setupAgentTest(t)
	defer f.cleanup()

	ctx := context.Background()

	t.Run("register new agent", func(t *testing.T) {
		agent, err := f.svc.RegisterAgent(ctx, f.projID, "test-agent", "A test agent", nil)
		if err != nil {
			t.Fatalf("RegisterAgent: %v", err)
		}
		if agent.ID == "" {
			t.Fatal("expected non-empty agent ID")
		}
		if agent.Name != "test-agent" {
			t.Errorf("Name = %q, want test-agent", agent.Name)
		}
		if agent.Status != "active" {
			t.Errorf("Status = %q, want active", agent.Status)
		}
		if agent.Version != 1 {
			t.Errorf("Version = %d, want 1", agent.Version)
		}
		if agent.CreatedAt.IsZero() {
			t.Error("CreatedAt is zero")
		}
	})

	t.Run("register agent with metadata", func(t *testing.T) {
		meta := map[string]interface{}{"framework": "langchain", "language": "python"}
		agent, err := f.svc.RegisterAgent(ctx, f.projID, "meta-agent", "With metadata", meta)
		if err != nil {
			t.Fatalf("RegisterAgent with metadata: %v", err)
		}
		if agent.Name != "meta-agent" {
			t.Errorf("Name = %q", agent.Name)
		}
	})

	t.Run("register duplicate name fails", func(t *testing.T) {
		_, err := f.svc.RegisterAgent(ctx, f.projID, "dup-agent", "first", nil)
		if err != nil {
			t.Fatalf("first RegisterAgent: %v", err)
		}
		_, err = f.svc.RegisterAgent(ctx, f.projID, "dup-agent", "second", nil)
		if err == nil {
			t.Fatal("expected error for duplicate agent name, got nil")
		}
	})

	t.Run("register with empty project ID fails", func(t *testing.T) {
		_, err := f.svc.RegisterAgent(ctx, "", "no-proj-agent", "no project", nil)
		if err == nil {
			t.Fatal("expected error for empty project ID")
		}
	})

	t.Run("register with empty name fails", func(t *testing.T) {
		_, err := f.svc.RegisterAgent(ctx, f.projID, "", "no name", nil)
		if err == nil {
			t.Fatal("expected error for empty name")
		}
	})
}

func TestListAgents(t *testing.T) {
	f := setupAgentTest(t)
	defer f.cleanup()

	ctx := context.Background()

	_, err := f.svc.RegisterAgent(ctx, f.projID, "agent-a", "Agent A", nil)
	if err != nil {
		t.Fatalf("RegisterAgent agent-a: %v", err)
	}
	_, err = f.svc.RegisterAgent(ctx, f.projID, "agent-b", "Agent B", nil)
	if err != nil {
		t.Fatalf("RegisterAgent agent-b: %v", err)
	}

	agents, err := f.svc.ListAgents(ctx, f.projID)
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(agents))
	}

	_, err = f.svc.ListAgents(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty project ID")
	}
}

func TestGetAgent(t *testing.T) {
	f := setupAgentTest(t)
	defer f.cleanup()

	ctx := context.Background()

	created, err := f.svc.RegisterAgent(ctx, f.projID, "get-me", "Get me", nil)
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	t.Run("get by ID", func(t *testing.T) {
		agent, err := f.svc.GetAgent(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetAgent: %v", err)
		}
		if agent.Name != "get-me" {
			t.Errorf("Name = %q", agent.Name)
		}
	})

	t.Run("get by name", func(t *testing.T) {
		agent, err := f.svc.GetAgentByName(ctx, f.projID, "get-me")
		if err != nil {
			t.Fatalf("GetAgentByName: %v", err)
		}
		if agent.ID != created.ID {
			t.Errorf("ID mismatch")
		}
	})

	t.Run("get non-existent returns error", func(t *testing.T) {
		_, err := f.svc.GetAgent(ctx, "00000000-0000-0000-0000-000000000000")
		if err == nil {
			t.Fatal("expected error for non-existent agent")
		}
	})
}

func TestUpdateAgentStatus(t *testing.T) {
	f := setupAgentTest(t)
	defer f.cleanup()

	ctx := context.Background()

	agent, err := f.svc.RegisterAgent(ctx, f.projID, "status-agent", "Status test", nil)
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	err = f.svc.UpdateAgentStatus(ctx, agent.ID, "inactive")
	if err != nil {
		t.Fatalf("UpdateAgentStatus: %v", err)
	}

	got, err := f.svc.GetAgent(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgent after update: %v", err)
	}
	if got.Status != "inactive" {
		t.Errorf("Status = %q, want inactive", got.Status)
	}
}

func TestAgentInstances(t *testing.T) {
	f := setupAgentTest(t)
	defer f.cleanup()

	ctx := context.Background()

	agent, err := f.svc.RegisterAgent(ctx, f.projID, "instance-agent", "Instance test", nil)
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	t.Run("register instance", func(t *testing.T) {
		inst, err := f.svc.RegisterInstance(ctx, agent.ID, "inst-1", "host-1", 12345, map[string]interface{}{
			"ip": "10.0.0.1",
		})
		if err != nil {
			t.Fatalf("RegisterInstance: %v", err)
		}
		if inst.ID == "" {
			t.Fatal("expected non-empty instance ID")
		}
		if inst.Status != "online" {
			t.Errorf("Status = %q, want online", inst.Status)
		}
		if inst.Hostname != "host-1" {
			t.Errorf("Hostname = %q", inst.Hostname)
		}
	})

	t.Run("list instances", func(t *testing.T) {
		instances, err := f.svc.ListInstances(ctx, agent.ID)
		if err != nil {
			t.Fatalf("ListInstances: %v", err)
		}
		if len(instances) != 1 {
			t.Errorf("expected 1 instance, got %d", len(instances))
		}
	})

	t.Run("register duplicate instance ID fails", func(t *testing.T) {
		_, err := f.svc.RegisterInstance(ctx, agent.ID, "inst-1", "host-other", 0, nil)
		if err == nil {
			t.Fatal("expected error for duplicate instance ID")
		}
	})

	t.Run("record heartbeat", func(t *testing.T) {
		_, err = f.svc.RegisterInstance(ctx, agent.ID, "hb-inst", "host-hb", 9999, nil)
		if err != nil {
			t.Fatalf("RegisterInstance: %v", err)
		}

		err = f.svc.RecordHeartbeat(ctx, agent.ID, "hb-inst", "online", map[string]interface{}{
			"cpu": 0.5,
			"mem": 1024,
		})
		if err != nil {
			t.Fatalf("RecordHeartbeat: %v", err)
		}
	})

	t.Run("heartbeat_auto_creates_instance", func(t *testing.T) {
		// Send heartbeat for an instance that has NOT been explicitly registered.
		// The heartbeat should auto-create it (upsert).
		err := f.svc.RecordHeartbeat(ctx, agent.ID, "auto-inst", "online", map[string]interface{}{
			"cpu": 0.3,
		})
		if err != nil {
			t.Fatalf("RecordHeartbeat (auto-create): %v", err)
		}

		// Verify the instance now exists
		instances, err := f.svc.ListInstances(ctx, agent.ID)
		if err != nil {
			t.Fatalf("ListInstances: %v", err)
		}
		found := false
		for _, inst := range instances {
			if inst.InstanceID == "auto-inst" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("instance 'auto-inst' not found after heartbeat, instances: %+v", instances)
		}
	})
}

func TestAgentCapabilities(t *testing.T) {
	f := setupAgentTest(t)
	defer f.cleanup()

	ctx := context.Background()

	agent, err := f.svc.RegisterAgent(ctx, f.projID, "cap-agent", "Capability test", nil)
	if err != nil {
		t.Fatalf("RegisterAgent: %v", err)
	}

	t.Run("set capability", func(t *testing.T) {
		err := f.svc.SetCapability(ctx, agent.ID, "skill-code-review", "Can review code", map[string]interface{}{
			"type": "skill",
		})
		if err != nil {
			t.Fatalf("SetCapability: %v", err)
		}
	})

	t.Run("list capabilities", func(t *testing.T) {
		err := f.svc.SetCapability(ctx, agent.ID, "tool-file-read", "Can read files", nil)
		if err != nil {
			t.Fatalf("SetCapability (tool): %v", err)
		}

		caps, err := f.svc.ListCapabilities(ctx, agent.ID)
		if err != nil {
			t.Fatalf("ListCapabilities: %v", err)
		}
		if len(caps) != 2 {
			t.Errorf("expected 2 capabilities, got %d", len(caps))
		}
	})

	t.Run("upsert capability", func(t *testing.T) {
		err := f.svc.SetCapability(ctx, agent.ID, "skill-code-review", "Updated description", map[string]interface{}{
			"type":  "skill",
			"level": "advanced",
		})
		if err != nil {
			t.Fatalf("SetCapability upsert: %v", err)
		}

		caps, err := f.svc.ListCapabilities(ctx, agent.ID)
		if err != nil {
			t.Fatalf("ListCapabilities: %v", err)
		}
		for _, c := range caps {
			if c.Name == "skill-code-review" {
				if c.Description != "Updated description" {
					t.Errorf("Description = %q, want 'Updated description'", c.Description)
				}
				return
			}
		}
		t.Error("skill-code-review not found after upsert")
	})
}

// ---- helpers ----

func dropAllTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	stmt := `DROP TABLE IF EXISTS
		agent_heartbeats,
		agent_capabilities,
		agent_instances,
		agent_versions,
		agents,
		eval_case_results,
		eval_jobs,
		experiments,
		evaluators,
		dataset_cases,
		datasets,
		runs,
		project_tokens,
		project_members,
		projects,
		users
	CASCADE`
	if _, err := pool.Exec(ctx, stmt); err != nil {
		t.Fatalf("drop tables: %v", err)
	}
}

func runMigration(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	b, err := os.ReadFile("../../migrations/00001_init.sql")
	if err != nil {
		t.Fatalf("read migration file: %v", err)
	}
	ctx := context.Background()
	if _, err := pool.Exec(ctx, string(b)); err != nil {
		t.Fatalf("run migration: %v", err)
	}
}
