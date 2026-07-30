"""Tests for the framework-neutral native tracing session."""

from __future__ import annotations

from unittest.mock import Mock

from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

from agentscope_sdk.runtime import RuntimeRun
from agentscope_sdk.runtime_trace import RuntimeTraceSession


class _RecordingRuntime:
    supports_explicit_run_handles = True
    agent_id = "agent-1"
    agent_name = "native-agent"
    instance_id = "instance-1"

    def __init__(self) -> None:
        self.started: list[dict] = []
        self.finished: list[dict] = []
        self.failed: list[dict] = []

    def start_run(self, **kwargs):
        self.started.append(kwargs)
        return RuntimeRun(id="run-1", trace_id=kwargs["trace_id"])

    def update_run_status(self, status, **kwargs):
        return True

    def finish_run(self, **kwargs):
        self.finished.append(kwargs)
        return True

    def fail_run(self, **kwargs):
        self.failed.append(kwargs)
        return True


class _UnavailableRuntime(_RecordingRuntime):
    def start_run(self, **kwargs):
        self.started.append(kwargs)
        return None


def _tracer_and_exporter():
    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    return provider.get_tracer("runtime-trace-session-test"), exporter


def test_native_session_uses_explicit_run_handle_and_does_not_sync_flush(monkeypatch):
    tracer, exporter = _tracer_and_exporter()
    runtime = _RecordingRuntime()
    flush = Mock()
    monkeypatch.setattr("agentscope_sdk.adapters.base.force_flush", flush)
    session = RuntimeTraceSession(runtime, tracer, metadata={"framework": "hermes"})

    run = session.start_run(
        "hello",
        session_id="session-1",
        metadata={"hermes.run.platform": "gateway"},
    )
    assert run is not None
    assert runtime.started[0]["track_current"] is False

    assert session.start_turn(
        "decision-1",
        turn_index=1,
        attributes={"hermes.turn.kind": "decision"},
    ) == ("decision-1", 1)
    session.start_llm(
        "hermes.llm",
        call_id="llm-1",
        provider="openai",
        model="test",
        attributes={"hermes.llm.streaming": True},
    )
    session.start_tool(
        "search",
        {"q": "hello"},
        tool_id="tool-1",
        attributes={"hermes.tool.execution_mode": "sequential"},
    )
    session.finish_tool(
        "search",
        "result",
        tool_id="tool-1",
        attributes={"hermes.tool.failure_category": "none"},
    )
    session.record_tool("cache", {"key": "a"}, "miss", success=False, error="cache unavailable")
    session.finish_llm(
        "hermes.llm",
        call_id="llm-1",
        output_summary="tool call",
        duration_s=0.25,
        attributes={"hermes.llm.retry_attempt": 1},
    )
    assert session.record_final_answer(
        "done",
        attributes={"hermes.answer.partial": False},
    ) is True
    assert session.finish_run("done", metrics={"hermes.run.api_calls": 1}) is True

    assert flush.call_count == 0
    assert runtime.finished[0]["run"] is run

    spans = {span.name: span for span in exporter.get_finished_spans()}
    root = spans["agent.run"]
    turn = spans["agent.turn"]
    assert root.attributes["hermes.run.platform"] == "gateway"
    assert root.attributes["hermes.run.api_calls"] == 1
    assert turn.parent.span_id == root.get_span_context().span_id
    assert spans["llm.hermes.llm"].parent.span_id == turn.get_span_context().span_id
    assert spans["llm.hermes.llm"].attributes["llm.duration_ms"] == 250
    assert spans["llm.hermes.llm"].attributes["hermes.llm.streaming"] is True
    assert spans["llm.hermes.llm"].attributes["hermes.llm.retry_attempt"] == 1
    assert turn.attributes["hermes.turn.kind"] == "decision"
    assert spans["tool.search"].parent.span_id == turn.get_span_context().span_id
    assert spans["tool.search"].attributes["hermes.tool.execution_mode"] == "sequential"
    assert spans["tool.search"].attributes["hermes.tool.failure_category"] == "none"
    assert spans["tool.cache"].parent.span_id == turn.get_span_context().span_id
    assert spans["tool.cache"].status.status_code.name == "ERROR"
    assert spans["agent.answer"].parent.span_id == turn.get_span_context().span_id
    assert spans["agent.answer"].attributes["hermes.answer.partial"] is False


def test_native_session_becomes_noop_when_runtime_start_is_unavailable():
    tracer, _ = _tracer_and_exporter()
    session = RuntimeTraceSession(_UnavailableRuntime(), tracer)

    assert session.start_run("hello") is None
    assert session.enabled is False
    assert session.start_turn("decision-1") is None
    session.start_llm("model", call_id="one")
    session.start_tool("search", {}, tool_id="tool-1")
    assert session.record_final_answer("done") is False
    assert session.finish_run("done") is False
    assert session.fail_run("ignored") is False
