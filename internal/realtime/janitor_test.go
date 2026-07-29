package realtime

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakePublishedCleaner struct {
	batches []int64
	calls   int
	err     error
	cutoffs []time.Time
	limits  []int
}

func (f *fakePublishedCleaner) DeletePublishedBefore(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	f.cutoffs = append(f.cutoffs, cutoff)
	f.limits = append(f.limits, limit)
	if f.err != nil {
		return 0, f.err
	}
	if f.calls >= len(f.batches) {
		return 0, nil
	}
	out := f.batches[f.calls]
	f.calls++
	return out, nil
}

func TestOutboxJanitorDrainsBoundedBatches(t *testing.T) {
	cleaner := &fakePublishedCleaner{batches: []int64{2, 2, 1}}
	janitor := NewOutboxJanitor(cleaner, OutboxJanitorOptions{Retention: 24 * time.Hour, BatchSize: 2})
	now := time.Date(2026, 7, 29, 0, 0, 0, 0, time.UTC)
	janitor.now = func() time.Time { return now }
	deleted, err := janitor.RunOnce(context.Background())
	if err != nil || deleted != 5 || cleaner.calls != 3 {
		t.Fatalf("deleted=%d calls=%d err=%v", deleted, cleaner.calls, err)
	}
	if got := cleaner.cutoffs[0]; !got.Equal(now.Add(-24 * time.Hour)) {
		t.Fatalf("cutoff=%s", got)
	}
}
func TestOutboxJanitorReturnsCleanerError(t *testing.T) {
	janitor := NewOutboxJanitor(&fakePublishedCleaner{err: errors.New("postgres down")}, OutboxJanitorOptions{})
	if _, err := janitor.RunOnce(context.Background()); err == nil {
		t.Fatal("want cleanup error")
	}
}
