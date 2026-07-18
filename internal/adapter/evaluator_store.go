package adapter

import (
	"context"
	"fmt"

	"AgentScope/internal/httpapi"
	"AgentScope/internal/store"
	"github.com/jackc/pgx/v5/pgtype"
)

// compile-time check
var _ httpapi.EvaluatorCreator = (*EvaluatorStoreAdapter)(nil)

// EvaluatorStoreAdapter implements httpapi.EvaluatorCreator using store.Queries.
type EvaluatorStoreAdapter struct {
	q *store.Queries
}

// NewEvaluatorStoreAdapter creates a new EvaluatorStoreAdapter.
func NewEvaluatorStoreAdapter(q *store.Queries) *EvaluatorStoreAdapter {
	return &EvaluatorStoreAdapter{q: q}
}

func (e *EvaluatorStoreAdapter) Create(ctx context.Context, projectID, name, evalType, config string) (map[string]interface{}, error) {
	storeEval, err := e.q.CreateEvaluator(ctx, store.CreateEvaluatorParams{
		ProjectID: parsePGUUID(projectID),
		Name:      name,
		Type:      evalType,
		Config:    []byte(config),
	})
	if err != nil {
		return nil, fmt.Errorf("create evaluator: %w", err)
	}
	return map[string]interface{}{
		"id":         UUIDToString(storeEval.ID.Bytes),
		"project_id": UUIDToString(storeEval.ProjectID.Bytes),
		"name":       storeEval.Name,
		"type":       storeEval.Type,
		"config":     string(storeEval.Config),
		"created_at": storeEval.CreatedAt.Time,
	}, nil
}

func (e *EvaluatorStoreAdapter) List(ctx context.Context, projectID string) ([]map[string]interface{}, error) {
	rows, err := e.q.ListEvaluatorsByProject(ctx, parsePGUUID(projectID))
	if err != nil {
		return nil, fmt.Errorf("list evaluators: %w", err)
	}
	result := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		convertNullUUID := func(u pgtype.UUID) string {
			if u.Valid {
				return UUIDToString(u.Bytes)
			}
			return ""
		}
		result = append(result, map[string]interface{}{
			"id":         convertNullUUID(row.ID),
			"project_id": convertNullUUID(row.ProjectID),
			"name":       row.Name,
			"type":       row.Type,
			"config":     string(row.Config),
			"created_at": row.CreatedAt.Time,
		})
	}
	return result, nil
}
