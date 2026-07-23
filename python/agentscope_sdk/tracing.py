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
import logging
import os
import re
import threading
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any, Iterator, Sequence

from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.exporter.otlp.proto.http import Compression
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import (
    BatchSpanProcessor,
    SimpleSpanProcessor,
    SpanExporter,
    SpanExportResult,
    SpanProcessor,
)

from agentscope_sdk.redact import maybe_redact

_logger = logging.getLogger(__name__)

# Process-level cache so configure_tracing is idempotent.
# Key: (server_url, project_token)  ->  TracerProvider
_providers: dict[tuple[str, str], TracerProvider] = {}
_tracers: dict[tuple[str, str], trace.Tracer] = {}
_stats: dict[tuple[str, str], "_TracingStatsCounter"] = {}


@dataclass(frozen=True)
class TracingStats:
    """Process-local exporter health statistics for one SDK destination."""

    batches_attempted: int = 0
    batches_succeeded: int = 0
    batches_failed: int = 0
    spans_attempted: int = 0
    spans_exported: int = 0
    spans_dropped: int = 0
    retries: int = 0
    last_error: str = ""
    last_error_at: datetime | None = None
    last_success_at: datetime | None = None


class _TracingStatsCounter:
    """Thread-safe mutable implementation behind immutable statistics snapshots."""

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._stats = TracingStats()

    def update(self, **changes: Any) -> None:
        with self._lock:
            values = self._stats.__dict__.copy()
            for key, value in changes.items():
                values[key] = value
            self._stats = TracingStats(**values)

    def increment(self, **changes: int) -> None:
        with self._lock:
            values = self._stats.__dict__.copy()
            for key, value in changes.items():
                values[key] += value
            self._stats = TracingStats(**values)

    def snapshot(self) -> TracingStats:
        with self._lock:
            return self._stats


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
        self._counter = _stats.setdefault(self._key, _TracingStatsCounter())

    def export(self, spans: Sequence[Any]) -> SpanExportResult:
        span_count = len(spans)
        self._counter.increment(batches_attempted=1, spans_attempted=span_count)
        last_error = "export failed"

        for attempt in range(self._max_retries + 1):
            try:
                result = self._inner.export(spans)
            except Exception as exc:  # exporter failures must not crash the application
                result = SpanExportResult.FAILURE
                last_error = _sanitize_export_error(exc)
            else:
                if result == SpanExportResult.SUCCESS:
                    self._counter.increment(batches_succeeded=1, spans_exported=span_count)
                    self._counter.update(last_success_at=datetime.now(timezone.utc))
                    return result
                last_error = "export returned failure"

            if attempt < self._max_retries:
                self._counter.increment(retries=1)

        self._counter.increment(batches_failed=1, spans_dropped=span_count)
        self._counter.update(
            last_error=last_error,
            last_error_at=datetime.now(timezone.utc),
        )
        return SpanExportResult.FAILURE

    def shutdown(self) -> None:
        self._inner.shutdown()

    def force_flush(self, timeout_millis: int = 30000) -> bool:
        return self._inner.force_flush(timeout_millis)


def get_tracing_stats(server_url: str, project_token: str) -> TracingStats:
    """Return an immutable process-local exporter statistics snapshot."""
    counter = _stats.get((server_url, project_token))
    return counter.snapshot() if counter is not None else TracingStats()


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

    endpoint = f"{server_url.rstrip('/')}/v1/traces"
    cache_key = (server_url, project_token)

    # Return cached tracer if already configured for this endpoint
    if cache_key in _tracers:
        return _tracers[cache_key]

    resource = Resource.create({"service.name": service_name})

    inner_exporter = OTLPSpanExporter(
        endpoint=endpoint,
        headers={"Authorization": f"Bearer {project_token}"},
        timeout=30,
        compression=Compression.Gzip if _compression and _compression.lower() == "gzip" else Compression.NoCompression,
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
        processor = BatchSpanProcessor(exporter, **bsp_kwargs)

    # Wrap the processor with redaction support
    if enable_redaction:
        processor = RedactingSpanProcessor(processor)

    provider.add_span_processor(processor)

    # Only set global provider on first creation
    trace.set_tracer_provider(provider)

    tracer = trace.get_tracer(service_name)

    _providers[cache_key] = provider
    _tracers[cache_key] = tracer

    return tracer


def shutdown_tracing() -> None:
    """Shut down all cached tracer providers, flushing any pending spans.

    Safe to call multiple times — subsequent calls are no-ops.
    Call once at process exit (e.g. after the runner loop completes).
    """
    # Iterate over a snapshot of the keys since we mutate the dict
    keys = list(_providers.keys())
    for key in keys:
        provider = _providers.pop(key, None)
        if provider is not None:
            try:
                provider.shutdown()
            except Exception:
                _logger.exception("error shutting down tracer provider for %s", key[0])
        _tracers.pop(key, None)


def force_flush() -> None:
    """Force-flush pending spans through all cached providers."""
    for provider in _providers.values():
        try:
            provider.force_flush()
        except Exception:
            _logger.exception("error flushing spans")


# ---- Redacting Span Processor ----


def set_redacted_attribute(span: trace.Span, key: str, value: Any) -> None:
    """Set a span attribute after applying redaction.

    Sensitive keys (``api_key``, ``token``, ``password``, etc.) are replaced
    with ``"[REDACTED]"``. PII patterns (email, phone, ID) are redacted in
    string values.

    Use this in all deep-trace helpers instead of raw ``span.set_attribute``
    to ensure secrets never reach the backend.
    """
    from agentscope_sdk.redact import maybe_redact

    redacted = maybe_redact(key, value)
    span.set_attribute(key, redacted)


class RedactingSpanProcessor(SpanProcessor):
    """A ``SpanProcessor`` wrapper that redacts sensitive span attributes
    ``on_start`` before the inner processor sees them.

    Sensitive keys (``api_key``, ``token``, ``password``, etc.) are replaced
    with ``"[REDACTED]"``. PII patterns (email, phone, ID) are redacted in
    string values.
    """

    def __init__(self, inner: SpanProcessor) -> None:
        self._inner = inner

    def on_start(self, span, parent_context=None) -> None:  # type: ignore[no-untyped-def]
        self._redact_attributes(span)
        self._inner.on_start(span, parent_context)

    def on_end(self, span_data) -> None:  # type: ignore[no-untyped-def]
        self._inner.on_end(span_data)

    def force_flush(self, timeout_millis: int = 30000) -> bool:
        return self._inner.force_flush(timeout_millis)

    def shutdown(self) -> None:
        self._inner.shutdown()

    @staticmethod
    def _redact_attributes(span) -> None:  # type: ignore[no-untyped-def]
        """Iterate over the span's attributes and redact sensitive ones."""
        try:
            # OTel SDK stores attributes in span._attributes (dict).
            # We iterate a snapshot and update in place.
            attrs = dict(span._attributes) if hasattr(span, "_attributes") else {}  # type: ignore[union-attr]
            if not attrs:
                return
            changed = False
            for key, value in attrs.items():
                redacted = maybe_redact(key, value)
                if redacted is not value:
                    span.set_attribute(key, redacted)  # type: ignore[union-attr]
                    changed = True
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
    cost: float | None = None,
    capture_content: bool = False,
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
    with tracer.start_as_current_span(name) as span:
        set_redacted_attribute(span, "openinference.span.kind", "LLM")
        set_redacted_attribute(span, "agentscope.span.kind", "LLM")
        if provider:
            set_redacted_attribute(span, "llm.provider", provider)
        if model:
            set_redacted_attribute(span, "llm.model", model)
        if input_summary:
            set_redacted_attribute(span, "llm.input_summary", _truncate(input_summary))
        if output_summary:
            set_redacted_attribute(span, "llm.output_summary", _truncate(output_summary))
        if input_tokens is not None:
            set_redacted_attribute(span, "llm.input_tokens", input_tokens)
        if output_tokens is not None:
            set_redacted_attribute(span, "llm.output_tokens", output_tokens)
        if input_tokens is not None and output_tokens is not None:
            set_redacted_attribute(span, "llm.total_tokens", input_tokens + output_tokens)
        if cost is not None:
            set_redacted_attribute(span, "llm.cost", cost)
        if capture_content:
            set_redacted_attribute(span, "agentscope.content_captured", True)
        for k, v in attrs.items():
            set_redacted_attribute(span, k, v)
        yield span


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
    with tracer.start_as_current_span(name) as span:
        set_redacted_attribute(span, "openinference.span.kind", "TOOL")
        set_redacted_attribute(span, "agentscope.span.kind", "TOOL")
        set_redacted_attribute(span, "tool.name", name)
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
        for k, v in attrs.items():
            set_redacted_attribute(span, k, v)
        yield span


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
    with tracer.start_as_current_span(name) as span:
        set_redacted_attribute(span, "openinference.span.kind", "RETRIEVER")
        set_redacted_attribute(span, "agentscope.span.kind", "RETRIEVER")
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
    with tracer.start_as_current_span(name) as span:
        set_redacted_attribute(span, "openinference.span.kind", "SKILL")
        set_redacted_attribute(span, "agentscope.span.kind", "SKILL")
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
    with tracer.start_as_current_span(name) as span:
        set_redacted_attribute(span, "agentscope.span.kind", "THOUGHT")
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
    with tracer.start_as_current_span(name) as span:
        set_redacted_attribute(span, "agentscope.span.kind", "FINAL_ANSWER")
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
    result: dict[str, Any] = {"agentscope.span.kind": "THOUGHT"}
    if summary:
        result["thought.summary"] = maybe_redact("thought.summary", _truncate(summary))
    if capture_content:
        result["agentscope.content_captured"] = True
        if content:
            result["thought.content"] = maybe_redact("thought.content", _truncate(content))
    if stage:
        result["thought.stage"] = stage
    if confidence is not None:
        result["thought.confidence"] = confidence
    for k, v in attrs.items():
        result[k] = maybe_redact(k, v)
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
    result: dict[str, Any] = {"agentscope.span.kind": "FINAL_ANSWER"}
    if summary:
        result["answer.summary"] = maybe_redact("answer.summary", _truncate(summary))
    if output:
        result["answer.output"] = maybe_redact("answer.output", _truncate(output))
        result["agentscope.content_captured"] = True
    if structured_output:
        result["answer.structured_output"] = maybe_redact("answer.structured_output", _truncate(structured_output))
        result["agentscope.content_captured"] = True
    for k, v in attrs.items():
        result[k] = maybe_redact(k, v)
    return result

def _clear_cache() -> None:
    """Clear the internal provider and tracer cache (for testing only)."""
    _providers.clear()
    _tracers.clear()
    _stats.clear()


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
        span.set_attribute("agentscope.run.id", run_id)
        return

    # No active span — create a short-lived span just to carry the attribute
    if tracer is None:
        tracer_provided = trace.get_tracer(__name__)
    else:
        tracer_provided = tracer
    with tracer_provided.start_as_current_span("agentscope.associate") as s:
        s.set_attribute("agentscope.run.id", run_id)
