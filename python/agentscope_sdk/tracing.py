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
from typing import Any, Iterator

from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor

_logger = logging.getLogger(__name__)

# Process-level cache so configure_tracing is idempotent.
# Key: (server_url, project_token)  ->  TracerProvider
_providers: dict[tuple[str, str], TracerProvider] = {}
_tracers: dict[tuple[str, str], trace.Tracer] = {}


def configure_tracing(
    server_url: str,
    project_token: str,
    *,
    service_name: str = "agentscope-runner",
) -> trace.Tracer:
    """Configure OpenTelemetry tracing with OTLP HTTP export.

    **Idempotent:** the second and subsequent calls with the same
    ``(server_url, project_token)`` pair return a cached tracer without
    creating a new ``TracerProvider`` or calling ``set_tracer_provider`` again.

    Args:
        server_url: Base URL of the AgentScope backend (e.g. ``http://127.0.0.1:8080``).
        project_token: Project token for bearer authentication.
        service_name: Logical service name reported in resource attributes.

    Returns:
        A ``trace.Tracer`` instance for creating spans.

    Raises:
        RuntimeError: If the project token is empty.
    """
    if not project_token:
        raise RuntimeError("project_token is required for tracing")

    endpoint = f"{server_url.rstrip('/')}/v1/traces"
    cache_key = (server_url, project_token)

    # Return cached tracer if already configured for this endpoint
    if cache_key in _tracers:
        return _tracers[cache_key]

    resource = Resource.create({"service.name": service_name})

    exporter = OTLPSpanExporter(
        endpoint=endpoint,
        headers={"Authorization": f"Bearer {project_token}"},
        timeout=30,
    )

    provider = TracerProvider(resource=resource)
    provider.add_span_processor(SimpleSpanProcessor(exporter))

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
        span.set_attribute("openinference.span.kind", "LLM")
        span.set_attribute("agentscope.span.kind", "LLM")
        if provider:
            span.set_attribute("llm.provider", provider)
        if model:
            span.set_attribute("llm.model", model)
        if input_summary:
            span.set_attribute("llm.input_summary", _truncate(input_summary))
        if output_summary:
            span.set_attribute("llm.output_summary", _truncate(output_summary))
        if input_tokens is not None:
            span.set_attribute("llm.input_tokens", input_tokens)
        if output_tokens is not None:
            span.set_attribute("llm.output_tokens", output_tokens)
        if input_tokens is not None and output_tokens is not None:
            span.set_attribute("llm.total_tokens", input_tokens + output_tokens)
        if cost is not None:
            span.set_attribute("llm.cost", cost)
        if capture_content:
            span.set_attribute("agentscope.content_captured", True)
        for k, v in attrs.items():
            span.set_attribute(k, v)
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
        span.set_attribute("openinference.span.kind", "TOOL")
        span.set_attribute("agentscope.span.kind", "TOOL")
        span.set_attribute("tool.name", name)
        if tool_type:
            span.set_attribute("tool.type", tool_type)
        if arguments_summary:
            span.set_attribute("tool.arguments_summary", _truncate(arguments_summary))
        if result_summary:
            span.set_attribute("tool.result_summary", _truncate(result_summary))
        span.set_attribute("tool.success", str(success).lower())
        span.set_attribute("tool.retry_count", retry_count)
        if side_effect_risk:
            span.set_attribute("tool.side_effect_risk", side_effect_risk)
        for k, v in attrs.items():
            span.set_attribute(k, v)
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
        span.set_attribute("openinference.span.kind", "RETRIEVER")
        span.set_attribute("agentscope.span.kind", "RETRIEVER")
        if knowledge_base:
            span.set_attribute("retrieval.knowledge_base", knowledge_base)
        if query_text:
            span.set_attribute("retrieval.query", _truncate(query_text))
        if top_k is not None:
            span.set_attribute("retrieval.top_k", top_k)
        if returned_count is not None:
            span.set_attribute("retrieval.returned_count", returned_count)
        if document_ids:
            span.set_attribute("retrieval.document_ids", str(document_ids))
        if scores:
            span.set_attribute("retrieval.scores", str(scores))
        for k, v in attrs.items():
            span.set_attribute(k, v)
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
        span.set_attribute("openinference.span.kind", "SKILL")
        span.set_attribute("agentscope.span.kind", "SKILL")
        span.set_attribute("skill.name", name)
        if skill_version:
            span.set_attribute("skill.version", skill_version)
        if input_summary:
            span.set_attribute("skill.input_summary", _truncate(input_summary))
        if output_summary:
            span.set_attribute("skill.output_summary", _truncate(output_summary))
        span.set_attribute("skill.success", str(success).lower())
        for k, v in attrs.items():
            span.set_attribute(k, v)
        yield span


# ---- Internal helpers for tests ----

def _clear_cache() -> None:
    """Clear the internal provider and tracer cache (for testing only)."""
    _providers.clear()
    _tracers.clear()
