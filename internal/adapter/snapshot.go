package adapter

import (
	"context"
	"errors"
	"fmt"

	"AgentScope/internal/snapshots"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5"
)

type SnapshotRepository struct {
	q *store.Queries
}

var _ snapshots.Repository = (*SnapshotRepository)(nil)

func NewSnapshotRepository(q *store.Queries) *SnapshotRepository {
	return &SnapshotRepository{q: q}
}

func (r *SnapshotRepository) Insert(
	ctx context.Context,
	snapshot snapshots.Snapshot,
) (snapshots.Snapshot, bool, error) {
	row, err := r.q.CreateRunSnapshot(ctx, store.CreateRunSnapshotParams{
		RunID:            parsePGUUID(snapshot.RunID),
		ProjectID:        parsePGUUID(snapshot.ProjectID),
		SchemaVersion:    snapshot.SchemaVersion,
		CanonicalPayload: snapshot.CanonicalPayload,
		PayloadHash:      snapshot.PayloadHash,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return snapshots.Snapshot{}, false, nil
	}
	if err != nil {
		return snapshots.Snapshot{}, false, fmt.Errorf("create run snapshot: %w", err)
	}
	return snapshotFromStore(row), true, nil
}

func (r *SnapshotRepository) GetByRun(
	ctx context.Context,
	runID, projectID string,
) (snapshots.Snapshot, error) {
	row, err := r.q.GetProjectRunSnapshot(ctx, store.GetProjectRunSnapshotParams{
		RunID:     parsePGUUID(runID),
		ProjectID: parsePGUUID(projectID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return snapshots.Snapshot{}, snapshots.ErrUnavailable
	}
	if err != nil {
		return snapshots.Snapshot{}, fmt.Errorf("get run snapshot: %w", err)
	}
	return snapshotFromStore(row), nil
}

func snapshotFromStore(row store.RunSnapshot) snapshots.Snapshot {
	return snapshots.Snapshot{
		ID:               UUIDToString(row.ID.Bytes),
		RunID:            UUIDToString(row.RunID.Bytes),
		ProjectID:        UUIDToString(row.ProjectID.Bytes),
		SchemaVersion:    row.SchemaVersion,
		CanonicalPayload: row.CanonicalPayload,
		PayloadHash:      row.PayloadHash,
		CreatedAt:        row.CreatedAt.Time,
	}
}
