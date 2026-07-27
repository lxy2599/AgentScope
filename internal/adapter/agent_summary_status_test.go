package adapter

import (
	"testing"

	"AgentScope/internal/store"
)

func TestDeriveStatusSummaryPrefersOfflineWhenNoInstancesAreOnline(t *testing.T) {
	status := deriveStatusSummary(store.ListAgentSummariesByProjectRow{
		InstanceCount:       1,
		OnlineInstanceCount: 0,
		ActiveRunCount:      1,
	})
	if status != "offline" {
		t.Errorf("deriveStatusSummary() = %q, want offline", status)
	}
}
