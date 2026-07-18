package store

import (
	"context"
	"os"
	"testing"
	"time"
)

// Note: These tests are database integration tests.
// Run them with AGENTSCOPE_TEST_DATABASE_URL set.
//
// TDD: These tests are written BEFORE the production code.
// They should fail initially (Agent tables/queries don't exist yet).

func TestAgentStore(t *testing.T) {
	dbURL := os.Getenv("AGENTSCOPE_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("SKIP: AGENTSCOPE_TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := Open(ctx, dbURL)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer pool.Close()

	requireTestDatabase(t, dbURL)
	dropAllTables(t, pool)
	runMigration(t, pool)

	q := New(pool)

	// ---- Create user and project ----
	_, err = q.CreateUser(ctx, CreateUserParams{
		Email:        "agent-store-test@test.com",
		PasswordHash: []byte("hash"),
		DisplayName:  "Agent Store Tester",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	proj, err := q.CreateProject(ctx, CreateProjectParams{
		Name: "Agent Store Test Project",
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	// ========================================================================
	// Agent CRUD
	// ========================================================================

	t.Run("CreateAgent", func(t *testing.T) {
		agent, err := q.CreateAgent(ctx, CreateAgentParams{
			ProjectID:   proj.ID,
			Name:        "test-agent",
			Description: "A test agent",
			Metadata:    []byte(`{"framework":"langchain"}`),
		})
		if err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}
		if !agent.ID.Valid {
			t.Fatal("agent.ID not set")
		}
		if agent.Name != "test-agent" {
			t.Errorf("agent.Name = %q, want test-agent", agent.Name)
		}
		if agent.Status != "active" {
			t.Errorf("agent.Status = %q, want active", agent.Status)
		}
		if agent.Version != 1 {
			t.Errorf("agent.Version = %d, want 1", agent.Version)
		}
	})

	t.Run("CreateAgent_duplicate_name_fails", func(t *testing.T) {
		_, err := q.CreateAgent(ctx, CreateAgentParams{
			ProjectID:   proj.ID,
			Name:        "duplicate-agent",
			Description: "first",
			Metadata:    []byte("{}"),
		})
		if err != nil {
			t.Fatalf("first CreateAgent: %v", err)
		}

		// Try to create another agent with the same project_id + name
		_, err = q.CreateAgent(ctx, CreateAgentParams{
			ProjectID:   proj.ID,
			Name:        "duplicate-agent",
			Description: "second",
			Metadata:    []byte("{}"),
		})
		if err == nil {
			t.Fatal("expected error for duplicate agent name, got nil")
		}
	})

	t.Run("ListAgentsByProject", func(t *testing.T) {
		// Create a second agent
		_, err := q.CreateAgent(ctx, CreateAgentParams{
			ProjectID:   proj.ID,
			Name:        "list-test-agent",
			Description: "list test",
			Metadata:    []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}

		agents, err := q.ListAgentsByProject(ctx, proj.ID)
		if err != nil {
			t.Fatalf("ListAgentsByProject: %v", err)
		}
		if len(agents) < 2 {
			t.Errorf("expected at least 2 agents, got %d", len(agents))
		}
	})

	t.Run("GetAgentByID", func(t *testing.T) {
		created, err := q.CreateAgent(ctx, CreateAgentParams{
			ProjectID:   proj.ID,
			Name:        "get-by-id-agent",
			Description: "get by id",
			Metadata:    []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}

		got, err := q.GetAgentByID(ctx, created.ID)
		if err != nil {
			t.Fatalf("GetAgentByID: %v", err)
		}
		if got.Name != "get-by-id-agent" {
			t.Errorf("got.Name = %q", got.Name)
		}
	})

	t.Run("GetAgentByProjectAndName", func(t *testing.T) {
		created, err := q.CreateAgent(ctx, CreateAgentParams{
			ProjectID:   proj.ID,
			Name:        "get-by-name-agent",
			Description: "get by name",
			Metadata:    []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}

		got, err := q.GetAgentByProjectAndName(ctx, GetAgentByProjectAndNameParams{
			ProjectID: proj.ID,
			Name:      "get-by-name-agent",
		})
		if err != nil {
			t.Fatalf("GetAgentByProjectAndName: %v", err)
		}
		if got.ID != created.ID {
			t.Errorf("ID mismatch")
		}
	})

	t.Run("UpdateAgentStatus", func(t *testing.T) {
		created, err := q.CreateAgent(ctx, CreateAgentParams{
			ProjectID:   proj.ID,
			Name:        "status-test-agent",
			Description: "status test",
			Metadata:    []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}

		updated, err := q.UpdateAgentStatus(ctx, UpdateAgentStatusParams{
			ID:     created.ID,
			Status: "inactive",
		})
		if err != nil {
			t.Fatalf("UpdateAgentStatus: %v", err)
		}
		if updated.Status != "inactive" {
			t.Errorf("updated.Status = %q, want inactive", updated.Status)
		}
	})

	// ========================================================================
	// Agent Versions
	// ========================================================================

	t.Run("CreateAgentVersion", func(t *testing.T) {
		agent, err := q.CreateAgent(ctx, CreateAgentParams{
			ProjectID:   proj.ID,
			Name:        "version-test-agent",
			Description: "version test",
			Metadata:    []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}

		ver, err := q.CreateAgentVersion(ctx, CreateAgentVersionParams{
			AgentID:     agent.ID,
			Version:     1,
			Description: "initial version",
			Metadata:    []byte(`{"changes":"initial"}`),
		})
		if err != nil {
			t.Fatalf("CreateAgentVersion: %v", err)
		}
		if !ver.ID.Valid {
			t.Fatal("version ID not set")
		}
		if ver.Version != 1 {
			t.Errorf("ver.Version = %d, want 1", ver.Version)
		}
	})

	// ========================================================================
	// Agent Instances
	// ========================================================================

	t.Run("CreateAgentInstance", func(t *testing.T) {
		agent, err := q.CreateAgent(ctx, CreateAgentParams{
			ProjectID:   proj.ID,
			Name:        "instance-test-agent",
			Description: "instance test",
			Metadata:    []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}

		inst, err := q.CreateAgentInstance(ctx, CreateAgentInstanceParams{
			ProjectID:    proj.ID,
			InstanceName: "instance-1",
			AgentID:    agent.ID,
			InstanceID: "instance-1",
			Status:     "online",
			Hostname:   "host-1",
			Pid:        12345,
			Metadata:   []byte(`{"ip":"10.0.0.1"}`),
		})
		if err != nil {
			t.Fatalf("CreateAgentInstance: %v", err)
		}
		if !inst.ID.Valid {
			t.Fatal("instance.ID not set")
		}
		if inst.Status != "online" {
			t.Errorf("inst.Status = %q, want online", inst.Status)
		}
	})

	t.Run("UpdateAgentInstanceHeartbeat", func(t *testing.T) {
		agent, err := q.CreateAgent(ctx, CreateAgentParams{
			ProjectID:   proj.ID,
			Name:        "hb-test-agent",
			Description: "heartbeat test",
			Metadata:    []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}

		_, err = q.CreateAgentInstance(ctx, CreateAgentInstanceParams{
			ProjectID:    proj.ID,
			InstanceName: "instance-1",
			AgentID:    agent.ID,
			InstanceID: "hb-instance",
			Status:     "online",
			Hostname:   "host-hb",
			Pid:        67890,
			Metadata:   []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAgentInstance: %v", err)
		}

		updated, err := q.UpdateAgentInstanceHeartbeat(ctx, UpdateAgentInstanceHeartbeatParams{
			AgentID:    agent.ID,
			InstanceID: "hb-instance",
			Status:     "online",
		})
		if err != nil {
			t.Fatalf("UpdateAgentInstanceHeartbeat: %v", err)
		}
		if updated.Status != "online" {
			t.Errorf("updated.Status = %q", updated.Status)
		}
		// Verify last_heartbeat_at was updated
		if !updated.LastHeartbeatAt.Time.After(time.Now().Add(-10 * time.Second)) {
			t.Errorf("last_heartbeat_at seems stale: %v", updated.LastHeartbeatAt.Time)
		}
	})

	t.Run("ListAgentInstancesByAgent", func(t *testing.T) {
		agent, err := q.CreateAgent(ctx, CreateAgentParams{
			ProjectID:   proj.ID,
			Name:        "list-inst-agent",
			Description: "list instances",
			Metadata:    []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}

		_, err = q.CreateAgentInstance(ctx, CreateAgentInstanceParams{
			ProjectID:    proj.ID,
			InstanceName: "instance-1",
			AgentID:    agent.ID,
			InstanceID: "inst-a",
			Status:     "online",
			Hostname:   "host-a",
			Pid:        1001,
			Metadata:   []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAgentInstance (a): %v", err)
		}
		_, err = q.CreateAgentInstance(ctx, CreateAgentInstanceParams{
			ProjectID:    proj.ID,
			InstanceName: "instance-1",
			AgentID:    agent.ID,
			InstanceID: "inst-b",
			Status:     "offline",
			Hostname:   "host-b",
			Pid:        1002,
			Metadata:   []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAgentInstance (b): %v", err)
		}

		instances, err := q.ListAgentInstancesByAgent(ctx, agent.ID)
		if err != nil {
			t.Fatalf("ListAgentInstancesByAgent: %v", err)
		}
		if len(instances) != 2 {
			t.Errorf("expected 2 instances, got %d", len(instances))
		}
	})

	// ========================================================================
	// Agent Heartbeats
	// ========================================================================

	t.Run("CreateAgentHeartbeat", func(t *testing.T) {
		agent, err := q.CreateAgent(ctx, CreateAgentParams{
			ProjectID:   proj.ID,
			Name:        "hb-log-test-agent",
			Description: "heartbeat log test",
			Metadata:    []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}

		inst, err := q.CreateAgentInstance(ctx, CreateAgentInstanceParams{
			ProjectID:    proj.ID,
			InstanceName: "instance-1",
			AgentID:    agent.ID,
			InstanceID: "hb-log-inst",
			Status:     "online",
			Hostname:   "host-hb-log",
			Pid:        9999,
			Metadata:   []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAgentInstance: %v", err)
		}

		hb, err := q.CreateAgentHeartbeat(ctx, CreateAgentHeartbeatParams{
			AgentID:    agent.ID,
			InstanceID: inst.ID,
			Status:     "online",
			Payload:    []byte(`{"cpu":0.5,"mem":1024}`),
		})
		if err != nil {
			t.Fatalf("CreateAgentHeartbeat: %v", err)
		}
		if !hb.ReceivedAt.Valid {
			t.Fatal("heartbeat received_at not set")
		}
	})

	// ========================================================================
	// Agent Capabilities
	// ========================================================================

	t.Run("CreateAgentCapability", func(t *testing.T) {
		agent, err := q.CreateAgent(ctx, CreateAgentParams{
			ProjectID:   proj.ID,
			Name:        "cap-test-agent",
			Description: "capability test",
			Metadata:    []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}

		cap, err := q.CreateAgentCapability(ctx, CreateAgentCapabilityParams{
			AgentID:     agent.ID,
			Name:        "skill-code-review",
			Description: "Can review code changes",
			Metadata:    []byte(`{"type":"skill"}`),
		})
		if err != nil {
			t.Fatalf("CreateAgentCapability: %v", err)
		}
		if !cap.ID.Valid {
			t.Fatal("capability ID not set")
		}
		if cap.Name != "skill-code-review" {
			t.Errorf("cap.Name = %q", cap.Name)
		}
	})

	t.Run("UpsertAgentCapability", func(t *testing.T) {
		agent, err := q.CreateAgent(ctx, CreateAgentParams{
			ProjectID:   proj.ID,
			Name:        "upsert-cap-agent",
			Description: "upsert capability test",
			Metadata:    []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}

		// Create first
		_, err = q.CreateAgentCapability(ctx, CreateAgentCapabilityParams{
			AgentID:     agent.ID,
			Name:        "skill-test",
			Description: "original description",
			Metadata:    []byte(`{"v":1}`),
		})
		if err != nil {
			t.Fatalf("CreateAgentCapability (first): %v", err)
		}

		// Upsert with same name but different description/metadata
		upserted, err := q.CreateAgentCapability(ctx, CreateAgentCapabilityParams{
			AgentID:     agent.ID,
			Name:        "skill-test",
			Description: "updated description",
			Metadata:    []byte(`{"v":2}`),
		})
		if err != nil {
			t.Fatalf("CreateAgentCapability (upsert): %v", err)
		}
		if upserted.Description != "updated description" {
			t.Errorf("upserted.Description = %q, want 'updated description'", upserted.Description)
		}
	})

	t.Run("ListAgentCapabilitiesByAgent", func(t *testing.T) {
		agent, err := q.CreateAgent(ctx, CreateAgentParams{
			ProjectID:   proj.ID,
			Name:        "list-cap-agent",
			Description: "list capabilities",
			Metadata:    []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}

		_, err = q.CreateAgentCapability(ctx, CreateAgentCapabilityParams{
			AgentID:     agent.ID,
			Name:        "skill-a",
			Description: "Skill A",
			Metadata:    []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAgentCapability (a): %v", err)
		}
		_, err = q.CreateAgentCapability(ctx, CreateAgentCapabilityParams{
			AgentID:     agent.ID,
			Name:        "skill-b",
			Description: "Skill B",
			Metadata:    []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAgentCapability (b): %v", err)
		}

		caps, err := q.ListAgentCapabilitiesByAgent(ctx, agent.ID)
		if err != nil {
			t.Fatalf("ListAgentCapabilitiesByAgent: %v", err)
		}
		if len(caps) != 2 {
			t.Errorf("expected 2 capabilities, got %d", len(caps))
		}
	})

	// ========================================================================
	// DropTables — verify our new tables are included in dropAllTables
	// ========================================================================



	// ============================================================
	// Upsert Agent Instance (heartbeat-driven instance creation)
	// ============================================================

	t.Run("UpsertAgentInstance_creates_new", func(t *testing.T) {
		agent, err := q.CreateAgent(ctx, CreateAgentParams{
			ProjectID:   proj.ID,
			Name:        "upsert-inst-agent",
			Description: "upsert instance test",
			Metadata:    []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAgent: %v", err)
		}

		inst, err := q.UpsertAgentInstance(ctx, UpsertAgentInstanceParams{
			ProjectID:    proj.ID,
			InstanceName: "upsert-inst-1",
			AgentID:    agent.ID,
			InstanceID: "upsert-inst-1",
			Status:     "online",
			Hostname:   "host-upsert",
			Pid:        8888,
			Metadata:   []byte(`{"created_by":"upsert"}`),
		})
		if err != nil {
			t.Fatalf("UpsertAgentInstance (create): %v", err)
		}
		if !inst.ID.Valid {
			t.Fatal("instance ID not set")
		}
		if inst.Status != "online" {
			t.Errorf("Status = %q, want online", inst.Status)
		}
		if inst.Hostname != "host-upsert" {
			t.Errorf("Hostname = %q", inst.Hostname)
		}
		if inst.Pid != 8888 {
			t.Errorf("Pid = %d, want 8888", inst.Pid)
		}

		// First heartbeat time
		initialHb := inst.LastHeartbeatAt.Time
		time.Sleep(5 * time.Millisecond)

		// Upsert again — should update, not duplicate
		updated, err := q.UpsertAgentInstance(ctx, UpsertAgentInstanceParams{
			ProjectID:    proj.ID,
			InstanceName: "upsert-inst-1",
			AgentID:    agent.ID,
			InstanceID: "upsert-inst-1",
			Status:     "online",
			Hostname:   "host-upsert",
			Pid:        8888,
			Metadata:   []byte(`{"created_by":"upsert"}`),
		})
		if err != nil {
			t.Fatalf("UpsertAgentInstance (update): %v", err)
		}
		if !updated.LastHeartbeatAt.Time.After(initialHb) {
			t.Errorf("last_heartbeat_at not updated: was %v, now %v", initialHb, updated.LastHeartbeatAt.Time)
		}

		// Verify only 1 instance exists
		instances, err := q.ListAgentInstancesByAgent(ctx, agent.ID)
		if err != nil {
			t.Fatalf("ListAgentInstancesByAgent: %v", err)
		}
		if len(instances) != 1 {
			t.Errorf("expected 1 instance after upsert, got %d", len(instances))
		}
	})

	// ============================================================
	// DropTables — verify our new tables are included in dropAllTables
	// ============================================================

	t.Run("VerifyTableDropping", func(t *testing.T) {
		// The dropAllTables function should handle our new tables.
		// If it doesn't, subsequent tests might leak state.
		// This is a design-level test — we just ensure no panic.
		dropAllTables(t, pool)
		runMigration(t, pool)

		// Verify we can create an agent in a clean state
		_, err := q.CreateUser(ctx, CreateUserParams{
			Email:        "clean-slate@test.com",
			PasswordHash: []byte("hash"),
			DisplayName:  "Clean Slate",
		})
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		proj2, err := q.CreateProject(ctx, CreateProjectParams{
			Name: "Clean Slate Project",
		})
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}

		_, err = q.CreateAgent(ctx, CreateAgentParams{
			ProjectID:   proj2.ID,
			Name:        "clean-slate-agent",
			Description: "After drop/re-migrate",
			Metadata:    []byte("{}"),
		})
		if err != nil {
			t.Fatalf("CreateAgent after re-migration: %v", err)
		}
	})
}
