package benchmarks

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// WorkerRegistration is the bounded liveness contract between AgentScope and
// an external benchmark worker. A registered worker claims durable eval_jobs
// through the existing runner API; registration never starts a process on the
// AgentScope server.
type WorkerRegistration struct {
	ProjectID      string   `json:"project_id"`
	Name           string   `json:"name"`
	ExecutionKinds []string `json:"execution_kinds"`
	Capacity       int      `json:"capacity"`
	Version        string   `json:"version,omitempty"`
}

// Worker is the non-sensitive read model for a currently registered worker.
type Worker struct {
	ProjectID      string    `json:"project_id"`
	Name           string    `json:"name"`
	ExecutionKinds []string  `json:"execution_kinds"`
	Capacity       int       `json:"capacity"`
	Version        string    `json:"version,omitempty"`
	LastSeenAt     time.Time `json:"last_seen_at"`
	ExpiresAt      time.Time `json:"expires_at"`
}

// WorkerAvailability summarizes whether an external, resident worker can
// claim a particular execution kind. It intentionally says nothing about a
// local subprocess because the control plane never launches one.
type WorkerAvailability struct {
	ProjectID          string   `json:"project_id,omitempty"`
	ExecutionKind      string   `json:"execution_kind"`
	Available          bool     `json:"available"`
	State              string   `json:"state"`
	Reason             string   `json:"reason,omitempty"`
	Blockers           []string `json:"blockers,omitempty"`
	RegisteredWorkers  int      `json:"registered_workers"`
	MatchingWorkers    int      `json:"matching_workers"`
	AdvertisedCapacity int      `json:"advertised_capacity"`
}

// WorkerRegistry is deliberately independent of durable job persistence.
// eval_jobs remain the source of truth for queued work while implementations
// of this interface only track short-lived worker liveness and capability.
type WorkerRegistry interface {
	Register(ctx context.Context, registration WorkerRegistration) (Worker, error)
	List(ctx context.Context, projectID string) ([]Worker, error)
	Availability(ctx context.Context, projectID, executionKind string) (WorkerAvailability, error)
}

type workerRegistryOptions struct {
	now func() time.Time
}

// WorkerRegistryOption configures a worker registry. It exists primarily
// to make expiry behavior deterministic in tests.
type WorkerRegistryOption func(*workerRegistryOptions)

// WithWorkerRegistryClock overrides the registry clock.
func WithWorkerRegistryClock(now func() time.Time) WorkerRegistryOption {
	return func(options *workerRegistryOptions) {
		options.now = now
	}
}

// InMemoryWorkerRegistry is a process-local read model. Workers must refresh
// registration before TTL expiry. The interface permits replacing this with a
// durable/distributed registry without changing HTTP or launch semantics.
type InMemoryWorkerRegistry struct {
	mu      sync.Mutex
	ttl     time.Duration
	now     func() time.Time
	workers map[string]Worker
}

// NewInMemoryWorkerRegistry creates a registry with the supplied liveness TTL.
func NewInMemoryWorkerRegistry(ttl time.Duration, options ...WorkerRegistryOption) *InMemoryWorkerRegistry {
	if ttl <= 0 {
		ttl = 45 * time.Second
	}
	configured := workerRegistryOptions{now: time.Now}
	for _, option := range options {
		option(&configured)
	}
	return &InMemoryWorkerRegistry{
		ttl:     ttl,
		now:     configured.now,
		workers: make(map[string]Worker),
	}
}

func (r *InMemoryWorkerRegistry) Register(_ context.Context, registration WorkerRegistration) (Worker, error) {
	registration, kinds, err := normalizeWorkerRegistration(registration)
	if err != nil {
		return Worker{}, err
	}
	now := r.now().UTC()
	worker := Worker{
		ProjectID:      registration.ProjectID,
		Name:           registration.Name,
		ExecutionKinds: kinds,
		Capacity:       registration.Capacity,
		Version:        registration.Version,
		LastSeenAt:     now,
		ExpiresAt:      now.Add(r.ttl),
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(now)
	r.workers[workerKey(worker.ProjectID, worker.Name)] = worker
	return cloneWorker(worker), nil
}

func (r *InMemoryWorkerRegistry) List(_ context.Context, projectID string) ([]Worker, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, errors.New("project_id is required")
	}
	now := r.now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(now)
	workers := make([]Worker, 0)
	for _, worker := range r.workers {
		if worker.ProjectID == projectID {
			workers = append(workers, cloneWorker(worker))
		}
	}
	sort.Slice(workers, func(i, j int) bool { return workers[i].Name < workers[j].Name })
	return workers, nil
}

func (r *InMemoryWorkerRegistry) Availability(_ context.Context, projectID, executionKind string) (WorkerAvailability, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return WorkerAvailability{}, errors.New("project_id is required")
	}
	if err := ValidateExecutionKind(executionKind); err != nil {
		return WorkerAvailability{}, err
	}

	availability := WorkerAvailability{ProjectID: projectID, ExecutionKind: executionKind, State: "unavailable"}
	now := r.now().UTC()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pruneLocked(now)
	for _, worker := range r.workers {
		if worker.ProjectID != projectID {
			continue
		}
		availability.RegisteredWorkers++
		if supportsExecutionKind(worker.ExecutionKinds, executionKind) {
			availability.MatchingWorkers++
			availability.AdvertisedCapacity += worker.Capacity
		}
	}
	if availability.MatchingWorkers == 0 {
		availability.Reason = fmt.Sprintf("No active benchmark worker supports execution kind %q.", executionKind)
		availability.Blockers = []string{availability.Reason}
		return availability, nil
	}
	availability.Available = true
	availability.State = "ready"
	return availability, nil
}

func (r *InMemoryWorkerRegistry) pruneLocked(now time.Time) {
	for key, worker := range r.workers {
		if !worker.ExpiresAt.After(now) {
			delete(r.workers, key)
		}
	}
}

func workerKey(projectID, name string) string { return projectID + "\x00" + name }

func supportsExecutionKind(kinds []string, wanted string) bool {
	for _, kind := range kinds {
		if kind == wanted {
			return true
		}
	}
	return false
}

func cloneWorker(worker Worker) Worker {
	worker.ExecutionKinds = append([]string(nil), worker.ExecutionKinds...)
	return worker
}

// normalizeWorkerRegistration centralizes the public registration contract so
// in-memory and durable registries have identical validation and normalization
// semantics.
func normalizeWorkerRegistration(registration WorkerRegistration) (WorkerRegistration, []string, error) {
	registration.ProjectID = strings.TrimSpace(registration.ProjectID)
	registration.Name = strings.TrimSpace(registration.Name)
	registration.Version = strings.TrimSpace(registration.Version)
	if registration.ProjectID == "" {
		return WorkerRegistration{}, nil, errors.New("project_id is required")
	}
	if !identifierPattern.MatchString(registration.Name) {
		return WorkerRegistration{}, nil, errors.New("worker name must be a lowercase identifier")
	}
	kinds, err := NormalizeCapabilities(registration.ExecutionKinds)
	if err != nil {
		return WorkerRegistration{}, nil, fmt.Errorf("execution_kinds: %w", err)
	}
	if len(kinds) == 0 {
		return WorkerRegistration{}, nil, errors.New("at least one execution_kind is required")
	}
	for _, kind := range kinds {
		if err := ValidateExecutionKind(kind); err != nil {
			return WorkerRegistration{}, nil, err
		}
	}
	if registration.Capacity <= 0 {
		registration.Capacity = 1
	}
	if registration.Capacity > 10_000 {
		return WorkerRegistration{}, nil, errors.New("capacity must not exceed 10000")
	}
	return registration, kinds, nil
}
