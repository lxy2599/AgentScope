"""Optional AgentScope configuration for the controlled OpenHands fork."""

from __future__ import annotations

import os
from collections.abc import Mapping
from dataclasses import dataclass, field


_TRUE_VALUES = frozenset({"1", "true", "yes", "on"})
_CAPTURE_LEVELS = frozenset({"L0", "L1", "L2"})
_BENCHMARK_CONTEXT_MAX_VALUE_LENGTH = 512
_MAX_SAFE_TRIAL_INDEX = 2**53 - 1


@dataclass(frozen=True)
class BenchmarkContext:
    """Optional task-level benchmark correlation values for one agent process."""

    experiment_id: str = ""
    eval_job_id: str = ""
    eval_trial_id: str = ""
    benchmark_key: str = ""
    benchmark_task_id: str = ""
    trial_index: int | None = None

    @property
    def enabled(self) -> bool:
        return bool(self.metadata())

    def metadata(self) -> dict[str, str | int]:
        return {
            **({"experiment_id": self.experiment_id} if self.experiment_id else {}),
            **({"eval_job_id": self.eval_job_id} if self.eval_job_id else {}),
            **({"eval_trial_id": self.eval_trial_id} if self.eval_trial_id else {}),
            **({"benchmark_key": self.benchmark_key} if self.benchmark_key else {}),
            **(
                {"benchmark_task_id": self.benchmark_task_id}
                if self.benchmark_task_id
                else {}
            ),
            **(
                {"trial_index": self.trial_index}
                if self.trial_index is not None
                else {}
            ),
        }

    def span_attributes(self) -> dict[str, str | int]:
        return {
            **(
                {"agentscope.experiment.id": self.experiment_id}
                if self.experiment_id
                else {}
            ),
            **(
                {"agentscope.eval.job.id": self.eval_job_id}
                if self.eval_job_id
                else {}
            ),
            **(
                {"agentscope.eval.trial.id": self.eval_trial_id}
                if self.eval_trial_id
                else {}
            ),
            **(
                {"agentscope.benchmark.key": self.benchmark_key}
                if self.benchmark_key
                else {}
            ),
            **(
                {"agentscope.benchmark.task.id": self.benchmark_task_id}
                if self.benchmark_task_id
                else {}
            ),
            **(
                {"agentscope.trial.index": self.trial_index}
                if self.trial_index is not None
                else {}
            ),
        }

    def snapshot_extra(self) -> dict[str, dict[str, str | int]]:
        metadata = self.metadata()
        return {"benchmark": metadata} if metadata else {}


@dataclass(frozen=True)
class AgentScopeSettings:
    """Resolved settings; credentials are intentionally excluded from repr."""

    enabled: bool = False
    server_url: str = ""
    project_token: str = field(default="", repr=False)
    environment: str = "dev"
    capture_level: str = "L0"
    benchmark_context: BenchmarkContext = field(default_factory=BenchmarkContext)
    reason: str = "disabled"


def load_agentscope_settings(
    env: Mapping[str, str] | None = None,
) -> AgentScopeSettings:
    """Load opt-in settings without making invalid configuration fatal."""
    values = os.environ if env is None else env
    enabled = (
        str(values.get("OPENHANDS_AGENTSCOPE_ENABLED", "")).strip().lower()
        in _TRUE_VALUES
    )
    if not enabled:
        return AgentScopeSettings()

    server_url = str(values.get("AGENTSCOPE_SERVER_URL", "")).strip().rstrip("/")
    project_token = str(values.get("AGENTSCOPE_PROJECT_TOKEN", "")).strip()
    environment = str(values.get("AGENTSCOPE_ENVIRONMENT", "dev")).strip() or "dev"
    capture_level = (
        str(values.get("OPENHANDS_AGENTSCOPE_CAPTURE_LEVEL", "L0")).strip().upper()
    )
    if capture_level not in _CAPTURE_LEVELS:
        capture_level = "L0"
    if not server_url:
        return AgentScopeSettings(
            environment=environment,
            capture_level=capture_level,
            reason="missing_server_url",
        )
    if not project_token:
        return AgentScopeSettings(
            server_url=server_url,
            environment=environment,
            capture_level=capture_level,
            reason="missing_project_token",
        )
    benchmark_context = _load_benchmark_context(values)
    return AgentScopeSettings(
        enabled=True,
        server_url=server_url,
        project_token=project_token,
        environment=environment,
        capture_level=capture_level,
        benchmark_context=benchmark_context,
        reason="enabled",
    )


def _load_benchmark_context(values: Mapping[str, str]) -> BenchmarkContext:
    trial_index = _optional_environment_value(values, "AGENTSCOPE_TRIAL_INDEX")
    parsed_trial_index = _parse_trial_index(trial_index)
    return BenchmarkContext(
        experiment_id=_optional_environment_value(values, "AGENTSCOPE_EXPERIMENT_ID"),
        eval_job_id=_optional_environment_value(values, "AGENTSCOPE_EVAL_JOB_ID"),
        eval_trial_id=_optional_environment_value(values, "AGENTSCOPE_EVAL_TRIAL_ID"),
        benchmark_key=_optional_environment_value(values, "AGENTSCOPE_BENCHMARK_KEY"),
        benchmark_task_id=_optional_environment_value(
            values, "AGENTSCOPE_BENCHMARK_TASK_ID"
        ),
        trial_index=parsed_trial_index,
    )


def _optional_environment_value(values: Mapping[str, str], name: str) -> str:
    value = str(values.get(name, "")).strip()
    if not value or len(value) > _BENCHMARK_CONTEXT_MAX_VALUE_LENGTH:
        return ""
    return value


def _parse_trial_index(value: str) -> int | None:
    if not value or not value.isascii() or not value.isdigit():
        return None
    parsed = int(value)
    return parsed if parsed <= _MAX_SAFE_TRIAL_INDEX else None
