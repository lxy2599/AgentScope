"""Behavior tests for the optional native AgentScope bridge."""

from __future__ import annotations

import sys
from dataclasses import dataclass, field
from typing import Any

from hermes_agentscope.bridge import AgentScopeBridge
from hermes_agentscope.config import load_agentscope_settings


@dataclass
class _Session:
    calls: list[tuple[str, tuple[Any, ...], dict[str, Any]]] = field(default_factory=list)

    def _record(self, name: str, *args: Any, **kwargs: Any):
        self.calls.append((name, args, kwargs))
        return True

    start_run = lambda self, *args, **kwargs: self._record("start_run", *args, **kwargs)
    start_turn = lambda self, *args, **kwargs: self._record("start_turn", *args, **kwargs)
    finish_turn = lambda self, *args, **kwargs: self._record("finish_turn", *args, **kwargs)
    record_llm = lambda self, *args, **kwargs: self._record("record_llm", *args, **kwargs)
    start_llm = lambda self, *args, **kwargs: self._record("start_llm", *args, **kwargs)
    finish_llm = lambda self, *args, **kwargs: self._record("finish_llm", *args, **kwargs)
    record_tool = lambda self, *args, **kwargs: self._record("record_tool", *args, **kwargs)
    start_tool = lambda self, *args, **kwargs: self._record("start_tool", *args, **kwargs)
    finish_tool = lambda self, *args, **kwargs: self._record("finish_tool", *args, **kwargs)
    record_final_answer = lambda self, *args, **kwargs: self._record("record_final_answer", *args, **kwargs)
    finish_run = lambda self, *args, **kwargs: self._record("finish_run", *args, **kwargs)
    fail_run = lambda self, *args, **kwargs: self._record("fail_run", *args, **kwargs)


def test_settings_are_disabled_by_default_and_never_expose_token():
    settings = load_agentscope_settings({})
    assert settings.enabled is False

    missing_token = load_agentscope_settings(
        {"HERMES_AGENTSCOPE_ENABLED": "true", "AGENTSCOPE_SERVER_URL": "http://agentscope"}
    )
    assert missing_token.enabled is False
    assert missing_token.reason == "missing_project_token"

    enabled = load_agentscope_settings(
        {
            "HERMES_AGENTSCOPE_ENABLED": "true",
            "AGENTSCOPE_SERVER_URL": "http://agentscope/",
            "AGENTSCOPE_PROJECT_TOKEN": "secret-token",
        }
    )
    assert enabled.enabled is True
    assert enabled.server_url == "http://agentscope"
    assert "secret-token" not in repr(enabled)


def test_disabled_bridge_never_imports_or_requires_agentscope_sdk(monkeypatch):
    monkeypatch.setitem(sys.modules, "agentscope_sdk", None)
    bridge = AgentScopeBridge(settings=load_agentscope_settings({}))

    bridge.on_user_turn_started("conversation-1", "hello")
    bridge.on_decision_turn_started("conversation-1", "turn-1", turn_index=1)
    bridge.trace_llm("conversation-1", "model")
    bridge.trace_tool("conversation-1", "search", {"q": "hello"}, "result")
    assert bridge.on_final_answer("conversation-1", "done") is False
    assert bridge.active_session_count == 0


def test_bridge_keeps_concurrent_conversation_sessions_isolated():
    sessions: list[_Session] = []

    def factory() -> _Session:
        session = _Session()
        sessions.append(session)
        return session

    settings = load_agentscope_settings(
        {
            "HERMES_AGENTSCOPE_ENABLED": "1",
            "AGENTSCOPE_SERVER_URL": "http://agentscope",
            "AGENTSCOPE_PROJECT_TOKEN": "token",
        }
    )
    bridge = AgentScopeBridge(settings=settings, session_factory=factory)

    first = bridge.on_user_turn_started("conversation-a", "first", session_id="a")
    second = bridge.on_user_turn_started("conversation-b", "second", session_id="b")
    bridge.on_decision_turn_started("conversation-a", "turn-a", turn_index=1)
    bridge.trace_tool("conversation-b", "lookup", {"q": "two"}, "result", tool_id="tool-b")

    assert first is not second
    assert bridge.active_session_count == 2
    assert [name for name, _, _ in first.calls] == ["start_run", "start_turn"]
    assert [name for name, _, _ in second.calls] == ["start_run", "record_tool"]

    assert bridge.on_final_answer("conversation-a", "answer-a") is True
    assert bridge.on_turn_failed("conversation-b", "network down") is True
    assert bridge.active_session_count == 0
    assert [name for name, _, _ in first.calls][-3:] == [
        "record_final_answer",
        "finish_turn",
        "finish_run",
    ]
    assert [name for name, _, _ in second.calls][-2:] == ["finish_turn", "fail_run"]


def test_bridge_turns_sdk_initialization_failure_into_a_noop():
    settings = load_agentscope_settings(
        {
            "HERMES_AGENTSCOPE_ENABLED": "true",
            "AGENTSCOPE_SERVER_URL": "http://agentscope",
            "AGENTSCOPE_PROJECT_TOKEN": "token",
        }
    )

    def unavailable() -> _Session:
        raise RuntimeError("SDK unavailable")

    bridge = AgentScopeBridge(settings=settings, session_factory=unavailable)
    session = bridge.on_user_turn_started("conversation-1", "hello")

    assert session.enabled is False
    assert bridge.on_final_answer("conversation-1", "done") is False
