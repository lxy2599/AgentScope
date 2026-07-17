"""OpenTelemetry tracing configuration for AgentScope.

Provides helpers to configure OpenTelemetry tracing with OTLP HTTP export
directed at the AgentScope backend's ``/v1/traces`` endpoint.

Usage::

    from agentscope_sdk.tracing import configure_tracing, shutdown_tracing

    tracer = configure_tracing(
        server_url="http://127.0.0.1:8080",
        project_token="ascope_...",
        service_name="my-runner",
    )

    with tracer.start_as_current_span("my-span") as span:
        span.set_attribute("key", "value")

    shutdown_tracing()
"""

from __future__ import annotations

import logging
from typing import Any

from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor

_logger = logging.getLogger(__name__)


def configure_tracing(
    server_url: str,
    project_token: str,
    *,
    service_name: str = "agentscope-runner",
    resource_attributes: dict[str, Any] | None = None,
) -> trace.Tracer:
    """Configure OpenTelemetry tracing with OTLP HTTP export.

    Sets up a global ``TracerProvider`` with an ``OTLPSpanExporter`` that
    sends spans to the AgentScope backend's ``/v1/traces`` OTLP ingestion
    endpoint.  Uses ``SimpleSpanProcessor`` so spans are exported
    synchronously — this makes failures visible to the caller.

    Args:
        server_url: Base URL of the AgentScope backend (e.g. ``http://127.0.0.1:8080``).
        project_token: Project token for bearer authentication.
        service_name: Logical service name reported in resource attributes.
        resource_attributes: Optional extra resource attributes to attach to
            every span (e.g. ``{"agentscope.run.id": "..."}``).

    Returns:
        A ``trace.Tracer`` instance for creating spans.

    Raises:
        RuntimeError: If the project token is empty.
    """
    if not project_token:
        raise RuntimeError("project_token is required for tracing")

    endpoint = f"{server_url.rstrip('/')}/v1/traces"

    attrs: dict[str, Any] = {"service.name": service_name}
    if resource_attributes:
        attrs.update(resource_attributes)

    resource = Resource.create(attrs)

    exporter = OTLPSpanExporter(
        endpoint=endpoint,
        headers={"Authorization": f"Bearer {project_token}"},
        timeout=30,
    )

    provider = TracerProvider(resource=resource)
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    trace.set_tracer_provider(provider)

    return trace.get_tracer(service_name)


def shutdown_tracing() -> None:
    """Shut down the global tracer provider, flushing any pending spans."""
    provider = trace.get_tracer_provider()
    try:
        provider.shutdown()
    except Exception:
        _logger.exception("error shutting down tracer provider")


def force_flush() -> None:
    """Force-flush any pending spans through the export pipeline."""
    provider = trace.get_tracer_provider()
    try:
        provider.force_flush()
    except Exception:
        _logger.exception("error flushing spans")
