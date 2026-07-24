package agents_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
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

// versionErrorQuerier wraps agents.Querier and fails on CreateAgentVersion.
type versionErrorQuerier struct {
	agents.Querier
}

func (q *versionErrorQuerier) CreateAgentVersion(ctx context.Context, agentID string, version int, description string, metadata []byte) error {
	return errors.New("version creation failed")
}

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

	t.Run("re-register same name returns same ID", func(t *testing.T) {
		id, err := f.svc.RegisterAgent(ctx, f.projID, "idempotent-agent", "first", nil)
		if err != nil {
			t.Fatalf("first RegisterAgent: %v", err)
		}

		same, err := f.svc.RegisterAgent(ctx, f.projID, "idempotent-agent", "second (updated)", nil)
		if err != nil {
			t.Fatalf("second RegisterAgent (idempotent): %v, want success", err)
		}
		if same.ID != id.ID {
			t.Errorf("same agent name returned different ID: first=%q second=%q", id.ID, same.ID)
		}
		if same.Version != 1 {
			t.Errorf("Version after re-register = %d, want 1 (no new version)", same.Version)
		}
	})

	t.Run("different project can register same name", func(t *testing.T) {
		store.New(f.pool)
		// Create a second project
		proj2, err := store.New(f.pool).CreateProject(ctx, store.CreateProjectParams{
			Name: "Second Project",
		})
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}
		proj2ID := adapter.UUIDToString(proj2.ID.Bytes)

		_, err = f.svc.RegisterAgent(ctx, proj2ID, "cross-project-agent", "Project 2", nil)
		if err != nil {
			t.Fatalf("RegisterAgent in second project: %v", err)
		}
	})
	t.Run("version creation failure returns error", func(t *testing.T) {
		f2 := setupAgentTest(t)
		defer f2.cleanup()

		q := store.New(f2.pool)
		agentQuerier := adapter.NewAgentQuerier(q)
		badQ := &versionErrorQuerier{Querier: agentQuerier}
		svc := agents.NewService(badQ)

		_, err := svc.RegisterAgent(ctx, f2.projID, "version-fail-agent", "Should fail", nil)
		if err == nil {
			t.Fatal("expected error when CreateAgentVersion fails")
		}
		if !strings.Contains(err.Error(), "create initial version") {
			t.Errorf("error = %q, want 'create initial version'", err.Error())
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
		inst, err := f.svc.RegisterInstance(ctx, agent.ID, f.projID, "inst-1", "", "host-1", 12345, map[string]interface{}{
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
		_, err := f.svc.RegisterInstance(ctx, agent.ID, f.projID, "inst-1", "", "host-other", 0, nil)
		if err == nil {
			t.Fatal("expected error for duplicate instance ID")
		}
	})

	t.Run("record heartbeat", func(t *testing.T) {
		_, err = f.svc.RegisterInstance(ctx, agent.ID, f.projID, "hb-inst", "", "host-hb", 9999, nil)
		if err != nil {
			t.Fatalf("RegisterInstance: %v", err)
		}

		err = f.svc.RecordHeartbeat(ctx, agent.ID, f.projID, "hb-inst", "", "online", map[string]interface{}{
			"cpu": 0.5,
			"mem": 1024,
		})
		if err != nil {
			t.Fatalf("RecordHeartbeat: %v", err)
		}
	})

	t.Run("heartbeat preserves instance metadata", func(t *testing.T) {
		// Register instance with specific hostname, pid, and metadata
		inst, err := f.svc.RegisterInstance(ctx, agent.ID, f.projID, "meta-inst", "", "my-host", 7777, map[string]interface{}{
			"ip":      "10.0.0.5",
			"version": "1.2.3",
		})
		if err != nil {
			t.Fatalf("RegisterInstance: %v", err)
		}
		originalStartedAt := inst.StartedAt
		originalLastHb := inst.LastHeartbeatAt

		// Record heartbeat without hostname/pid/metadata
		err = f.svc.RecordHeartbeat(ctx, agent.ID, f.projID, "meta-inst", "", "online", map[string]interface{}{
			"cpu": 0.7,
		})
		if err != nil {
			t.Fatalf("RecordHeartbeat: %v", err)
		}

		// List instances and verify metadata is preserved
		instances, err := f.svc.ListInstances(ctx, agent.ID)
		if err != nil {
			t.Fatalf("ListInstances: %v", err)
		}
		var found *agents.AgentInstance
		for i := range instances {
			if instances[i].InstanceID == "meta-inst" {
				found = &instances[i]
				break
			}
		}
		if found == nil {
			t.Fatal("instance 'meta-inst' not found after heartbeat")
		}

		// Assert hostname, pid, metadata are preserved (not overwritten)
		if found.Hostname != "my-host" {
			t.Errorf("Hostname = %q, want 'my-host'", found.Hostname)
		}
		if found.PID != 7777 {
			t.Errorf("PID = %d, want 7777", found.PID)
		}
		if found.Metadata == nil || found.Metadata["ip"] != "10.0.0.5" {
			t.Errorf("Metadata['ip'] = %v, want '10.0.0.5'", found.Metadata["ip"])
		}
		if found.Metadata == nil || found.Metadata["version"] != "1.2.3" {
			t.Errorf("Metadata['version'] = %v, want '1.2.3'", found.Metadata["version"])
		}
		if found.Metadata == nil || found.Metadata["cpu"] != 0.7 {
			t.Errorf("Metadata['cpu'] = %v, want 0.7", found.Metadata["cpu"])
		}

		// Assert last_heartbeat_at is updated
		if !found.LastHeartbeatAt.After(originalLastHb) && !originalLastHb.IsZero() {
			t.Error("LastHeartbeatAt was not updated")
		}

		// Assert started_at is preserved (not reset)
		if !found.StartedAt.Equal(originalStartedAt) {
			t.Error("StartedAt changed after heartbeat")
		}
	})

	t.Run("heartbeat_auto_creates_instance", func(t *testing.T) {
		// Send heartbeat for an instance that has NOT been explicitly registered.
		// The heartbeat should auto-create it (upsert).
		err := f.svc.RecordHeartbeat(ctx, agent.ID, f.projID, "auto-inst", "", "online", map[string]interface{}{
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
			comparison_case_results,
			experiment_comparisons,
			annotation_items,
			annotation_queues,
			trace_sampling_rules,
			run_state_events,
			agent_heartbeats,
			agent_capabilities,
			agent_instances,
			agent_versions,
			agents,
			eval_metric_definitions,
			eval_scores,
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
	files, err := filepath.Glob("../../migrations/*.sql")
	if err != nil {
		t.Fatalf("list migration files: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no migration files found")
	}
	sort.Strings(files)

	ctx := context.Background()
	for _, filename := range files {
		b, err := os.ReadFile(filename)
		if err != nil {
			t.Fatalf("read migration %s: %v", filepath.Base(filename), err)
		}
		if _, err := pool.Exec(ctx, string(b)); err != nil {
			t.Fatalf("run migration %s: %v", filepath.Base(filename), err)
		}
	}
}
