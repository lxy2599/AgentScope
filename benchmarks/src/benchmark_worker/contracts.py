from __future__ import annotations

from dataclasses import dataclass
from enum import StrEnum
from typing import Any, Mapping, Protocol


TASK_RUNNER_PROTOCOL = "v1"
HARBOR_TOOL_GATEWAY_PROTOCOL = "agentlab.harbor-tool-gateway.v1"


class TaskRunState(StrEnum):
    QUEUED = "queued"
    RUNNING = "running"
    SUCCEEDED = "succeeded"
    FAILED = "failed"
    CANCELLED = "cancelled"
    TIMED_OUT = "timed_out"

    @property
    def terminal(self) -> bool:
        return self in {self.SUCCEEDED, self.FAILED, self.CANCELLED, self.TIMED_OUT}


@dataclass(frozen=True)
class Correlation:
    experiment_id: str
    eval_job_id: str
    trial_index: int


@dataclass(frozen=True)
class TaskSpec:
    task_run_id: str
    task_id: str
    instruction: str
    workspace: str
    timeout_seconds: int
    tool_gateway_endpoint: str
    tool_gateway_protocol: str
    agent_profile_id: str
    model_profile_id: str
    correlation: Correlation
    case_metadata: Mapping[str, Any] | None = None
    execution_config: Mapping[str, Any] | None = None


@dataclass(frozen=True)
class TaskRun:
    task_run_id: str
    state: TaskRunState
    final_text: str | None = None
    error_code: str | None = None
    run_id: str | None = None
    metadata: Mapping[str, str] | None = None


class AgentAdapter(Protocol):
    """Framework-specific sidecar adapter used by the future Harbor executor."""

    def healthcheck(self) -> bool: ...

    def start_task(self, task: TaskSpec) -> TaskRun: ...

    def get_task(self, task_run_id: str) -> TaskRun: ...

    def cancel_task(self, task_run_id: str) -> TaskRun: ...


@dataclass(frozen=True)
class LeasedJob:
    eval_job_id: str
    task: TaskSpec


@dataclass(frozen=True)
class JobResult:
    eval_job_id: str
    task_run: TaskRun
    verifier_status: str
    score: float | None
    official_metrics: Mapping[str, Any] | None = None
    artifact_manifest: tuple[Mapping[str, Any], ...] = ()
    # Wall-clock duration from sidecar launch through official-verifier output.
    # It is an execution signal, never a score.
    latency_ms: int = 0


class WorkerBackend(Protocol):
    """Platform-facing queue interface. A later AgentScope client implements it."""

    def claim(self, worker_id: str) -> LeasedJob | None: ...

    def heartbeat(self, worker_id: str, eval_job_id: str) -> None: ...

    def submit_result(self, worker_id: str, result: JobResult) -> None: ...

    def fail(self, worker_id: str, eval_job_id: str, error_type: str) -> None: ...
