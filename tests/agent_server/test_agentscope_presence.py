from __future__ import annotations

import sys
from types import ModuleType

from openhands.agent_server import agentscope_presence
from openhands.sdk.agentscope.config import AgentScopeSettings


def _enabled_settings() -> AgentScopeSettings:
    return AgentScopeSettings(
        enabled=True,
        server_url="http://agentscope.test",
        project_token="runtime-token",
        environment="test",
        reason="enabled",
    )


def test_presence_initializes_runtime_and_stops_heartbeat_without_exposing_token(
    monkeypatch,
) -> None:
    calls: list[tuple[str, object]] = []
    sdk = ModuleType("agentscope_sdk")
    sdk.get_runtime = lambda: None  # type: ignore[attr-defined]

    def init(server_url: str, token: str, name: str, **kwargs: object) -> object:
        calls.append(
            (
                "init",
                {"server_url": server_url, "token": token, "name": name, **kwargs},
            )
        )
        return object()

    sdk.init = init  # type: ignore[attr-defined]
    sdk.shutdown_runtime = lambda: calls.append(("shutdown", object()))  # type: ignore[attr-defined]
    monkeypatch.setitem(sys.modules, "agentscope_sdk", sdk)
    monkeypatch.setattr(
        agentscope_presence,
        "load_agentscope_settings",
        _enabled_settings,
    )
    agentscope_presence.reset_agentscope_presence_for_tests()

    assert agentscope_presence.start_agentscope_presence() is True
    assert agentscope_presence.start_agentscope_presence() is True
    assert [name for name, _ in calls] == ["init"]
    init_payload = calls[0][1]
    assert isinstance(init_payload, dict)
    assert init_payload["name"] == "openhands-agent"
    assert init_payload["framework"] == "openhands"
    assert init_payload["heartbeat_interval"] == 30
    assert "runtime-token" not in repr(agentscope_presence)

    agentscope_presence.stop_agentscope_presence()
    assert [name for name, _ in calls] == ["init", "shutdown"]


def test_presence_is_disabled_without_observability_config(monkeypatch) -> None:
    monkeypatch.setattr(
        agentscope_presence,
        "load_agentscope_settings",
        lambda: AgentScopeSettings(),
    )
    agentscope_presence.reset_agentscope_presence_for_tests()

    assert agentscope_presence.start_agentscope_presence() is False
