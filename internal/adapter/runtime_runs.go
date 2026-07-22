package adapter

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"AgentScope/internal/runs"
	"AgentScope/internal/store"
)

// RuntimeRunQuerier implements runs.RuntimeQuerier using store.Queries.
type RuntimeRunQuerier struct {
	q *store.Queries
}

var _ runs.RuntimeQuerier = (*RuntimeRunQuerier)(nil)

// NewRuntimeRunQuerier creates a new RuntimeRunQuerier.
func NewRuntimeRunQuerier(q *store.Queries) *RuntimeRunQuerier {
	return &RuntimeRunQuerier{q: q}
}

func (r *RuntimeRunQuerier) CreateRun(ctx context.Context, projectID, source, status, traceID, agentName, agentVersion string, metadata []byte) (runs.Run, error) {
	storeRun, err := r.q.CreateRun(ctx, store.CreateRunParams{
		ProjectID:    parsePGUUID(projectID),
		Source:       source,
		Status:       status,
		TraceID:      pgText(traceID),
		AgentName:    agentName,
		AgentVersion: pgText(agentVersion),
		Metadata:     metadata,
	})
	if err != nil {
		return runs.Run{}, err
	}
	return storeRunToDomain(storeRun), nil
}

func (r *RuntimeRunQuerier) GetRunByID(ctx context.Context, runID string) (runs.Run, error) {
	storeRun, err := r.q.GetRunByID(ctx, parsePGUUID(runID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return runs.Run{}, fmt.Errorf("run not found: %s", runID)
		}
		return runs.Run{}, err
	}
	return storeRunToDomain(storeRun), nil
}

func (r *RuntimeRunQuerier) UpdateRunStatus(ctx context.Context, runID, projectID, status, errorStr string) (runs.Run, error) {
	var errorPg pgtype.Text
	if errorStr != "" {
		errorPg = pgText(errorStr)
	}
	storeRun, err := r.q.UpdateRunStatus(ctx, store.UpdateRunStatusParams{
		ID:        parsePGUUID(runID),
		ProjectID: parsePGUUID(projectID),
		Status:    status,
		Error:     errorPg,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return runs.Run{}, fmt.Errorf("run not found: %s", runID)
		}
		return runs.Run{}, err
	}
	return storeRunToDomain(storeRun), nil
}

func (r *RuntimeRunQuerier) StartRuntimeRun(ctx context.Context, projectID, status, traceID, agentName, inputSummary string, metadata []byte) (runs.Run, error) {
	storeRun, err := r.q.StartRuntimeRun(ctx, store.StartRuntimeRunParams{
		ProjectID:    parsePGUUID(projectID),
		Status:       status,
		TraceID:      pgText(traceID),
		AgentName:    agentName,
		InputSummary: pgText(inputSummary),
		Metadata:     metadata,
	})
	if err != nil {
		return runs.Run{}, fmt.Errorf("start runtime run: %w", err)
	}
	return storeRunToDomain(storeRun), nil
}

func (r *RuntimeRunQuerier) CompleteRuntimeRun(ctx context.Context, runID, projectID, outputSummary string, latencyMs int64) (runs.Run, error) {
	storeRun, err := r.q.CompleteRuntimeRun(ctx, store.CompleteRuntimeRunParams{
		ID:            parsePGUUID(runID),
		ProjectID:     parsePGUUID(projectID),
		OutputSummary: pgText(outputSummary),
		LatencyMs:     pgtype.Int8{Int64: latencyMs, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return runs.Run{}, fmt.Errorf("run not found: %s", runID)
		}
		return runs.Run{}, fmt.Errorf("complete run: %w", err)
	}
	return storeRunToDomain(storeRun), nil
}

func (r *RuntimeRunQuerier) FailRuntimeRun(ctx context.Context, runID, projectID, errorStr string, latencyMs int64) (runs.Run, error) {
	var errorPg pgtype.Text
	if errorStr != "" {
		errorPg = pgText(errorStr)
	}
	var latencyPg pgtype.Int8
	if latencyMs > 0 {
		latencyPg = pgtype.Int8{Int64: latencyMs, Valid: true}
	}
	storeRun, err := r.q.FailRuntimeRun(ctx, store.FailRuntimeRunParams{
		ID:        parsePGUUID(runID),
		ProjectID: parsePGUUID(projectID),
		Error:     errorPg,
		LatencyMs: latencyPg,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return runs.Run{}, fmt.Errorf("run not found: %s", runID)
		}
		return runs.Run{}, fmt.Errorf("fail run: %w", err)
	}
	return storeRunToDomain(storeRun), nil
}

func (r *RuntimeRunQuerier) CreateRunStateEvent(ctx context.Context, projectID, runID string, agentID, agentInstanceID *string, fromStatus, toStatus, message string, attributes []byte) (runs.RunStateEvent, error) {
	var agentIDPg pgtype.UUID
	if agentID != nil && *agentID != "" {
		agentIDPg = parsePGUUID(*agentID)
	}
	var instanceIDPg pgtype.UUID
	if agentInstanceID != nil && *agentInstanceID != "" {
		instanceIDPg = parsePGUUID(*agentInstanceID)
	}
	evt, err := r.q.CreateRunStateEvent(ctx, store.CreateRunStateEventParams{
		ProjectID:       parsePGUUID(projectID),
		RunID:           parsePGUUID(runID),
		AgentID:         agentIDPg,
		AgentInstanceID: instanceIDPg,
		FromStatus:      pgText(fromStatus),
		ToStatus:        toStatus,
		Message:         pgText(message),
		Attributes:      attributes,
	})
	if err != nil {
		return runs.RunStateEvent{}, err
	}
	return runs.RunStateEvent{
		ID:         UUIDToString(evt.ID.Bytes),
		RunID:      UUIDToString(evt.RunID.Bytes),
		FromStatus: strOrZero(evt.FromStatus),
		ToStatus:   evt.ToStatus,
		Message:    strOrZero(evt.Message),
		CreatedAt:  evt.CreatedAt.Time,
	}, nil
}

func (r *RuntimeRunQuerier) ListRunStateEvents(ctx context.Context, runID string) ([]runs.RunStateEvent, error) {
	storeEvents, err := r.q.ListRunStateEventsByRun(ctx, parsePGUUID(runID))
	if err != nil {
		return nil, err
	}
	result := make([]runs.RunStateEvent, 0, len(storeEvents))
	for _, e := range storeEvents {
		result = append(result, runs.RunStateEvent{
			ID:         UUIDToString(e.ID.Bytes),
			RunID:      UUIDToString(e.RunID.Bytes),
			FromStatus: strOrZero(e.FromStatus),
			ToStatus:   e.ToStatus,
			Message:    strOrZero(e.Message),
			CreatedAt:  e.CreatedAt.Time,
		})
	}
	return result, nil
}

func (r *RuntimeRunQuerier) ListActiveRunsByProject(ctx context.Context, projectID string, limit, offset int) ([]runs.ActiveRun, error) {
	rows, err := r.q.ListActiveRunsByProject(ctx, store.ListActiveRunsByProjectParams{
		ProjectID: parsePGUUID(projectID),
		Limit:     int32(limit),
		Offset:    int32(offset),
	})
	if err != nil {
		return nil, err
	}
	result := make([]runs.ActiveRun, 0, len(rows))
	for _, row := range rows {
		ar := runs.ActiveRun{
			ID:        UUIDToString(row.ID.Bytes),
			ProjectID: UUIDToString(row.ProjectID.Bytes),
			Status:    row.Status,
			TraceID:   strOrZero(row.TraceID),
			CreatedAt: row.CreatedAt.Time,
			UpdatedAt: row.UpdatedAt.Time,
		}
		if row.InputSummary.Valid {
			ar.InputSummary = row.InputSummary.String
		}
		if row.AgentID.Valid {
			ar.AgentID = UUIDToString(row.AgentID.Bytes)
		}
		if row.AgentName.Valid {
			ar.AgentName = row.AgentName.String
		}
		if row.AgentInstanceID.Valid {
			ar.AgentInstanceID = UUIDToString(row.AgentInstanceID.Bytes)
		}
		if row.AgentInstanceName.Valid {
			ar.AgentInstanceName = row.AgentInstanceName.String
		}
		if row.LastStateEventAt.Valid {
			ar.LastStateEventAt = &row.LastStateEventAt.Time
		}
		if row.LastState != "" {
			ar.LastState = row.LastState
		}
		result = append(result, ar)
	}
	return result, nil
}

func (r *RuntimeRunQuerier) GetRunRuntimeDetail(ctx context.Context, runID, projectID string) (*runs.RuntimeRunDetail, error) {
	row, err := r.q.GetRunRuntimeDetail(ctx, store.GetRunRuntimeDetailParams{
		ID:        parsePGUUID(runID),
		ProjectID: parsePGUUID(projectID),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("run not found: %s", runID)
		}
		return nil, err
	}

	detail := &runs.RuntimeRunDetail{
		RuntimeRun: runs.RuntimeRun{
			ID:        UUIDToString(row.ID.Bytes),
			ProjectID: UUIDToString(row.ProjectID.Bytes),
			Status:    row.Status,
			TraceID:   strOrZero(row.TraceID),
			CreatedAt: row.CreatedAt.Time,
			UpdatedAt: row.UpdatedAt.Time,
		},
	}
	if row.InputSummary.Valid {
		detail.Input = row.InputSummary.String
	}
	if row.AgentID.Valid {
		detail.AgentID = UUIDToString(row.AgentID.Bytes)
	}
	if row.AgentName.Valid {
		detail.AgentName = row.AgentName.String
	}
	if row.AgentInstanceID.Valid {
		detail.AgentInstanceID = UUIDToString(row.AgentInstanceID.Bytes)
	}
	if row.AgentInstanceName.Valid {
		detail.AgentInstanceName = row.AgentInstanceName.String
	}
	return detail, nil
}

func (r *RuntimeRunQuerier) SetAgentInstanceActiveRun(ctx context.Context, projectID, agentID, instanceID, runID string) (string, error) {
	dbID, err := r.q.SetAgentInstanceActiveRunByExternalID(ctx, store.SetAgentInstanceActiveRunByExternalIDParams{
		ProjectID:   parsePGUUID(projectID),
		AgentID:     parsePGUUID(agentID),
		InstanceID:  instanceID,
		ActiveRunID: pgtype.UUID{Bytes: parsePGUUID(runID).Bytes, Valid: true},
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("agent instance not found: project=%s agent=%s instance=%s", projectID, agentID, instanceID)
		}
		return "", fmt.Errorf("set agent instance active run: %w", err)
	}
	return UUIDToString(dbID.Bytes), nil
}

func (r *RuntimeRunQuerier) ClearAgentInstanceActiveRun(ctx context.Context, projectID, agentID, instanceID string) (string, error) {
	dbID, err := r.q.ClearAgentInstanceActiveRunByExternalID(ctx, store.ClearAgentInstanceActiveRunByExternalIDParams{
		ProjectID:  parsePGUUID(projectID),
		AgentID:    parsePGUUID(agentID),
		InstanceID: instanceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("agent instance not found: project=%s agent=%s instance=%s", projectID, agentID, instanceID)
		}
		return "", fmt.Errorf("clear agent instance active run: %w", err)
	}
	return UUIDToString(dbID.Bytes), nil
}

func (r *RuntimeRunQuerier) GetAgentInstanceDBID(ctx context.Context, projectID, agentID, instanceID string) (string, error) {
	dbID, err := r.q.GetAgentInstanceDBID(ctx, store.GetAgentInstanceDBIDParams{
		ProjectID:  parsePGUUID(projectID),
		AgentID:    parsePGUUID(agentID),
		InstanceID: instanceID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("agent instance not found: project=%s agent=%s instance=%s", projectID, agentID, instanceID)
		}
		return "", fmt.Errorf("get agent instance db id: %w", err)
	}
	return UUIDToString(dbID.Bytes), nil
}

func (r *RuntimeRunQuerier) GetAgentNameByID(ctx context.Context, agentID string) (string, error) {
	agent, err := r.q.GetAgentByID(ctx, parsePGUUID(agentID))
	if err != nil {
		return "", fmt.Errorf("get agent by id: %w", err)
	}
	return agent.Name, nil
}

func (r *RuntimeRunQuerier) ListRecentRunsByAgent(ctx context.Context, projectID, agentName string, limit int) ([]runs.RecentRunEntry, error) {
	rows, err := r.q.ListRecentRunsByAgent(ctx, store.ListRecentRunsByAgentParams{
		ProjectID: parsePGUUID(projectID),
		AgentName: agentName,
		Limit:     int32(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("list recent runs: %w", err)
	}
	result := make([]runs.RecentRunEntry, 0, len(rows))
	for _, row := range rows {
		entry := runs.RecentRunEntry{
			ID:           UUIDToString(row.ID.Bytes),
			ProjectID:    UUIDToString(row.ProjectID.Bytes),
			Status:       row.Status,
			InputSummary: strOrZero(row.InputSummary),
			Error:        strOrZero(row.Error),
			TraceID:      strOrZero(row.TraceID),
			LatencyMs:    pgInt8Ptr(row.LatencyMs),
			AgentName:    row.AgentName,
			CreatedAt:    row.CreatedAt.Time,
			UpdatedAt:    row.UpdatedAt.Time,
		}
		result = append(result, entry)
	}
	return result, nil
}


