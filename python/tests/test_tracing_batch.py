"""Tests for Phase 2.7 batch export and redaction in AgentScope tracing."""
from __future__ import annotations

from unittest.mock import MagicMock, patch

import pytest


@pytest.fixture(autouse=True)
def clear_tracing_cache():
    """Clear the tracing provider cache before each test."""
    from agentscope_sdk.tracing import _clear_cache

    _clear_cache()
    yield
    _clear_cache()


class TestBatchExport:
    """Tests for observable batch processor configuration."""

    def test_configure_uses_batch_processor_by_default(self) -> None:
        """Default configure_tracing should use the observable batch processor."""
        from agentscope_sdk.tracing import configure_tracing

        with (
            patch("agentscope_sdk.tracing.TracerProvider") as mock_provider,
            patch("agentscope_sdk.tracing.OTLPSpanExporter") as mock_exporter,
            patch("agentscope_sdk.tracing.ObservableBatchSpanProcessor") as mock_bsp,
        ):
            mock_provider_instance = MagicMock()
            mock_provider.return_value = mock_provider_instance

            configure_tracing(
                server_url="http://test:8080",
                project_token="ascope_batch_test",
            )

            # The bounded observable processor should be created.
            mock_bsp.assert_called_once()

    def test_export_batch_size_passed_to_batch_processor(self) -> None:
        """Batch settings should be passed to the observable processor."""
        from agentscope_sdk.tracing import configure_tracing

        with (
            patch("agentscope_sdk.tracing.TracerProvider") as mock_provider,
            patch("agentscope_sdk.tracing.OTLPSpanExporter") as mock_exporter,
            patch("agentscope_sdk.tracing.ObservableBatchSpanProcessor") as mock_bsp,
        ):
            mock_provider.return_value = MagicMock()

            configure_tracing(
                server_url="http://test:8080",
                project_token="ascope_batch_size",
                export_batch_size=128,
                export_delay_ms=2000,
                export_timeout_ms=15000,
            )

            mock_bsp.assert_called_once()
            _, kwargs = mock_bsp.call_args
            assert kwargs.get("max_export_batch_size") == 128
            assert kwargs.get("schedule_delay_millis") == 2000
            assert kwargs.get("export_timeout_millis") == 15000

    def test_simple_span_processor_when_batch_size_is_one(self) -> None:
        """Setting export_batch_size <= 1 should use SimpleSpanProcessor."""
        from agentscope_sdk.tracing import configure_tracing

        with (
            patch("agentscope_sdk.tracing.TracerProvider") as mock_provider,
            patch("agentscope_sdk.tracing.OTLPSpanExporter") as mock_exporter,
            patch("agentscope_sdk.tracing.SimpleSpanProcessor") as mock_ssp,
        ):
            mock_provider.return_value = MagicMock()

            configure_tracing(
                server_url="http://test:8080",
                project_token="ascope_simple",
                export_batch_size=0,
            )

            mock_ssp.assert_called_once()

    def test_config_from_env_vars(self) -> None:
        """Environment variables should be read as defaults."""
        from agentscope_sdk.tracing import configure_tracing

        with (
            patch("agentscope_sdk.tracing.TracerProvider") as mock_provider,
            patch("agentscope_sdk.tracing.OTLPSpanExporter") as mock_exporter,
            patch("agentscope_sdk.tracing.ObservableBatchSpanProcessor") as mock_bsp,
        ):
            mock_provider.return_value = MagicMock()
            mock_exporter.return_value = MagicMock()

            with patch.dict("os.environ", {
                "AGENTSCOPE_EXPORT_BATCH_SIZE": "256",
                "AGENTSCOPE_EXPORT_DELAY_MS": "3000",
                "AGENTSCOPE_EXPORT_TIMEOUT_MS": "20000",
                "AGENTSCOPE_EXPORT_COMPRESSION": "gzip",
            }, clear=False):
                tracer = configure_tracing(
                    server_url="http://test:8080",
                    project_token="ascope_env",
                )

                # Verify tracer was returned
                assert tracer is not None
                # Verify the observable processor received env configuration.
                mock_bsp.assert_called_once()
                _, kwargs = mock_bsp.call_args
                assert kwargs.get("max_export_batch_size") == 256, f"Got {kwargs}"
                assert kwargs.get("schedule_delay_millis") == 3000
                assert kwargs.get("export_timeout_millis") == 20000

    def test_compression_default_gzip(self) -> None:
        """The exporter should be created with compression=Compression.Gzip by default."""
        from agentscope_sdk.tracing import configure_tracing
        from opentelemetry.exporter.otlp.proto.http import Compression

        with (
            patch("agentscope_sdk.tracing.TracerProvider") as mock_provider,
            patch("agentscope_sdk.tracing.OTLPSpanExporter") as mock_exporter,
        ):
            mock_provider.return_value = MagicMock()

            configure_tracing(
                server_url="http://test:8080",
                project_token="ascope_comp",
            )

            mock_exporter.assert_called_once()
            _, kwargs = mock_exporter.call_args
            assert kwargs.get("compression") == Compression.Gzip


class TestRedaction:
    """Tests for the RedactingSpanProcessor integration."""

    def test_redacting_span_processor_wrapped(self) -> None:
        """When redaction is enabled, RedactingSpanProcessor should wrap the processor."""
        from agentscope_sdk.tracing import configure_tracing, RedactingSpanProcessor

        with (
            patch("agentscope_sdk.tracing.TracerProvider") as mock_provider,
            patch("agentscope_sdk.tracing.OTLPSpanExporter") as mock_exporter,
            patch("agentscope_sdk.tracing.ObservableBatchSpanProcessor") as mock_bsp,
        ):
            mock_provider_instance = MagicMock()
            mock_provider.return_value = mock_provider_instance

            configure_tracing(
                server_url="http://test:8080",
                project_token="ascope_redact",
            )

            # add_span_processor should have been called with a RedactingSpanProcessor
            assert mock_provider_instance.add_span_processor.called

    def test_redacting_span_processor_redacts_attributes(self) -> None:
        """Verify the RedactingSpanProcessor redacts sensitive attributes on_start."""
        from agentscope_sdk.tracing import RedactingSpanProcessor
        from opentelemetry.sdk.trace import Span as SDKThresholdSpan
        from opentelemetry.sdk.trace.export import SimpleSpanProcessor

        inner = MagicMock(spec=SimpleSpanProcessor)
        processor = RedactingSpanProcessor(inner)

        # Create a real SDK span so we can set attributes
        from opentelemetry.sdk.trace import TracerProvider as SDKProvider

        provider = SDKProvider()
        tracer = provider.get_tracer("test")
        span = tracer.start_span("test-span")
        span.set_attribute("api_key", "sk-abc123")
        span.set_attribute("model", "gpt-4")
        span.set_attribute("user_input", "my email is user@example.com")

        processor.on_start(span)

        # After on_start, sensitive attributes should be redacted
        attrs = dict(span._attributes)  # type: ignore[union-attr]
        assert attrs.get("api_key") == "[REDACTED]"
        assert attrs.get("model") == "gpt-4"
        assert "[EMAIL_REDACTED]" in attrs.get("user_input", "")

        span.end()
        provider.shutdown()


class TestDeepTraceRedaction:
    """Tests that deep trace helpers redact sensitive in-band."""

    def test_trace_llm_redacts_api_key_in_attrs(self) -> None:
        """trace_llm with api_key in **attrs should have it redacted."""
        from opentelemetry.sdk.trace import TracerProvider as SDKProvider
        from agentscope_sdk.tracing import trace_llm

        provider = SDKProvider()
        tracer = provider.get_tracer("test")
        with trace_llm(
            tracer,
            "test-call",
            provider="openai",
            model="gpt-4",
            api_key="sk-abc123",
            input_summary="user@example.com",
            input_tokens=10,
            output_tokens=20,
        ) as span:
            attrs = dict(span._attributes)  # type: ignore[union-attr]
            assert attrs.get("api_key") == "[REDACTED]"
            input_val = attrs.get("llm.input_summary", "")
            assert "[EMAIL_REDACTED]" in input_val
            assert attrs.get("llm.provider") == "openai"
        provider.shutdown()

    def test_llm_metrics_are_consistent_and_actual_cost_wins(self) -> None:
        from opentelemetry.sdk.trace import TracerProvider as SDKProvider
        from agentscope_sdk.tracing import trace_llm

        provider = SDKProvider()
        tracer = provider.get_tracer("test")
        with trace_llm(
            tracer,
            "llm",
            input_tokens=100,
            output_tokens=20,
            total_tokens=80,
            cache_read_tokens=60,
            cache_write_tokens=5,
            ttft_ms=125,
            estimated_cost=0.02,
            actual_cost=0.015,
            cost_source="estimated",
        ) as span:
            attrs = dict(span._attributes)  # type: ignore[union-attr]
            assert attrs["llm.total_tokens"] == 120
            assert attrs["llm.cache_read_tokens"] == 60
            assert attrs["llm.cache_write_tokens"] == 5
            assert attrs["llm.ttft_ms"] == 125
            assert attrs["llm.cost"] == 0.015
            assert attrs["llm.estimated_cost"] == 0.02
            assert attrs["llm.actual_cost"] == 0.015
            assert attrs["llm.cost_source"] == "actual"
        provider.shutdown()

    def test_trace_tool_redacts_token_in_attrs(self) -> None:
        """trace_tool with access_token in **attrs should redact it."""
        from opentelemetry.sdk.trace import TracerProvider as SDKProvider
        from agentscope_sdk.tracing import trace_tool

        provider = SDKProvider()
        tracer = provider.get_tracer("test")
        with trace_tool(
            tracer, "search", tool_type="api",
            arguments_summary='{"q":"test"}',
            access_token="eyJhbGciOiJIUzI1NiJ9",
        ) as span:
            attrs = dict(span._attributes)  # type: ignore[union-attr]
            assert attrs.get("access_token") == "[REDACTED]"
        provider.shutdown()

    def test_all_helpers_redact_api_key(self) -> None:
        """All four deep trace helpers should redact sensitive **attrs."""
        from opentelemetry.sdk.trace import TracerProvider as SDKProvider
        from agentscope_sdk.tracing import (
            trace_llm,
            trace_retrieval,
            trace_skill,
            trace_tool,
        )

        provider = SDKProvider()
        tracer = provider.get_tracer("test")

        with trace_llm(tracer, "l", api_key="sk-xxx") as s:
            assert s._attributes.get("api_key") == "[REDACTED]"  # type: ignore[union-attr]
        with trace_tool(tracer, "t", api_key="sk-xxx") as s:
            assert s._attributes.get("api_key") == "[REDACTED]"  # type: ignore[union-attr]
        with trace_retrieval(tracer, "r", api_key="sk-xxx") as s:
            assert s._attributes.get("api_key") == "[REDACTED]"  # type: ignore[union-attr]
        with trace_skill(tracer, "s", api_key="sk-xxx") as s:
            assert s._attributes.get("api_key") == "[REDACTED]"  # type: ignore[union-attr]

        provider.shutdown()


class TestForceFlushAndShutdown:
    """Tests for force_flush and shutdown."""

    def test_shutdown_shuts_down_providers(self) -> None:
        """shutdown_tracing should shut down all cached providers."""
        from agentscope_sdk.tracing import configure_tracing, shutdown_tracing

        with (
            patch("agentscope_sdk.tracing.TracerProvider") as mock_provider,
            patch("agentscope_sdk.tracing.OTLPSpanExporter") as mock_exporter,
            patch("agentscope_sdk.tracing.ObservableBatchSpanProcessor") as mock_bsp,
        ):
            mock_provider1 = MagicMock()
            mock_provider2 = MagicMock()
            mock_provider.side_effect = [mock_provider1, mock_provider2]

            configure_tracing(server_url="http://a:8080", project_token="ascope_a")
            configure_tracing(server_url="http://b:8080", project_token="ascope_b")

            shutdown_tracing()

            mock_provider1.shutdown.assert_called_once()
            mock_provider2.shutdown.assert_called_once()

    def test_force_flush_flushes_providers(self) -> None:
        """force_flush should flush all cached providers."""
        from agentscope_sdk.tracing import configure_tracing, force_flush

        with (
            patch("agentscope_sdk.tracing.TracerProvider") as mock_provider,
            patch("agentscope_sdk.tracing.OTLPSpanExporter") as mock_exporter,
            patch("agentscope_sdk.tracing.ObservableBatchSpanProcessor") as mock_bsp,
        ):
            mock_provider1 = MagicMock()
            mock_provider.side_effect = [mock_provider1, MagicMock()]

            configure_tracing(server_url="http://x:8080", project_token="ascope_x")
            force_flush()

            mock_provider1.force_flush.assert_called_once()


class TestObservedExporterStats:
    """Tests for exporter-level production health statistics."""

    def test_successful_export_is_reflected_in_stats(self) -> None:
        from agentscope_sdk.tracing import ObservedSpanExporter, get_tracing_stats
        from opentelemetry.sdk.trace.export import SpanExportResult

        inner = MagicMock()
        inner.export.return_value = SpanExportResult.SUCCESS
        exporter = ObservedSpanExporter(
            inner, server_url="http://collector", project_token="ascope_secret"
        )

        assert exporter.export([MagicMock(), MagicMock()]) == SpanExportResult.SUCCESS

        stats = get_tracing_stats("http://collector", "ascope_secret")
        assert stats.batches_attempted == 1
        assert stats.batches_succeeded == 1
        assert stats.spans_attempted == 2
        assert stats.spans_exported == 2
        assert stats.spans_dropped == 0

    def test_failed_export_retries_then_succeeds(self) -> None:
        from agentscope_sdk.tracing import ObservedSpanExporter, get_tracing_stats
        from opentelemetry.sdk.trace.export import SpanExportResult

        inner = MagicMock()
        inner.export.side_effect = [SpanExportResult.FAILURE, SpanExportResult.SUCCESS]
        exporter = ObservedSpanExporter(
            inner,
            server_url="http://collector",
            project_token="ascope_secret",
            max_retries=2,
        )

        assert exporter.export([MagicMock()]) == SpanExportResult.SUCCESS

        stats = get_tracing_stats("http://collector", "ascope_secret")
        assert stats.batches_attempted == 1
        assert stats.batches_succeeded == 1
        assert stats.batches_failed == 0
        assert stats.retries == 1
        assert stats.spans_exported == 1

    def test_permanent_failure_drops_spans_without_leaking_token(self) -> None:
        from agentscope_sdk.tracing import ObservedSpanExporter, get_tracing_stats
        from opentelemetry.sdk.trace.export import SpanExportResult

        inner = MagicMock()
        inner.export.side_effect = RuntimeError("Bearer ascope_secret failed")
        exporter = ObservedSpanExporter(
            inner,
            server_url="http://collector",
            project_token="ascope_secret",
            max_retries=1,
        )

        assert exporter.export([MagicMock(), MagicMock()]) == SpanExportResult.FAILURE

        stats = get_tracing_stats("http://collector", "ascope_secret")
        assert stats.batches_attempted == 1
        assert stats.batches_failed == 1
        assert stats.spans_dropped == 2
        assert stats.retries == 1
        assert "ascope_secret" not in stats.last_error

    def test_queue_size_is_passed_to_batch_processor(self) -> None:
        from agentscope_sdk.tracing import configure_tracing

        with (
            patch("agentscope_sdk.tracing.TracerProvider") as mock_provider,
            patch("agentscope_sdk.tracing.OTLPSpanExporter"),
            patch("agentscope_sdk.tracing.ObservableBatchSpanProcessor") as mock_bsp,
        ):
            mock_provider.return_value = MagicMock()
            configure_tracing(
                server_url="http://test:8080",
                project_token="ascope_queue",
                export_queue_size=1024,
            )

            _, kwargs = mock_bsp.call_args
            assert kwargs["max_queue_size"] == 1024

    def test_zero_retry_configuration_does_not_retry(self) -> None:
        from agentscope_sdk.tracing import ObservedSpanExporter, get_tracing_stats
        from opentelemetry.sdk.trace.export import SpanExportResult

        inner = MagicMock()
        inner.export.return_value = SpanExportResult.FAILURE
        exporter = ObservedSpanExporter(
            inner,
            server_url="http://collector",
            project_token="ascope_no_retry",
            max_retries=0,
        )

        assert exporter.export([MagicMock()]) == SpanExportResult.FAILURE
        assert get_tracing_stats("http://collector", "ascope_no_retry").retries == 0
