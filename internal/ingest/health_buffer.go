package ingest

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var (
	ErrHealthQueueFull      = errors.New("ingest health queue is full")
	ErrHealthRecorderClosed = errors.New("ingest health recorder is closed")
)

const (
	defaultHealthQueueCapacity = 4096
	defaultHealthBatchSize     = 500
	defaultHealthFlushInterval = 200 * time.Millisecond
	defaultHealthWriteTimeout  = 5 * time.Second
)

// HealthBatchSink persists a batch of best-effort ingest health observations.
type HealthBatchSink interface {
	RecordHealthEvents(ctx context.Context, events []HealthEvent) error
}

// HealthBufferOptions controls the bounded health-event buffer.
type HealthBufferOptions struct {
	QueueCapacity int
	BatchSize     int
	FlushInterval time.Duration
	WriteTimeout  time.Duration
	OnError       func(error)
}

// HealthBufferStats is a point-in-time snapshot of recorder counters.
type HealthBufferStats struct {
	Enqueued uint64
	Dropped  uint64
	Written  uint64
	Failed   uint64
	Batches  uint64
}

// BufferedHealthRecorder keeps one-row operational health writes out of the
// request critical path. It deliberately uses a bounded queue: health evidence
// is best effort and must not consume unbounded memory when its sink is slow.
type BufferedHealthRecorder struct {
	sink          HealthBatchSink
	queue         chan HealthEvent
	batchSize     int
	flushInterval time.Duration
	writeTimeout  time.Duration
	onError       func(error)

	mu        sync.RWMutex
	closed    bool
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	closeErr  error

	enqueued atomic.Uint64
	dropped  atomic.Uint64
	written  atomic.Uint64
	failed   atomic.Uint64
	batches  atomic.Uint64
}

func NewBufferedHealthRecorder(sink HealthBatchSink, options HealthBufferOptions) *BufferedHealthRecorder {
	if options.QueueCapacity <= 0 {
		options.QueueCapacity = defaultHealthQueueCapacity
	}
	if options.BatchSize <= 0 {
		options.BatchSize = defaultHealthBatchSize
	}
	if options.BatchSize > options.QueueCapacity {
		options.BatchSize = options.QueueCapacity
	}
	if options.FlushInterval <= 0 {
		options.FlushInterval = defaultHealthFlushInterval
	}
	if options.WriteTimeout <= 0 {
		options.WriteTimeout = defaultHealthWriteTimeout
	}
	recorder := &BufferedHealthRecorder{
		sink:          sink,
		queue:         make(chan HealthEvent, options.QueueCapacity),
		batchSize:     options.BatchSize,
		flushInterval: options.FlushInterval,
		writeTimeout:  options.WriteTimeout,
		onError:       options.OnError,
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
	}
	go recorder.run()
	return recorder
}

func (r *BufferedHealthRecorder) RecordHealthEvent(_ context.Context, event HealthEvent) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return ErrHealthRecorderClosed
	}
	select {
	case r.queue <- event:
		r.enqueued.Add(1)
		return nil
	default:
		r.dropped.Add(1)
		return ErrHealthQueueFull
	}
}

func (r *BufferedHealthRecorder) Stats() HealthBufferStats {
	return HealthBufferStats{
		Enqueued: r.enqueued.Load(),
		Dropped:  r.dropped.Load(),
		Written:  r.written.Load(),
		Failed:   r.failed.Load(),
		Batches:  r.batches.Load(),
	}
}

func (r *BufferedHealthRecorder) Close() error {
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		close(r.stop)
		r.mu.Unlock()
		<-r.done
	})
	return r.closeErr
}

func (r *BufferedHealthRecorder) run() {
	defer close(r.done)
	ticker := time.NewTicker(r.flushInterval)
	defer ticker.Stop()
	batch := make([]HealthEvent, 0, r.batchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		events := append([]HealthEvent(nil), batch...)
		batch = batch[:0]
		ctx, cancel := context.WithTimeout(context.Background(), r.writeTimeout)
		err := r.sink.RecordHealthEvents(ctx, events)
		cancel()
		if err != nil {
			r.failed.Add(uint64(len(events)))
			r.closeErr = err
			if r.onError != nil {
				r.onError(err)
			}
			return
		}
		r.written.Add(uint64(len(events)))
		r.batches.Add(1)
	}

	for {
		select {
		case event := <-r.queue:
			batch = append(batch, event)
			if len(batch) >= r.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-r.stop:
			for {
				select {
				case event := <-r.queue:
					batch = append(batch, event)
					if len(batch) >= r.batchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

var _ HealthRecorder = (*BufferedHealthRecorder)(nil)
