from __future__ import annotations

import threading
import time
from types import SimpleNamespace
from unittest.mock import MagicMock

from opentelemetry.sdk.trace.export import SpanExportResult

from agentscope_sdk.export_metrics import (
    ExportMetricsRecorder,
    ObservableBatchSpanProcessor,
)


def _processor(exporter: MagicMock, *, capacity: int = 4, batch: int = 2):
    recorder = ExportMetricsRecorder(queue_capacity=capacity)
    processor = ObservableBatchSpanProcessor(
        exporter,
        recorder=recorder,
        max_queue_size=capacity,
        max_export_batch_size=batch,
        schedule_delay_millis=60_000,
        export_timeout_millis=500,
    )
    return processor, recorder


def test_queue_full_drops_without_blocking_business_thread() -> None:
    release = threading.Event()
    exporter = MagicMock()
    exporter.export.side_effect = lambda spans: (
        release.wait(1),
        SpanExportResult.SUCCESS,
    )[1]
    exporter.force_flush.return_value = True
    processor, recorder = _processor(exporter, capacity=2, batch=1)
    started = time.monotonic()
    for _ in range(100):
        processor.on_end(SimpleNamespace(context=None))
    elapsed = time.monotonic() - started

    snapshot = recorder.snapshot()
    assert elapsed < 0.1
    assert snapshot.queue_capacity == 2
    assert snapshot.queue_drops > 0
    assert snapshot.spans_dropped == snapshot.queue_drops
    release.set()
    processor.shutdown()


def test_force_flush_exports_pending_spans_and_records_result() -> None:
    exporter = MagicMock()
    exporter.export.return_value = SpanExportResult.SUCCESS
    exporter.force_flush.return_value = True
    processor, recorder = _processor(exporter)

    processor.on_end(MagicMock(context=None))

    assert processor.force_flush(1000) is True
    assert exporter.export.call_count == 1
    assert recorder.snapshot().queue_depth == 0
    assert recorder.snapshot().last_flush_ok is True
    processor.shutdown()
    assert recorder.snapshot().last_shutdown_ok is True


def test_force_flush_timeout_is_queryable() -> None:
    release = threading.Event()
    exporter = MagicMock()
    exporter.export.side_effect = lambda spans: (
        release.wait(1),
        SpanExportResult.SUCCESS,
    )[1]
    exporter.force_flush.return_value = True
    processor, recorder = _processor(exporter, batch=1)

    processor.on_end(MagicMock(context=None))

    assert processor.force_flush(10) is False
    assert recorder.snapshot().last_flush_ok is False
    release.set()
    assert processor.force_flush(1000) is True
    processor.shutdown()


def test_snapshot_attributes_never_include_last_error_content() -> None:
    recorder = ExportMetricsRecorder(queue_capacity=8)
    recorder.update(last_error="Bearer secret response body")

    attrs = recorder.snapshot().as_attributes()

    assert "agentscope.sdk.export.last_error" not in attrs
    assert attrs["agentscope.sdk.export.queue_capacity"] == 8
