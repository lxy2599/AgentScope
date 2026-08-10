"""Benchmark Task Runner HTTP v1 for an owned OpenHands deployment.

The runner is deliberately a small, private HTTP service.  A benchmark worker
starts one runner per task sidecar and injects model credentials and AgentScope
settings as environment variables.  Request bodies never carry credentials and
this module never logs task payloads or exception strings.

The native executor builds a real :class:`openhands.sdk.Conversation`.  That
means the existing OpenHands AgentScope bridge remains the only observability
path: Conversation, Agent, LLM, and Tool events are emitted by their normal
execution hooks.  Benchmark correlation is read by that bridge from the
``AGENTSCOPE_*`` runtime environment variables; the HTTP API intentionally
never mutates process environment variables.
"""

from __future__ import annotations

import argparse
import asyncio
import math
import os
import tempfile
import threading
from collections.abc import Mapping
from dataclasses import dataclass, field
from datetime import UTC, datetime
from enum import StrEnum
from pathlib import Path
from typing import Annotated, Any, Literal, Protocol
from urllib.parse import urlparse
from uuid import UUID

import httpx
import uvicorn
from fastapi import FastAPI, HTTPException, status
from fastapi.exceptions import RequestValidationError
from fastapi.responses import JSONResponse
from pydantic import (
    BaseModel,
    ConfigDict,
    Field,
    SecretStr,
    field_validator,
)

from openhands.sdk.logger import get_logger
from openhands.sdk.tool import ToolExecutor
from openhands.tools.terminal import TerminalAction, TerminalObservation
from openhands.tools.terminal.metadata import CmdOutputMetadata


logger = get_logger(__name__)

TASK_RUNNER_PROTOCOL_VERSION = "v1"
HARBOR_TOOL_GATEWAY_PROTOCOL = "agentlab.harbor-tool-gateway.v1"
_TASK_RUNNER_EXECUTOR_ENV = "OPENHANDS_TASK_RUNNER_EXECUTOR"
_LLM_API_KEY_ENV = "LLM_API_KEY"
_LLM_MODEL_ENV = "LLM_MODEL"
_LLM_BASE_URL_ENV = "LLM_BASE_URL"
_TOOL_GATEWAY_TOKEN_ENV = "AGENTLAB_TOOL_GATEWAY_TOKEN"
_DEFAULT_LLM_MODEL = "gpt-5.5"


class TaskRunStatus(StrEnum):
    """Stable task states exposed by Task Runner HTTP v1."""

    QUEUED = "queued"
    RUNNING = "running"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    CANCELLED = "cancelled"
    TIMED_OUT = "timed_out"

    @property
    def terminal(self) -> bool:
        return self in {
            TaskRunStatus.SUCCEEDED,
            TaskRunStatus.FAILED,
            TaskRunStatus.CANCELLED,
            TaskRunStatus.TIMED_OUT,
        }


class TaskRunnerModel(BaseModel):
    """Strict v1 request/response base model."""

    model_config = ConfigDict(extra="forbid")


class TaskSpec(TaskRunnerModel):
    task_id: Annotated[str, Field(min_length=1, max_length=512)]
    instruction: Annotated[str, Field(min_length=1, max_length=200_000)]
    workspace: str
    timeout_seconds: Annotated[int, Field(ge=1, le=86_400)]

    @field_validator("workspace")
    @classmethod
    def validate_workspace(cls, value: str) -> str:
        workspace = Path(value)
        if not workspace.is_absolute():
            raise ValueError("workspace must be an absolute path")
        return str(workspace)


class ToolGatewaySpec(TaskRunnerModel):
    # This is intentionally distinct from the Task Runner HTTP protocol.  A
    # Harbor benchmark must opt in to the exact gateway protocol we advertise
    # on /healthz; unknown/legacy values are rejected before execution.
    protocol_version: Literal["agentlab.harbor-tool-gateway.v1"]
    endpoint: Annotated[str, Field(min_length=1, max_length=2_048)]

    @field_validator("endpoint")
    @classmethod
    def validate_private_http_endpoint(cls, value: str) -> str:
        parsed = urlparse(value)
        if parsed.scheme not in {"http", "https"} or not parsed.netloc:
            raise ValueError("endpoint must be an absolute HTTP(S) URL")
        if parsed.username is not None or parsed.password is not None:
            raise ValueError("endpoint must not contain credentials")
        if parsed.query or parsed.fragment:
            raise ValueError("endpoint must not contain query or fragment data")
        return value


class TaskProfiles(TaskRunnerModel):
    agent_profile: Annotated[str, Field(min_length=1, max_length=512)]
    model_profile: Annotated[str, Field(min_length=1, max_length=512)]


class TaskCorrelation(TaskRunnerModel):
    experiment_id: Annotated[str, Field(min_length=1, max_length=512)]
    eval_job_id: Annotated[str, Field(min_length=1, max_length=512)]
    trial_index: Annotated[int, Field(ge=0, le=2**53 - 1)]


class CreateTaskRunRequest(TaskRunnerModel):
    """Request accepted by ``POST /v1/task-runs``."""

    task_run_id: UUID
    task: TaskSpec
    tool_gateway: ToolGatewaySpec
    profiles: TaskProfiles
    correlation: TaskCorrelation


class TaskRunError(TaskRunnerModel):
    """Public, sanitized execution error.

    ``code`` is allowlisted by the runner.  The module deliberately does not
    serialize Python exception text because provider errors can include request
    metadata or credentials.
    """

    code: str


class TaskRunResponse(TaskRunnerModel):
    task_run_id: UUID
    # Public Task Runner v1 field name; all framework adapters expose this.
    state: TaskRunStatus
    created_at: datetime
    started_at: datetime | None = None
    completed_at: datetime | None = None
    final_output: str | None = None
    error: TaskRunError | None = None


class HealthResponse(TaskRunnerModel):
    # Stable cross-framework health shape for Task Runner Protocol v1.
    ok: Literal[True] = True
    protocol_version: Literal["v1"] = TASK_RUNNER_PROTOCOL_VERSION
    # Worker admission uses this explicit capability to reject older sidecars
    # before it launches a Harbor task.
    tool_gateway_protocols: tuple[Literal["agentlab.harbor-tool-gateway.v1"], ...] = (
        HARBOR_TOOL_GATEWAY_PROTOCOL,
    )
    executor: str


@dataclass(frozen=True)
class TaskExecutionResult:
    """Non-sensitive result produced by an executor."""

    final_output: str | None = None


class TaskExecutionError(RuntimeError):
    """Expected failure with a public code, never a raw provider message."""

    def __init__(self, code: str) -> None:
        self.code = code
        super().__init__(code)


def _safe_unexpected_executor_error_code(exc: Exception) -> str:
    """Classify native executor failures without retaining exception text.

    Task Runner responses are observable by the trusted Worker but raw exception
    messages may carry model-provider request details, gateway URLs, or task
    data.  Keep diagnosis to a short set based solely on exception type/module.
    """

    name = type(exc).__name__
    module = type(exc).__module__
    if isinstance(exc, (ModuleNotFoundError, ImportError)):
        return "runtime_dependency_missing"
    if name == "LLMContextWindowTooSmallError":
        return "model_context_window_too_small"
    if module.startswith(("litellm", "openai", "anthropic")):
        return "model_provider_error"
    if module.startswith(("httpx", "httpcore")):
        return "model_provider_connection_error"
    if isinstance(exc, (PermissionError, FileNotFoundError, NotADirectoryError)):
        return "agent_runtime_filesystem_error"
    if isinstance(exc, (ValueError, TypeError)):
        return "agent_runtime_configuration_error"
    return "agent_execution_failed"


class TaskExecutor(Protocol):
    """Small execution seam for the HTTP service and benchmark adapters."""

    name: str

    async def execute(
        self,
        request: CreateTaskRunRequest,
        cancellation: asyncio.Event,
    ) -> TaskExecutionResult: ...

    async def cancel(self, task_run_id: UUID) -> None: ...


class FakeTaskExecutor:
    """Deterministic executor for API smoke tests and local contract checks.

    It is only selected when ``OPENHANDS_TASK_RUNNER_EXECUTOR=fake`` or injected
    into ``create_task_runner_app``.  Production sidecars default to the native
    OpenHands executor.
    """

    name = "fake"

    async def execute(
        self,
        request: CreateTaskRunRequest,
        cancellation: asyncio.Event,
    ) -> TaskExecutionResult:
        if cancellation.is_set():
            raise TaskExecutionError("cancelled")
        return TaskExecutionResult(
            final_output=f"fake completed {request.task.task_id}"
        )

    async def cancel(self, task_run_id: UUID) -> None:
        del task_run_id
        return None


class GatewayTerminalExecutor(ToolExecutor[TerminalAction, TerminalObservation]):
    """Terminal executor that can reach only the task-scoped Tool Gateway.

    This executor deliberately has no local shell fallback.  The sidecar's
    ``Conversation`` still needs a scratch ``LocalWorkspace`` for OpenHands
    runtime state, but every benchmark terminal command is sent to Harbor via
    this bounded HTTP API.
    """

    _INTERACTIVE_ERROR = (
        "terminal_gateway_interactive_mode_not_supported: "
        "is_input and reset are unavailable for benchmark terminal commands."
    )
    _EMPTY_COMMAND_ERROR = (
        "terminal_gateway_empty_command_not_supported: "
        "submit a non-empty shell command instead of polling a local terminal."
    )
    _EXECUTION_ERROR = (
        "terminal_gateway_execution_failed: "
        "the benchmark terminal gateway did not return a valid command result."
    )

    def __init__(
        self,
        *,
        endpoint: str,
        token: str,
        task_timeout_seconds: int,
        client: httpx.Client | None = None,
    ) -> None:
        self._terminal_endpoint = f"{endpoint.rstrip('/')}/terminal/exec"
        self._token = token
        self._task_timeout_seconds = task_timeout_seconds
        self._client = client or httpx.Client()
        self._owns_client = client is None

    @staticmethod
    def _error(action: TerminalAction, message: str) -> TerminalObservation:
        return TerminalObservation.from_text(
            text=message,
            is_error=True,
            command=action.command,
            exit_code=-1,
            metadata=CmdOutputMetadata(exit_code=-1),
        )

    def _timeout_seconds(self, action: TerminalAction) -> int:
        if action.timeout is None:
            return self._task_timeout_seconds
        # The gateway protocol uses integer seconds.  Never allow one tool call
        # to outlive the task's hard limit.
        requested = max(1, math.ceil(action.timeout))
        return min(requested, self._task_timeout_seconds)

    def __call__(
        self,
        action: TerminalAction,
        conversation: Any | None = None,
    ) -> TerminalObservation:
        del conversation
        if action.is_input or action.reset:
            return self._error(action, self._INTERACTIVE_ERROR)
        if not action.command.strip():
            return self._error(action, self._EMPTY_COMMAND_ERROR)

        timeout_seconds = self._timeout_seconds(action)
        try:
            response = self._client.post(
                self._terminal_endpoint,
                json={
                    "command": action.command,
                    "timeout_seconds": timeout_seconds,
                },
                headers={"X-AgentLab-Tool-Gateway-Token": self._token},
                timeout=timeout_seconds + 5.0,
            )
            response.raise_for_status()
            body = response.json()
            if not isinstance(body, dict):
                raise ValueError("gateway response must be an object")
            stdout = body.get("stdout")
            stderr = body.get("stderr")
            return_code = body.get("return_code")
            if (
                not isinstance(stdout, str)
                or not isinstance(stderr, str)
                or not isinstance(return_code, int)
                or isinstance(return_code, bool)
            ):
                raise ValueError("gateway response has an invalid result shape")
        except (httpx.HTTPError, TypeError, ValueError):
            # Do not serialize status/body/exception text: it could contain
            # request or runtime secrets controlled by another service.
            return self._error(action, self._EXECUTION_ERROR)

        output = stdout
        if stderr:
            output = (
                f"{output}\n[stderr]\n{stderr}"
                if output
                else f"[stderr]\n{stderr}"
            )
        return TerminalObservation.from_text(
            text=output,
            command=action.command,
            exit_code=return_code,
            metadata=CmdOutputMetadata(exit_code=return_code),
        )

    def close(self) -> None:
        if self._owns_client:
            self._client.close()


class OpenHandsConversationExecutor:
    """Run one real OpenHands Conversation using runtime model configuration.

    All native AgentScope bridge events are emitted by ``Conversation`` and its
    descendants.  This executor does not reimplement tracing, registration, or
    heartbeat and never reads/logs AgentScope tokens itself.
    """

    name = "native"

    def __init__(self, env: Mapping[str, str] | None = None) -> None:
        self._env = os.environ if env is None else env
        self._conversations: dict[UUID, Any] = {}
        self._lock = threading.Lock()

    async def execute(
        self,
        request: CreateTaskRunRequest,
        cancellation: asyncio.Event,
    ) -> TaskExecutionResult:
        if cancellation.is_set():
            raise TaskExecutionError("cancelled")

        gateway_token = self._resolve_gateway_token()
        if gateway_token is None:
            raise TaskExecutionError("tool_gateway_not_configured")

        api_key = self._env.get(_LLM_API_KEY_ENV, "").strip()
        if not api_key:
            raise TaskExecutionError("missing_llm_api_key")

        model = self._env.get(_LLM_MODEL_ENV, _DEFAULT_LLM_MODEL).strip()
        if not model:
            raise TaskExecutionError("missing_llm_model")
        base_url = self._env.get(_LLM_BASE_URL_ENV) or None

        return await asyncio.to_thread(
            self._run_conversation,
            request,
            gateway_token,
            api_key,
            model,
            base_url,
            cancellation,
        )

    def _resolve_gateway_token(self) -> str | None:
        token = self._env.get(_TOOL_GATEWAY_TOKEN_ENV, "")
        # Header values cannot safely contain control characters.  Keep this
        # validation local and return only a public configuration error.
        if (
            not token.strip()
            or len(token) > 4096
            or any(ch in token for ch in "\r\n")
        ):
            return None
        return token

    def _run_conversation(
        self,
        request: CreateTaskRunRequest,
        gateway_token: str,
        api_key: str,
        model: str,
        base_url: str | None,
        cancellation: asyncio.Event,
    ) -> TaskExecutionResult:
        # Imports are delayed so fake/API contract tests do not initialize the
        # SDK's runtime dependencies or require an LLM credential.
        from openhands.sdk import LLM, Agent, Conversation, Tool
        from openhands.sdk.conversation.state import ConversationExecutionStatus
        from openhands.tools.task_tracker import TaskTrackerTool
        from openhands.tools.terminal import TerminalTool

        if cancellation.is_set():
            raise TaskExecutionError("cancelled")

        try:
            llm = LLM(
                usage_id="benchmark-task-runner",
                model=model,
                base_url=base_url,
                api_key=SecretStr(api_key),
            )
        except Exception:
            # Construction failures can contain provider/model values in their
            # messages; expose only this fixed stage code.
            raise TaskExecutionError("model_initialization_failed") from None
        try:
            terminal_executor = GatewayTerminalExecutor(
                endpoint=request.tool_gateway.endpoint,
                token=gateway_token,
                task_timeout_seconds=request.task.timeout_seconds,
            )
            agent = Agent(
                llm=llm,
                tools=[
                    # The executor is explicitly injected through OpenHands'
                    # tool registry. It has no subprocess/tmux fallback.
                    Tool(name=TerminalTool.name, params={"executor": terminal_executor}),
                    Tool(name=TaskTrackerTool.name),
                ],
            )
        except Exception:
            raise TaskExecutionError("agent_initialization_failed") from None
        # ``Conversation`` requires a local workspace for its own runtime
        # metadata. This scratch directory is never the task workspace and the
        # only task-mutating tool (terminal) is gateway-backed above.
        with tempfile.TemporaryDirectory(prefix="openhands-benchmark-") as scratch_dir:
            try:
                conversation = Conversation(agent=agent, workspace=Path(scratch_dir))
            except Exception:
                raise TaskExecutionError("conversation_initialization_failed") from None
            with self._lock:
                self._conversations[request.task_run_id] = conversation

            try:
                # The real Conversation execution path invokes the existing native
                # AgentScope bridge.  Correlation remains environment-derived to
                # avoid process-global configuration mutation between requests.
                conversation.send_message(request.task.instruction)
                if cancellation.is_set():
                    conversation.interrupt()
                    raise TaskExecutionError("cancelled")
                conversation.run()
                if cancellation.is_set():
                    raise TaskExecutionError("cancelled")

                execution_status = conversation.state.execution_status
                if execution_status == ConversationExecutionStatus.FINISHED:
                    # Final output is intentionally omitted.  The native bridge's
                    # capture policy controls any trace content; the HTTP API does
                    # not duplicate potentially sensitive agent output.
                    return TaskExecutionResult()
                if execution_status in {
                    ConversationExecutionStatus.PAUSED,
                    ConversationExecutionStatus.WAITING_FOR_CONFIRMATION,
                }:
                    raise TaskExecutionError("waiting_for_input")
                raise TaskExecutionError(f"conversation_{execution_status.value}")
            finally:
                with self._lock:
                    self._conversations.pop(request.task_run_id, None)
                conversation.close()

    async def cancel(self, task_run_id: UUID) -> None:
        with self._lock:
            conversation = self._conversations.get(task_run_id)
        if conversation is not None:
            await asyncio.to_thread(conversation.interrupt)


@dataclass
class _TaskRunRecord:
    request: CreateTaskRunRequest
    status: TaskRunStatus = TaskRunStatus.QUEUED
    created_at: datetime = field(default_factory=lambda: datetime.now(UTC))
    started_at: datetime | None = None
    completed_at: datetime | None = None
    final_output: str | None = None
    error: TaskRunError | None = None
    cancellation: asyncio.Event = field(default_factory=asyncio.Event)
    background_task: asyncio.Task[None] | None = None

    def response(self) -> TaskRunResponse:
        return TaskRunResponse(
            task_run_id=self.request.task_run_id,
            state=self.status,
            created_at=self.created_at,
            started_at=self.started_at,
            completed_at=self.completed_at,
            final_output=self.final_output,
            error=self.error,
        )


class TaskRunManager:
    """In-memory scheduler for a one-task benchmark sidecar."""

    def __init__(self, executor: TaskExecutor) -> None:
        self._executor = executor
        self._records: dict[UUID, _TaskRunRecord] = {}
        self._lock = asyncio.Lock()

    @property
    def executor_name(self) -> str:
        return self._executor.name

    async def create(self, request: CreateTaskRunRequest) -> TaskRunResponse:
        async with self._lock:
            if request.task_run_id in self._records:
                raise TaskExecutionError("task_run_already_exists")
            record = _TaskRunRecord(request=request)
            self._records[request.task_run_id] = record
            record.background_task = asyncio.create_task(self._execute(record))
            return record.response()

    async def get(self, task_run_id: UUID) -> TaskRunResponse | None:
        async with self._lock:
            record = self._records.get(task_run_id)
            return record.response() if record else None

    async def cancel(self, task_run_id: UUID) -> TaskRunResponse | None:
        async with self._lock:
            record = self._records.get(task_run_id)
            if record is None:
                return None
            if record.status.terminal:
                return record.response()
            record.cancellation.set()
            record.status = TaskRunStatus.CANCELLED
            record.completed_at = datetime.now(UTC)
            record.error = TaskRunError(code="cancelled")

        try:
            await self._executor.cancel(task_run_id)
        except Exception:
            # Cancellation must be best effort and must never expose executor
            # exception text through logs or the public response.
            logger.debug("Task runner executor cancellation failed")
        return record.response()

    async def _execute(self, record: _TaskRunRecord) -> None:
        async with self._lock:
            if record.cancellation.is_set():
                return
            record.status = TaskRunStatus.RUNNING
            record.started_at = datetime.now(UTC)

        try:
            result = await asyncio.wait_for(
                self._executor.execute(record.request, record.cancellation),
                timeout=record.request.task.timeout_seconds,
            )
        except TimeoutError:
            record.cancellation.set()
            try:
                await self._executor.cancel(record.request.task_run_id)
            except Exception:
                # The executor can be concurrently tearing down; the timeout
                # verdict remains authoritative and must not expose errors.
                logger.debug("Task runner executor timeout cancellation failed")
            await self._set_terminal(
                record, TaskRunStatus.TIMED_OUT, error_code="timeout"
            )
        except TaskExecutionError as exc:
            terminal_status = (
                TaskRunStatus.CANCELLED
                if exc.code == "cancelled" or record.cancellation.is_set()
                else TaskRunStatus.FAILED
            )
            await self._set_terminal(record, terminal_status, error_code=exc.code)
        except Exception as exc:
            # Do not log or serialize exception text: provider failures and tool
            # errors may contain sensitive request or environment information.
            # The public code is derived only from a fixed exception type/module
            # allowlist, never from task/provider content.
            await self._set_terminal(
                record,
                TaskRunStatus.FAILED,
                error_code=_safe_unexpected_executor_error_code(exc),
            )
        else:
            await self._set_terminal(
                record,
                TaskRunStatus.SUCCEEDED,
                final_output=result.final_output,
            )

    async def _set_terminal(
        self,
        record: _TaskRunRecord,
        task_status: TaskRunStatus,
        *,
        final_output: str | None = None,
        error_code: str | None = None,
    ) -> None:
        async with self._lock:
            # A cancel request wins over a late executor success/failure.
            if record.status == TaskRunStatus.CANCELLED:
                return
            record.status = task_status
            record.completed_at = datetime.now(UTC)
            record.final_output = final_output
            record.error = TaskRunError(code=error_code) if error_code else None


def build_task_executor_from_env(env: Mapping[str, str] | None = None) -> TaskExecutor:
    """Choose the executor without ever serializing environment values."""
    values = os.environ if env is None else env
    selected = values.get(_TASK_RUNNER_EXECUTOR_ENV, "native").strip().lower()
    if selected in {"", "native"}:
        return OpenHandsConversationExecutor(env=values)
    if selected == "fake":
        return FakeTaskExecutor()
    raise ValueError(
        f"Unsupported {_TASK_RUNNER_EXECUTOR_ENV} mode; expected native or fake"
    )


def create_task_runner_app(executor: TaskExecutor | None = None) -> FastAPI:
    """Create the private benchmark Task Runner HTTP v1 application."""
    resolved_executor = executor or build_task_executor_from_env()
    app = FastAPI(
        title="OpenHands Benchmark Task Runner",
        version=TASK_RUNNER_PROTOCOL_VERSION,
        docs_url=None,
        redoc_url=None,
        openapi_url=None,
    )
    manager = TaskRunManager(resolved_executor)
    app.state.task_run_manager = manager

    @app.exception_handler(RequestValidationError)
    async def invalid_task_run_request(
        _request: Any, _exc: RequestValidationError
    ) -> JSONResponse:
        # FastAPI's default response serializes rejected input values.  Task
        # Runner requests must never echo an accidental credential back to a
        # caller, log collector, or benchmark artifact.
        return JSONResponse(
            status_code=status.HTTP_422_UNPROCESSABLE_CONTENT,
            content={"detail": "invalid_task_run_request"},
        )

    @app.get("/healthz", response_model=HealthResponse)
    async def healthz() -> HealthResponse:
        return HealthResponse(executor=manager.executor_name)

    @app.post(
        "/v1/task-runs",
        status_code=status.HTTP_202_ACCEPTED,
        response_model=TaskRunResponse,
    )
    async def create_task_run(request: CreateTaskRunRequest) -> TaskRunResponse:
        try:
            return await manager.create(request)
        except TaskExecutionError as exc:
            if exc.code == "task_run_already_exists":
                raise HTTPException(
                    status_code=status.HTTP_409_CONFLICT, detail=exc.code
                )
            raise HTTPException(
                status_code=status.HTTP_400_BAD_REQUEST, detail=exc.code
            )

    @app.get("/v1/task-runs/{task_run_id}", response_model=TaskRunResponse)
    async def get_task_run(task_run_id: UUID) -> TaskRunResponse:
        task_run = await manager.get(task_run_id)
        if task_run is None:
            raise HTTPException(
                status_code=status.HTTP_404_NOT_FOUND, detail="not_found"
            )
        return task_run

    @app.post("/v1/task-runs/{task_run_id}/cancel", response_model=TaskRunResponse)
    async def cancel_task_run(task_run_id: UUID) -> TaskRunResponse:
        task_run = await manager.cancel(task_run_id)
        if task_run is None:
            raise HTTPException(
                status_code=status.HTTP_404_NOT_FOUND, detail="not_found"
            )
        return task_run

    return app


def main() -> None:
    """Run the Task Runner as the benchmark sidecar entrypoint."""
    parser = argparse.ArgumentParser(description="OpenHands Benchmark Task Runner v1")
    parser.add_argument("--host", default="127.0.0.1")
    parser.add_argument("--port", type=int, default=8001)
    args = parser.parse_args()

    # Resolve the executor before binding the port so a configuration error is
    # obvious without exposing any environment values.
    app = create_task_runner_app()
    uvicorn.run(app, host=args.host, port=args.port, log_level="info")


if __name__ == "__main__":
    main()
