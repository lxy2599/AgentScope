package ingest

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type recordingTraceBatchWriter struct {
	mu      sync.Mutex
	batches []*TraceBatch
	started chan struct{}
	release <-chan struct{}
	err     error
	closed  bool
}

func (w *recordingTraceBatchWriter) WriteBatch(_ context.Context, batch *TraceBatch) error {
	w.mu.Lock()
	w.batches = append(w.batches, batch)
	w.mu.Unlock()
	if w.started != nil {
		select {
		case w.started <- struct{}{}:
		default:
		}
	}
	if w.release != nil {
		<-w.release
	}
	return w.err
}

func (w *recordingTraceBatchWriter) Close() error {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	return nil
}

func (w *recordingTraceBatchWriter) snapshot() ([]*TraceBatch, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]*TraceBatch(nil), w.batches...), w.closed
}

func traceBatchWithSpans(count int) *TraceBatch {
	return &TraceBatch{Spans: make([]SpanRecord, count)}
}

func TestBufferedTraceWriterMergesRequestsAndAcknowledgesAfterFlush(t *testing.T) {
	release := make(chan struct{})
	sinkStarted := make(chan struct{}, 1)
	sink := &recordingTraceBatchWriter{started: sinkStarted, release: release}
	writer := NewBufferedTraceWriter(sink, TraceBufferOptions{
		QueueCapacity: 8,
		MaxBatchSpans: 300,
		FlushInterval: time.Hour,
		WriteTimeout:  time.Second,
	})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
		_ = writer.Close()
	})

	results := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() {
			results <- writer.WriteBatch(context.Background(), traceBatchWithSpans(100))
		}()
	}

	select {
	case <-sinkStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for size-triggered trace flush")
	}
	select {
	case err := <-results:
		t.Fatalf("request returned before durable flush completed: %v", err)
	default:
	}

	batches, _ := sink.snapshot()
	if len(batches) != 1 || len(batches[0].Spans) != 300 {
		t.Fatalf("sink batches = %#v, want one merged 300-span batch", batches)
	}
	close(release)
	for i := 0; i < 3; i++ {
		if err := <-results; err != nil {
			t.Fatalf("WriteBatch(%d): %v", i, err)
		}
	}
}

func TestBufferedTraceWriterFlushesOnInterval(t *testing.T) {
	sinkStarted := make(chan struct{}, 1)
	sink := &recordingTraceBatchWriter{started: sinkStarted}
	writer := NewBufferedTraceWriter(sink, TraceBufferOptions{
		QueueCapacity: 4,
		MaxBatchSpans: 1000,
		FlushInterval: 10 * time.Millisecond,
		WriteTimeout:  time.Second,
	})
	t.Cleanup(func() { _ = writer.Close() })

	result := make(chan error, 1)
	go func() {
		result <- writer.WriteBatch(context.Background(), traceBatchWithSpans(100))
	}()
	select {
	case <-sinkStarted:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for interval-triggered trace flush")
	}
	if err := <-result; err != nil {
		t.Fatal(err)
	}
}

func TestBufferedTraceWriterPropagatesFlushFailureToEveryRequest(t *testing.T) {
	sinkErr := errors.New("clickhouse unavailable")
	sink := &recordingTraceBatchWriter{err: sinkErr}
	writer := NewBufferedTraceWriter(sink, TraceBufferOptions{
		QueueCapacity: 4,
		MaxBatchSpans: 200,
		FlushInterval: time.Hour,
		WriteTimeout:  time.Second,
	})
	t.Cleanup(func() { _ = writer.Close() })

	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			results <- writer.WriteBatch(context.Background(), traceBatchWithSpans(100))
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-results; !errors.Is(err, sinkErr) {
			t.Fatalf("result %d = %v, want sink error", i, err)
		}
	}
}

func TestBufferedTraceWriterCloseDrainsAndRejectsNewWrites(t *testing.T) {
	sink := &recordingTraceBatchWriter{}
	writer := NewBufferedTraceWriter(sink, TraceBufferOptions{
		QueueCapacity: 4,
		MaxBatchSpans: 1000,
		FlushInterval: time.Hour,
		WriteTimeout:  time.Second,
	})

	result := make(chan error, 1)
	go func() {
		result <- writer.WriteBatch(context.Background(), traceBatchWithSpans(100))
	}()
	deadline := time.Now().Add(time.Second)
	for writer.Stats().EnqueuedRequests != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if writer.Stats().EnqueuedRequests != 1 {
		t.Fatal("request was not enqueued")
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := <-result; err != nil {
		t.Fatalf("drained request: %v", err)
	}
	batches, closed := sink.snapshot()
	if len(batches) != 1 || len(batches[0].Spans) != 100 || !closed {
		t.Fatalf("batches=%#v sinkClosed=%t", batches, closed)
	}
	if err := writer.WriteBatch(context.Background(), traceBatchWithSpans(1)); !errors.Is(err, ErrTraceWriterClosed) {
		t.Fatalf("post-close error = %v, want ErrTraceWriterClosed", err)
	}
}
