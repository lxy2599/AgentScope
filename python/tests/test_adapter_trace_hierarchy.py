"""Focused tests for SDK trace parent contexts and explicit turns."""

from __future__ import annotations

from dataclasses import dataclass

from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter

from agentscope_sdk.adapters import AdapterSession
from agentscope_sdk.tracing import trace_llm, trace_tool


@dataclass
class _Run:
    id: str = "run-1"


class _Runtime:
    agent_id = "agent-1"
    agent_name = "agent"
    instance_id = "instance-1"

    def start_run(self, **kwargs):
        return _Run()

    def finish_run(self, **kwargs):
        return True

    def fail_run(self, **kwargs):
        return True


def _tracer_and_exporter():
    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    return provider.get_tracer("trace-hierarchy-test"), exporter


def test_trace_helper_keeps_current_context_when_parent_context_is_omitted():
    tracer, exporter = _tracer_and_exporter()

    with tracer.start_as_current_span("parent") as parent, trace_llm(tracer, "llm.call") as child:
        assert child.parent.span_id == parent.get_span_context().span_id

    spans = exporter.get_finished_spans()
    assert [span.name for span in spans] == ["llm.call", "parent"]


def test_trace_helper_uses_explicit_parent_context_without_changing_current_context():
    tracer, exporter = _tracer_and_exporter()

    with tracer.start_as_current_span("parent") as parent:
        parent_context = trace.set_span_in_context(parent)
        with tracer.start_as_current_span("current") as current:
            with trace_tool(tracer, "tool.call", parent_context=parent_context) as child:
                assert child.parent.span_id == parent.get_span_context().span_id
                assert trace.get_current_span().get_span_context().span_id == child.get_span_context().span_id
            assert trace.get_current_span().get_span_context().span_id == current.get_span_context().span_id

    spans = {span.name: span for span in exporter.get_finished_spans()}
    assert spans["tool.call"].parent.span_id == parent.get_span_context().span_id
    assert spans["current"].parent.span_id == parent.get_span_context().span_id


def test_adapter_session_builds_turn_tree_and_closes_cross_callback_spans_directly():
    tracer, exporter = _tracer_and_exporter()
    session = AdapterSession(_Runtime(), tracer)
    session.start_run("hello")
    root = trace.get_current_span()
    session.start_turn("turn-1", turn_index=1)

    session.on_llm_start("model", call_id="llm-1", input_summary="prompt")
    assert trace.get_current_span().get_span_context().span_id == root.get_span_context().span_id
    session.on_tool_start("search", tool_id="tool-1", arguments={"q": "hello"})
    assert trace.get_current_span().get_span_context().span_id == root.get_span_context().span_id
    session.on_tool_finish("search", "result", tool_id="tool-1")
    session.on_llm_finish("model", call_id="llm-1", output_summary="tool call")
    session.finish_turn("continue")

    session.start_turn(turn_index=2)
    session.on_llm_call("model", output_summary="final")
    session.finish_run("final answer")

    spans = exporter.get_finished_spans()
    by_name = {}
    for span in spans:
        by_name.setdefault(span.name, []).append(span)

    root_span = by_name["agent.run"][0]
    turns = by_name["agent.turn"]
    assert len(turns) == 2
    assert all(turn.parent.span_id == root_span.get_span_context().span_id for turn in turns)
    assert turns[0].attributes["agentscope.turn.id"] == "turn-1"
    assert turns[0].attributes["agentscope.turn.index"] == 1
    assert turns[0].attributes["agentscope.turn.outcome"] == "continue"
    assert turns[0].attributes["agentscope.turn.llm_count"] == 1
    assert turns[0].attributes["agentscope.turn.tool_count"] == 1
    assert turns[1].attributes["agentscope.turn.llm_count"] == 1
    assert turns[1].attributes["agentscope.turn.tool_count"] == 0
    assert turns[1].attributes["agentscope.turn.outcome"] == "final"

    llm_spans = by_name["llm.model"]
    tool_span = by_name["tool.search"][0]
    assert llm_spans[0].parent.span_id == turns[0].get_span_context().span_id
    assert tool_span.parent.span_id == turns[0].get_span_context().span_id
    assert llm_spans[0].attributes["agentscope.run.id"] == "run-1"
    assert llm_spans[0].attributes["agentscope.turn.id"] == "turn-1"
    assert tool_span.attributes["agentscope.turn.index"] == 1
    assert by_name["agent.answer"][0].parent.span_id == turns[1].get_span_context().span_id


def test_unmatched_correlated_finishes_and_post_run_callbacks_do_not_create_spans():
    tracer, exporter = _tracer_and_exporter()
    session = AdapterSession(_Runtime(), tracer)
    session.start_run("hello")
    session.start_turn("turn-1")
    session.on_llm_start("model", call_id="llm-1")
    session.on_tool_start("search", tool_id="tool-1")
    session.finish_turn("continue")

    count_after_turn = len(exporter.get_finished_spans())
    session.on_llm_finish("model", call_id="llm-1", output_summary="late")
    session.on_llm_finish("model", call_id="missing", output_summary="duplicate")
    session.on_tool_finish("search", "late", tool_id="tool-1")
    session.on_tool_finish("search", "duplicate", tool_id="missing")
    assert len(exporter.get_finished_spans()) == count_after_turn

    session.finish_run("done")
    count_after_run = len(exporter.get_finished_spans())
    session.on_llm_finish("model", output_summary="late completion-only")
    session.on_tool_finish("search", "late completion-only")
    session.on_llm_call("model", output_summary="late call")
    session.on_tool_start("search")
    assert len(exporter.get_finished_spans()) == count_after_run
