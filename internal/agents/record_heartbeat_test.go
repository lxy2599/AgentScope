package agents

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

// fakeQuerier implements Querier with captured calls for testing.
type fakeQuerier struct {
	mu                       sync.Mutex
	upsertAgentInstanceCalls []upsertCall
	createHeartbeatCalls     []heartbeatCall
}

type upsertCall struct {
	agentID      string
	projectID    string
	instanceID   string
	instanceName string
	status       string
	hostname     string
	pid          int
	metadata     []byte
}

type heartbeatCall struct {
	agentID    string
	instanceID string
	status     string
	payload    []byte
}

func (f *fakeQuerier) CreateAgent(ctx context.Context, projectID, name, description string, metadata []byte) (Agent, error) {
	return Agent{}, errors.New("not implemented")
}

func (f *fakeQuerier) GetAgentByID(ctx context.Context, agentID string) (Agent, error) {
	return Agent{}, errors.New("not implemented")
}

func (f *fakeQuerier) GetAgentByProjectAndName(ctx context.Context, projectID, name string) (Agent, error) {
	return Agent{}, errors.New("not implemented")
}

func (f *fakeQuerier) ListAgentsByProject(ctx context.Context, projectID string) ([]Agent, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeQuerier) UpdateAgentStatus(ctx context.Context, agentID, status string) error {
	return errors.New("not implemented")
}

func (f *fakeQuerier) CreateAgentVersion(ctx context.Context, agentID string, version int, description string, metadata []byte) error {
	return errors.New("not implemented")
}

func (f *fakeQuerier) CreateAgentInstance(ctx context.Context, agentID, projectID, instanceID, instanceName, status, hostname string, pid int, metadata []byte) (AgentInstance, error) {
	return AgentInstance{}, errors.New("not implemented")
}

func (f *fakeQuerier) UpdateAgentInstanceHeartbeat(ctx context.Context, agentID, instanceID, status string) (time.Time, error) {
	return time.Time{}, errors.New("not implemented")
}

func (f *fakeQuerier) ListAgentInstancesByAgent(ctx context.Context, agentID string) ([]AgentInstance, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeQuerier) UpsertAgentInstance(ctx context.Context, agentID, projectID, instanceID, instanceName, status, hostname string, pid int, metadata []byte) (AgentInstance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.upsertAgentInstanceCalls = append(f.upsertAgentInstanceCalls, upsertCall{
		agentID: agentID, projectID: projectID, instanceID: instanceID,
		instanceName: instanceName, status: status, hostname: hostname, pid: pid,
		metadata: metadata,
	})
	return AgentInstance{
		ID: "inst-uuid", AgentID: agentID, ProjectID: projectID,
		InstanceID: instanceID, InstanceName: instanceName, Status: status,
		Hostname: hostname, PID: pid,
	}, nil
}

func (f *fakeQuerier) CreateAgentHeartbeat(ctx context.Context, agentID, instanceID, status string, payload []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.createHeartbeatCalls = append(f.createHeartbeatCalls, heartbeatCall{
		agentID: agentID, instanceID: instanceID, status: status, payload: payload,
	})
	return nil
}

func (f *fakeQuerier) SetCapability(ctx context.Context, agentID, name, description string, metadata []byte) error {
	return errors.New("not implemented")
}

func (f *fakeQuerier) ListCapabilitiesByAgent(ctx context.Context, agentID string) ([]AgentCapability, error) {
	return nil, errors.New("not implemented")
}

func TestRecordHeartbeatPreservesFullPayloadForHistory(t *testing.T) {
	fq := &fakeQuerier{}
	svc := NewService(fq)

	ctx := context.Background()
	payload := map[string]interface{}{
		"contract_version":  "agent-runtime-v1",
		"runtime":           "python",
		"framework":         "custom-python",
		"environment":       "dev",
		"hostname":          "my-host",
		"pid":               float64(12345),
		"process_start_time": "2026-07-22T00:00:00Z",
		"sdk_version":       "0.1.0",
		"capacity":          float64(1),
		"inflight_runs":     float64(0),
	}

	err := svc.RecordHeartbeat(ctx, "agent-1", "proj-1", "inst-1", "inst-1", "online", payload)
	if err != nil {
		t.Fatalf("RecordHeartbeat: %v", err)
	}

	// ---- Verify heartbeat history retains full payload ----
	if len(fq.createHeartbeatCalls) != 1 {
		t.Fatalf("expected 1 heartbeat call, got %d", len(fq.createHeartbeatCalls))
	}
	hb := fq.createHeartbeatCalls[0]
	var hbPayload map[string]interface{}
	if err := json.Unmarshal(hb.payload, &hbPayload); err != nil {
		t.Fatalf("unmarshal heartbeat payload: %v", err)
	}

	// History MUST have process_start_time
	if hbPayload["process_start_time"] != "2026-07-22T00:00:00Z" {
		t.Fatalf("history payload missing process_start_time, got %v", hbPayload["process_start_time"])
	}
	// History MUST have hostname and pid
	if hbPayload["hostname"] != "my-host" {
		t.Fatalf("history payload hostname = %v", hbPayload["hostname"])
	}
	if hbPayload["pid"] != float64(12345) {
		t.Fatalf("history payload pid = %v", hbPayload["pid"])
	}
	// History MUST have all other metadata fields
	if hbPayload["contract_version"] != "agent-runtime-v1" {
		t.Fatalf("history payload contract_version = %v", hbPayload["contract_version"])
	}
	if hbPayload["runtime"] != "python" {
		t.Fatalf("history payload runtime = %v", hbPayload["runtime"])
	}

	// ---- Verify upsert instance has correct hostname/pid columns ----
	if len(fq.upsertAgentInstanceCalls) != 1 {
		t.Fatalf("expected 1 upsert call, got %d", len(fq.upsertAgentInstanceCalls))
	}
	up := fq.upsertAgentInstanceCalls[0]
	if up.hostname != "my-host" {
		t.Fatalf("instance hostname = %q, want 'my-host'", up.hostname)
	}
	if up.pid != 12345 {
		t.Fatalf("instance pid = %d, want 12345", up.pid)
	}

	// ---- Verify instance metadata excludes hostname/pid/process_start_time ----
	var instMeta map[string]interface{}
	if err := json.Unmarshal(up.metadata, &instMeta); err != nil {
		t.Fatalf("unmarshal instance metadata: %v", err)
	}
	if _, ok := instMeta["hostname"]; ok {
		t.Fatal("instance metadata should not contain hostname")
	}
	if _, ok := instMeta["pid"]; ok {
		t.Fatal("instance metadata should not contain pid")
	}
	if _, ok := instMeta["process_start_time"]; ok {
		t.Fatal("instance metadata should not contain process_start_time")
	}
	// Instance metadata SHOULD have other fields
	if instMeta["contract_version"] != "agent-runtime-v1" {
		t.Fatalf("instance metadata contract_version = %v", instMeta["contract_version"])
	}
	if instMeta["framework"] != "custom-python" {
		t.Fatalf("instance metadata framework = %v", instMeta["framework"])
	}

	// ---- Verify original payload is NOT mutated ----
	if _, ok := payload["hostname"]; !ok {
		t.Fatal("original payload was mutated: hostname missing")
	}
	if _, ok := payload["pid"]; !ok {
		t.Fatal("original payload was mutated: pid missing")
	}
	if _, ok := payload["process_start_time"]; !ok {
		t.Fatal("original payload was mutated: process_start_time missing")
	}
}

func TestRecordHeartbeatNilPayload(t *testing.T) {
	fq := &fakeQuerier{}
	svc := NewService(fq)
	ctx := context.Background()

	err := svc.RecordHeartbeat(ctx, "agent-1", "proj-1", "inst-1", "inst-1", "online", nil)
	if err != nil {
		t.Fatalf("RecordHeartbeat: %v", err)
	}

	if len(fq.createHeartbeatCalls) != 1 {
		t.Fatalf("expected 1 heartbeat call, got %d", len(fq.createHeartbeatCalls))
	}
	if len(fq.upsertAgentInstanceCalls) != 1 {
		t.Fatalf("expected 1 upsert call, got %d", len(fq.upsertAgentInstanceCalls))
	}

	// Verify instance metadata is not nil
	up := fq.upsertAgentInstanceCalls[0]
	if up.hostname != "" {
		t.Fatalf("expected empty hostname, got %q", up.hostname)
	}
	if up.pid != 0 {
		t.Fatalf("expected pid 0, got %d", up.pid)
	}
}
