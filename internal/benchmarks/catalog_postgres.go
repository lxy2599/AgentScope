package benchmarks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"AgentScope/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresCatalogStore persists immutable catalog revision payloads and a
// per-project current-revision pointer. The revision table is append-only from
// this component's perspective: syncing a known revision with a different
// payload is rejected rather than rewriting a value an experiment may have
// already pinned.
type PostgresCatalogStore struct {
	pool    *pgxpool.Pool
	queries *store.Queries
}

func NewPostgresCatalogStore(pool *pgxpool.Pool) *PostgresCatalogStore {
	return &PostgresCatalogStore{pool: pool, queries: store.New(pool)}
}

// SyncCatalog validates before serializing or writing anything. In particular,
// ValidateCatalog rejects secret-bearing configurations so they cannot enter
// the durable JSONB payload through this API.
func (s *PostgresCatalogStore) SyncCatalog(ctx context.Context, projectID string, catalog Catalog) (Catalog, error) {
	if s == nil || s.pool == nil || s.queries == nil {
		return Catalog{}, errors.New("benchmark catalog store is not configured")
	}
	projectUUID, err := catalogProjectUUID(projectID)
	if err != nil {
		return Catalog{}, err
	}
	normalized, err := ValidateCatalog(catalog)
	if err != nil {
		return Catalog{}, err
	}
	payload, err := json.Marshal(normalized)
	if err != nil {
		return Catalog{}, fmt.Errorf("marshal validated benchmark catalog: %w", err)
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return Catalog{}, fmt.Errorf("begin benchmark catalog sync: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	queries := s.queries.WithTx(tx)

	_, err = queries.CreateBenchmarkCatalogRevision(ctx, store.CreateBenchmarkCatalogRevisionParams{
		ProjectID: projectUUID,
		Revision:  normalized.Revision,
		Payload:   payload,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return Catalog{}, fmt.Errorf("create benchmark catalog revision: %w", err)
		}
		existing, getErr := queries.GetBenchmarkCatalogRevision(ctx, store.GetBenchmarkCatalogRevisionParams{
			ProjectID: projectUUID,
			Revision:  normalized.Revision,
		})
		if getErr != nil {
			return Catalog{}, fmt.Errorf("read existing benchmark catalog revision: %w", getErr)
		}
		if !sameCatalogPayload(existing.Payload, payload) {
			return Catalog{}, errors.New("benchmark catalog revision already exists with a different immutable payload")
		}
	}
	if _, err := queries.UpsertBenchmarkCatalogHead(ctx, store.UpsertBenchmarkCatalogHeadParams{
		ProjectID: projectUUID,
		Revision:  normalized.Revision,
	}); err != nil {
		return Catalog{}, fmt.Errorf("advance benchmark catalog head: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Catalog{}, fmt.Errorf("commit benchmark catalog sync: %w", err)
	}
	return cloneCatalog(normalized), nil
}

// ReadCatalog returns the current catalog head for exactly one project. An
// unsynchronized project deliberately retains the existing API behavior of an
// empty, "unconfigured" catalog rather than returning a not-found error.
func (s *PostgresCatalogStore) ReadCatalog(ctx context.Context, projectID string) (Catalog, error) {
	if s == nil || s.queries == nil {
		return Catalog{}, errors.New("benchmark catalog store is not configured")
	}
	projectUUID, err := catalogProjectUUID(projectID)
	if err != nil {
		return Catalog{}, err
	}
	payload, err := s.queries.GetCurrentBenchmarkCatalog(ctx, projectUUID)
	if errors.Is(err, pgx.ErrNoRows) {
		return unconfiguredCatalog(), nil
	}
	if err != nil {
		return Catalog{}, fmt.Errorf("read benchmark catalog head: %w", err)
	}
	var catalog Catalog
	if err := json.Unmarshal(payload, &catalog); err != nil {
		return Catalog{}, errors.New("stored benchmark catalog payload is invalid")
	}
	normalized, err := ValidateCatalog(catalog)
	if err != nil {
		return Catalog{}, errors.New("stored benchmark catalog payload is invalid")
	}
	return normalized, nil
}

func catalogProjectUUID(projectID string) (pgtype.UUID, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(projectID))
	if err != nil || parsed == uuid.Nil {
		return pgtype.UUID{}, errors.New("project_id must be a valid UUID")
	}
	return pgtype.UUID{Bytes: parsed, Valid: true}, nil
}

// sameCatalogPayload compares JSON semantically so PostgreSQL JSONB's object
// key reordering does not cause a false immutable-revision conflict. UseNumber
// avoids lossy float conversion for large pinned seeds or numeric settings.
func sameCatalogPayload(left, right []byte) bool {
	var leftValue, rightValue any
	if !decodeJSONValue(left, &leftValue) || !decodeJSONValue(right, &rightValue) {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func decodeJSONValue(raw []byte, destination *any) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return false
	}
	return decoder.Decode(&struct{}{}) == io.EOF
}
