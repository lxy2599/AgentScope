"""Harbor custom agent that drives one owned Task Runner v1 sidecar.

The module is imported by Harbor inside the long-running benchmark worker
Python environment.  It deliberately does not import OpenHands/OpenClaw nor
install anything at task time.  The Worker has already started a digest-pinned
agent container and supplies only its loopback HTTP endpoint as a per-job
constructor value.
"""

from __future__ import annotations

import asyncio
import os
import re
from pathlib import Path
from typing import Any
from uuid import UUID

from .contracts import HARBOR_TOOL_GATEWAY_PROTOCOL, Correlation, TaskRunState, TaskSpec
from .task_runner_client import TaskRunnerError, TaskRunnerHTTPAdapter
from .tool_gateway import HarborToolGateway, ToolGatewayError

try:  # Harbor is a runtime dependency of the Worker image, not the catalog library.
    from harbor.agents.base import BaseAgent
    from harbor.environments.base import BaseEnvironment
    from harbor.models.agent.context import AgentContext
except ImportError:  # pragma: no cover - makes catalog/unit-test imports dependency-free.
    BaseAgent = object  # type: ignore[assignment,misc]
    BaseEnvironment = Any  # type: ignore[misc,assignment]
    AgentContext = Any  # type: ignore[misc,assignment]


class TaskRunnerAgent(BaseAgent):  # type: ignore[misc]
    """Harbor ``BaseAgent`` implementation for the private Task Runner API."""

    SUPPORTS_WINDOWS = False

    def __init__(
        self,
        logs_dir: Path,
        model_name: str | None = None,
        *,
        task_runner_endpoint: str,
        task_run_id: str,
        task_id: str,
        timeout_seconds: int = 1800,
        experiment_id: str,
        eval_job_id: str,
        trial_index: int,
        **kwargs: Any,
    ) -> None:
        super().__init__(logs_dir=logs_dir, model_name=model_name, **kwargs)
        if not _is_task_runner_endpoint(task_runner_endpoint):
            raise ValueError("task_runner_endpoint must be a worker-local or managed sidecar URL")
        self._task_runner_endpoint = task_runner_endpoint
        self._task_run_id = UUID(task_run_id)
        self._task_id = _nonempty(task_id, "task_id")
        if isinstance(timeout_seconds, bool) or not isinstance(timeout_seconds, int) or timeout_seconds <= 0:
            raise ValueError("timeout_seconds must be positive")
        self._timeout_seconds = timeout_seconds
        self._correlation = Correlation(
            experiment_id=_nonempty(experiment_id, "experiment_id"),
            eval_job_id=_nonempty(eval_job_id, "eval_job_id"),
            trial_index=_nonnegative_int(trial_index, "trial_index"),
        )

    @staticmethod
    def name() -> str:
        return "agentlab-task-runner"

    def version(self) -> str:
        return "v1"

    async def setup(self, environment: BaseEnvironment) -> None:
        del environment
        adapter = TaskRunnerHTTPAdapter(self._task_runner_endpoint)
        if not await asyncio.to_thread(adapter.healthcheck):
            raise RuntimeError("Task Runner sidecar health check failed")

    async def run(self, instruction: str, environment: BaseEnvironment, context: AgentContext) -> None:
        capability = os.environ.get("AGENTLAB_TOOL_GATEWAY_TOKEN", "")
        try:
            async with HarborToolGateway(
                environment,
                capability_token=capability,
                max_timeout_seconds=self._timeout_seconds,
            ) as gateway:
                adapter = TaskRunnerHTTPAdapter(self._task_runner_endpoint)
                task = TaskSpec(
                    task_run_id=str(self._task_run_id),
                    task_id=self._task_id,
                    instruction=instruction,
                    # The sidecar only has an inert, per-job /workspace mount.
                    # Terminal/file effects must cross this gateway into Harbor's
                    # current sandbox, never into the worker workspace.
                    workspace="/workspace",
                    timeout_seconds=self._timeout_seconds,
                    tool_gateway_endpoint=gateway.endpoint.endpoint,
                    tool_gateway_protocol=HARBOR_TOOL_GATEWAY_PROTOCOL,
                    agent_profile_id="harbor",
                    model_profile_id="harbor",
                    correlation=self._correlation,
                )
                run = await asyncio.to_thread(adapter.start_task, task)
                while not run.state.terminal:
                    await asyncio.sleep(0.5)
                    run = await asyncio.to_thread(adapter.get_task, task.task_run_id)
        except (TaskRunnerError, ToolGatewayError) as exc:
            raise RuntimeError("Task Runner sidecar request failed") from exc
        safe_error_code = _safe_task_runner_error_code(run.error_code)
        context.metadata = {"task_runner_state": run.state.value, **({"task_runner_error_code": safe_error_code} if safe_error_code else {})}
        if run.state is not TaskRunState.SUCCEEDED:
            suffix = f" ({safe_error_code})" if safe_error_code else ""
            raise RuntimeError(f"Task Runner sidecar did not complete successfully{suffix}")


def _safe_task_runner_error_code(value: object) -> str:
    """Keep only an allowlisted-shaped sidecar code in Harbor diagnostics."""

    if isinstance(value, str) and re.fullmatch(r"[a-z][a-z0-9_]{0,63}", value):
        return value
    return ""


def _nonempty(value: str, name: str) -> str:
    if not isinstance(value, str) or not value.strip() or "\x00" in value:
        raise ValueError(f"{name} must be a non-empty string")
    return value.strip()


def _nonnegative_int(value: int, name: str) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or value < 0:
        raise ValueError(f"{name} must be a non-negative integer")
    return value


def _is_task_runner_endpoint(value: object) -> bool:
    if not isinstance(value, str) or not value.startswith("http://"):
        return False
    # Docker-network mode uses a per-trial managed sidecar DNS name. It is
    # generated by DockerSidecarLauncher, not catalog data, and is reachable
    # only from the trusted Worker network. Host mode remains loopback/gateway.
    return bool(
        re.fullmatch(
            r"http://(?:127\.0\.0\.1|host\.docker\.internal|agentlab-sidecar-[a-f0-9]{24}):[1-9][0-9]{0,4}",
            value,
        )
    )
