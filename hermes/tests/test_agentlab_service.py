from __future__ import annotations

import httpx
import pytest

from hermes_cli.agentlab_service import _build_agent, create_interactive_app


class _Agent:
    session_id = "initial"

    def run_conversation(self, message: str, *, task_id: str):
        if message == "fail":
            return {"completed": False, "interrupted": False}
        return {"completed": True, "final_response": f"answer:{task_id}"}


def test_interactive_service_defaults_to_openai_provider_for_direct_runtime_config(monkeypatch: pytest.MonkeyPatch) -> None:
    captured: dict[str, object] = {}

    class _CapturedAgent:
        def __init__(self, **kwargs: object) -> None:
            captured.update(kwargs)

    import sys
    import types

    monkeypatch.setitem(sys.modules, "run_agent", types.SimpleNamespace(AIAgent=_CapturedAgent))
    _build_agent(
        {
            "LLM_MODEL": "openai/deepseek-v4-flash",
            "LLM_API_KEY": "test-key",
            "LLM_BASE_URL": "https://provider.example/v1",
        }
    )

    assert captured["provider"] == "openai"
    assert captured["model"] == "openai/deepseek-v4-flash"
    assert captured["base_url"] == "https://provider.example/v1"
    assert captured["disabled_toolsets"] == ["skills"]


def test_interactive_service_allows_explicit_provider_override(monkeypatch: pytest.MonkeyPatch) -> None:
    captured: dict[str, object] = {}

    class _CapturedAgent:
        def __init__(self, **kwargs: object) -> None:
            captured.update(kwargs)

    import sys
    import types

    monkeypatch.setitem(sys.modules, "run_agent", types.SimpleNamespace(AIAgent=_CapturedAgent))
    _build_agent(
        {
            "LLM_MODEL": "model",
            "LLM_API_KEY": "test-key",
            "HERMES_PROVIDER": "custom-provider",
        }
    )

    assert captured["provider"] == "custom-provider"



@pytest.mark.asyncio
async def test_interactive_service_uses_native_agent_and_sanitizes_failure() -> None:
    app = create_interactive_app(agent_factory=lambda _env: _Agent())
    async with app.router.lifespan_context(app):
        transport = httpx.ASGITransport(app=app)
        async with httpx.AsyncClient(transport=transport, base_url="http://service") as client:
            health = await client.get("/healthz")
            assert health.json() == {"ok": True, "service": "hermes-interactive", "ready": True}
            response = await client.post(
                "/v1/chat",
                json={"message": "hello", "session_id": "session-1", "task_id": "task-1"},
            )
            assert response.status_code == 200
            assert response.json() == {
                "completed": True,
                "task_id": "task-1",
                "final_output": "answer:task-1",
                "error": None,
            }
            failed = await client.post("/v1/chat", json={"message": "fail", "task_id": "task-2"})
            assert failed.json() == {
                "completed": False,
                "task_id": "task-2",
                "final_output": None,
                "error": "agent_execution_failed",
            }


@pytest.mark.asyncio
async def test_interactive_service_returns_not_ready_without_config_details() -> None:
    app = create_interactive_app(agent_factory=lambda _env: (_ for _ in ()).throw(ValueError("sensitive config")))
    async with app.router.lifespan_context(app):
        transport = httpx.ASGITransport(app=app)
        async with httpx.AsyncClient(transport=transport, base_url="http://service") as client:
            health = await client.get("/healthz")
            assert health.json()["ready"] is False
            response = await client.post("/v1/chat", json={"message": "hello"})
            assert response.status_code == 503
            assert "sensitive config" not in response.text
