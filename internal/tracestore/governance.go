package tracestore

import (
	"context"
	"fmt"
	"time"

	"AgentScope/internal/anomalies"
	"github.com/google/uuid"
)

// ListSeries returns hourly weighted means for anomaly detection.
// SummingMergeTree rows are merged at query time because background merges are
// asynchronous and cannot be assumed to have completed.
func (s *Store) ListSeries(
	ctx context.Context,
	from time.Time,
	to time.Time,
) ([]anomalies.Series, error) {
	rows, err := s.conn.Query(ctx, `
		SELECT
			project_id,
			agent_name,
			agent_version,
			environment,
			metric,
			bucket,
			sum(value_sum) / sum(sample_count) AS value,
			sum(sample_count) AS samples
		FROM trace_governance_rollup_hourly
		WHERE bucket >= ? AND bucket < ?
		GROUP BY
			project_id, agent_name, agent_version, environment, metric, bucket
		ORDER BY
			project_id, agent_name, agent_version, environment, metric, bucket
	`, from.UTC(), to.UTC())
	if err != nil {
		return nil, fmt.Errorf("query governance rollup: %w", err)
	}
	defer rows.Close()

	series := make([]anomalies.Series, 0)
	var current *anomalies.Series
	var currentKey string
	for rows.Next() {
		var (
			projectID    uuid.UUID
			agentName    string
			agentVersion string
			environment  string
			metric       string
			bucket       time.Time
			value        float64
			samples      uint64
		)
		if err := rows.Scan(
			&projectID,
			&agentName,
			&agentVersion,
			&environment,
			&metric,
			&bucket,
			&value,
			&samples,
		); err != nil {
			return nil, fmt.Errorf("scan governance rollup: %w", err)
		}
		key := projectID.String() + "\x00" + agentName + "\x00" + agentVersion +
			"\x00" + environment + "\x00" + metric
		if current == nil || key != currentKey {
			series = append(series, anomalies.Series{Group: anomalies.Group{
				ProjectID:    projectID.String(),
				AgentName:    agentName,
				AgentVersion: agentVersion,
				Environment:  environment,
				Metric:       anomalies.Metric(metric),
			}})
			current = &series[len(series)-1]
			currentKey = key
		}
		current.Points = append(current.Points, anomalies.Point{
			Bucket:      bucket.UTC(),
			Value:       value,
			SampleCount: int64(samples),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate governance rollup: %w", err)
	}
	return series, nil
}
