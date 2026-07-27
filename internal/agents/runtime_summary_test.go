package agents_test

import (
	"testing"
	"time"

	"AgentScope/internal/agents"
)

func TestSummarizeRuntime(t *testing.T) {
	now := time.Date(2026, time.July, 26, 12, 0, 0, 0, time.UTC)
	instances := []agents.AgentInstance{
		{InstanceID: "online", LastHeartbeatAt: now.Add(-89 * time.Second)},
		{InstanceID: "offline", LastHeartbeatAt: now.Add(-91 * time.Second)},
	}

	summary := agents.SummarizeRuntime(instances, now)
	if summary.TotalInstanceCount != 2 {
		t.Fatalf("TotalInstanceCount = %d, want 2", summary.TotalInstanceCount)
	}
	if summary.OnlineInstanceCount != 1 {
		t.Errorf("OnlineInstanceCount = %d, want 1", summary.OnlineInstanceCount)
	}
	if summary.OfflineInstanceCount != 1 {
		t.Errorf("OfflineInstanceCount = %d, want 1", summary.OfflineInstanceCount)
	}
	if summary.ConnectionStatus != "online" {
		t.Errorf("ConnectionStatus = %q, want online", summary.ConnectionStatus)
	}
	if summary.LastHeartbeatAt == nil || !summary.LastHeartbeatAt.Equal(now.Add(-89*time.Second)) {
		t.Errorf("LastHeartbeatAt = %v, want %v", summary.LastHeartbeatAt, now.Add(-89*time.Second))
	}
}

func TestConnectionStatus(t *testing.T) {
	now := time.Date(2026, time.July, 26, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		lastHeartbeat time.Time
		want          string
	}{
		{name: "at threshold is online", lastHeartbeat: now.Add(-agents.AgentInstanceOnlineWindow), want: "online"},
		{name: "after threshold is offline", lastHeartbeat: now.Add(-agents.AgentInstanceOnlineWindow - time.Second), want: "offline"},
		{name: "missing heartbeat is unknown", want: "unknown"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := agents.ConnectionStatus(test.lastHeartbeat, now); got != test.want {
				t.Errorf("ConnectionStatus() = %q, want %q", got, test.want)
			}
		})
	}
}
