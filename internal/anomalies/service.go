package anomalies

import (
	"context"
	"errors"
	"math"
	"sort"
	"time"
)

type Metric string

const (
	MetricRunLatency       Metric = "run_latency_ms"
	MetricLLMTokens        Metric = "llm_total_tokens"
	MetricLLMCost          Metric = "llm_cost"
	MetricLLMTTFT          Metric = "llm_ttft_ms"
	MetricToolErrorRate    Metric = "tool_error_rate"
	MetricIngestRejectRate Metric = "ingest_reject_rate"
)

type Status string

const (
	StatusNormal               Status = "normal"
	StatusAnomaly              Status = "anomaly"
	StatusInsufficientBaseline Status = "insufficient_baseline"
)

type Group struct {
	ProjectID    string `json:"project_id"`
	AgentName    string `json:"agent_name"`
	AgentVersion string `json:"agent_version"`
	Environment  string `json:"environment"`
	Metric       Metric `json:"metric"`
}

type Point struct {
	Bucket      time.Time
	Value       float64
	SampleCount int64
}

type Series struct {
	Group  Group
	Points []Point
}

type Record struct {
	ID                  string    `json:"id"`
	Group               Group     `json:"group"`
	WindowStart         time.Time `json:"window_start"`
	WindowEnd           time.Time `json:"window_end"`
	ObservedValue       float64   `json:"observed_value"`
	BaselineMedian      float64   `json:"baseline_median"`
	BaselineMAD         float64   `json:"baseline_mad"`
	DeviationScore      float64   `json:"deviation_score"`
	SampleCount         int       `json:"sample_count"`
	BaselineSampleCount int       `json:"baseline_sample_count"`
	Status              Status    `json:"status"`
	FirstDetectedAt     time.Time `json:"first_detected_at"`
	LastDetectedAt      time.Time `json:"last_detected_at"`
}

type Reader interface {
	ListSeries(ctx context.Context, from, to time.Time) ([]Series, error)
}

type Repository interface {
	Upsert(ctx context.Context, record Record) (Record, error)
	ListProject(ctx context.Context, projectID string, limit int) ([]Record, error)
}

type Options struct {
	BaselineWindow     time.Duration
	RecentWindow       time.Duration
	MinBaselineSamples int
	Threshold          float64
	Now                func() time.Time
	OnError            func(error)
}

type Service struct {
	reader Reader
	repo   Repository
	opts   Options
}

func NewService(reader Reader, repo Repository, opts Options) *Service {
	if opts.BaselineWindow <= 0 {
		opts.BaselineWindow = 24 * time.Hour
	}
	if opts.RecentWindow <= 0 {
		opts.RecentWindow = time.Hour
	}
	if opts.MinBaselineSamples <= 0 {
		opts.MinBaselineSamples = 6
	}
	if opts.Threshold <= 0 {
		opts.Threshold = 6
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Service{reader: reader, repo: repo, opts: opts}
}

func (s *Service) Detect(ctx context.Context) ([]Record, error) {
	return s.detect(ctx, "")
}

func (s *Service) DetectProject(ctx context.Context, projectID string) ([]Record, error) {
	if projectID == "" {
		return nil, errors.New("project ID is required")
	}
	return s.detect(ctx, projectID)
}

func (s *Service) detect(ctx context.Context, projectID string) ([]Record, error) {
	if s.reader == nil || s.repo == nil {
		return nil, errors.New("anomaly reader and repository are required")
	}
	now := s.opts.Now().UTC()
	recentStart := now.Add(-s.opts.RecentWindow)
	series, err := s.reader.ListSeries(ctx, now.Add(-s.opts.BaselineWindow), now)
	if err != nil {
		return nil, err
	}

	results := make([]Record, 0, len(series))
	for _, item := range series {
		if projectID != "" && item.Group.ProjectID != projectID {
			continue
		}
		var baseline, recent []float64
		recentSamples := 0
		baselineSamples := 0
		for _, point := range item.Points {
			samples := int(point.SampleCount)
			if samples <= 0 {
				samples = 1
			}
			if point.Bucket.Before(recentStart) {
				baseline = append(baseline, point.Value)
				baselineSamples += samples
			} else {
				recent = append(recent, point.Value)
				recentSamples += samples
			}
		}
		if len(recent) == 0 {
			continue
		}
		observed, _ := MedianMAD(recent)
		record := Record{
			Group: item.Group, WindowStart: recentStart, WindowEnd: now,
			ObservedValue: observed, SampleCount: recentSamples,
			BaselineSampleCount: baselineSamples, LastDetectedAt: now,
		}
		if baselineSamples < s.opts.MinBaselineSamples {
			record.Status = StatusInsufficientBaseline
		} else {
			record.BaselineMedian, record.BaselineMAD = MedianMAD(baseline)
			record.DeviationScore = robustDeviation(
				record.ObservedValue,
				record.BaselineMedian,
				record.BaselineMAD,
			)
			if record.ObservedValue > record.BaselineMedian &&
				record.DeviationScore >= s.opts.Threshold {
				record.Status = StatusAnomaly
			} else {
				record.Status = StatusNormal
			}
		}
		saved, err := s.repo.Upsert(ctx, record)
		if err != nil {
			return nil, err
		}
		results = append(results, saved)
	}
	return results, nil
}

func (s *Service) ListProject(ctx context.Context, projectID string, limit int) ([]Record, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	return s.repo.ListProject(ctx, projectID, limit)
}

func (s *Service) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := s.Detect(ctx); err != nil && !errors.Is(err, context.Canceled) {
			if s.opts.OnError != nil {
				s.opts.OnError(err)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func MedianMAD(values []float64) (median, mad float64) {
	if len(values) == 0 {
		return 0, 0
	}
	ordered := append([]float64(nil), values...)
	sort.Float64s(ordered)
	median = middle(ordered)
	deviations := make([]float64, len(ordered))
	for i, value := range ordered {
		deviations[i] = math.Abs(value - median)
	}
	sort.Float64s(deviations)
	return median, middle(deviations)
}

func middle(values []float64) float64 {
	n := len(values)
	if n%2 == 1 {
		return values[n/2]
	}
	return (values[n/2-1] + values[n/2]) / 2
}

func robustDeviation(observed, median, mad float64) float64 {
	delta := math.Abs(observed - median)
	if mad > 0 {
		return delta / (1.4826 * mad)
	}
	if delta == 0 {
		return 0
	}
	floor := math.Max(math.Abs(median)*0.01, 1e-9)
	return delta / floor
}
