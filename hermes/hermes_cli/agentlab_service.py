"""Small owned interactive HTTP service for the AgentLab Hermes fork.

It runs the real :class:`run_agent.AIAgent` conversation loop.  AgentScope
registration and heartbeat are therefore initialized by Hermes source itself,
not by a plugin or an external observer.
"""

from __future__ import annotations

import argparse
import asyncio
import os
import uuid
from collections.abc import Mapping
from contextlib import asynccontextmanager
from typing import Annotated, Any, Callable, Literal

import uvicorn
from fastapi import FastAPI, HTTPException, status
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from pydantic import BaseModel, ConfigDict, Field

_MAX_FINAL_OUTPUT_CHARS = 64_000


class _Model(BaseModel):
    model_config = ConfigDict(extra="forbid")


class ChatRequest(_Model):
    message: Annotated[str, Field(min_length=1, max_length=200_000)]
    session_id: Annotated[str | None, Field(max_length=512)] = None
    task_id: Annotated[str | None, Field(max_length=512)] = None


class ChatResponse(_Model):
    completed: bool
    task_id: str
    final_output: str | None = None
    error: str | None = None


class HealthResponse(_Model):
    ok: Literal[True] = True
    service: Literal["hermes-interactive"] = "hermes-interactive"
    ready: bool


def _first_env(values: Mapping[str, str], *names: str) -> str:
    for name in names:
        value = values.get(name)
        if isinstance(value, str) and value.strip():
            return value.strip()
    return ""


def _build_agent(values: Mapping[str, str]) -> Any:
    from run_agent import AIAgent

    model = _first_env(values, "HERMES_MODEL", "LLM_MODEL")
    api_key = _first_env(values, "HERMES_API_KEY", "LLM_API_KEY")
    base_url = _first_env(values, "HERMES_BASE_URL", "LLM_BASE_URL")
    # AgentLab supplies an OpenAI-compatible endpoint/key directly rather than
    # a user-level Hermes provider profile.  Use ``openai`` by default so the
    # real AIAgent constructor does not fall back to an absent local profile.
    provider = _first_env(values, "HERMES_PROVIDER", "LLM_PROVIDER") or "openai"
    if not model or not api_key:
        raise ValueError("interactive model configuration is missing")
    try:
        max_iterations = int(str(values.get("HERMES_INTERACTIVE_MAX_ITERATIONS", "120")))
    except ValueError as exc:
        raise ValueError("interactive max iterations is invalid") from exc
    if not 1 <= max_iterations <= 1_000:
        raise ValueError("interactive max iterations is invalid")
    return AIAgent(
        api_key=api_key,
        base_url=base_url or None,
        provider=provider,
        model=model,
        max_iterations=max_iterations,
        quiet_mode=True,
        save_trajectories=False,
    )


def create_interactive_app(
    *,
    env: Mapping[str, str] | None = None,
    agent_factory: Callable[[Mapping[str, str]], Any] = _build_agent,
) -> FastAPI:
    """Create the interactive server without logging request/model content."""
    values = os.environ if env is None else env

    @asynccontextmanager
    async def lifespan(app: FastAPI):
        app.state.agent = None
        app.state.agent_lock = asyncio.Lock()
        app.state.ready = False
        try:
            # This is the genuine Hermes constructor. It directly invokes the
            # AgentScope SDK initialization in agent/agent_init.py when enabled.
            app.state.agent = await asyncio.to_thread(agent_factory, values)
            app.state.ready = True
        except Exception:
            # /healthz reports not-ready without serializing the provider/config
            # failure, which can contain a URL or credential-bearing details.
            app.state.agent = None
            app.state.ready = False
        try:
            yield
        finally:
            # Process-level runtime/tracing resources are owned by the service
            # lifetime, never a user turn. Shutdown remains best-effort.
            try:
                from agentscope_sdk import shutdown_runtime, shutdown_tracing

                shutdown_runtime()
                shutdown_tracing()
            except Exception:
                pass

    app = FastAPI(
        title="Hermes Interactive Service",
        version="v1",
        docs_url=None,
        redoc_url=None,
        openapi_url=None,
        lifespan=lifespan,
    )

    @app.exception_handler(RequestValidationError)
    async def invalid_request(_request: Any, _exc: RequestValidationError) -> JSONResponse:
        return JSONResponse(status_code=status.HTTP_422_UNPROCESSABLE_CONTENT, content={"detail": "invalid_request"})

    @app.get("/healthz", response_model=HealthResponse)
    async def healthz() -> HealthResponse:
        return HealthResponse(ready=bool(app.state.ready))

    @app.post("/v1/chat", response_model=ChatResponse)
    async def chat(request: ChatRequest) -> ChatResponse:
        agent = app.state.agent
        if agent is None or not app.state.ready:
            raise HTTPException(status_code=status.HTTP_503_SERVICE_UNAVAILABLE, detail="service_not_ready")
        task_id = request.task_id or f"interactive-{uuid.uuid4().hex}"
        async with app.state.agent_lock:
            prior_session_id = getattr(agent, "session_id", None)
            if request.session_id:
                agent.session_id = request.session_id
            try:
                result = await asyncio.to_thread(agent.run_conversation, request.message, task_id=task_id)
            except Exception:
                return ChatResponse(completed=False, task_id=task_id, error="agent_execution_failed")
            finally:
                if request.session_id:
                    agent.session_id = prior_session_id
        if result.get("completed"):
            output = result.get("final_response")
            return ChatResponse(
                completed=True,
                task_id=task_id,
                final_output=output[:_MAX_FINAL_OUTPUT_CHARS] if isinstance(output, str) else "",
            )
        return ChatResponse(
            completed=False,
            task_id=task_id,
            error="cancelled" if result.get("interrupted") else "agent_execution_failed",
        )

    return app


def main() -> None:
    parser = argparse.ArgumentParser(description="Hermes interactive HTTP service")
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--port", type=int, default=8000)
    args = parser.parse_args()
    uvicorn.run(create_interactive_app(), host=args.host, port=args.port, log_level="warning", access_log=False)


if __name__ == "__main__":
    main()
