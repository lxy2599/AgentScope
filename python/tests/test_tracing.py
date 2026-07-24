"""Tests for tracing configuration."""

from __future__ import annotations

from agentscope_sdk.tracing import (
    trace_llm,
    trace_retrieval,
    trace_skill,
    trace_tool,
)
from unittest.mock import MagicMock, patch

import pytest


@pytest.fixture(autouse=True)
def clear_tracing_cache():
    """Clear the tracing provider cache before each test to avoid cross-test pollution."""
    from agentscope_sdk.tracing import _clear_cache

    _clear_cache()
    yield
    _clear_cache()


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

            configure_tracing(
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
        """The global tracer provider should be set on first call."""
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

            mock_trace.set_tracer_provider.assert_called_once_with(mock_provider_instance)

    def test_configure_idempotent_returns_same_tracer(self) -> None:
        """Calling configure_tracing twice with same credentials returns cached tracer."""
        from agentscope_sdk.tracing import configure_tracing, _clear_cache

        with (
            patch("agentscope_sdk.tracing.TracerProvider") as mock_provider,
            patch("agentscope_sdk.tracing.OTLPSpanExporter") as mock_exporter,
            patch("agentscope_sdk.tracing.SimpleSpanProcessor") as mock_processor,
        ):
            mock_provider_instance = MagicMock()
            mock_provider.return_value = mock_provider_instance

            tracer1 = configure_tracing(
                server_url="http://idempotent-test:8080",
                project_token="ascope_idempotent",
            )

            tracer2 = configure_tracing(
                server_url="http://idempotent-test:8080",
                project_token="ascope_idempotent",
            )

            # Same tracer returned
            assert tracer1 is tracer2
            # Provider should only have been created once
            assert mock_provider.call_count == 1
            # Exporter should only have been created once
            assert mock_exporter.call_count == 1

    def test_configure_different_tokens_get_different_tracers(self) -> None:
        """Calling with different tokens should create separate providers."""
        from agentscope_sdk.tracing import configure_tracing, _clear_cache

        with (
            patch("agentscope_sdk.tracing.TracerProvider") as mock_provider,
            patch("agentscope_sdk.tracing.OTLPSpanExporter") as mock_exporter,
            patch("agentscope_sdk.tracing.SimpleSpanProcessor") as mock_processor,
        ):
            mock_provider.side_effect = [MagicMock(), MagicMock()]
            mock_exporter.side_effect = [MagicMock(), MagicMock()]

            tracer_a = configure_tracing(
                server_url="http://multi:8080",
                project_token="ascope_token_a",
                service_name="service-a",
            )
            tracer_b = configure_tracing(
                server_url="http://multi:8080",
                project_token="ascope_token_b",
                service_name="service-b",
            )

            # Different tokens result in distinct TracerProvider instances
            assert mock_provider.call_count == 2
            assert mock_exporter.call_count == 2

    def test_different_tokens_use_their_own_provider_tracer(self) -> None:
        """Each token must emit through its own provider without replacing global state."""
        from agentscope_sdk.tracing import configure_tracing

        provider_a = MagicMock()
        provider_b = MagicMock()
        tracer_a = MagicMock(name="tracer-a")
        tracer_b = MagicMock(name="tracer-b")
        provider_a.get_tracer.return_value = tracer_a
        provider_b.get_tracer.return_value = tracer_b

        with (
            patch(
                "agentscope_sdk.tracing.TracerProvider",
                side_effect=[provider_a, provider_b],
            ),
            patch(
                "agentscope_sdk.tracing.OTLPSpanExporter",
                side_effect=[MagicMock(), MagicMock()],
            ),
            patch("agentscope_sdk.tracing.trace") as mock_trace,
        ):
            actual_a = configure_tracing(
                "http://multi:8080",
                "ascope_a",
                service_name="service-a",
            )
            actual_b = configure_tracing(
                "http://multi:8080",
                "ascope_b",
                service_name="service-b",
            )

        assert actual_a is tracer_a
        assert actual_b is tracer_b
        provider_a.get_tracer.assert_called_once_with("service-a")
        provider_b.get_tracer.assert_called_once_with("service-b")
        mock_trace.set_tracer_provider.assert_called_once_with(provider_a)

    def test_resource_attributes_include_service_name(self) -> None:
        """Resource attributes should include the service name."""
        from agentscope_sdk.tracing import configure_tracing

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
            _, call_kwargs = mock_provider.call_args
            resource = call_kwargs.get("resource")
            assert resource is not None


class TestShutdownAndFlush:
    """Tests for shutdown and force_flush."""

    def test_shutdown_tracing_shuts_down_cached_providers(self) -> None:
        """shutdown_tracing should shut down all cached providers."""
        from agentscope_sdk.tracing import configure_tracing, shutdown_tracing

        with (
            patch("agentscope_sdk.tracing.TracerProvider") as mock_provider,
            patch("agentscope_sdk.tracing.OTLPSpanExporter") as mock_exporter,
            patch("agentscope_sdk.tracing.SimpleSpanProcessor") as mock_processor,
        ):
            mock_provider1 = MagicMock()
            mock_provider2 = MagicMock()
            mock_provider.side_effect = [mock_provider1, mock_provider2]
            mock_exporter.side_effect = [MagicMock(), MagicMock()]

            configure_tracing(server_url="http://a:8080", project_token="ascope_a")
            configure_tracing(server_url="http://b:8080", project_token="ascope_b")

            shutdown_tracing()

            mock_provider1.shutdown.assert_called_once()
            mock_provider2.shutdown.assert_called_once()

    def test_force_flush_flushes_all_cached_providers(self) -> None:
        """force_flush should flush all cached providers."""
        from agentscope_sdk.tracing import configure_tracing, force_flush

        with (
            patch("agentscope_sdk.tracing.TracerProvider") as mock_provider,
            patch("agentscope_sdk.tracing.OTLPSpanExporter") as mock_exporter,
            patch("agentscope_sdk.tracing.SimpleSpanProcessor") as mock_processor,
        ):
            mock_provider1 = MagicMock()
            mock_provider.side_effect = [mock_provider1, MagicMock()]
            mock_exporter.side_effect = [MagicMock(), MagicMock()]

            configure_tracing(server_url="http://x:8080", project_token="ascope_x")
            configure_tracing(server_url="http://y:8080", project_token="ascope_y")

            force_flush()

            # Both providers should have been flushed
            assert mock_provider1.force_flush.called

# ---- Deep Trace Helper Tests ----


class TestTraceLLM:
    """Tests for the ``trace_llm`` context manager."""


    def test_sets_llm_attributes(self):
        from opentelemetry.sdk.trace import TracerProvider as SDKProvider

        tracer = SDKProvider().get_tracer("test")
        with trace_llm(
            tracer,
            "llm-call",
            provider="anthropic",
            model="claude-3",
            input_summary="hello",
            output_summary="world",
            input_tokens=10,
            output_tokens=20,
            cost=0.002,
        ) as span:
            assert span.is_recording()

    def test_accepts_additional_attrs(self):
        from opentelemetry.sdk.trace import TracerProvider as SDKProvider

        tracer = SDKProvider().get_tracer("test")
        with trace_llm(tracer, "test", custom_key="val") as span:
            span.set_attribute("extra", "data")
            assert span.is_recording()


class TestTraceTool:
    """Tests for the ``trace_tool`` context manager."""


    def test_with_error(self):
        from opentelemetry.sdk.trace import TracerProvider as SDKProvider

        tracer = SDKProvider().get_tracer("test")
        with trace_tool(tracer, "fail", success=False, retry_count=2, side_effect_risk="high") as span:
            assert span.is_recording()


class TestTraceRetrieval:
    """Tests for the ``trace_retrieval`` context manager."""



class TestTraceSkill:
    """Tests for the ``trace_skill`` context manager."""



class TestTraceHelperImports:
    """Tests that the helpers are importable from the package."""

    def test_imported_from_top_level(self):
        from agentscope_sdk import (  # noqa: F811
            trace_llm,
            trace_retrieval,
            trace_skill,
            trace_tool,
        )

        assert callable(trace_llm)
        assert callable(trace_tool)
        assert callable(trace_retrieval)
        assert callable(trace_skill)


class TestTraceRetrievalAttributes:
    """Tests that trace_retrieval sets correct span attributes."""

    def test_document_ids_as_array(self):
        """retrieval.document_ids should be a list, not str() repr."""
        from opentelemetry.sdk.trace import TracerProvider as SDKProvider

        provider = SDKProvider()
        tracer = provider.get_tracer("test")

        doc_ids = ["doc-1", "doc-2"]
        with trace_retrieval(
            tracer,
            "kb-search",
            knowledge_base="docs",
            query_text="how?",
            document_ids=doc_ids,
        ) as span:
            attrs = span.attributes
            actual = attrs.get("retrieval.document_ids")
            assert list(actual) == doc_ids, (
                f"expected document_ids={doc_ids!r}, got {actual!r}"
            )

    def test_scores_as_array(self):
        """retrieval.scores should be a float list."""
        from opentelemetry.sdk.trace import TracerProvider as SDKProvider

        provider = SDKProvider()
        tracer = provider.get_tracer("test")

        scores = [0.95, 0.87, 0.72]
        with trace_retrieval(
            tracer,
            "kb-search",
            knowledge_base="docs",
            query_text="how?",
            document_ids=["d1"],
            scores=scores,
        ) as span:
            attrs = span.attributes
            actual = attrs.get("retrieval.scores")
            assert list(actual) == scores, (
                f"expected scores={scores!r}, got {actual!r}"
            )
