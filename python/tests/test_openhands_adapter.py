"""Tests for the callback-only OpenHands Software Agent SDK adapter."""

from __future__ import annotations

from dataclasses import dataclass
from types import SimpleNamespace

from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import SimpleSpanProcessor
from opentelemetry.sdk.trace.export.in_memory_span_exporter import (
    InMemorySpanExporter,
)


@dataclass
class _Run:
    id: str = "run-openhands-1"


class _Runtime:
    agent_id = "agent-openhands-1"
    agent_name = "openhands-agent"
    instance_id = "openhands-instance-1"

    def __init__(self) -> None:
        self.states: list[tuple[str, str, dict]] = []

    def start_run(self, **kwargs):
        self.start_kwargs = kwargs
        return _Run()

    def update_run_status(self, status, message="", attributes=None):
        self.states.append((status, message, attributes or {}))
        return True

    def finish_run(self, **kwargs):
        self.finish_kwargs = kwargs
        return True

    def fail_run(self, **kwargs):
        self.fail_kwargs = kwargs
        return True


class MessageEvent:
    def __init__(
        self,
        source: str,
        text: str,
        *,
        event_id: str,
        reasoning_content: str = "",
        activated_skills: list[str] | None = None,
        llm_response_id: str | None = None,
    ) -> None:
        self.id = event_id
        self.source = source
        self.llm_message = SimpleNamespace(
            content=[SimpleNamespace(text=text)],
            reasoning_content=reasoning_content,
        )
        self.reasoning_content = reasoning_content
        self.activated_skills = activated_skills or []
        self.llm_response_id = llm_response_id


class ActionEvent:
    def __init__(
        self,
        tool_name: str,
        tool_call_id: str,
        arguments: dict,
        *,
        event_id: str,
        llm_response_id: str,
        thought: str = "",
    ) -> None:
        self.id = event_id
        self.source = "agent"
        self.tool_name = tool_name
        self.tool_call_id = tool_call_id
        self.llm_response_id = llm_response_id
        self.thought = [SimpleNamespace(text=thought)] if thought else []
        self.reasoning_content = ""
        self.action = SimpleNamespace(
            model_dump=lambda **_: arguments,
        )
        self.security_risk = "unknown"


class ObservationEvent:
    def __init__(
        self,
        tool_name: str,
        tool_call_id: str,
        result: dict,
        *,
        event_id: str,
    ) -> None:
        self.id = event_id
        self.source = "environment"
        self.tool_name = tool_name
        self.tool_call_id = tool_call_id
        self.observation = SimpleNamespace(model_dump=lambda **_: result)


class AgentErrorEvent:
    id = "event-agent-error"
    source = "agent"
    tool_name = "terminal"
    tool_call_id = "parallel-2"
    error = "command exited with status 1"


class ConversationErrorEvent:
    id = "event-conversation-error"
    source = "environment"
    code = "runtime_error"
    detail = "workspace disappeared"


class ConversationStateUpdateEvent:
    id = "event-state"
    source = "environment"
    key = "execution_status"
    value = "waiting_for_confirmation"


class PauseEvent:
    id = "event-pause"
    source = "user"


class SystemPromptEvent:
    id = "event-system"
    source = "agent"
    system_prompt = SimpleNamespace(text="You are a software engineering agent.")
    dynamic_context = SimpleNamespace(text="workspace=/tmp/project")
    tools = [
        SimpleNamespace(name="terminal"),
        SimpleNamespace(name="file_editor"),
    ]


class Condensation:
    id = "event-condensation"
    source = "environment"
    forgotten_event_ids = {"event-1", "event-2"}
    summary = "The agent inspected the repository and found one failing test."


class FutureOpenHandsEvent:
    id = "event-future"
    source = "environment"
    payload = {"new_field": True}


def _adapter():
    from agentscope_sdk.adapters import AdapterSession
    from agentscope_sdk.adapters.openhands import OpenHandsAdapter

    exporter = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exporter))
    runtime = _Runtime()
    session = AdapterSession(
        runtime,
        provider.get_tracer("openhands-test"),
        metadata={"adapter_name": "openhands"},
    )
    adapter = OpenHandsAdapter(
        session,
        capture_reasoning=True,
        capture_system_prompt=True,
    )
    return adapter, runtime, exporter


def test_openhands_adapter_maps_public_events_and_parallel_tool_ids():
    from agentscope_sdk.adapters.openhands import OPENHANDS_UPSTREAM_COMMIT

    adapter, runtime, exporter = _adapter()
    adapter.start("Fix the failing test.", conversation_id="conversation-1")

    adapter.on_event(
        MessageEvent(
            "user",
            "Fix the failing test.",
            event_id="event-user",
        )
    )
    adapter.on_event(
        ActionEvent(
            "terminal",
            "parallel-1",
            {"command": "pytest -q"},
            event_id="event-action-1",
            llm_response_id="response-1",
            thought="Run the test suite first.",
        )
    )
    adapter.on_event(
        ActionEvent(
            "file_editor",
            "parallel-2",
            {"path": "answer.txt", "content": "fixed"},
            event_id="event-action-2",
            llm_response_id="response-1",
        )
    )
    adapter.on_event(
        ObservationEvent(
            "terminal",
            "parallel-1",
            {"text": "1 passed", "exit_code": 0},
            event_id="event-observation-1",
        )
    )
    adapter.on_event(
        ObservationEvent(
            "file_editor",
            "parallel-2",
            {"text": "updated answer.txt"},
            event_id="event-observation-2",
        )
    )
    adapter.on_event(
        MessageEvent(
            "agent",
            "I verified the correction.",
            event_id="event-agent",
            reasoning_content="I verified the correction.",
            activated_skills=["testing"],
            llm_response_id="response-2",
        )
    )
    adapter.on_event(
        ActionEvent(
            "finish",
            "finish-1",
            {"message": "测试已修复并验证通过。"},
            event_id="event-finish-action",
            llm_response_id="response-3",
        )
    )
    adapter.on_event(
        ObservationEvent(
            "finish",
            "finish-1",
            {"content": [{"text": "测试已修复并验证通过。"}]},
            event_id="event-finish-observation",
        )
    )
    adapter.finish()

    spans = exporter.get_finished_spans()
    names = [span.name for span in spans]
    by_name = {span.name: span for span in spans}
    assert runtime.start_kwargs["conversation_id"] == "conversation-1"
    assert names.count("tool.terminal") == 1
    assert names.count("tool.file_editor") == 1
    assert by_name["tool.terminal"].attributes["tool.success"] == "true"
    assert by_name["tool.file_editor"].attributes["tool.success"] == "true"
    assert by_name["agent.run"].attributes["openhands.upstream_commit"] == (
        OPENHANDS_UPSTREAM_COMMIT
    )
    assert by_name["agent.answer"].attributes["agentscope.span.kind"] == (
        "FINAL_ANSWER"
    )
    assert runtime.finish_kwargs["output"] == "测试已修复并验证通过。"


def test_openhands_adapter_maps_errors_state_prompt_condensation_and_unknown_event():
    adapter, runtime, exporter = _adapter()
    adapter.start("Inspect the workspace.")

    adapter.on_event(SystemPromptEvent())
    adapter.on_event(ConversationStateUpdateEvent())
    adapter.on_event(PauseEvent())
    adapter.on_event(Condensation())
    adapter.on_event(
        ActionEvent(
            "terminal",
            "parallel-2",
            {"command": "false"},
            event_id="event-action",
            llm_response_id="response-error",
        )
    )
    adapter.on_event(AgentErrorEvent())
    adapter.on_event(FutureOpenHandsEvent())
    adapter.on_event(ConversationErrorEvent())
    adapter.fail()

    spans = {span.name: span for span in exporter.get_finished_spans()}
    assert runtime.states[0][0] == "waiting_user"
    assert runtime.states[1][0] == "waiting_user"
    assert "openhands.system_prompt" in spans
    assert "agent.context_condensation" in spans
    assert "openhands.event.FutureOpenHandsEvent" in spans
    assert spans["tool.terminal"].status.status_code.name == "ERROR"
    assert runtime.fail_kwargs["error"] == "workspace disappeared"


def test_openhands_token_callback_records_ttft_and_never_raises():
    adapter, _, exporter = _adapter()
    adapter.start("Stream a response.")

    chunk = SimpleNamespace(
        choices=[
            SimpleNamespace(
                delta=SimpleNamespace(
                    content="hello",
                    reasoning_content="thinking",
                    tool_calls=None,
                )
            )
        ]
    )
    adapter.on_token(chunk)
    adapter.on_token(chunk)

    original = adapter.session.on_external_event
    adapter.session.on_external_event = lambda *args, **kwargs: (_ for _ in ()).throw(
        RuntimeError("observer failure")
    )
    adapter.on_event(FutureOpenHandsEvent())
    adapter.session.on_external_event = original
    adapter.finish("hellohello")

    first_token = next(
        span
        for span in exporter.get_finished_spans()
        if span.name == "llm.first_token"
    )
    assert first_token.attributes["llm.ttft_ms"] >= 0


def test_openhands_callbacks_are_directly_usable_by_conversation_constructor():
    adapter, _, _ = _adapter()
    assert adapter.callbacks == [adapter.on_event]
    assert adapter.token_callbacks == [adapter.on_token]
