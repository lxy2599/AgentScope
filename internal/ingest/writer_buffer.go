package ingest

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

var ErrTraceWriterClosed = errors.New("trace batch writer is closed")

const (
	defaultTraceQueueCapacity = 1024
	defaultTraceBatchSpans    = 1000
	defaultTraceFlushInterval = 10 * time.Millisecond
	defaultTraceWriteTimeout  = 30 * time.Second
	defaultTraceWorkers       = 4
)

type TraceBufferOptions struct {
	QueueCapacity int
	MaxBatchSpans int
	FlushInterval time.Duration
	WriteTimeout  time.Duration
	Workers       int
}

type TraceWriterStats struct {
	EnqueuedRequests uint64
	WrittenRequests  uint64
	FailedRequests   uint64
	WrittenSpans     uint64
	Flushes          uint64
}

type queuedTraceBatch struct {
	batch  *TraceBatch
	result chan error
}

// BufferedTraceWriter coalesces several request-level trace batches into one
// sink write. A request is acknowledged only after the merged sink write has
// completed, preserving the existing "success means persisted" contract.
type BufferedTraceWriter struct {
	sink          TraceBatchWriter
	queue         chan queuedTraceBatch
	maxBatchSpans int
	flushInterval time.Duration
	writeTimeout  time.Duration
	workers       int

	mu        sync.RWMutex
	closed    bool
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	workerWG  sync.WaitGroup
	errMu     sync.Mutex
	closeErr  error

	enqueuedRequests atomic.Uint64
	writtenRequests  atomic.Uint64
	failedRequests   atomic.Uint64
	writtenSpans     atomic.Uint64
	flushes          atomic.Uint64
}

func NewBufferedTraceWriter(sink TraceBatchWriter, options TraceBufferOptions) *BufferedTraceWriter {
	if options.QueueCapacity <= 0 {
		options.QueueCapacity = defaultTraceQueueCapacity
	}
	if options.MaxBatchSpans <= 0 {
		options.MaxBatchSpans = defaultTraceBatchSpans
	}
	if options.FlushInterval <= 0 {
		options.FlushInterval = defaultTraceFlushInterval
	}
	if options.WriteTimeout <= 0 {
		options.WriteTimeout = defaultTraceWriteTimeout
	}
	if options.Workers <= 0 {
		options.Workers = defaultTraceWorkers
	}
	writer := &BufferedTraceWriter{
		sink:          sink,
		queue:         make(chan queuedTraceBatch, options.QueueCapacity),
		maxBatchSpans: options.MaxBatchSpans,
		flushInterval: options.FlushInterval,
		writeTimeout:  options.WriteTimeout,
		workers:       options.Workers,
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
	}
	writer.workerWG.Add(writer.workers)
	for i := 0; i < writer.workers; i++ {
		go func() {
			defer writer.workerWG.Done()
			writer.runWorker()
		}()
	}
	go func() {
		writer.workerWG.Wait()
		writer.recordError(writer.sink.Close())
		close(writer.done)
	}()
	return writer
}

func (w *BufferedTraceWriter) WriteBatch(ctx context.Context, batch *TraceBatch) error {
	request := queuedTraceBatch{batch: batch, result: make(chan error, 1)}
	w.mu.RLock()
	if w.closed {
		w.mu.RUnlock()
		return ErrTraceWriterClosed
	}
	select {
	case w.queue <- request:
		w.enqueuedRequests.Add(1)
		w.mu.RUnlock()
	case <-ctx.Done():
		w.mu.RUnlock()
		return ctx.Err()
	}

	select {
	case err := <-request.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *BufferedTraceWriter) Stats() TraceWriterStats {
	return TraceWriterStats{
		EnqueuedRequests: w.enqueuedRequests.Load(),
		WrittenRequests:  w.writtenRequests.Load(),
		FailedRequests:   w.failedRequests.Load(),
		WrittenSpans:     w.writtenSpans.Load(),
		Flushes:          w.flushes.Load(),
	}
}

func (w *BufferedTraceWriter) Close() error {
	w.closeOnce.Do(func() {
		w.mu.Lock()
		w.closed = true
		close(w.stop)
		w.mu.Unlock()
		<-w.done
	})
	w.errMu.Lock()
	defer w.errMu.Unlock()
	return w.closeErr
}

func (w *BufferedTraceWriter) recordError(err error) {
	if err == nil {
		return
	}
	w.errMu.Lock()
	w.closeErr = errors.Join(w.closeErr, err)
	w.errMu.Unlock()
}

func (w *BufferedTraceWriter) runWorker() {
	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()
	pending := make([]queuedTraceBatch, 0, 16)
	pendingSpans := 0

	flush := func() {
		if len(pending) == 0 {
			return
		}
		requests := pending
		spanCount := pendingSpans
		pending = make([]queuedTraceBatch, 0, 16)
		pendingSpans = 0

		merged := mergeTraceBatches(requests)
		ctx, cancel := context.WithTimeout(context.Background(), w.writeTimeout)
		err := w.sink.WriteBatch(ctx, merged)
		cancel()
		if err != nil {
			w.failedRequests.Add(uint64(len(requests)))
			w.recordError(err)
		} else {
			w.writtenRequests.Add(uint64(len(requests)))
			w.writtenSpans.Add(uint64(spanCount))
			w.flushes.Add(1)
		}
		for _, request := range requests {
			request.result <- err
		}
	}

	appendRequest := func(request queuedTraceBatch) {
		pending = append(pending, request)
		if request.batch != nil {
			pendingSpans += len(request.batch.Spans)
		}
		if pendingSpans >= w.maxBatchSpans {
			flush()
		}
	}

	for {
		select {
		case request := <-w.queue:
			appendRequest(request)
		case <-ticker.C:
			flush()
		case <-w.stop:
			for {
				select {
				case request := <-w.queue:
					appendRequest(request)
				default:
					flush()
					return
				}
			}
		}
	}
}

func mergeTraceBatches(requests []queuedTraceBatch) *TraceBatch {
	var spanCount, eventCount, llmCount, toolCount int
	var retrievalCount, skillCount, thoughtCount, finalAnswerCount int
	for _, request := range requests {
		if request.batch == nil {
			continue
		}
		spanCount += len(request.batch.Spans)
		eventCount += len(request.batch.Events)
		llmCount += len(request.batch.LLMCalls)
		toolCount += len(request.batch.ToolCalls)
		retrievalCount += len(request.batch.Retrievals)
		skillCount += len(request.batch.Skills)
		thoughtCount += len(request.batch.Thoughts)
		finalAnswerCount += len(request.batch.FinalAnswers)
	}
	merged := &TraceBatch{
		Spans:        make([]SpanRecord, 0, spanCount),
		Events:       make([]SpanEventRecord, 0, eventCount),
		LLMCalls:     make([]LLMCallRecord, 0, llmCount),
		ToolCalls:    make([]ToolCallRecord, 0, toolCount),
		Retrievals:   make([]RetrievalCallRecord, 0, retrievalCount),
		Skills:       make([]SkillCallRecord, 0, skillCount),
		Thoughts:     make([]ThoughtRecord, 0, thoughtCount),
		FinalAnswers: make([]FinalAnswerRecord, 0, finalAnswerCount),
	}
	for _, request := range requests {
		if request.batch == nil {
			continue
		}
		merged.Spans = append(merged.Spans, request.batch.Spans...)
		merged.Events = append(merged.Events, request.batch.Events...)
		merged.LLMCalls = append(merged.LLMCalls, request.batch.LLMCalls...)
		merged.ToolCalls = append(merged.ToolCalls, request.batch.ToolCalls...)
		merged.Retrievals = append(merged.Retrievals, request.batch.Retrievals...)
		merged.Skills = append(merged.Skills, request.batch.Skills...)
		merged.Thoughts = append(merged.Thoughts, request.batch.Thoughts...)
		merged.FinalAnswers = append(merged.FinalAnswers, request.batch.FinalAnswers...)
	}
	return merged
}

var _ TraceBatchWriter = (*BufferedTraceWriter)(nil)
