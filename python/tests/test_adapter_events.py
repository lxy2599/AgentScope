"""Tests for framework-neutral AgentScope adapter events."""

from __future__ import annotations

from dataclasses import dataclass

from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter


@dataclass
class _Run:
    id: str


class _Runtime:
    def __init__(self) -> None:
        self.agent_id = "agent-1"
        self.agent_name = "adapter-agent"
        self.instance_id = "instance-1"
        self.started: list[dict] = []
        self.finished: list[dict] = []
        self.failed: list[dict] = []

    def start_run(self, **kwargs):
        self.started.append(kwargs)
        return _Run("run-1")

    def finish_run(self, **kwargs):
        self.finished.append(kwargs)
        return True

    def fail_run(self, **kwargs):
        self.failed.append(kwargs)
        return True


def _session():
    from agentscope_sdk.adapters import AdapterSession

    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    return AdapterSession(_Runtime(), provider.get_tracer("adapter-test")), exporter


def test_normalize_memory_and_guardrail_events_have_explicit_semantics():
    from agentscope_sdk.adapters.events import normalize_event

    memory = normalize_event(
        "memory.read",
        "user_profile",
        arguments={"key": "profile:42"},
        result="preferred language is Chinese",
    )
    guardrail = normalize_event(
        "guardrail.blocked",
        "shell_policy",
        result="destructive command denied",
        success=False,
    )

    assert memory.semantic_kind == "MEMORY"
    assert memory.name == "memory.read.user_profile"
    assert memory.attributes["memory.operation"] == "read"
    assert guardrail.semantic_kind == "GUARDRAIL"
    assert guardrail.attributes["guardrail.blocked"] is True
    assert guardrail.success is False


def test_adapter_session_links_runtime_run_and_semantic_spans():
    session, exporter = _session()

    run = session.start_run("look up the account", session_id="session-1")
    session.on_thought("planning", "find account information", capture_content=True)
    session.on_tool_start("lookup", {"account_id": "42"}, tool_id="tool-1")
    session.on_tool_finish("lookup", "found account", tool_id="tool-1")
    session.on_memory_read("profile", "preferred language is Chinese")
    session.on_skill_call("account-support", "handled the account request")
    session.on_guardrail("shell-policy", "safe", blocked=False)
    session.finish_run("account found")

    spans = exporter.get_finished_spans()
    by_name = {span.name: span for span in spans}
    assert run.id == "run-1"
    assert by_name["agent.run"].attributes["agentscope.run.id"] == "run-1"
    assert by_name["tool.lookup"].attributes["agentscope.span.kind"] == "TOOL"
    assert by_name["memory.read.profile"].attributes["agentscope.span.kind"] == "MEMORY"
    assert by_name["skill.account-support"].attributes["agentscope.span.kind"] == "SKILL"
    assert by_name["guardrail.shell-policy"].attributes["agentscope.span.kind"] == "GUARDRAIL"
    assert by_name["agent.answer"].attributes["agentscope.span.kind"] == "FINAL_ANSWER"


def test_adapter_session_fails_runtime_and_marks_root_span_error():
    session, exporter = _session()

    session.start_run("do work")
    session.fail_run("upstream failed")

    root = next(span for span in exporter.get_finished_spans() if span.name == "agent.run")
    assert root.status.status_code.name == "ERROR"
    assert root.attributes["agentscope.run.id"] == "run-1"
    assert session.runtime.failed == [{"error": "upstream failed", "attributes": {}}]
