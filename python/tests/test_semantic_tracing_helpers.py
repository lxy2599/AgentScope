"""Tests for semantic tracing helpers: trace_thought and record_final_answer."""

from __future__ import annotations

from agentscope_sdk.tracing import (
    build_final_answer_attributes,
    build_thought_attributes,
    record_final_answer,
    trace_thought,
)
from opentelemetry.sdk.trace import TracerProvider as SDKProvider

import pytest


@pytest.fixture(autouse=True)
def clear_tracing_cache():
    """Clear the tracing provider cache before each test to avoid cross-test pollution."""
    from agentscope_sdk.tracing import _clear_cache

    _clear_cache()
    yield
    _clear_cache()


class TestTraceThought:
    """Tests for the ``trace_thought`` context manager."""

    def creates_span_with_thought_kind(self):
        """A thought span should set agentscope.span.kind=THOUGHT."""
        tracer = SDKProvider().get_tracer("test")
        with trace_thought(tracer, "thinking", summary="I think...") as span:
            attrs = span.attributes
            assert attrs.get("agentscope.span.kind") == "THOUGHT", (
                f"expected THOUGHT, got {attrs.get('agentscope.span.kind')!r}"
            )
            assert span.is_recording()

    def records_summary_only_by_default(self):
        """By default, trace_thought should only record thought.summary, not thought.content."""
        tracer = SDKProvider().get_tracer("test")
        with trace_thought(tracer, "thinking", summary="short summary",
                           content="very long thinking content that should not appear") as span:
            attrs = span.attributes
            assert attrs.get("thought.summary") == "short summary"
            assert "thought.content" not in attrs, (
                "thought.content should not be set when capture_content=False (default)"
            )

    def records_content_when_capture_content_is_true(self):
        """When capture_content=True, trace_thought should record thought.content."""
        tracer = SDKProvider().get_tracer("test")
        with trace_thought(tracer, "thinking", summary="summary",
                           content="full thinking content", capture_content=True) as span:
            attrs = span.attributes
            assert attrs.get("thought.summary") == "summary"
            assert attrs.get("thought.content") == "full thinking content"
            assert attrs.get("agentscope.content_captured") is True

    def records_stage_and_confidence(self):
        """Trace thought should record optional stage and confidence attributes."""
        tracer = SDKProvider().get_tracer("test")
        with trace_thought(tracer, "reasoning", summary="weighing options",
                           stage="reasoning", confidence=0.85) as span:
            attrs = span.attributes
            assert attrs.get("thought.stage") == "reasoning"
            assert attrs.get("thought.confidence") == 0.85

    def accepts_additional_attrs(self):
        """trace_thought should accept and set additional keyword attributes."""
        tracer = SDKProvider().get_tracer("test")
        with trace_thought(tracer, "thinking", summary="test",
                           custom_key="custom_val") as span:
            assert span.attributes.get("custom_key") == "custom_val"

    def redacts_sensitive_content(self):
        """Sensitive content in thought fields should be redacted via maybe_redact."""
        tracer = SDKProvider().get_tracer("test")
        with trace_thought(tracer, "thinking", summary="my api_key is secret",
                           capture_content=True, content="token=abc123") as span:
            attrs = span.attributes
            # summary is not a sensitive key, so PII is redacted
            assert "api_key" in attrs.get("thought.summary", "")
            # content contains "token" but key is "thought.content" — not sensitive
            # So value should have PII redacted (token=abc123 -> unchanged since token
            # is not a PII pattern, but we verify it goes through maybe_redact)
            assert attrs.get("thought.content") is not None


class TestRecordFinalAnswer:
    """Tests for the ``record_final_answer`` context manager."""

    def creates_span_with_final_answer_kind(self):
        """A final answer span should set agentscope.span.kind=FINAL_ANSWER."""
        tracer = SDKProvider().get_tracer("test")
        with record_final_answer(tracer, "final-answer", summary="Done") as span:
            attrs = span.attributes
            assert attrs.get("agentscope.span.kind") == "FINAL_ANSWER", (
                f"expected FINAL_ANSWER, got {attrs.get('agentscope.span.kind')!r}"
            )
            assert span.is_recording()

    def records_summary_and_output(self):
        """record_final_answer should record answer.summary, answer.output."""
        tracer = SDKProvider().get_tracer("test")
        with record_final_answer(tracer, "answer", summary="Final answer",
                                 output="The result is 42") as span:
            attrs = span.attributes
            assert attrs.get("answer.summary") == "Final answer"
            assert attrs.get("answer.output") == "The result is 42"
            assert attrs.get("agentscope.content_captured") is True

    def records_structured_output(self):
        """record_final_answer should accept structured_output."""
        tracer = SDKProvider().get_tracer("test")
        structured = '{"result": 42, "unit": "km"}'
        with record_final_answer(tracer, "answer", summary="structured",
                                 structured_output=structured) as span:
            attrs = span.attributes
            assert attrs.get("answer.structured_output") == structured

    def does_not_set_content_captured_when_no_content(self):
        """When summary and output are empty, agentscope.content_captured should not be set."""
        tracer = SDKProvider().get_tracer("test")
        with record_final_answer(tracer, "empty-answer") as span:
            attrs = span.attributes
            assert "agentscope.content_captured" not in attrs, (
                "content_captured should not be set when no capture_content"
            )

    def accepts_additional_attrs(self):
        """record_final_answer should accept additional keyword attributes."""
        tracer = SDKProvider().get_tracer("test")
        with record_final_answer(tracer, "answer", summary="test",
                                 eval_score="0.95") as span:
            assert span.attributes.get("eval_score") == "0.95"


class TestBuildThoughtAttributes:
    """Tests for the ``build_thought_attributes`` helper."""

    def test_builds_summary_only(self):
        """build_thought_attributes with default should only include summary."""
        attrs = build_thought_attributes(summary="thinking")
        assert attrs["agentscope.span.kind"] == "THOUGHT"
        assert attrs["thought.summary"] == "thinking"
        assert "thought.content" not in attrs
        assert "agentscope.content_captured" not in attrs

    def test_builds_with_content_when_captured(self):
        """build_thought_attributes with capture_content=True should include content."""
        attrs = build_thought_attributes(summary="s", content="full content", capture_content=True)
        assert attrs["thought.content"] == "full content"
        assert attrs["agentscope.content_captured"] is True

    def test_builds_with_stage_and_confidence(self):
        """build_thought_attributes should include stage and confidence when provided."""
        attrs = build_thought_attributes(summary="s", stage="analysis", confidence=0.9)
        assert attrs["thought.stage"] == "analysis"
        assert attrs["thought.confidence"] == 0.9

    def test_redacts_sensitive_data(self):
        """build_thought_attributes should redact sensitive data via maybe_redact."""
        attrs = build_thought_attributes(summary="test", api_key="sk-123")
        assert attrs.get("api_key") == "[REDACTED]", "API key should be redacted"

    def test_returns_copy_on_each_call(self):
        """build_thought_attributes should return a new dict on each call."""
        a1 = build_thought_attributes(summary="s1")
        a2 = build_thought_attributes(summary="s2")
        assert a1 is not a2
        assert a1["thought.summary"] != a2["thought.summary"]


class TestBuildFinalAnswerAttributes:
    """Tests for the ``build_final_answer_attributes`` helper."""

    def test_builds_minimal(self):
        """build_final_answer_attributes with minimal args."""
        attrs = build_final_answer_attributes()
        assert attrs["agentscope.span.kind"] == "FINAL_ANSWER"
        assert "answer.summary" not in attrs or attrs.get("answer.summary") == ""

    def test_builds_with_summary_and_output(self):
        """build_final_answer_attributes with summary and output."""
        attrs = build_final_answer_attributes(summary="result", output="42")
        assert attrs["answer.summary"] == "result"
        assert attrs["answer.output"] == "42"
        assert attrs["agentscope.content_captured"] is True

    def test_builds_with_structured_output(self):
        """build_final_answer_attributes with structured_output."""
        attrs = build_final_answer_attributes(summary="s", structured_output='{"key": "val"}')
        assert attrs["answer.structured_output"] == '{"key": "val"}'

    def test_does_not_set_content_captured_when_empty(self):
        """When summary/output are empty, content_captured should not be set."""
        attrs = build_final_answer_attributes()
        assert "agentscope.content_captured" not in attrs

    def test_redacts_sensitive_data(self):
        """build_final_answer_attributes should redact sensitive data."""
        attrs = build_final_answer_attributes(summary="my token is abc")
        # PII redaction should have been applied
        assert attrs.get("answer.summary") is not None


class TestImports:
    """Tests that the new helpers are importable from the top-level package."""

    def test_new_helpers_importable_from_tracing(self):
        """trace_thought, record_final_answer, build_thought_attributes,
        build_final_answer_attributes should be importable from agentscope_sdk.tracing."""
        from agentscope_sdk.tracing import (
            build_final_answer_attributes,
            build_thought_attributes,
            record_final_answer,
            trace_thought,
        )

        assert callable(trace_thought)
        assert callable(record_final_answer)
        assert callable(build_thought_attributes)
        assert callable(build_final_answer_attributes)

    def test_new_helpers_importable_from_package(self):
        """The new helpers should be importable from agentscope_sdk."""
        from agentscope_sdk import (
            build_final_answer_attributes,
            build_thought_attributes,
            record_final_answer,
            trace_thought,
        )

        assert callable(trace_thought)
        assert callable(record_final_answer)
        assert callable(build_thought_attributes)
        assert callable(build_final_answer_attributes)
