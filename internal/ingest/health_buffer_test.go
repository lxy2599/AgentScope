package ingest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type recordingHealthBatchSink struct {
	mu      sync.Mutex
	batches [][]HealthEvent
	called  chan struct{}
	block   <-chan struct{}
}

func (s *recordingHealthBatchSink) RecordHealthEvents(_ context.Context, events []HealthEvent) error {
	if s.called != nil {
		select {
		case s.called <- struct{}{}:
		default:
		}
	}
	if s.block != nil {
		<-s.block
	}
	copyOfEvents := append([]HealthEvent(nil), events...)
	s.mu.Lock()
	s.batches = append(s.batches, copyOfEvents)
	s.mu.Unlock()
	return nil
}

func (s *recordingHealthBatchSink) snapshot() [][]HealthEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]HealthEvent(nil), s.batches...)
}

func TestBufferedHealthRecorderBatchesBySize(t *testing.T) {
	sink := &recordingHealthBatchSink{called: make(chan struct{}, 1)}
	recorder := NewBufferedHealthRecorder(sink, HealthBufferOptions{
		QueueCapacity: 8,
		BatchSize:     3,
		FlushInterval: time.Hour,
		WriteTimeout:  time.Second,
	})
	t.Cleanup(func() { _ = recorder.Close() })

	for i := 0; i < 3; i++ {
		if err := recorder.RecordHealthEvent(context.Background(), HealthEvent{ProjectID: "project"}); err != nil {
			t.Fatalf("RecordHealthEvent(%d): %v", i, err)
		}
	}

	select {
	case <-sink.called:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for size-triggered flush")
	}

	batches := sink.snapshot()
	if len(batches) != 1 || len(batches[0]) != 3 {
		t.Fatalf("batches = %#v, want one batch with three events", batches)
	}
}

func TestBufferedHealthRecorderIsBoundedAndNonBlocking(t *testing.T) {
	release := make(chan struct{})
	sinkStarted := make(chan struct{}, 1)
	sink := &recordingHealthBatchSink{called: sinkStarted, block: release}
	recorder := NewBufferedHealthRecorder(sink, HealthBufferOptions{
		QueueCapacity: 1,
		BatchSize:     1,
		FlushInterval: time.Hour,
		WriteTimeout:  time.Second,
	})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		_ = recorder.Close()
	})

	if err := recorder.RecordHealthEvent(context.Background(), HealthEvent{ProjectID: "first"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-sinkStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for blocked sink")
	}
	if err := recorder.RecordHealthEvent(context.Background(), HealthEvent{ProjectID: "queued"}); err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	err := recorder.RecordHealthEvent(context.Background(), HealthEvent{ProjectID: "dropped"})
	if !errors.Is(err, ErrHealthQueueFull) {
		t.Fatalf("error = %v, want ErrHealthQueueFull", err)
	}
	if elapsed := time.Since(started); elapsed > 50*time.Millisecond {
		t.Fatalf("full-queue call blocked for %s", elapsed)
	}
	if got := recorder.Stats().Dropped; got != 1 {
		t.Fatalf("Dropped = %d, want 1", got)
	}
}

func TestBufferedHealthRecorderCloseDrainsPendingEvents(t *testing.T) {
	sink := &recordingHealthBatchSink{}
	recorder := NewBufferedHealthRecorder(sink, HealthBufferOptions{
		QueueCapacity: 8,
		BatchSize:     8,
		FlushInterval: time.Hour,
		WriteTimeout:  time.Second,
	})

	for i := 0; i < 2; i++ {
		if err := recorder.RecordHealthEvent(context.Background(), HealthEvent{ProjectID: "project"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := recorder.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	batches := sink.snapshot()
	if len(batches) != 1 || len(batches[0]) != 2 {
		t.Fatalf("batches = %#v, want one drained batch with two events", batches)
	}
	if err := recorder.RecordHealthEvent(context.Background(), HealthEvent{}); !errors.Is(err, ErrHealthRecorderClosed) {
		t.Fatalf("post-close error = %v, want ErrHealthRecorderClosed", err)
	}
}
