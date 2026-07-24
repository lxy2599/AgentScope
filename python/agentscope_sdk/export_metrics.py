"""Observable, bounded span export primitives.

The processor in this module only uses OpenTelemetry's public
``SpanProcessor`` and ``SpanExporter`` contracts. Ending a business span never
waits for network I/O: a full local queue drops the span and records the loss.
"""

from __future__ import annotations

import logging
import queue
import threading
import time
from dataclasses import asdict, dataclass, replace
from datetime import datetime
from typing import Any

from opentelemetry.sdk.trace.export import SpanExporter, SpanProcessor

_logger = logging.getLogger(__name__)


@dataclass(frozen=True)
class ExportMetricsSnapshot:
    """Immutable process-local export health snapshot."""

    queue_depth: int = 0
    queue_capacity: int = 0
    queue_high_watermark: int = 0
    spans_enqueued: int = 0
    batches_attempted: int = 0
    batches_succeeded: int = 0
    batches_failed: int = 0
    spans_attempted: int = 0
    spans_exported: int = 0
    spans_dropped: int = 0
    queue_drops: int = 0
    retries: int = 0
    last_batch_size: int = 0
    last_export_latency_ms: float | None = None
    last_flush_ok: bool | None = None
    last_shutdown_ok: bool | None = None
    last_error: str = ""
    last_error_at: datetime | None = None
    last_success_at: datetime | None = None

    def as_attributes(self) -> dict[str, Any]:
        """Return low-cardinality, content-free span/heartbeat attributes."""
        values = asdict(self)
        return {
            f"agentscope.sdk.export.{key}": (
                value.isoformat() if isinstance(value, datetime) else value
            )
            for key, value in values.items()
            if value is not None and key not in {"last_error"}
        }


class ExportMetricsRecorder:
    """Thread-safe mutable state behind immutable snapshots."""

    def __init__(self, *, queue_capacity: int = 0) -> None:
        self._lock = threading.Lock()
        self._snapshot = ExportMetricsSnapshot(queue_capacity=queue_capacity)

    def update(self, **changes: Any) -> None:
        with self._lock:
            self._snapshot = replace(self._snapshot, **changes)

    def increment(self, **changes: int) -> None:
        with self._lock:
            values = {
                key: getattr(self._snapshot, key) + value
                for key, value in changes.items()
            }
            self._snapshot = replace(self._snapshot, **values)

    def queue_changed(self, depth: int) -> None:
        with self._lock:
            self._snapshot = replace(
                self._snapshot,
                queue_depth=depth,
                queue_high_watermark=max(
                    self._snapshot.queue_high_watermark,
                    depth,
                ),
            )

    def snapshot(self) -> ExportMetricsSnapshot:
        with self._lock:
            return self._snapshot


class ObservableBatchSpanProcessor(SpanProcessor):
    """Bounded non-blocking batch processor with public health metrics."""

    def __init__(
        self,
        span_exporter: SpanExporter,
        *,
        recorder: ExportMetricsRecorder,
        max_queue_size: int = 2048,
        schedule_delay_millis: int = 5000,
        max_export_batch_size: int = 512,
        export_timeout_millis: int = 30000,
    ) -> None:
        if max_queue_size <= 0:
            raise ValueError("max_queue_size must be positive")
        if max_export_batch_size <= 0:
            raise ValueError("max_export_batch_size must be positive")
        if max_export_batch_size > max_queue_size:
            raise ValueError("max_export_batch_size must not exceed max_queue_size")

        self._exporter = span_exporter
        self._recorder = recorder
        self._queue: queue.Queue[Any] = queue.Queue(maxsize=max_queue_size)
        self._schedule_delay = max(1, schedule_delay_millis) / 1000
        self._batch_size = max_export_batch_size
        self._export_timeout_millis = export_timeout_millis
        self._wake = threading.Event()
        self._drained = threading.Event()
        self._drained.set()
        self._pending_lock = threading.Lock()
        self._pending = 0
        self._shutdown_lock = threading.Lock()
        self._shutdown = False
        self._worker = threading.Thread(
            target=self._worker_loop,
            name="agentscope-span-exporter",
            daemon=True,
        )
        self._worker.start()

    def on_start(self, span: Any, parent_context: Any = None) -> None:
        return None

    def on_end(self, span: Any) -> None:
        context = getattr(span, "context", None)
        trace_flags = getattr(context, "trace_flags", None)
        if trace_flags is not None and not bool(getattr(trace_flags, "sampled", True)):
            return
        if self._shutdown:
            self._recorder.increment(spans_dropped=1, queue_drops=1)
            return
        with self._pending_lock:
            try:
                self._queue.put_nowait(span)
            except queue.Full:
                self._recorder.increment(spans_dropped=1, queue_drops=1)
                return
            self._pending += 1
            self._drained.clear()
        depth = self._queue.qsize()
        self._recorder.increment(spans_enqueued=1)
        self._recorder.queue_changed(depth)
        if depth >= self._batch_size:
            self._wake.set()

    def force_flush(self, timeout_millis: int = 30000) -> bool:
        deadline = time.monotonic() + max(0, timeout_millis) / 1000
        self._wake.set()
        remaining = max(0.0, deadline - time.monotonic())
        drained = self._drained.wait(remaining)
        if not drained:
            self._recorder.update(last_flush_ok=False)
            return False
        remaining_ms = max(0, int((deadline - time.monotonic()) * 1000))
        try:
            result = bool(self._exporter.force_flush(remaining_ms))
        except Exception:
            _logger.exception("span exporter force_flush failed")
            result = False
        self._recorder.update(last_flush_ok=result)
        return result

    def shutdown(self) -> None:
        with self._shutdown_lock:
            if self._shutdown:
                return
            flush_ok = self.force_flush(self._export_timeout_millis)
            self._shutdown = True
            self._wake.set()
            self._worker.join(self._export_timeout_millis / 1000)
            shutdown_ok = flush_ok and not self._worker.is_alive()
            try:
                self._exporter.shutdown()
            except Exception:
                _logger.exception("span exporter shutdown failed")
                shutdown_ok = False
            self._recorder.update(last_shutdown_ok=shutdown_ok)

    def _worker_loop(self) -> None:
        while True:
            self._wake.wait(self._schedule_delay)
            self._wake.clear()
            self._drain_available()
            if self._shutdown and self._queue.empty():
                return

    def _drain_available(self) -> None:
        while True:
            batch: list[Any] = []
            for _ in range(self._batch_size):
                try:
                    batch.append(self._queue.get_nowait())
                except queue.Empty:
                    break
            if not batch:
                self._recorder.queue_changed(0)
                return
            try:
                self._exporter.export(batch)
            except Exception:
                _logger.exception("uncaught span exporter failure")
            finally:
                for _ in batch:
                    self._queue.task_done()
                with self._pending_lock:
                    self._pending -= len(batch)
                    if self._pending == 0:
                        self._drained.set()
                self._recorder.queue_changed(self._queue.qsize())
