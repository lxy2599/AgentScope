"""Owned Hermes Benchmark Task Runner HTTP v1.

This private service is intentionally narrow. It accepts only the cross-framework
AgentLab task contract, starts Hermes' real native conversation loop in a worker
thread, and exposes sanitized state to the benchmark worker. Credentials and the
Harbor capability token are runtime environment only; neither is accepted nor
returned in an HTTP body.
"""

from __future__ import annotations

import argparse
import asyncio
import os
import threading
from collections.abc import Mapping
from dataclasses import dataclass, field
from datetime import UTC, datetime
from enum import StrEnum
from pathlib import Path
from typing import Annotated, Any, Literal, Protocol
from urllib.parse import urlparse
from uuid import UUID

import uvicorn
from fastapi import FastAPI, HTTPException, status
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from pydantic import BaseModel, ConfigDict, Field, field_validator

from agent.harbor_tool_gateway import HarborToolGateway

TASK_RUNNER_PROTOCOL_VERSION = "v1"
HARBOR_TOOL_GATEWAY_PROTOCOL = "agentlab.harbor-tool-gateway.v1"
_TASK_RUNNER_EXECUTOR_ENV = "HERMES_TASK_RUNNER_EXECUTOR"
_TOOL_GATEWAY_TOKEN_ENV = "AGENTLAB_TOOL_GATEWAY_TOKEN"
_MAX_FINAL_OUTPUT_CHARS = 64_000
_MAX_TOOL_GATEWAY_TIMEOUT_SECONDS = 3_600
_DISABLE_STREAMING_ENV = "HERMES_TASK_RUNNER_DISABLE_STREAMING"


class TaskRunStatus(StrEnum):
    QUEUED = "queued"
    RUNNING = "running"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    CANCELLED = "cancelled"
    TIMED_OUT = "timed_out"

    @property
    def terminal(self) -> bool:
        return self in {
            self.SUCCEEDED,
            self.FAILED,
            self.CANCELLED,
            self.TIMED_OUT,
        }


class TaskRunnerModel(BaseModel):
    model_config = ConfigDict(extra="forbid")


class TaskSpec(TaskRunnerModel):
    task_id: Annotated[str, Field(min_length=1, max_length=512)]
    instruction: Annotated[str, Field(min_length=1, max_length=200_000)]
    workspace: str
    timeout_seconds: Annotated[int, Field(ge=1, le=86_400)]

    @field_validator("workspace")
    @classmethod
    def validate_workspace(cls, value: str) -> str:
        if not Path(value).is_absolute():
            raise ValueError("workspace must be an absolute path")
        return value


class ToolGatewaySpec(TaskRunnerModel):
    protocol_version: Literal["agentlab.harbor-tool-gateway.v1"]
    endpoint: Annotated[str, Field(min_length=1, max_length=2_048)]

    @field_validator("endpoint")
    @classmethod
    def validate_endpoint(cls, value: str) -> str:
        parsed = urlparse(value)
        if parsed.scheme not in {"http", "https"} or not parsed.netloc:
            raise ValueError("endpoint must be an absolute HTTP(S) URL")
        if parsed.username is not None or parsed.password is not None:
            raise ValueError("endpoint must not contain credentials")
        if parsed.query or parsed.fragment:
            raise ValueError("endpoint must not contain query or fragment")
        return value.rstrip("/")


class TaskProfiles(TaskRunnerModel):
    agent_profile: Annotated[str, Field(min_length=1, max_length=512)]
    model_profile: Annotated[str, Field(min_length=1, max_length=512)]


class TaskCorrelation(TaskRunnerModel):
    experiment_id: Annotated[str, Field(min_length=1, max_length=512)]
    eval_job_id: Annotated[str, Field(min_length=1, max_length=512)]
    trial_index: Annotated[int, Field(ge=0, le=2**53 - 1)]


class CreateTaskRunRequest(TaskRunnerModel):
    task_run_id: UUID
    task: TaskSpec
    tool_gateway: ToolGatewaySpec
    profiles: TaskProfiles
    correlation: TaskCorrelation


class TaskRunError(TaskRunnerModel):
    code: str


class TaskRunResponse(TaskRunnerModel):
    task_run_id: UUID
    state: TaskRunStatus
    created_at: datetime
    started_at: datetime | None = None
    completed_at: datetime | None = None
    final_output: str | None = None
    run_id: str | None = None
    error: TaskRunError | None = None


class HealthResponse(TaskRunnerModel):
    ok: Literal[True] = True
    protocol_version: Literal["v1"] = TASK_RUNNER_PROTOCOL_VERSION
    tool_gateway_protocols: tuple[Literal["agentlab.harbor-tool-gateway.v1"], ...] = (
        HARBOR_TOOL_GATEWAY_PROTOCOL,
    )
    executor: str


@dataclass(frozen=True)
class TaskExecutionResult:
    final_output: str | None = None
    run_id: str | None = None


class TaskExecutionError(RuntimeError):
    """Public error code only; never serialize exception text."""

    def __init__(self, code: str) -> None:
        self.code = code
        super().__init__(code)


class TaskExecutor(Protocol):
    name: str

    async def execute(
        self,
        request: CreateTaskRunRequest,
        cancellation: asyncio.Event,
    ) -> TaskExecutionResult: ...

    async def cancel(self, task_run_id: UUID) -> None: ...


class FakeTaskExecutor:
    """Deterministic test executor selected only by explicit configuration."""

    name = "fake"

    async def execute(
        self,
        request: CreateTaskRunRequest,
        cancellation: asyncio.Event,
    ) -> TaskExecutionResult:
        if cancellation.is_set():
            raise TaskExecutionError("cancelled")
        return TaskExecutionResult(final_output=f"fake completed {request.task.task_id}")

    async def cancel(self, task_run_id: UUID) -> None:
        del task_run_id


class HermesConversationExecutor:
    """Run the real Hermes source loop with a task-scoped Harbor gateway."""

    name = "native"

    def __init__(self, env: Mapping[str, str] | None = None) -> None:
        self._env = os.environ if env is None else env
        self._active_agents: dict[UUID, Any] = {}
        self._lock = threading.Lock()

    async def execute(
        self,
        request: CreateTaskRunRequest,
        cancellation: asyncio.Event,
    ) -> TaskExecutionResult:
        if cancellation.is_set():
            raise TaskExecutionError("cancelled")
        return await asyncio.to_thread(self._execute_blocking, request, cancellation)

    async def cancel(self, task_run_id: UUID) -> None:
        with self._lock:
            agent = self._active_agents.get(task_run_id)
        if agent is not None:
            try:
                agent.interrupt("benchmark task cancellation")
            except Exception:
                # Cancellation remains cooperative and must not expose a
                # framework exception to the Task Runner response.
                return

    def _execute_blocking(
        self,
        request: CreateTaskRunRequest,
        cancellation: asyncio.Event,
    ) -> TaskExecutionResult:
        if cancellation.is_set():
            raise TaskExecutionError("cancelled")
        agent = self._build_agent(request)
        with self._lock:
            self._active_agents[request.task_run_id] = agent
        try:
            result = agent.run_conversation(
                request.task.instruction,
                task_id=request.task.task_id,
            )
        except TaskExecutionError:
            raise
        except Exception as exc:
            raise TaskExecutionError(_safe_exception_code(exc)) from exc
        finally:
            with self._lock:
                self._active_agents.pop(request.task_run_id, None)

        if cancellation.is_set() or result.get("interrupted"):
            raise TaskExecutionError("cancelled")
        if result.get("failed") or not result.get("completed"):
            raise TaskExecutionError("agent_execution_failed")
        final_output = result.get("final_response")
        if not isinstance(final_output, str):
            final_output = ""
        run_id = getattr(agent, "_agentlab_agentscope_run_id", "")
        if not isinstance(run_id, str) or not run_id.strip():
            run_id = None
        return TaskExecutionResult(final_output=final_output[:_MAX_FINAL_OUTPUT_CHARS], run_id=run_id)

    def _build_agent(self, request: CreateTaskRunRequest) -> Any:
        from run_agent import AIAgent

        token = str(self._env.get(_TOOL_GATEWAY_TOKEN_ENV, "")).strip()
        if len(token) < 24:
            raise TaskExecutionError("gateway_capability_missing")
        model = _first_env(self._env, "HERMES_MODEL", "LLM_MODEL")
        api_key = _first_env(self._env, "HERMES_API_KEY", "LLM_API_KEY")
        base_url = _first_env(self._env, "HERMES_BASE_URL", "LLM_BASE_URL")
        if not model or not api_key:
            raise TaskExecutionError("model_configuration_missing")
        try:
            max_iterations = int(str(self._env.get("HERMES_TASK_RUNNER_MAX_ITERATIONS", "80")))
        except ValueError as exc:
            raise TaskExecutionError("agent_runtime_configuration_error") from exc
        if not 1 <= max_iterations <= 1_000:
            raise TaskExecutionError("agent_runtime_configuration_error")
        # Harbor's task timeout covers the complete agent run and can be much
        # larger than the maximum duration permitted for one terminal command.
        # Keep those limits independent so long-running benchmark tasks do not
        # fail during gateway construction before Hermes makes its first call.
        gateway_timeout_seconds = min(
            request.task.timeout_seconds,
            _MAX_TOOL_GATEWAY_TIMEOUT_SECONDS,
        )
        api_mode = _first_env(self._env, "HERMES_TASK_RUNNER_API_MODE")
        if api_mode and api_mode not in {"chat_completions", "codex_responses"}:
            raise TaskExecutionError("agent_runtime_configuration_error")
        try:
            agent = AIAgent(
                api_key=api_key,
                base_url=base_url or None,
                api_mode=api_mode or None,
                model=model,
                max_iterations=max_iterations,
                enabled_toolsets=["terminal"],
                quiet_mode=True,
                skip_context_files=True,
                skip_memory=True,
                save_trajectories=False,
            )
            agent._agentlab_harbor_tool_gateway = HarborToolGateway(
                endpoint=request.tool_gateway.endpoint,
                capability_token=token,
                max_timeout_seconds=gateway_timeout_seconds,
            )
            # Long Terminal-Bench conversations can exceed the upstream
            # reverse proxy's SSE lifetime. Benchmark runs do not need live
            # token rendering, so allow the Worker to select the more robust
            # complete-response path without changing interactive Hermes.
            if _is_truthy(self._env.get(_DISABLE_STREAMING_ENV)):
                agent._disable_streaming = True
            agent._agentscope_benchmark_context = {
                "experiment_id": request.correlation.experiment_id,
                "eval_job_id": request.correlation.eval_job_id,
                "eval_trial_id": f"{request.correlation.eval_job_id}:{request.correlation.trial_index}",
                "benchmark_key": str(self._env.get("AGENTSCOPE_BENCHMARK_KEY", "harbor")),
                "benchmark_task_id": request.task.task_id,
                "trial_index": request.correlation.trial_index,
            }
            return agent
        except TaskExecutionError:
            raise
        except Exception as exc:
            raise TaskExecutionError(_safe_exception_code(exc)) from exc


@dataclass
class _TaskRunRecord:
    request: CreateTaskRunRequest
    status: TaskRunStatus = TaskRunStatus.QUEUED
    created_at: datetime = field(default_factory=lambda: datetime.now(UTC))
    started_at: datetime | None = None
    completed_at: datetime | None = None
    final_output: str | None = None
    run_id: str | None = None
    error: TaskRunError | None = None
    cancellation: asyncio.Event = field(default_factory=asyncio.Event)
    task: asyncio.Task[None] | None = None

    def response(self) -> TaskRunResponse:
        return TaskRunResponse(
            task_run_id=self.request.task_run_id,
            state=self.status,
            created_at=self.created_at,
            started_at=self.started_at,
            completed_at=self.completed_at,
            final_output=self.final_output,
            run_id=self.run_id,
            error=self.error,
        )


class TaskRunManager:
    def __init__(self, executor: TaskExecutor) -> None:
        self.executor = executor
        self._records: dict[UUID, _TaskRunRecord] = {}
        self._lock = asyncio.Lock()

    @property
    def executor_name(self) -> str:
        return self.executor.name

    async def create(self, request: CreateTaskRunRequest) -> TaskRunResponse:
        async with self._lock:
            if request.task_run_id in self._records:
                raise TaskExecutionError("task_run_already_exists")
            record = _TaskRunRecord(request=request)
            self._records[request.task_run_id] = record
            record.task = asyncio.create_task(self._execute(record))
            return record.response()

    async def get(self, task_run_id: UUID) -> TaskRunResponse | None:
        async with self._lock:
            record = self._records.get(task_run_id)
            return record.response() if record is not None else None

    async def cancel(self, task_run_id: UUID) -> TaskRunResponse | None:
        async with self._lock:
            record = self._records.get(task_run_id)
            if record is None:
                return None
            if record.status.terminal:
                return record.response()
            record.cancellation.set()
            if record.status is TaskRunStatus.QUEUED:
                record.status = TaskRunStatus.CANCELLED
                record.completed_at = datetime.now(UTC)
                record.error = TaskRunError(code="cancelled")
                return record.response()
        await self.executor.cancel(task_run_id)
        return await self.get(task_run_id)

    async def _execute(self, record: _TaskRunRecord) -> None:
        async with self._lock:
            if record.status is TaskRunStatus.CANCELLED:
                return
            record.status = TaskRunStatus.RUNNING
            record.started_at = datetime.now(UTC)
        try:
            result = await asyncio.wait_for(
                self.executor.execute(record.request, record.cancellation),
                timeout=record.request.task.timeout_seconds,
            )
        except TimeoutError:
            record.cancellation.set()
            await self.executor.cancel(record.request.task_run_id)
            await self._set_terminal(record, TaskRunStatus.TIMED_OUT, error_code="timeout")
        except TaskExecutionError as exc:
            terminal = TaskRunStatus.CANCELLED if exc.code == "cancelled" else TaskRunStatus.FAILED
            await self._set_terminal(record, terminal, error_code=exc.code)
        except Exception as exc:
            await self._set_terminal(record, TaskRunStatus.FAILED, error_code=_safe_exception_code(exc))
        else:
            await self._set_terminal(record, TaskRunStatus.SUCCEEDED, final_output=result.final_output, run_id=result.run_id)

    async def _set_terminal(
        self,
        record: _TaskRunRecord,
        state: TaskRunStatus,
        *,
        final_output: str | None = None,
        run_id: str | None = None,
        error_code: str | None = None,
    ) -> None:
        async with self._lock:
            if record.status.terminal:
                return
            if record.cancellation.is_set() and state is not TaskRunStatus.TIMED_OUT:
                state = TaskRunStatus.CANCELLED
                error_code = "cancelled"
                final_output = None
            record.status = state
            record.completed_at = datetime.now(UTC)
            record.final_output = final_output[:_MAX_FINAL_OUTPUT_CHARS] if isinstance(final_output, str) else None
            record.run_id = run_id if isinstance(run_id, str) and run_id else None
            record.error = TaskRunError(code=error_code) if error_code else None


def build_task_executor_from_env(env: Mapping[str, str] | None = None) -> TaskExecutor:
    values = os.environ if env is None else env
    selected = str(values.get(_TASK_RUNNER_EXECUTOR_ENV, "native")).strip().lower()
    if selected in {"", "native"}:
        return HermesConversationExecutor(values)
    if selected == "fake":
        return FakeTaskExecutor()
    raise ValueError("unsupported Hermes task runner executor")


def create_task_runner_app(executor: TaskExecutor | None = None) -> FastAPI:
    manager = TaskRunManager(executor or build_task_executor_from_env())
    app = FastAPI(
        title="Hermes Benchmark Task Runner",
        version=TASK_RUNNER_PROTOCOL_VERSION,
        docs_url=None,
        redoc_url=None,
        openapi_url=None,
    )
    app.state.task_run_manager = manager

    @app.exception_handler(RequestValidationError)
    async def invalid_request(_request: Any, _exc: RequestValidationError) -> JSONResponse:
        return JSONResponse(status_code=status.HTTP_422_UNPROCESSABLE_CONTENT, content={"detail": "invalid_task_run_request"})

    @app.get("/healthz", response_model=HealthResponse)
    async def healthz() -> HealthResponse:
        return HealthResponse(executor=manager.executor_name)

    @app.post("/v1/task-runs", status_code=status.HTTP_202_ACCEPTED, response_model=TaskRunResponse)
    async def create_task_run(request: CreateTaskRunRequest) -> TaskRunResponse:
        try:
            return await manager.create(request)
        except TaskExecutionError as exc:
            code = status.HTTP_409_CONFLICT if exc.code == "task_run_already_exists" else status.HTTP_400_BAD_REQUEST
            raise HTTPException(status_code=code, detail=exc.code) from exc

    @app.get("/v1/task-runs/{task_run_id}", response_model=TaskRunResponse)
    async def get_task_run(task_run_id: UUID) -> TaskRunResponse:
        task = await manager.get(task_run_id)
        if task is None:
            raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="not_found")
        return task

    @app.post("/v1/task-runs/{task_run_id}/cancel", response_model=TaskRunResponse)
    async def cancel_task_run(task_run_id: UUID) -> TaskRunResponse:
        task = await manager.cancel(task_run_id)
        if task is None:
            raise HTTPException(status_code=status.HTTP_404_NOT_FOUND, detail="not_found")
        return task

    return app


def _first_env(values: Mapping[str, str], *names: str) -> str:
    for name in names:
        value = values.get(name)
        if isinstance(value, str) and value.strip():
            return value.strip()
    return ""


def _is_truthy(value: object) -> bool:
    return str(value or "").strip().lower() in {"1", "true", "yes", "on"}


def _safe_exception_code(exc: Exception) -> str:
    if isinstance(exc, (ModuleNotFoundError, ImportError)):
        return "runtime_dependency_missing"
    if isinstance(exc, (ValueError, TypeError)):
        return "agent_runtime_configuration_error"
    module = type(exc).__module__
    if module.startswith(("openai", "httpx", "httpcore", "anthropic")):
        return "model_provider_error"
    return "agent_execution_failed"


def main() -> None:
    parser = argparse.ArgumentParser(description="Hermes Benchmark Task Runner v1")
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8001)
    args = parser.parse_args()
    app = create_task_runner_app()
    uvicorn.run(app, host=args.host, port=args.port, log_level="warning", access_log=False)


if __name__ == "__main__":
    main()
