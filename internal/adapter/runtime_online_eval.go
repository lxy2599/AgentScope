package adapter

import (
	"context"
	"errors"
	"fmt"

	"AgentScope/internal/onlinerval"
	"AgentScope/internal/runs"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// CompleteRuntimeRunAndEnqueue transitions a runtime Run to completed, records
// its terminal event, clears the active instance link, and creates the durable
// online-evaluation Job in one PostgreSQL transaction. This prevents terminal
// Runs from being acknowledged without durable evaluation work.
func (r *RuntimeRunQuerier) CompleteRuntimeRunAndEnqueue(
	ctx context.Context,
	runID, projectID, outputSummary string,
	latencyMs int64,
	agentID, instanceID string,
) (runs.Run, error) {
	return r.transitionRuntimeRunAndEnqueue(ctx, runtimeTerminalTransition{
		runID:         runID,
		projectID:     projectID,
		status:        "completed",
		outputSummary: outputSummary,
		latencyMs:     latencyMs,
		agentID:       agentID,
		instanceID:    instanceID,
		stateMessage:  "completed",
		stateAttrs:    []byte("{}"),
	})
}

// FailRuntimeRunAndEnqueue is the failed-state counterpart of
// CompleteRuntimeRunAndEnqueue. The Run, state event, instance cleanup, and
// online-evaluation Job either commit together or are all rolled back.
func (r *RuntimeRunQuerier) FailRuntimeRunAndEnqueue(
	ctx context.Context,
	runID, projectID, errorMessage string,
	latencyMs int64,
	agentID, instanceID string,
	attributes []byte,
) (runs.Run, error) {
	return r.transitionRuntimeRunAndEnqueue(ctx, runtimeTerminalTransition{
		runID:        runID,
		projectID:    projectID,
		status:       "failed",
		errorMessage: errorMessage,
		latencyMs:    latencyMs,
		agentID:      agentID,
		instanceID:   instanceID,
		stateMessage: errorMessage,
		stateAttrs:   attributes,
	})
}

type runtimeTerminalTransition struct {
	runID, projectID            string
	status                      string
	outputSummary, errorMessage string
	latencyMs                   int64
	agentID, instanceID         string
	stateMessage                string
	stateAttrs                  []byte
}

func (r *RuntimeRunQuerier) transitionRuntimeRunAndEnqueue(ctx context.Context, input runtimeTerminalTransition) (runs.Run, error) {
	tx, err := r.qPoolBegin(ctx)
	if err != nil {
		return runs.Run{}, fmt.Errorf("begin terminal run transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := r.q.WithTx(tx)

	before, err := q.GetRunByID(ctx, parsePGUUID(input.runID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return runs.Run{}, fmt.Errorf("run not found: %s", input.runID)
		}
		return runs.Run{}, fmt.Errorf("get run for terminal transition: %w", err)
	}
	if UUIDToString(before.ProjectID.Bytes) != input.projectID {
		return runs.Run{}, fmt.Errorf("run not found in project")
	}

	var updated store.Run
	if input.status == "completed" {
		updated, err = q.CompleteRuntimeRun(ctx, store.CompleteRuntimeRunParams{
			ID:            parsePGUUID(input.runID),
			ProjectID:     parsePGUUID(input.projectID),
			OutputSummary: pgText(input.outputSummary),
			LatencyMs:     pgtype.Int8{Int64: input.latencyMs, Valid: true},
		})
	} else {
		var latency pgtype.Int8
		if input.latencyMs > 0 {
			latency = pgtype.Int8{Int64: input.latencyMs, Valid: true}
		}
		updated, err = q.FailRuntimeRun(ctx, store.FailRuntimeRunParams{
			ID:        parsePGUUID(input.runID),
			ProjectID: parsePGUUID(input.projectID),
			Error:     pgText(input.errorMessage),
			LatencyMs: latency,
		})
	}
	if err != nil {
		return runs.Run{}, fmt.Errorf("transition runtime run: %w", err)
	}

	var agentInstanceID pgtype.UUID
	if input.agentID != "" && input.instanceID != "" {
		agentInstanceID, err = q.ClearAgentInstanceActiveRunByExternalID(ctx, store.ClearAgentInstanceActiveRunByExternalIDParams{
			ProjectID:  parsePGUUID(input.projectID),
			AgentID:    parsePGUUID(input.agentID),
			InstanceID: input.instanceID,
		})
		if err != nil {
			return runs.Run{}, fmt.Errorf("clear agent instance active run: %w", err)
		}
	}

	if _, err := q.CreateRunStateEvent(ctx, store.CreateRunStateEventParams{
		ProjectID:       parsePGUUID(input.projectID),
		RunID:           parsePGUUID(input.runID),
		AgentInstanceID: agentInstanceID,
		FromStatus:      pgText(before.Status),
		ToStatus:        input.status,
		Message:         pgText(input.stateMessage),
		Attributes:      input.stateAttrs,
	}); err != nil {
		return runs.Run{}, fmt.Errorf("create terminal run state event: %w", err)
	}

	traceID := ""
	if updated.TraceID.Valid {
		traceID = updated.TraceID.String
	}
	if _, err := tx.Exec(ctx, `INSERT INTO online_eval_jobs (project_id,run_id,trace_id,evaluator_version)
VALUES ($1,$2,$3,$4)
ON CONFLICT (project_id,run_id,evaluator_version) DO UPDATE SET trace_id=EXCLUDED.trace_id, updated_at=now()`,
		parsePGUUID(input.projectID), parsePGUUID(input.runID), traceID, onlinerval.DefaultJobEvaluatorVersion); err != nil {
		return runs.Run{}, fmt.Errorf("enqueue durable online evaluation job: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return runs.Run{}, fmt.Errorf("commit terminal run transaction: %w", err)
	}
	return storeRunToDomain(updated), nil
}
