"""Tests for the callback-only Hermes Agent adapter."""

from __future__ import annotations

from dataclasses import dataclass

import pytest

from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter


@dataclass
class _Run:
    id: str = "run-hermes-1"


class _Runtime:
    agent_id = "agent-hermes-1"
    agent_name = "hermes-agent"
    instance_id = "hermes-instance-1"

    def start_run(self, **kwargs):
        self.start_kwargs = kwargs
        return _Run()

    def finish_run(self, **kwargs):
        self.finish_kwargs = kwargs
        return True

    def fail_run(self, **kwargs):
        self.fail_kwargs = kwargs
        return True


class _HermesLikeAgent:
    session_id = "hermes-session-1"
    provider = "openai-compatible"
    model = "deepseek-chat"

    def __init__(self) -> None:
        self.tool_progress_callback = lambda *args, **kwargs: setattr(self, "original_tool_called", True)
        self.reasoning_callback = None
        self.step_callback = None
        self.stream_delta_callback = None
        self.event_callback = None

    def run_conversation(self, prompt, task_id=None):
        self.step_callback(1, [])
        self.reasoning_callback("I need the user's stored preference")
        self.tool_progress_callback("tool.started", "memory", "read profile", {"action": "read", "target": "profile"})
        self.tool_progress_callback(
            "tool.completed", "memory", None, None, duration=0.02, is_error=False, result="language=Chinese"
        )
        self.tool_progress_callback("tool.started", "skill_view", "load customer support", {"name": "customer-support"})
        self.tool_progress_callback(
            "tool.completed", "skill_view", None, None, duration=0.01, is_error=False, result="skill loaded"
        )
        self.tool_progress_callback("tool.output_risk", "terminal", None, None, risk_metadata={"risk": "high"})
        self.stream_delta_callback("The account preference is Chinese.")
        self.stream_delta_callback(None)
        return {"final_response": "The account preference is Chinese."}


class _FailedHermesLikeAgent(_HermesLikeAgent):
    def run_conversation(self, prompt, task_id=None):
        return {
            "completed": False,
            "failed": True,
            "failure_reason": "Connection error.",
            "error": "Connection error.",
            "final_response": "API call failed after retries.",
        }


def test_hermes_adapter_preserves_callbacks_and_maps_real_callback_shapes():
    from agentscope_sdk.adapters import AdapterSession
    from agentscope_sdk.adapters.hermes import HERMES_UPSTREAM_COMMIT, HermesAdapter

    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    runtime = _Runtime()
    agent = _HermesLikeAgent()
    session = AdapterSession(runtime, provider.get_tracer("hermes-test"), metadata={"adapter_name": "hermes"})

    result = HermesAdapter(session).run(agent, "What language does the account use?", task_id="task-1")

    names = {span.name: span for span in exporter.get_finished_spans()}
    assert result == "The account preference is Chinese."
    assert agent.original_tool_called is True
    assert runtime.start_kwargs["session_id"] == "hermes-session-1"
    assert runtime.start_kwargs["task_id"] == "task-1"
    assert names["llm.hermes.call.1"].attributes["agentscope.span.kind"] == "LLM"
    assert names["memory.read.memory"].attributes["agentscope.span.kind"] == "MEMORY"
    assert names["skill.skill_view"].attributes["agentscope.span.kind"] == "SKILL"
    assert names["guardrail.tool_output_risk"].attributes["agentscope.span.kind"] == "GUARDRAIL"
    assert names["agent.run"].attributes["hermes.upstream_commit"] == HERMES_UPSTREAM_COMMIT
    assert names["agent.answer"].attributes["agentscope.span.kind"] == "FINAL_ANSWER"
    assert session.last_run_id == "run-hermes-1"


def test_hermes_adapter_marks_structured_upstream_failures_as_failed_runs():
    from agentscope_sdk.adapters import AdapterSession
    from agentscope_sdk.adapters.hermes import HermesAdapter

    runtime = _Runtime()
    provider = TracerProvider()
    session = AdapterSession(runtime, provider.get_tracer("hermes-failure-test"))

    with pytest.raises(RuntimeError, match="Connection error"):
        HermesAdapter(session).run(_FailedHermesLikeAgent(), "Reply with one word.")

    assert runtime.fail_kwargs["error"] == "Connection error."


def test_hermes_observer_hooks_use_request_and_tool_correlation_ids():
    from agentscope_sdk.adapters import AdapterSession
    from agentscope_sdk.adapters.hermes import HermesAdapter

    class _Context:
        def __init__(self):
            self.hooks = {}

        def register_hook(self, name, callback):
            self.hooks[name] = callback

    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    session = AdapterSession(_Runtime(), provider.get_tracer("hermes-observer-test"))
    session.start_run("inspect account")
    adapter = HermesAdapter(session)
    context = _Context()
    adapter.register_observer_hooks(context)

    context.hooks["pre_api_request"](
        api_request_id="request-1",
        model="deepseek-chat",
        provider="openai-compatible",
        request={"body": {"messages": ["inspect"]}},
    )
    context.hooks["post_api_request"](
        api_request_id="request-1",
        response={"body": {"content": "done"}},
        usage={"prompt_tokens": 3, "completion_tokens": 5},
    )
    context.hooks["pre_tool_call"](
        tool_call_id="tool-1", tool_name="memory", args={"action": "read", "target": "profile"}
    )
    context.hooks["post_tool_call"](
        tool_call_id="tool-1",
        tool_name="memory",
        args={"action": "read", "target": "profile"},
        result="language=Chinese",
        status="ok",
        duration_ms=12,
    )
    session.finish_run("done")

    names = {span.name: span for span in exporter.get_finished_spans()}
    assert names["llm.hermes.api.request-1"].attributes["llm.total_tokens"] == 8
    assert names["tool.memory"].attributes["tool.duration_ms"] == 12
    assert names["memory.read.profile"].attributes["agentscope.span.kind"] == "MEMORY"
