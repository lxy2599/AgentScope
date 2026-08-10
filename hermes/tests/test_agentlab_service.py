from __future__ import annotations

import httpx
import pytest

from hermes_cli.agentlab_service import create_interactive_app


class _Agent:
    session_id = "initial"

    def run_conversation(self, message: str, *, task_id: str):
        if message == "fail":
            return {"completed": False, "interrupted": False}
        return {"completed": True, "final_response": f"answer:{task_id}"}


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
