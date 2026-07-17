"""OpenTelemetry tracing configuration for AgentScope.

Provides helpers to configure OpenTelemetry tracing with OTLP HTTP export
directed at the AgentScope backend's ``/v1/traces`` endpoint.

**Lifecycle:** ``configure_tracing`` is idempotent — calling it multiple times
with the same ``(server_url, project_token)`` pair returns the same tracer.
The underlying ``TracerProvider`` is created once per connection.

Job-level attributes (``agentscope.run.id``, etc.) must be set as **span**
attributes, **not** passed as resource attributes, so they vary per span
without creating a new provider per job.

Usage::

    from agentscope_sdk.tracing import configure_tracing, shutdown_tracing

    tracer = configure_tracing(
        server_url="http://127.0.0.1:8080",
        project_token="ascope_...",
    )

    with tracer.start_as_current_span("my-span") as span:
        span.set_attribute("key", "value")

    shutdown_tracing()   # call once at process exit
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


# ---- Internal helpers for tests ----

def _clear_cache() -> None:
    """Clear the internal provider and tracer cache (for testing only)."""
    _providers.clear()
    _tracers.clear()
