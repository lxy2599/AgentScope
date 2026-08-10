package benchmarks

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestInMemoryWorkerRegistryRegistersAvailabilityAndExpires(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	registry := NewInMemoryWorkerRegistry(30*time.Second, WithWorkerRegistryClock(func() time.Time { return now }))

	availability, err := registry.Availability(context.Background(), "project-1", "harbor")
	if err != nil {
		t.Fatalf("Availability() error = %v", err)
	}
	if availability.Available || availability.State != "unavailable" || len(availability.Blockers) != 1 {
		t.Fatalf("initial availability = %#v", availability)
	}

	worker, err := registry.Register(context.Background(), WorkerRegistration{
		ProjectID: "project-1", Name: "harbor_worker", ExecutionKinds: []string{"harbor", "tau2"}, Capacity: 2, Version: "v1",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if worker.LastSeenAt != now || worker.ExpiresAt != now.Add(30*time.Second) {
		t.Fatalf("worker timestamps = %#v", worker)
	}

	availability, err = registry.Availability(context.Background(), "project-1", "harbor")
	if err != nil {
		t.Fatalf("Availability() error = %v", err)
	}
	if !availability.Available || availability.State != "ready" || availability.MatchingWorkers != 1 || availability.AdvertisedCapacity != 2 {
		t.Fatalf("available = %#v", availability)
	}

	now = now.Add(30 * time.Second)
	availability, err = registry.Availability(context.Background(), "project-1", "harbor")
	if err != nil {
		t.Fatalf("Availability() error = %v", err)
	}
	if availability.Available || availability.RegisteredWorkers != 0 {
		t.Fatalf("expired availability = %#v", availability)
	}
}

func TestInMemoryWorkerRegistryRejectsInvalidRegistration(t *testing.T) {
	registry := NewInMemoryWorkerRegistry(time.Minute)
	_, err := registry.Register(context.Background(), WorkerRegistration{ProjectID: "project-1", Name: "Bad Name", ExecutionKinds: []string{"harbor"}})
	if err == nil || !strings.Contains(err.Error(), "worker name") {
		t.Fatalf("Register() error = %v", err)
	}
	_, err = registry.Register(context.Background(), WorkerRegistration{ProjectID: "project-1", Name: "worker", ExecutionKinds: nil})
	if err == nil || !strings.Contains(err.Error(), "execution_kind") {
		t.Fatalf("Register() error = %v", err)
	}
}
