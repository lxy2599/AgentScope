package anomalies

import (
	"context"
	"math"
	"testing"
	"time"
)

type fakeReader struct {
	series []Series
	err    error
}

func (f *fakeReader) ListSeries(context.Context, time.Time, time.Time) ([]Series, error) {
	return f.series, f.err
}

type fakeRepository struct {
	records []Record
}

func (f *fakeRepository) Upsert(_ context.Context, record Record) (Record, error) {
	f.records = append(f.records, record)
	return record, nil
}

func (f *fakeRepository) ListProject(context.Context, string, int) ([]Record, error) {
	return f.records, nil
}

func TestMedianAndMAD(t *testing.T) {
	median, mad := MedianMAD([]float64{10, 11, 12, 13, 100})
	if median != 12 || mad != 1 {
		t.Fatalf("median/mad = %v/%v, want 12/1", median, mad)
	}
}

func TestDetectMarksLargePositiveDeviationAsAnomaly(t *testing.T) {
	now := time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC)
	repo := &fakeRepository{}
	service := NewService(&fakeReader{series: []Series{{
		Group: Group{ProjectID: "p1", AgentName: "agent", AgentVersion: "v1", Environment: "prod", Metric: MetricRunLatency},
		Points: []Point{
			{Bucket: now.Add(-5 * time.Hour), Value: 100},
			{Bucket: now.Add(-4 * time.Hour), Value: 105},
			{Bucket: now.Add(-3 * time.Hour), Value: 95},
			{Bucket: now.Add(-2 * time.Hour), Value: 102},
			{Bucket: now.Add(-time.Hour), Value: 98},
			{Bucket: now.Add(-5 * time.Minute), Value: 250},
		},
	}}}, repo, Options{
		BaselineWindow:     24 * time.Hour,
		RecentWindow:       15 * time.Minute,
		MinBaselineSamples: 5,
		Threshold:          6,
		Now:                func() time.Time { return now },
	})

	records, err := service.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Status != StatusAnomaly {
		t.Fatalf("records = %+v", records)
	}
	if records[0].BaselineMedian != 100 || records[0].BaselineMAD != 2 {
		t.Fatalf("baseline = %v/%v", records[0].BaselineMedian, records[0].BaselineMAD)
	}
	if records[0].DeviationScore <= 6 {
		t.Fatalf("deviation score = %v", records[0].DeviationScore)
	}
}

func TestDetectHandlesZeroMADWithoutInfinity(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeRepository{}
	service := NewService(&fakeReader{series: []Series{{
		Group: Group{ProjectID: "p1", Metric: MetricLLMCost},
		Points: []Point{
			{Bucket: now.Add(-4 * time.Hour), Value: 1},
			{Bucket: now.Add(-3 * time.Hour), Value: 1},
			{Bucket: now.Add(-2 * time.Hour), Value: 1},
			{Bucket: now.Add(-time.Hour), Value: 1},
			{Bucket: now.Add(-time.Minute), Value: 2},
		},
	}}}, repo, Options{
		BaselineWindow: 24 * time.Hour, RecentWindow: 15 * time.Minute,
		MinBaselineSamples: 4, Threshold: 6, Now: func() time.Time { return now },
	})

	records, err := service.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if records[0].Status != StatusAnomaly || math.IsInf(records[0].DeviationScore, 0) {
		t.Fatalf("zero-MAD result = %+v", records[0])
	}
}

func TestDetectReportsInsufficientBaselineSeparately(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeRepository{}
	service := NewService(&fakeReader{series: []Series{{
		Group: Group{ProjectID: "p1", Metric: MetricIngestRejectRate},
		Points: []Point{
			{Bucket: now.Add(-time.Hour), Value: 0},
			{Bucket: now.Add(-time.Minute), Value: 1},
		},
	}}}, repo, Options{
		BaselineWindow: 24 * time.Hour, RecentWindow: 15 * time.Minute,
		MinBaselineSamples: 4, Threshold: 6, Now: func() time.Time { return now },
	})

	records, err := service.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if records[0].Status != StatusInsufficientBaseline {
		t.Fatalf("status = %q", records[0].Status)
	}
	if records[0].SampleCount != 1 || records[0].BaselineSampleCount != 1 {
		t.Fatalf("sample counts = %+v", records[0])
	}
}

func TestDetectUsesAggregatedSampleCountForBaselineThreshold(t *testing.T) {
	now := time.Now().UTC()
	repo := &fakeRepository{}
	service := NewService(&fakeReader{series: []Series{{
		Group: Group{ProjectID: "p1", Metric: MetricLLMTokens},
		Points: []Point{
			{Bucket: now.Add(-time.Hour), Value: 100, SampleCount: 10},
			{Bucket: now.Add(-time.Minute), Value: 101, SampleCount: 2},
		},
	}}}, repo, Options{
		BaselineWindow: 24 * time.Hour, RecentWindow: 15 * time.Minute,
		MinBaselineSamples: 6, Threshold: 6, Now: func() time.Time { return now },
	})

	records, err := service.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if records[0].Status != StatusNormal {
		t.Fatalf("status = %q, want %q", records[0].Status, StatusNormal)
	}
	if records[0].BaselineSampleCount != 10 {
		t.Fatalf("baseline sample count = %d, want 10", records[0].BaselineSampleCount)
	}
}

func TestDetectPreservesGroupingDimensions(t *testing.T) {
	now := time.Now().UTC()
	group := Group{
		ProjectID: "p1", AgentName: "research-agent",
		AgentVersion: "v2", Environment: "staging", Metric: MetricToolErrorRate,
	}
	repo := &fakeRepository{}
	service := NewService(&fakeReader{series: []Series{{
		Group: group,
		Points: []Point{
			{Bucket: now.Add(-3 * time.Hour), Value: 0},
			{Bucket: now.Add(-2 * time.Hour), Value: 0},
			{Bucket: now.Add(-time.Hour), Value: 0},
			{Bucket: now.Add(-time.Minute), Value: 0},
		},
	}}}, repo, Options{
		BaselineWindow: 24 * time.Hour, RecentWindow: 15 * time.Minute,
		MinBaselineSamples: 3, Threshold: 6, Now: func() time.Time { return now },
	})

	records, err := service.Detect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if records[0].Group != group || records[0].Status != StatusNormal {
		t.Fatalf("record = %+v", records[0])
	}
}

func TestRunReportsDetectionErrors(t *testing.T) {
	reported := make(chan error, 1)
	service := NewService(
		&fakeReader{err: context.DeadlineExceeded},
		&fakeRepository{},
		Options{OnError: func(err error) { reported <- err }},
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- service.Run(ctx, time.Hour)
	}()

	select {
	case err := <-reported:
		if err != context.DeadlineExceeded {
			t.Fatalf("reported error = %v", err)
		}
		cancel()
	case <-time.After(time.Second):
		t.Fatal("detection error was not reported")
	}
	<-done
}
