"""Tests for tracing configuration."""

from __future__ import annotations

from unittest.mock import MagicMock, patch

import pytest


class TestConfigureTracing:
    """Tests for the ``configure_tracing`` function."""

    def test_configure_uses_v1_traces_endpoint(self) -> None:
        """The OTLP exporter should be configured with the /v1/traces endpoint."""
        from agentscope_sdk.tracing import configure_tracing

        with (
            patch("agentscope_sdk.tracing.TracerProvider") as mock_provider,
            patch("agentscope_sdk.tracing.OTLPSpanExporter") as mock_exporter,
            patch("agentscope_sdk.tracing.SimpleSpanProcessor") as mock_processor,
        ):
            mock_exporter_instance = MagicMock()
            mock_exporter.return_value = mock_exporter_instance

            tracer = configure_tracing(
                server_url="http://test-server:8080",
                project_token="ascope_testtoken123",
                service_name="test-runner",
            )

            # Verify the exporter was created with /v1/traces endpoint
            mock_exporter.assert_called_once()
            call_kwargs = mock_exporter.call_args.kwargs
            assert call_kwargs["endpoint"] == "http://test-server:8080/v1/traces"

    def test_authorization_header_set(self) -> None:
        """The exporter should have the Authorization header set correctly."""
        from agentscope_sdk.tracing import configure_tracing

        with (
            patch("agentscope_sdk.tracing.TracerProvider") as mock_provider,
            patch("agentscope_sdk.tracing.OTLPSpanExporter") as mock_exporter,
            patch("agentscope_sdk.tracing.SimpleSpanProcessor") as mock_processor,
        ):
            configure_tracing(
                server_url="http://test-server:8080",
                project_token="ascope_sectoken456",
                service_name="test-runner",
            )

            mock_exporter.assert_called_once()
            call_kwargs = mock_exporter.call_args.kwargs
            headers = call_kwargs.get("headers", {})
            assert headers.get("Authorization") == "Bearer ascope_sectoken456"

    def test_server_url_trailing_slash_stripped(self) -> None:
        """A trailing slash on server_url should be stripped before appending /v1/traces."""
        from agentscope_sdk.tracing import configure_tracing

        with (
            patch("agentscope_sdk.tracing.TracerProvider") as mock_provider,
            patch("agentscope_sdk.tracing.OTLPSpanExporter") as mock_exporter,
            patch("agentscope_sdk.tracing.SimpleSpanProcessor") as mock_processor,
        ):
            configure_tracing(
                server_url="http://test-server:8080/",
                project_token="ascope_test",
            )

            mock_exporter.assert_called_once()
            call_kwargs = mock_exporter.call_args.kwargs
            assert call_kwargs["endpoint"] == "http://test-server:8080/v1/traces"

    def test_empty_token_raises(self) -> None:
        """Passing an empty project_token should raise RuntimeError."""
        from agentscope_sdk.tracing import configure_tracing

        with pytest.raises(RuntimeError, match="project_token is required"):
            configure_tracing(
                server_url="http://test-server:8080",
                project_token="",
            )

    def test_configure_sets_global_tracer_provider(self) -> None:
        """The global tracer provider should be set."""
        from agentscope_sdk.tracing import configure_tracing

        with (
            patch("agentscope_sdk.tracing.TracerProvider") as mock_provider,
            patch("agentscope_sdk.tracing.OTLPSpanExporter") as mock_exporter,
            patch("agentscope_sdk.tracing.SimpleSpanProcessor") as mock_processor,
            patch("agentscope_sdk.tracing.trace") as mock_trace,
        ):
            mock_provider_instance = MagicMock()
            mock_provider.return_value = mock_provider_instance

            configure_tracing(
                server_url="http://test-server:8080",
                project_token="ascope_token",
            )

            # set_tracer_provider should be called
            mock_trace.set_tracer_provider.assert_called_once_with(mock_provider_instance)

    def test_resource_attributes_include_service_name(self) -> None:
        """Resource attributes should include the service name."""
        from agentscope_sdk.tracing import configure_tracing
        from opentelemetry.sdk.resources import Resource

        with (
            patch("agentscope_sdk.tracing.TracerProvider") as mock_provider,
            patch("agentscope_sdk.tracing.OTLPSpanExporter") as mock_exporter,
            patch("agentscope_sdk.tracing.SimpleSpanProcessor") as mock_processor,
        ):
            configure_tracing(
                server_url="http://test-server:8080",
                project_token="ascope_token",
                service_name="my-custom-service",
            )

            mock_provider.assert_called_once()
            # The resource should have been created with service.name
            _, call_kwargs = mock_provider.call_args
            resource = call_kwargs.get("resource")
            assert resource is not None

    def test_resource_attributes_include_custom(self) -> None:
        """Custom resource attributes should be forwarded."""
        from agentscope_sdk.tracing import configure_tracing

        with (
            patch("agentscope_sdk.tracing.TracerProvider") as mock_provider,
            patch("agentscope_sdk.tracing.OTLPSpanExporter") as mock_exporter,
            patch("agentscope_sdk.tracing.SimpleSpanProcessor") as mock_processor,
            patch("agentscope_sdk.tracing.trace") as mock_trace,
        ):
            configure_tracing(
                server_url="http://test-server:8080",
                project_token="ascope_token",
                resource_attributes={"agentscope.run.id": "run-123", "agentscope.agent.name": "test-agent"},
            )

            mock_provider.assert_called_once()
            _, call_kwargs = mock_provider.call_args
            resource = call_kwargs.get("resource")
            assert resource is not None
            attrs = resource.attributes
            # service.name should still be set
            assert "service.name" in attrs
            # custom attributes should be present
            assert attrs.get("agentscope.run.id") == "run-123"


class TestShutdownAndFlush:
    """Tests for shutdown and force_flush."""

    def test_shutdown_tracing_calls_provider_shutdown(self) -> None:
        """``shutdown_tracing`` should call ``shutdown()`` on the global provider."""
        from agentscope_sdk.tracing import shutdown_tracing

        mock_provider = MagicMock()
        with patch("agentscope_sdk.tracing.trace") as mock_trace:
            mock_trace.get_tracer_provider.return_value = mock_provider
            shutdown_tracing()
            mock_provider.shutdown.assert_called_once()

    def test_force_flush_calls_provider_force_flush(self) -> None:
        """``force_flush`` should call ``force_flush()`` on the global provider."""
        from agentscope_sdk.tracing import force_flush

        mock_provider = MagicMock()
        with patch("agentscope_sdk.tracing.trace") as mock_trace:
            mock_trace.get_tracer_provider.return_value = mock_provider
            force_flush()
            mock_provider.force_flush.assert_called_once()
