package benchmarks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresWorkerRegistry stores the short-lived worker liveness contract in
// PostgreSQL. It deliberately persists registrations across AgentScope server
// restarts, but every read filters by expires_at so a stopped worker cannot
// remain available after its TTL.
type PostgresWorkerRegistry struct {
	pool *pgxpool.Pool
	ttl  time.Duration
	now  func() time.Time
}

// NewPostgresWorkerRegistry creates a durable registry with the supplied
// liveness TTL. A non-positive TTL retains the existing 45-second default.
func NewPostgresWorkerRegistry(pool *pgxpool.Pool, ttl time.Duration, options ...WorkerRegistryOption) *PostgresWorkerRegistry {
	if ttl <= 0 {
		ttl = 45 * time.Second
	}
	configured := workerRegistryOptions{now: time.Now}
	for _, option := range options {
		option(&configured)
	}
	return &PostgresWorkerRegistry{pool: pool, ttl: ttl, now: configured.now}
}

// Register upserts a project-scoped worker heartbeat. The primary key is the
// project and worker name, so refreshes update capability, capacity, version,
// and expiry atomically without creating duplicate workers.
func (r *PostgresWorkerRegistry) Register(ctx context.Context, registration WorkerRegistration) (Worker, error) {
	if r == nil || r.pool == nil {
		return Worker{}, errors.New("benchmark worker registry is not configured")
	}
	registration, kinds, err := normalizeWorkerRegistration(registration)
	if err != nil {
		return Worker{}, err
	}
	projectID, err := workerProjectUUID(registration.ProjectID)
	if err != nil {
		return Worker{}, err
	}
	encodedKinds, err := json.Marshal(kinds)
	if err != nil {
		return Worker{}, fmt.Errorf("marshal execution kinds: %w", err)
	}
	now := r.now().UTC()
	expiresAt := now.Add(r.ttl)
	row := r.pool.QueryRow(ctx, `
		INSERT INTO benchmark_workers (
			project_id, worker_name, execution_kinds, capacity, version,
			last_seen_at, expires_at
		) VALUES ($1, $2, $3::jsonb, $4, $5, $6, $7)
		ON CONFLICT (project_id, worker_name) DO UPDATE
		SET execution_kinds = EXCLUDED.execution_kinds,
			capacity = EXCLUDED.capacity,
			version = EXCLUDED.version,
			last_seen_at = EXCLUDED.last_seen_at,
			expires_at = EXCLUDED.expires_at
		RETURNING project_id, worker_name, execution_kinds, capacity, version,
			last_seen_at, expires_at`,
		projectID, registration.Name, encodedKinds, registration.Capacity,
		registration.Version, now, expiresAt,
	)
	worker, err := scanWorker(row)
	if err != nil {
		return Worker{}, fmt.Errorf("register benchmark worker: %w", err)
	}
	return worker, nil
}

// List returns only currently-live workers for one project. Expired rows may
// remain as storage cleanup candidates, but never affect the public read model
// or scheduling availability.
func (r *PostgresWorkerRegistry) List(ctx context.Context, projectID string) ([]Worker, error) {
	if r == nil || r.pool == nil {
		return nil, errors.New("benchmark worker registry is not configured")
	}
	projectUUID, err := workerProjectUUID(projectID)
	if err != nil {
		return nil, err
	}
	now := r.now().UTC()
	rows, err := r.pool.Query(ctx, `
		SELECT project_id, worker_name, execution_kinds, capacity, version,
			last_seen_at, expires_at
		FROM benchmark_workers
		WHERE project_id = $1 AND expires_at > $2
		ORDER BY worker_name ASC`, projectUUID, now)
	if err != nil {
		return nil, fmt.Errorf("list benchmark workers: %w", err)
	}
	defer rows.Close()
	workers := make([]Worker, 0)
	for rows.Next() {
		worker, scanErr := scanWorker(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("scan benchmark worker: %w", scanErr)
		}
		workers = append(workers, worker)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate benchmark workers: %w", err)
	}
	return workers, nil
}

// Availability computes project+execution-kind availability from the same
// unexpired rows exposed by List. It does not rely on process-local state, so
// a newly started AgentScope server observes still-live workers immediately.
func (r *PostgresWorkerRegistry) Availability(ctx context.Context, projectID, executionKind string) (WorkerAvailability, error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return WorkerAvailability{}, errors.New("project_id is required")
	}
	if err := ValidateExecutionKind(executionKind); err != nil {
		return WorkerAvailability{}, err
	}
	workers, err := r.List(ctx, projectID)
	if err != nil {
		return WorkerAvailability{}, err
	}
	availability := WorkerAvailability{
		ProjectID:     projectID,
		ExecutionKind: executionKind,
		State:         "unavailable",
	}
	for _, worker := range workers {
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

func workerProjectUUID(projectID string) (pgtype.UUID, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(projectID))
	if err != nil || parsed == uuid.Nil {
		return pgtype.UUID{}, errors.New("project_id must be a valid UUID")
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}, nil
}

type workerRow interface {
	Scan(dest ...any) error
}

func scanWorker(row workerRow) (Worker, error) {
	var (
		projectID      pgtype.UUID
		name           string
		executionKinds []byte
		worker         Worker
	)
	if err := row.Scan(
		&projectID,
		&name,
		&executionKinds,
		&worker.Capacity,
		&worker.Version,
		&worker.LastSeenAt,
		&worker.ExpiresAt,
	); err != nil {
		return Worker{}, err
	}
	if !projectID.Valid {
		return Worker{}, errors.New("stored benchmark worker has no project_id")
	}
	if err := json.Unmarshal(executionKinds, &worker.ExecutionKinds); err != nil {
		return Worker{}, errors.New("stored benchmark worker execution_kinds are invalid")
	}
	normalizedKinds, err := NormalizeCapabilities(worker.ExecutionKinds)
	if err != nil || len(normalizedKinds) == 0 {
		return Worker{}, errors.New("stored benchmark worker execution_kinds are invalid")
	}
	for _, kind := range normalizedKinds {
		if err := ValidateExecutionKind(kind); err != nil {
			return Worker{}, errors.New("stored benchmark worker execution_kinds are invalid")
		}
	}
	worker.ProjectID = uuid.UUID(projectID.Bytes).String()
	worker.Name = name
	worker.ExecutionKinds = normalizedKinds
	worker.LastSeenAt = worker.LastSeenAt.UTC()
	worker.ExpiresAt = worker.ExpiresAt.UTC()
	return worker, nil
}

var _ WorkerRegistry = (*PostgresWorkerRegistry)(nil)
