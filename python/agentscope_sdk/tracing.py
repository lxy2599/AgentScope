"""OpenTelemetry tracing configuration for AgentScope.

Provides helpers to configure OpenTelemetry tracing with OTLP HTTP export
directed at the AgentScope backend's ``/v1/traces`` endpoint, as well as
context-manager helpers for deep-trace instrumentation of LLM calls, tool
calls, knowledge-base retrievals, and skill invocations.

**Lifecycle:** ``configure_tracing`` is idempotent — calling it multiple times
with the same ``(server_url, project_token)`` pair returns the same tracer.
The underlying ``TracerProvider`` is created once per connection.

**Deep Trace Helpers:**

Each helper creates an OTel span with ``agentscope.*`` and ``openinference.*``
attributes that the backend uses to extract structured call records.  Usage::

    from agentscope_sdk.tracing import (
        configure_tracing,
        trace_llm,
        trace_tool,
        trace_retrieval,
        trace_skill,
    )

    tracer = configure_tracing(
        server_url="http://127.0.0.1:8080",
        project_token="ascope_...",
    )

    with trace_llm(tracer, "llm-call", provider="openai", model="gpt-4",
                   input_summary="hello", output_summary="world",
                   input_tokens=10, output_tokens=20):
        ...

    with trace_tool(tracer, "search", tool_type="api",
                    arguments_summary='{"q":"test"}',
                    result_summary="found 3 results") as span:
        span.set_attribute("custom.key", "value")

    with trace_retrieval(tracer, "kb-search",
                         knowledge_base="docs", query_text="how to?",
                         top_k=5, returned_count=3):
        ...

    with trace_skill(tracer, "refund-check",
                     skill_version="1.0.0",
                     input_summary="order-123"):
        ...
"""

from __future__ import annotations

import contextlib
import json
import logging
import os
import re
import threading
import time
from collections.abc import Iterator, Mapping, Sequence
from copy import copy
from datetime import UTC, datetime
from typing import Any

from opentelemetry import trace
from opentelemetry.context import Context
from opentelemetry.exporter.otlp.proto.http import Compression
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import (
    SimpleSpanProcessor,
    SpanExporter,
    SpanExportResult,
    SpanProcessor,
)

from agentscope_sdk.export_metrics import (
    ExportMetricsRecorder,
    ExportMetricsSnapshot,
    ObservableBatchSpanProcessor,
)
from agentscope_sdk.redact import is_sensitive_key, maybe_redact
from agentscope_sdk.tracing_contract import SemanticKind, SpanAttribute, SpanName

_logger = logging.getLogger(__name__)

# Process-level cache so configure_tracing is idempotent.
# Key: (server_url, project_token)  ->  TracerProvider
_providers: dict[tuple[str, str], TracerProvider] = {}
_tracers: dict[tuple[str, str], trace.Tracer] = {}
_stats: dict[tuple[str, str], ExportMetricsRecorder] = {}
_cache_lock = threading.RLock()
_global_provider_registered = False


TracingStats = ExportMetricsSnapshot


def _sanitize_export_error(error: BaseException | str) -> str:
    """Return an error category without retaining credentials or response bodies."""
    value = str(error)
    value = re.sub(r"(?i)bearer\\s+[^\\s]+", "Bearer [REDACTED]", value)
    value = re.sub(r"ascope_[A-Za-z0-9_\\-]+", "[REDACTED]", value)
    return _truncate(value, 256)


class ObservedSpanExporter(SpanExporter):
    """Exporter wrapper that records outcomes and retries transient exporter failures."""

    def __init__(
        self,
        inner: SpanExporter,
        *,
        server_url: str,
        project_token: str,
        max_retries: int = 2,
    ) -> None:
        self._inner = inner
        self._key = (server_url, project_token)
        self._max_retries = max(0, max_retries)
        with _cache_lock:
            self._counter = _stats.setdefault(
                self._key,
                ExportMetricsRecorder(),
            )

    def export(self, spans: Sequence[Any]) -> SpanExportResult:
        span_count = len(spans)
        self._counter.increment(batches_attempted=1, spans_attempted=span_count)
        self._counter.update(last_batch_size=span_count)
        last_error = "export failed"
        started_at = time.monotonic()

        for attempt in range(self._max_retries + 1):
            try:
                result = self._inner.export(spans)
            except Exception as exc:  # exporter failures must not crash the application
                result = SpanExportResult.FAILURE
                last_error = _sanitize_export_error(exc)
            else:
                if result == SpanExportResult.SUCCESS:
                    self._counter.increment(batches_succeeded=1, spans_exported=span_count)
                    self._counter.update(
                        last_export_latency_ms=(time.monotonic() - started_at) * 1000,
                        last_success_at=datetime.now(UTC),
                    )
                    return result
                last_error = "export returned failure"

            if attempt < self._max_retries:
                self._counter.increment(retries=1)

        self._counter.increment(batches_failed=1, spans_dropped=span_count)
        self._counter.update(
            last_export_latency_ms=(time.monotonic() - started_at) * 1000,
            last_error=last_error,
            last_error_at=datetime.now(UTC),
        )
        return SpanExportResult.FAILURE

    def shutdown(self) -> None:
        self._inner.shutdown()

    def force_flush(self, timeout_millis: int = 30000) -> bool:
        return self._inner.force_flush(timeout_millis)


def get_tracing_stats(server_url: str, project_token: str) -> TracingStats:
    """Return an immutable process-local exporter statistics snapshot."""
    with _cache_lock:
        counter = _stats.get((server_url.rstrip("/"), project_token))
    return counter.snapshot() if counter is not None else ExportMetricsSnapshot()


def record_export_metrics(server_url: str, project_token: str) -> bool:
    """Emit one content-free SDK exporter health span when tracing is configured."""
    key = (server_url.rstrip("/"), project_token)
    with _cache_lock:
        tracer = _tracers.get(key)
        counter = _stats.get(key)
    if tracer is None or counter is None:
        return False
    with tracer.start_as_current_span(SpanName.EXPORT_METRICS) as span:
        set_redacted_attribute(span, SpanAttribute.SEMANTIC_KIND, SemanticKind.SDK_EXPORT_METRICS)
        for name, value in counter.snapshot().as_attributes().items():
            set_redacted_attribute(span, name, value)
    return True


def _positive_int(value: int | None, env_name: str, default: int) -> int:
    """Resolve a positive config value, falling back to a safe default."""
    if value is None:
        try:
            value = int(os.environ.get(env_name, str(default)))
        except (TypeError, ValueError):
            value = default
    return value if value > 0 else default


def _non_negative_int(value: int | None, env_name: str, default: int) -> int:
    """Resolve a non-negative config value, preserving an explicit zero."""
    if value is None:
        try:
            value = int(os.environ.get(env_name, str(default)))
        except (TypeError, ValueError):
            value = default
    return value if value >= 0 else default


def configure_tracing(
    server_url: str,
    project_token: str,
    *,
    service_name: str = "agentscope-runner",
    export_batch_size: int | None = None,
    export_delay_ms: int | None = None,
    export_timeout_ms: int | None = None,
    export_compression: str | None = None,
    export_queue_size: int | None = None,
    max_export_retries: int | None = None,
    capture_content: bool = False,
    enable_redaction: bool = True,
) -> trace.Tracer:
    """Configure OpenTelemetry tracing with OTLP HTTP export.

    **Idempotent:** the second and subsequent calls with the same
    ``(server_url, project_token)`` pair return a cached tracer without
    creating a new ``TracerProvider`` or calling ``set_tracer_provider`` again.

    Args:
        server_url: Base URL of the AgentScope backend (e.g. ``http://127.0.0.1:8080``).
        project_token: Project token for bearer authentication.
        service_name: Logical service name reported in resource attributes.
        export_batch_size: Max spans per batch. ``None`` (default) reads from
            env ``AGENTSCOPE_EXPORT_BATCH_SIZE`` (default 512). ``0`` or ``1``
            uses ``SimpleSpanProcessor`` (synchronous, each span sent immediately).
        export_delay_ms: Batch processor schedule delay in ms.  Reads from env
            ``AGENTSCOPE_EXPORT_DELAY_MS`` (default OTel: 5000).
        export_timeout_ms: Export timeout in ms.  Reads from env
            ``AGENTSCOPE_EXPORT_TIMEOUT_MS`` (default OTel: 30000).
        export_compression: Compression algorithm for exported payloads.
            Reads from env ``AGENTSCOPE_EXPORT_COMPRESSION`` (default ``"gzip"``).
            Set to ``"none"`` to disable compression.
        export_queue_size: Maximum local SDK queue size. Reads from
            ``AGENTSCOPE_EXPORT_QUEUE_SIZE`` (default 2048).
        max_export_retries: Exporter retry count after a failed batch. Reads
            from ``AGENTSCOPE_EXPORT_MAX_RETRIES`` (default 2).
        capture_content: If ``True``, spans will carry the
            ``agentscope.content_captured`` attribute.  Default ``False``.
            Reads from env ``AGENTSCOPE_CAPTURE_CONTENT``.
        enable_redaction: If ``True`` (default), sensitive keys
            (``api_key``, ``token``, ``password``, etc.) and PII in span
            attributes are redacted before export.

    Returns:
        A ``trace.Tracer`` instance for creating spans.

    Raises:
        RuntimeError: If the project token is empty.
    """
    if not project_token:
        raise RuntimeError("project_token is required for tracing")
    server_url = server_url.rstrip("/")

    # Resolve config from env vars first, then kwargs
    _batch_size = export_batch_size
    if _batch_size is None:
        try:
            _batch_size = int(os.environ.get("AGENTSCOPE_EXPORT_BATCH_SIZE", "512"))
        except (ValueError, TypeError):
            _batch_size = 512

    _delay_ms = export_delay_ms
    if _delay_ms is None:
        try:
            _delay_ms = int(os.environ.get("AGENTSCOPE_EXPORT_DELAY_MS", "5000"))
        except (ValueError, TypeError):
            _delay_ms = 5000

    _timeout_ms = export_timeout_ms
    if _timeout_ms is None:
        try:
            _timeout_ms = int(os.environ.get("AGENTSCOPE_EXPORT_TIMEOUT_MS", "30000"))
        except (ValueError, TypeError):
            _timeout_ms = 30000

    _compression = export_compression or os.environ.get("AGENTSCOPE_EXPORT_COMPRESSION", "gzip")
    _queue_size = _positive_int(export_queue_size, "AGENTSCOPE_EXPORT_QUEUE_SIZE", 2048)
    _max_retries = _non_negative_int(max_export_retries, "AGENTSCOPE_EXPORT_MAX_RETRIES", 2)

    _capture = capture_content
    if not _capture:
        _capture = os.environ.get("AGENTSCOPE_CAPTURE_CONTENT", "").lower() in ("1", "true", "yes")

    endpoint = f"{server_url}/v1/traces"
    cache_key = (server_url, project_token)

    # Keep the lookup and construction in one critical section. Configuration
    # is rare, while returning two live providers for the same credentials
    # would duplicate exports and leak resources.
    with _cache_lock:
        cached_tracer = _tracers.get(cache_key)
        if cached_tracer is not None:
            return cached_tracer

        resource = Resource.create({"service.name": service_name})

        inner_exporter = OTLPSpanExporter(
            endpoint=endpoint,
            headers={"Authorization": f"Bearer {project_token}"},
            timeout=30,
            compression=(
                Compression.Gzip if _compression and _compression.lower() == "gzip" else Compression.NoCompression
            ),
        )
        recorder = _stats.setdefault(
            cache_key,
            ExportMetricsRecorder(queue_capacity=_queue_size),
        )
        exporter: SpanExporter = ObservedSpanExporter(
            inner_exporter,
            server_url=server_url,
            project_token=project_token,
            max_retries=_max_retries,
        )

        provider = TracerProvider(resource=resource)

        # Determine span processor: Batch (default) or Simple
        if _batch_size is not None and _batch_size <= 1:
            processor: SpanProcessor = SimpleSpanProcessor(exporter)
        else:
            _queue_size = max(_queue_size, _batch_size)
            bsp_kwargs: dict[str, Any] = {
                "max_export_batch_size": _batch_size,
                "schedule_delay_millis": _delay_ms,
                "export_timeout_millis": _timeout_ms,
                "max_queue_size": _queue_size,
            }
            processor = ObservableBatchSpanProcessor(
                exporter,
                recorder=recorder,
                **bsp_kwargs,
            )

        if enable_redaction:
            processor = RedactingSpanProcessor(processor)

        provider.add_span_processor(processor)

        # Preserve the historical global registration for instrumentation
        # that relies on it, but never obtain this destination's tracer from
        # the global provider: OpenTelemetry accepts global registration only
        # once, so doing so would bind later destinations to the first one.
        global _global_provider_registered
        if not _global_provider_registered:
            trace.set_tracer_provider(provider)
            _global_provider_registered = True

        tracer = provider.get_tracer(service_name)
        _providers[cache_key] = provider
        _tracers[cache_key] = tracer
        return tracer


def shutdown_tracing() -> dict[tuple[str, str], TracingStats]:
    """Shut down all cached tracer providers, flushing any pending spans.

    Safe to call multiple times — subsequent calls are no-ops.
    Call once at process exit (e.g. after the runner loop completes).
    """
    with _cache_lock:
        keys = list(_providers)
    results: dict[tuple[str, str], TracingStats] = {}
    for key in keys:
        record_export_metrics(*key)
        with _cache_lock:
            provider = _providers.pop(key, None)
            _tracers.pop(key, None)
        if provider is not None:
            try:
                provider.shutdown()
            except Exception:
                _logger.exception("error shutting down tracer provider for %s", key[0])
        results[key] = get_tracing_stats(*key)
    return results


def force_flush(timeout_millis: int = 30000) -> dict[tuple[str, str], bool]:
    """Force-flush pending spans through all cached providers."""
    with _cache_lock:
        providers = list(_providers.items())

    results: dict[tuple[str, str], bool] = {}
    for key, provider in providers:
        try:
            record_export_metrics(*key)
            results[key] = bool(provider.force_flush(timeout_millis))
        except Exception:
            _logger.exception("error flushing spans")
            results[key] = False
    return results


# ---- Redacting Span Processor ----


_MAX_REDACTION_DEPTH = 20


def _redact_attribute_value(
    key: str,
    value: Any,
    *,
    _depth: int = 0,
) -> Any:
    """Apply the redaction contract recursively to an attribute value.

    OpenTelemetry attributes are normally scalars or homogeneous sequences,
    but payloads are often stored as JSON strings. Recursing into mappings,
    sequences, and JSON containers prevents a nested sensitive key from
    bypassing the key-based redaction rules.
    """
    if _depth >= _MAX_REDACTION_DEPTH:
        return "[REDACTED]"

    redacted = maybe_redact(key, value)
    if is_sensitive_key(key):
        return redacted

    if isinstance(redacted, str):
        stripped = redacted.strip()
        if not (
            (stripped.startswith("{") and stripped.endswith("}"))
            or (stripped.startswith("[") and stripped.endswith("]"))
        ):
            return redacted
        try:
            parsed = json.loads(redacted)
        except (TypeError, ValueError):
            return redacted
        nested = _redact_attribute_value("", parsed, _depth=_depth + 1)
        if nested == parsed:
            return redacted
        return json.dumps(
            nested,
            ensure_ascii=False,
            separators=(",", ":"),
        )

    if isinstance(redacted, Mapping):
        return {
            str(nested_key): _redact_attribute_value(
                str(nested_key),
                nested_value,
                _depth=_depth + 1,
            )
            for nested_key, nested_value in redacted.items()
        }

    if isinstance(redacted, tuple):
        return tuple(_redact_attribute_value("", item, _depth=_depth + 1) for item in redacted)

    if isinstance(redacted, list):
        return [_redact_attribute_value("", item, _depth=_depth + 1) for item in redacted]

    return redacted


def _redact_attribute_mapping(
    attributes: Mapping[str, Any] | None,
) -> dict[str, Any]:
    """Return a fail-closed redacted copy of an OTel attribute mapping."""
    if not attributes:
        return {}

    redacted: dict[str, Any] = {}
    for key, value in attributes.items():
        try:
            redacted[key] = _redact_attribute_value(key, value)
        except Exception:
            # Attribute types are constrained by OTel, but a custom
            # implementation should not turn an unexpected type into a leak.
            redacted[key] = "[REDACTED]"
            _logger.debug(
                "redaction failed for span attribute; value suppressed",
                exc_info=True,
            )
    return redacted


def _copy_with_redacted_attributes(item: Any) -> Any:
    """Shallow-copy an OTel event/link and replace its immutable attributes."""
    redacted_item = copy(item)
    redacted_item._attributes = _redact_attribute_mapping(
        item.attributes,
    )
    return redacted_item


def set_redacted_attribute(span: trace.Span, key: str, value: Any) -> None:
    """Set a span attribute after applying redaction.

    Sensitive keys (``api_key``, ``token``, ``password``, etc.) are replaced
    with ``"[REDACTED]"``. PII patterns (email, phone, ID) are redacted in
    string values.

    Use this in all deep-trace helpers instead of raw ``span.set_attribute``
    to ensure secrets never reach the backend.
    """
    redacted = _redact_attribute_value(key, value)
    span.set_attribute(key, redacted)


class RedactingSpanProcessor(SpanProcessor):
    """A ``SpanProcessor`` wrapper that redacts sensitive span attributes
    before the inner processor can hand them to an exporter.

    Sensitive keys (``api_key``, ``token``, ``password``, etc.) are replaced
    with ``"[REDACTED]"``. PII patterns (email, phone, ID) are redacted in
    string values. A redacted ``ReadableSpan`` snapshot is made at ``on_end``
    so attributes and events added after ``on_start`` cannot bypass redaction.
    """

    def __init__(self, inner: SpanProcessor) -> None:
        self._inner = inner

    def on_start(self, span, parent_context=None) -> None:
        self._redact_attributes(span)
        self._inner.on_start(span, parent_context)

    def on_end(self, span_data) -> None:
        try:
            redacted_span = copy(span_data)
            redacted_span._attributes = _redact_attribute_mapping(
                span_data.attributes,
            )
            redacted_span._events = tuple(_copy_with_redacted_attributes(event) for event in span_data.events)
            redacted_span._links = tuple(_copy_with_redacted_attributes(link) for link in span_data.links)
        except Exception:
            # Privacy is fail-closed: a malformed/custom span is dropped
            # rather than forwarding an unredacted object to the exporter.
            _logger.warning(
                "dropping span because a redacted export snapshot could not be created",
                exc_info=True,
            )
            return
        self._inner.on_end(redacted_span)

    def force_flush(self, timeout_millis: int = 30000) -> bool:
        return self._inner.force_flush(timeout_millis)

    def shutdown(self) -> None:
        self._inner.shutdown()

    @staticmethod
    def _redact_attributes(span) -> None:
        """Iterate over the span's attributes and redact sensitive ones."""
        try:
            # OTel SDK stores attributes in span._attributes (dict).
            # We iterate a snapshot and update in place.
            attrs = dict(span._attributes) if hasattr(span, "_attributes") else {}
            if not attrs:
                return
            for key, value in attrs.items():
                redacted = _redact_attribute_value(key, value)
                if redacted != value:
                    span.set_attribute(key, redacted)
        except Exception:
            _logger.debug("redaction error (non-fatal)", exc_info=True)


# ---- Deep Trace Helper Context Managers ----

_MAX_CONTENT_BYTES = 4096


def _truncate(value: str, max_bytes: int = _MAX_CONTENT_BYTES) -> str:
    """Truncate a string to *max_bytes* bytes."""
    if not value:
        return ""
    encoded = value.encode("utf-8")
    if len(encoded) <= max_bytes:
        return value
    return encoded[:max_bytes].decode("utf-8", errors="ignore")


def _start_llm_span(
    tracer: trace.Tracer,
    name: str,
    *,
    parent_context: Context | None = None,
    provider: str = "",
    model: str = "",
    input_summary: str = "",
    output_summary: str = "",
    input_tokens: int | None = None,
    output_tokens: int | None = None,
    total_tokens: int | None = None,
    cache_read_tokens: int | None = None,
    cache_write_tokens: int | None = None,
    ttft_ms: int | None = None,
    cost: float | None = None,
    estimated_cost: float | None = None,
    actual_cost: float | None = None,
    cost_source: str = "",
    capture_content: bool = False,
    **attrs: Any,
) -> trace.Span:
    span = tracer.start_span(name, context=parent_context)
    set_redacted_attribute(span, SpanAttribute.OPENINFERENCE_KIND, SemanticKind.LLM)
    set_redacted_attribute(span, SpanAttribute.SEMANTIC_KIND, SemanticKind.LLM)
    if provider:
        set_redacted_attribute(span, SpanAttribute.LLM_PROVIDER, provider)
    if model:
        set_redacted_attribute(span, SpanAttribute.LLM_MODEL, model)
    if input_summary:
        set_redacted_attribute(span, "llm.input_summary", _truncate(input_summary))
    if output_summary:
        set_redacted_attribute(span, "llm.output_summary", _truncate(output_summary))
    set_llm_metrics(
        span,
        input_tokens=input_tokens,
        output_tokens=output_tokens,
        total_tokens=total_tokens,
        cache_read_tokens=cache_read_tokens,
        cache_write_tokens=cache_write_tokens,
        ttft_ms=ttft_ms,
        cost=cost,
        estimated_cost=estimated_cost,
        actual_cost=actual_cost,
        cost_source=cost_source,
    )
    if capture_content:
        set_redacted_attribute(span, "agentscope.content_captured", True)
    for key, value in attrs.items():
        set_redacted_attribute(span, key, value)
    return span


@contextlib.contextmanager
def trace_llm(
    tracer: trace.Tracer,
    name: str,
    *,
    provider: str = "",
    model: str = "",
    input_summary: str = "",
    output_summary: str = "",
    input_tokens: int | None = None,
    output_tokens: int | None = None,
    total_tokens: int | None = None,
    cache_read_tokens: int | None = None,
    cache_write_tokens: int | None = None,
    ttft_ms: int | None = None,
    cost: float | None = None,
    estimated_cost: float | None = None,
    actual_cost: float | None = None,
    cost_source: str = "",
    capture_content: bool = False,
    parent_context: Context | None = None,
    **attrs: Any,
) -> Iterator[trace.Span]:
    """Context manager that creates a span for a single LLM call.

    The span carries ``agentscope.*`` and ``openinference.*`` attributes
    so the backend can extract a structured ``llm_calls`` record.

    Args:
        tracer: OpenTelemetry tracer instance.
        name: Span name (e.g. ``"llm.call"``).
        provider: LLM provider name (e.g. ``"openai"``, ``"anthropic"``).
        model: Model identifier (e.g. ``"gpt-4"``).
        input_summary: Short summary of the input / prompt.
        output_summary: Short summary of the output / response.
        input_tokens: Number of input (prompt) tokens.
        output_tokens: Number of output (completion) tokens.
        cost: Cost of this LLM call in USD (or arbitrary unit).
        capture_content: If ``True``, store the full input/output
            (up to ``_MAX_CONTENT_BYTES``).  Default ``False``.
        **attrs: Additional key-value attributes to attach to the span.

    Yields:
        The created ``trace.Span`` for further attribute manipulation.
    """
    span = _start_llm_span(
        tracer,
        name,
        parent_context=parent_context,
        provider=provider,
        model=model,
        input_summary=input_summary,
        output_summary=output_summary,
        input_tokens=input_tokens,
        output_tokens=output_tokens,
        total_tokens=total_tokens,
        cache_read_tokens=cache_read_tokens,
        cache_write_tokens=cache_write_tokens,
        ttft_ms=ttft_ms,
        cost=cost,
        estimated_cost=estimated_cost,
        actual_cost=actual_cost,
        cost_source=cost_source,
        capture_content=capture_content,
        **attrs,
    )
    with trace.use_span(span, end_on_exit=True):
        yield span


def set_llm_metrics(
    span: trace.Span,
    *,
    input_tokens: int | None = None,
    output_tokens: int | None = None,
    total_tokens: int | None = None,
    cache_read_tokens: int | None = None,
    cache_write_tokens: int | None = None,
    ttft_ms: int | None = None,
    cost: float | None = None,
    estimated_cost: float | None = None,
    actual_cost: float | None = None,
    cost_source: str = "",
) -> None:
    """Attach normalized LLM performance and cost attributes."""
    if input_tokens is not None:
        set_redacted_attribute(span, "llm.input_tokens", max(0, input_tokens))
    if output_tokens is not None:
        set_redacted_attribute(span, "llm.output_tokens", max(0, output_tokens))
    minimum_total = (
        max(0, input_tokens) + max(0, output_tokens) if input_tokens is not None and output_tokens is not None else None
    )
    if total_tokens is not None or minimum_total is not None:
        normalized_total = max(0, total_tokens or 0)
        if minimum_total is not None:
            normalized_total = max(normalized_total, minimum_total)
        set_redacted_attribute(span, "llm.total_tokens", normalized_total)
    if cache_read_tokens is not None:
        set_redacted_attribute(span, "llm.cache_read_tokens", max(0, cache_read_tokens))
    if cache_write_tokens is not None:
        set_redacted_attribute(span, "llm.cache_write_tokens", max(0, cache_write_tokens))
    if ttft_ms is not None:
        set_redacted_attribute(span, "llm.ttft_ms", max(0, ttft_ms))
    if estimated_cost is not None:
        set_redacted_attribute(span, "llm.estimated_cost", max(0.0, estimated_cost))
    selected_cost: float | None
    if actual_cost is not None:
        set_redacted_attribute(span, "llm.actual_cost", max(0.0, actual_cost))

    if actual_cost is not None:
        selected_cost = actual_cost
        selected_source = "actual"
    else:
        selected_cost = cost
        selected_source = cost_source or "reported"
    if selected_cost is None:
        selected_cost = estimated_cost
        selected_source = "estimated"
    if selected_cost is not None:
        set_redacted_attribute(span, "llm.cost", max(0.0, selected_cost))
        set_redacted_attribute(span, "llm.cost_source", selected_source)


@contextlib.contextmanager
def trace_tool(
    tracer: trace.Tracer,
    name: str,
    *,
    tool_type: str = "",
    arguments_summary: str = "",
    result_summary: str = "",
    success: bool = True,
    retry_count: int = 0,
    side_effect_risk: str = "",
    parent_context: Context | None = None,
    **attrs: Any,
) -> Iterator[trace.Span]:
    """Context manager that creates a span for a tool call.

    The span carries ``openinference.span.kind = "TOOL"`` so the backend
    can extract a structured ``tool_calls`` record.

    Args:
        tracer: OpenTelemetry tracer instance.
        name: Span name (e.g. ``"search_order"``).
        tool_type: Type of the tool (e.g. ``"api"``, ``"function"``).
        arguments_summary: Short summary of the invocation arguments.
        result_summary: Short summary of the tool's result.
        success: Whether the tool call succeeded.
        retry_count: Number of retries.
        side_effect_risk: Risk level (``"low"``, ``"medium"``, ``"high"``).
        **attrs: Additional key-value attributes.

    Yields:
        The created ``trace.Span``.
    """
    span = _start_tool_span(
        tracer,
        name,
        parent_context=parent_context,
        tool_type=tool_type,
        arguments_summary=arguments_summary,
        result_summary=result_summary,
        success=success,
        retry_count=retry_count,
        side_effect_risk=side_effect_risk,
        **attrs,
    )
    with trace.use_span(span, end_on_exit=True):
        yield span


def _start_tool_span(
    tracer: trace.Tracer,
    name: str,
    *,
    parent_context: Context | None = None,
    tool_type: str = "",
    arguments_summary: str = "",
    result_summary: str = "",
    success: bool = True,
    retry_count: int = 0,
    side_effect_risk: str = "",
    **attrs: Any,
) -> trace.Span:
    span = tracer.start_span(name, context=parent_context)
    set_redacted_attribute(span, SpanAttribute.OPENINFERENCE_KIND, SemanticKind.TOOL)
    set_redacted_attribute(span, SpanAttribute.SEMANTIC_KIND, SemanticKind.TOOL)
    set_redacted_attribute(span, SpanAttribute.TOOL_NAME, name)
    if tool_type:
        set_redacted_attribute(span, "tool.type", tool_type)
    if arguments_summary:
        set_redacted_attribute(span, "tool.arguments_summary", _truncate(arguments_summary))
    if result_summary:
        set_redacted_attribute(span, "tool.result_summary", _truncate(result_summary))
    set_redacted_attribute(span, "tool.success", str(success).lower())
    set_redacted_attribute(span, "tool.retry_count", retry_count)
    if side_effect_risk:
        set_redacted_attribute(span, "tool.side_effect_risk", side_effect_risk)
    for key, value in attrs.items():
        set_redacted_attribute(span, key, value)
    return span


@contextlib.contextmanager
def trace_retrieval(
    tracer: trace.Tracer,
    name: str,
    *,
    knowledge_base: str = "",
    query_text: str = "",
    top_k: int | None = None,
    returned_count: int | None = None,
    document_ids: list[str] | None = None,
    scores: list[float] | None = None,
    parent_context: Context | None = None,
    **attrs: Any,
) -> Iterator[trace.Span]:
    """Context manager that creates a span for a knowledge-base retrieval.

    The span carries ``openinference.span.kind = "RETRIEVER"`` so the
    backend can extract a structured ``retrieval_calls`` record.

    Args:
        tracer: OpenTelemetry tracer instance.
        name: Span name (e.g. ``"kb.search"``).
        knowledge_base: Name of the knowledge base or index.
        query_text: The search query.
        top_k: Number of requested results.
        returned_count: Number of actual returned results.
        document_ids: List of returned document identifiers.
        scores: List of relevance scores.
        **attrs: Additional key-value attributes.

    Yields:
        The created ``trace.Span``.
    """
    with tracer.start_as_current_span(name, context=parent_context) as span:
        set_redacted_attribute(span, SpanAttribute.OPENINFERENCE_KIND, SemanticKind.RETRIEVER)
        set_redacted_attribute(span, SpanAttribute.SEMANTIC_KIND, SemanticKind.RETRIEVER)
        if knowledge_base:
            set_redacted_attribute(span, "retrieval.knowledge_base", knowledge_base)
        if query_text:
            set_redacted_attribute(span, "retrieval.query", _truncate(query_text))
        if top_k is not None:
            set_redacted_attribute(span, "retrieval.top_k", top_k)
        if returned_count is not None:
            set_redacted_attribute(span, "retrieval.returned_count", returned_count)
        if document_ids:
            set_redacted_attribute(span, "retrieval.document_ids", document_ids)
        if scores:
            set_redacted_attribute(span, "retrieval.scores", scores)
        for k, v in attrs.items():
            set_redacted_attribute(span, k, v)
        yield span


@contextlib.contextmanager
def trace_skill(
    tracer: trace.Tracer,
    name: str,
    *,
    skill_version: str = "",
    input_summary: str = "",
    output_summary: str = "",
    success: bool = True,
    parent_context: Context | None = None,
    **attrs: Any,
) -> Iterator[trace.Span]:
    """Context manager that creates a span for a skill invocation.

    The span carries ``openinference.span.kind = "SKILL"`` so the backend
    can extract a structured ``skill_calls`` record.

    Args:
        tracer: OpenTelemetry tracer instance.
        name: Span name (skill name).
        skill_version: Version of the skill being invoked.
        input_summary: Short input summary.
        output_summary: Short output summary.
        success: Whether the skill completed successfully.
        **attrs: Additional key-value attributes.

    Yields:
        The created ``trace.Span``.
    """
    with tracer.start_as_current_span(name, context=parent_context) as span:
        set_redacted_attribute(span, SpanAttribute.OPENINFERENCE_KIND, SemanticKind.SKILL)
        set_redacted_attribute(span, SpanAttribute.SEMANTIC_KIND, SemanticKind.SKILL)
        set_redacted_attribute(span, "skill.name", name)
        if skill_version:
            set_redacted_attribute(span, "skill.version", skill_version)
        if input_summary:
            set_redacted_attribute(span, "skill.input_summary", _truncate(input_summary))
        if output_summary:
            set_redacted_attribute(span, "skill.output_summary", _truncate(output_summary))
        set_redacted_attribute(span, "skill.success", str(success).lower())
        for k, v in attrs.items():
            set_redacted_attribute(span, k, v)
        yield span


# ---- Semantic Agent Trace Helpers (Thought & Final Answer) ----


@contextlib.contextmanager
def trace_thought(
    tracer: trace.Tracer,
    name: str,
    *,
    summary: str = "",
    capture_content: bool = False,
    content: str = "",
    stage: str = "",
    confidence: float | None = None,
    parent_context: Context | None = None,
    **attrs: Any,
) -> Iterator[trace.Span]:
    """Context manager that creates a span for an agent's internal thought.

    By default, only ``thought.summary`` is recorded.  Set *capture_content*
    to ``True`` to also record the full thought content.

    The span carries ``agentscope.span.kind = "THOUGHT"`` so the backend
    can extract a structured ``trace_thoughts`` record.

    Args:
        tracer: OpenTelemetry tracer instance.
        name: Span name (e.g. ``"agent.think"``).
        summary: Short summary of the thought (always recorded).
        capture_content: If ``True``, store the full thought content.
        content: The full thought text (only stored when *capture_content* is set).
        stage: Optional stage label (e.g. ``"reasoning"``, ``"planning"``).
        confidence: Optional confidence score (0.0 to 1.0).
        **attrs: Additional key-value attributes.

    Yields:
        The created ``trace.Span`` for further attribute manipulation.
    """
    with tracer.start_as_current_span(name, context=parent_context) as span:
        set_redacted_attribute(span, SpanAttribute.SEMANTIC_KIND, SemanticKind.THOUGHT)
        if summary:
            set_redacted_attribute(span, "thought.summary", _truncate(summary))
        if capture_content:
            set_redacted_attribute(span, "agentscope.content_captured", True)
            if content:
                set_redacted_attribute(span, "thought.content", _truncate(content))
        if stage:
            set_redacted_attribute(span, "thought.stage", stage)
        if confidence is not None:
            set_redacted_attribute(span, "thought.confidence", confidence)
        for k, v in attrs.items():
            set_redacted_attribute(span, k, v)
        yield span


@contextlib.contextmanager
def record_final_answer(
    tracer: trace.Tracer,
    name: str,
    *,
    summary: str = "",
    output: str = "",
    structured_output: str = "",
    parent_context: Context | None = None,
    **attrs: Any,
) -> Iterator[trace.Span]:
    """Context manager that creates a span for the agent's final answer.

    The span carries ``agentscope.span.kind = "FINAL_ANSWER"`` so the
    backend can extract a structured ``trace_final_answers`` record.

    Args:
        tracer: OpenTelemetry tracer instance.
        name: Span name (e.g. ``"agent.finish"``).
        summary: Short summary of the final answer.
        output: The full output text of the final answer.
        structured_output: Optional structured representation (JSON, etc.).
        **attrs: Additional key-value attributes.

    Yields:
        The created ``trace.Span`` for further attribute manipulation.
    """
    with tracer.start_as_current_span(name, context=parent_context) as span:
        set_redacted_attribute(span, SpanAttribute.SEMANTIC_KIND, SemanticKind.FINAL_ANSWER)
        if summary:
            set_redacted_attribute(span, "answer.summary", _truncate(summary))
        if output:
            set_redacted_attribute(span, "answer.output", _truncate(output))
            set_redacted_attribute(span, "agentscope.content_captured", True)
        if structured_output:
            set_redacted_attribute(span, "answer.structured_output", _truncate(structured_output))
            set_redacted_attribute(span, "agentscope.content_captured", True)
        for k, v in attrs.items():
            set_redacted_attribute(span, k, v)
        yield span


# ---- Attribute Builders (convenience, no tracer needed) ----


def build_thought_attributes(
    *,
    summary: str = "",
    capture_content: bool = False,
    content: str = "",
    stage: str = "",
    confidence: float | None = None,
    **attrs: Any,
) -> dict[str, Any]:
    """Build a dictionary of span attributes for a THOUGHT span.

    Unlike ``trace_thought``, this function does not create a span — it only
    returns the attributes dict.  Useful when you want to batch-set attributes
    on an existing span::

        span.set_attributes(build_thought_attributes(summary="..."))

    Args:
        summary: Short summary of the thought.
        capture_content: If ``True``, include the full content.
        content: The full thought text.
        stage: Optional stage label.
        confidence: Optional confidence score.
        **attrs: Additional attributes (will be redacted).

    Returns:
        A dict of attributes safe to pass to ``span.set_attributes``.
    """
    result: dict[str, Any] = {SpanAttribute.SEMANTIC_KIND: SemanticKind.THOUGHT}
    if summary:
        result["thought.summary"] = _redact_attribute_value(
            "thought.summary",
            _truncate(summary),
        )
    if capture_content:
        result["agentscope.content_captured"] = True
        if content:
            result["thought.content"] = _redact_attribute_value(
                "thought.content",
                _truncate(content),
            )
    if stage:
        result["thought.stage"] = stage
    if confidence is not None:
        result["thought.confidence"] = confidence
    for k, v in attrs.items():
        result[k] = _redact_attribute_value(k, v)
    return result


def build_final_answer_attributes(
    *,
    summary: str = "",
    output: str = "",
    structured_output: str = "",
    **attrs: Any,
) -> dict[str, Any]:
    """Build a dictionary of span attributes for a FINAL_ANSWER span.

    Unlike ``record_final_answer``, this function does not create a span.
    Useful when you want to batch-set attributes on an existing span.

    Args:
        summary: Short summary of the final answer.
        output: The full output text.
        structured_output: Optional structured representation.
        **attrs: Additional attributes (will be redacted).

    Returns:
        A dict of attributes safe to pass to ``span.set_attributes``.
    """
    result: dict[str, Any] = {SpanAttribute.SEMANTIC_KIND: SemanticKind.FINAL_ANSWER}
    if summary:
        result["answer.summary"] = _redact_attribute_value(
            "answer.summary",
            _truncate(summary),
        )
    if output:
        result["answer.output"] = _redact_attribute_value(
            "answer.output",
            _truncate(output),
        )
        result["agentscope.content_captured"] = True
    if structured_output:
        result["answer.structured_output"] = _redact_attribute_value(
            "answer.structured_output",
            _truncate(structured_output),
        )
        result["agentscope.content_captured"] = True
    for k, v in attrs.items():
        result[k] = _redact_attribute_value(k, v)
    return result


def _clear_cache() -> None:
    """Clear the internal provider and tracer cache (for testing only)."""
    global _global_provider_registered
    with _cache_lock:
        _providers.clear()
        _tracers.clear()
        _stats.clear()
        _global_provider_registered = False


def associate_run_with_trace(run_id: str, tracer: trace.Tracer | None = None) -> None:
    """Associate a run ID with the current trace context.

    Sets ``agentscope.run.id`` on the current span so that the ingestion
    handler can find the existing SDK-created run and attach spans to it,
    instead of creating a duplicate run.

    Call this immediately after ``runtime.start_run()`` returns, e.g.::

        run = ascope_runtime.start_run(input=case_input)
        if run and run.id:
            associate_run_with_trace(run.id, tracer)

    Args:
        run_id: The run ID returned by ``start_run()``.
        tracer: Optional OTel tracer. If omitted, uses the current tracer
            from ``opentelemetry.trace``.
    """
    # Always use the current span if one exists (e.g. runner's root span)
    span = trace.get_current_span()
    if span and span.is_recording():
        span.set_attribute(SpanAttribute.RUN_ID, run_id)
        return

    # No active span — create a short-lived span just to carry the attribute
    tracer_provided = trace.get_tracer(__name__) if tracer is None else tracer
    with tracer_provided.start_as_current_span(SpanName.ASSOCIATE) as span:
        span.set_attribute(SpanAttribute.RUN_ID, run_id)
