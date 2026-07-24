package adapter

import (
	"context"
	"fmt"
	"time"

	"AgentScope/internal/anomalies"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AnomalyRepository stores detector output in PostgreSQL. Its unique key makes
// repeated detector passes for the same window idempotent.
type AnomalyRepository struct {
	pool *pgxpool.Pool
}

func NewAnomalyRepository(pool *pgxpool.Pool) *AnomalyRepository {
	return &AnomalyRepository{pool: pool}
}

func (r *AnomalyRepository) Upsert(
	ctx context.Context,
	record anomalies.Record,
) (anomalies.Record, error) {
	detectedAt := record.LastDetectedAt
	if detectedAt.IsZero() {
		detectedAt = time.Now().UTC()
	}
	firstDetectedAt := record.FirstDetectedAt
	if firstDetectedAt.IsZero() {
		firstDetectedAt = detectedAt
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO operational_anomalies (
			project_id, agent_name, agent_version, environment, metric,
			window_start, window_end, observed_value, baseline_median,
			baseline_mad, deviation_score, sample_count, baseline_sample_count,
			status, first_detected_at, last_detected_at
		) VALUES (
			$1::uuid, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
			$14, $15, $16
		)
		ON CONFLICT (
			project_id, agent_name, agent_version, environment, metric, window_start
		) DO UPDATE SET
			window_end = EXCLUDED.window_end,
			observed_value = EXCLUDED.observed_value,
			baseline_median = EXCLUDED.baseline_median,
			baseline_mad = EXCLUDED.baseline_mad,
			deviation_score = EXCLUDED.deviation_score,
			sample_count = EXCLUDED.sample_count,
			baseline_sample_count = EXCLUDED.baseline_sample_count,
			status = EXCLUDED.status,
			last_detected_at = EXCLUDED.last_detected_at,
			updated_at = now()
		RETURNING
			id::text, project_id::text, agent_name, agent_version, environment,
			metric, window_start, window_end, observed_value, baseline_median,
			baseline_mad, deviation_score, sample_count, baseline_sample_count,
			status, first_detected_at, last_detected_at
	`,
		record.Group.ProjectID,
		record.Group.AgentName,
		record.Group.AgentVersion,
		record.Group.Environment,
		string(record.Group.Metric),
		record.WindowStart,
		record.WindowEnd,
		record.ObservedValue,
		record.BaselineMedian,
		record.BaselineMAD,
		record.DeviationScore,
		record.SampleCount,
		record.BaselineSampleCount,
		string(record.Status),
		firstDetectedAt,
		detectedAt,
	)
	saved, err := scanAnomaly(row)
	if err != nil {
		return anomalies.Record{}, fmt.Errorf("upsert operational anomaly: %w", err)
	}
	return saved, nil
}

func (r *AnomalyRepository) ListProject(
	ctx context.Context,
	projectID string,
	limit int,
) ([]anomalies.Record, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT
			id::text, project_id::text, agent_name, agent_version, environment,
			metric, window_start, window_end, observed_value, baseline_median,
			baseline_mad, deviation_score, sample_count, baseline_sample_count,
			status, first_detected_at, last_detected_at
		FROM operational_anomalies
		WHERE project_id = $1::uuid
		ORDER BY window_end DESC, metric, agent_name
		LIMIT $2
	`, projectID, limit)
	if err != nil {
		return nil, fmt.Errorf("list operational anomalies: %w", err)
	}
	defer rows.Close()

	records := make([]anomalies.Record, 0)
	for rows.Next() {
		record, err := scanAnomaly(rows)
		if err != nil {
			return nil, fmt.Errorf("scan operational anomaly: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate operational anomalies: %w", err)
	}
	return records, nil
}

type anomalyScanner interface {
	Scan(dest ...any) error
}

func scanAnomaly(row anomalyScanner) (anomalies.Record, error) {
	var (
		record anomalies.Record
		metric string
		status string
	)
	err := row.Scan(
		&record.ID,
		&record.Group.ProjectID,
		&record.Group.AgentName,
		&record.Group.AgentVersion,
		&record.Group.Environment,
		&metric,
		&record.WindowStart,
		&record.WindowEnd,
		&record.ObservedValue,
		&record.BaselineMedian,
		&record.BaselineMAD,
		&record.DeviationScore,
		&record.SampleCount,
		&record.BaselineSampleCount,
		&status,
		&record.FirstDetectedAt,
		&record.LastDetectedAt,
	)
	if err != nil {
		return anomalies.Record{}, err
	}
	record.Group.Metric = anomalies.Metric(metric)
	record.Status = anomalies.Status(status)
	return record, nil
}

var _ anomalyScanner = pgx.Row(nil)
