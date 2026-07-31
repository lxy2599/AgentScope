from __future__ import annotations

from typing import Any

from openhands.sdk.agentscope.bridge import AgentScopeBridge
from openhands.sdk.agentscope.config import AgentScopeSettings, load_agentscope_settings


class RecordingSession:
    def __init__(self) -> None:
        self.calls: list[tuple[str, tuple[Any, ...], dict[str, Any]]] = []

    def _record(self, method: str, *args: Any, **kwargs: Any) -> bool:
        self.calls.append((method, args, kwargs))
        return True

    def start_run(self, *args: Any, **kwargs: Any) -> bool:
        return self._record("start_run", *args, **kwargs)

    def update_run_status(self, *args: Any, **kwargs: Any) -> bool:
        return self._record("update_run_status", *args, **kwargs)

    def start_turn(self, *args: Any, **kwargs: Any) -> bool:
        return self._record("start_turn", *args, **kwargs)

    def finish_turn(self, *args: Any, **kwargs: Any) -> bool:
        return self._record("finish_turn", *args, **kwargs)

    def start_llm(self, *args: Any, **kwargs: Any) -> bool:
        return self._record("start_llm", *args, **kwargs)

    def finish_llm(self, *args: Any, **kwargs: Any) -> bool:
        return self._record("finish_llm", *args, **kwargs)

    def start_tool(self, *args: Any, **kwargs: Any) -> bool:
        return self._record("start_tool", *args, **kwargs)

    def finish_tool(self, *args: Any, **kwargs: Any) -> bool:
        return self._record("finish_tool", *args, **kwargs)

    def record_semantic_event(self, *args: Any, **kwargs: Any) -> bool:
        return self._record("record_semantic_event", *args, **kwargs)

    def record_final_answer(self, *args: Any, **kwargs: Any) -> bool:
        return self._record("record_final_answer", *args, **kwargs)

    def finish_run(self, *args: Any, **kwargs: Any) -> bool:
        return self._record("finish_run", *args, **kwargs)

    def fail_run(self, *args: Any, **kwargs: Any) -> bool:
        return self._record("fail_run", *args, **kwargs)


def enabled_settings(capture_level: str = "L0") -> AgentScopeSettings:
    return AgentScopeSettings(
        enabled=True,
        server_url="http://agentscope.test",
        project_token="test-token",
        capture_level=capture_level,
        reason="enabled",
    )


def test_default_configuration_is_disabled_and_invalid_capture_level_falls_back_to_l0():
    assert not load_agentscope_settings({}).enabled
    settings = load_agentscope_settings(
        {
            "OPENHANDS_AGENTSCOPE_ENABLED": "true",
            "AGENTSCOPE_SERVER_URL": "http://agentscope.test",
            "AGENTSCOPE_PROJECT_TOKEN": "test-token",
            "OPENHANDS_AGENTSCOPE_CAPTURE_LEVEL": "unexpected",
        }
    )
    assert settings.enabled
    assert settings.capture_level == "L0"


def test_native_bridge_creates_one_run_per_task_and_redacts_l0_content():
    sessions: list[RecordingSession] = []
    bridge = AgentScopeBridge(
        settings=enabled_settings(),
        session_factory=lambda: sessions.append(RecordingSession()) or sessions[-1],
    )

    first_key = bridge.on_run_started(
        "conversation-1", "secret user request", task_id="message-1"
    )
    same_key = bridge.on_run_started(
        "conversation-1", "ignored duplicate start", task_id="message-1"
    )
    bridge.on_turn_started("conversation-1", turn_id="turn-1", turn_index=1)
    bridge.on_llm_started(
        "conversation-1",
        "openhands.llm",
        call_id="llm-1",
        input_summary="secret prompt",
    )
    bridge.on_llm_finished(
        "conversation-1",
        "openhands.llm",
        call_id="llm-1",
        output_summary="secret output",
    )
    bridge.on_tool_started(
        "conversation-1", "terminal", {"command": "cat .env"}, tool_id="tool-1"
    )
    bridge.on_tool_finished(
        "conversation-1", "terminal", {"output": "secret"}, tool_id="tool-1"
    )
    bridge.on_final_answer("conversation-1", "secret final")

    assert first_key == same_key == "conversation-1:task:1"
    assert len(sessions) == 1
    calls = sessions[0].calls
    assert [call[0] for call in calls] == [
        "start_run",
        "update_run_status",
        "update_run_status",
        "start_turn",
        "start_llm",
        "finish_llm",
        "start_tool",
        "finish_tool",
        "record_final_answer",
        "finish_turn",
        "finish_run",
    ]
    assert "secret user request" not in calls[0][1][0]
    assert "sha256=" in calls[0][1][0]
    assert calls[0][2]["metadata"]["integration"] == "native"
    assert bridge.active_session_count == 0


def test_visible_reasoning_is_only_recorded_at_l2():
    l0_session = RecordingSession()
    l0 = AgentScopeBridge(
        settings=enabled_settings("L0"), session_factory=lambda: l0_session
    )
    l0.on_run_started("c0", "task")
    l0.on_semantic_event("c0", "THOUGHT", "openhands.thought", output_summary="hidden")
    assert not [call for call in l0_session.calls if call[0] == "record_semantic_event"]

    l2_session = RecordingSession()
    l2 = AgentScopeBridge(
        settings=enabled_settings("L2"), session_factory=lambda: l2_session
    )
    l2.on_run_started("c2", "task")
    l2.on_semantic_event("c2", "THOUGHT", "openhands.thought", output_summary="visible")
    semantic = [call for call in l2_session.calls if call[0] == "record_semantic_event"]
    assert len(semantic) == 1
    assert semantic[0][2]["output_summary"] == "visible"
