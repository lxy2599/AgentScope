package adapter

import (
	"context"
	"encoding/json"
	"fmt"

	"AgentScope/internal/evals"
	"AgentScope/internal/evaluators"
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

// ValidateEvalCreatorConfig validates an evaluator config before creation.
// It handles both single evaluator configs and multi_metrics arrays.
// Exported so tests can verify validation without needing a DB connection.
func ValidateEvalCreatorConfig(config string) error {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return fmt.Errorf("invalid evaluator config JSON: %w", err)
	}

	if multiMetrics, ok := cfg["multi_metrics"].([]interface{}); ok && len(multiMetrics) > 0 {
		for i, mm := range multiMetrics {
			mmCfg, ok := mm.(map[string]interface{})
			if !ok {
				return fmt.Errorf("multi_metrics[%d]: must be a JSON object", i)
			}
			if err := evaluators.ValidateEvaluatorConfig(mmCfg); err != nil {
				return fmt.Errorf("multi_metrics[%d]: %w", i, err)
			}
			metricKind, _ := mmCfg["metric_kind"].(string)
			if metricKind == "" {
				metricKind, _ = mmCfg["kind"].(string)
			}
			if metricKind != "" && !evals.SupportedMetricKinds[metricKind] {
				return fmt.Errorf("multi_metrics[%d]: unsupported metric_kind %q", i, metricKind)
			}
		}
		if err := evaluators.ValidateEvaluatorConfig(cfg); err != nil {
			return fmt.Errorf("evaluator config: %w", err)
		}
	} else {
		if err := evaluators.ValidateEvaluatorConfig(cfg); err != nil {
			return fmt.Errorf("evaluator config: %w", err)
		}
	}
	return nil
}

func (e *EvaluatorStoreAdapter) Create(ctx context.Context, projectID, name, evalType, config string) (map[string]interface{}, error) {
	if err := ValidateEvalCreatorConfig(config); err != nil {
		return nil, err
	}

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

// Get returns a single evaluator by ID.
func (e *EvaluatorStoreAdapter) Get(ctx context.Context, evaluatorID string) (map[string]interface{}, error) {
	row, err := e.q.GetEvaluatorByID(ctx, parsePGUUID(evaluatorID))
	if err != nil {
		return nil, fmt.Errorf("get evaluator: %w", err)
	}
	convertNullUUID := func(u pgtype.UUID) string {
		if u.Valid {
			return UUIDToString(u.Bytes)
		}
		return ""
	}
	return map[string]interface{}{
		"id":         convertNullUUID(row.ID),
		"project_id": convertNullUUID(row.ProjectID),
		"name":       row.Name,
		"type":       row.Type,
		"config":     string(row.Config),
		"created_at": row.CreatedAt.Time,
	}, nil
}
