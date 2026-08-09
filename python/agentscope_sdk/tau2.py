"""Official τ²-bench executor for AgentScope benchmark jobs.

The executor invokes the τ² CLI (or a user-provided bootstrap which invokes
the official τ² Python entrypoint) and only normalizes the benchmark's
official output. τ² continues to own the customer-service domains, user
simulator, environment, and verifier.
"""

from __future__ import annotations

import json
import os
import re
import subprocess
from collections.abc import Callable, Iterable, Mapping
from dataclasses import dataclass
from pathlib import Path
from statistics import fmean
from typing import Any

from agentscope_sdk.executors import (
    BenchmarkArtifact,
    BenchmarkContractError,
    BenchmarkExecutionRequest,
    BenchmarkExecutionResult,
    BenchmarkHarnessError,
    BenchmarkVerifierError,
)

_SAFE_JOB_NAME_PATTERN = re.compile(r"[^A-Za-z0-9_.-]+")
_ENV_KEY_PATTERN = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")
_AGENT_NAME_PATTERN = re.compile(r"^[A-Za-z][A-Za-z0-9_.-]*$")
_MAX_ARTIFACTS = 250
_MAX_ENV_VALUE_LENGTH = 16_384
_SAFE_CLI_ENV_KEYS = (
    "PATH",
    "HOME",
    "LANG",
    "LC_ALL",
    "LC_CTYPE",
    "SSL_CERT_FILE",
    "SSL_CERT_DIR",
    "XDG_CACHE_HOME",
    "XDG_CONFIG_HOME",
)
_ALLOWED_ENV_KEYS = frozenset(
    {
        "ANTHROPIC_API_KEY",
        "ANTHROPIC_BASE_URL",
        "AZURE_OPENAI_API_KEY",
        "AZURE_OPENAI_API_VERSION",
        "AZURE_OPENAI_ENDPOINT",
        "GEMINI_API_KEY",
        "GOOGLE_API_KEY",
        "OPENAI_API_KEY",
        "OPENAI_BASE_URL",
        "OPENROUTER_API_KEY",
        "OPENROUTER_BASE_URL",
    }
)
_ALLOWED_ENV_PREFIXES = ("LITELLM_", "OPENHANDS_", "OPENCLAW_", "TAU2_")
_RUNTIME_ENV_KEYS = frozenset(
    {
        "AGENTSCOPE_PROJECT_TOKEN",
        "AGENTSCOPE_SERVER_URL",
        "OPENHANDS_AGENTSCOPE_CAPTURE_LEVEL",
        "OPENHANDS_AGENTSCOPE_ENABLED",
        "OPENAI_API_KEY",
        "OPENAI_BASE_URL",
    }
)
_ALLOWED_CONFIG_KEYS = frozenset(
    {
        "executable",
        "working_dir",
        "domain",
        "agent_llm",
        "user_llm",
        "num_trials",
        "num_tasks",
        "seed",
        "output_dir",
        "timeout_s",
        "env",
        "agent_adapter",
        "pass_threshold",
        "max_artifacts",
        "trial_index",
    }
)


@dataclass(frozen=True)
class Tau2Command:
    """Validated official τ² invocation details for a claimed job."""

    argv: list[str]
    cwd: Path
    env: dict[str, str]
    job_dir: Path
    timeout_s: float


@dataclass(frozen=True)
class Tau2AgentAdapter:
    """The τ² registry name and command that supplies an agent implementation.

    ``kind='official'`` executes the configured τ² CLI directly. For
    ``kind='command'`` and ``kind='path'``, the supplied bootstrap must
    register ``name`` with τ² and invoke its official Python entrypoint with
    the appended ``run`` arguments. This executor deliberately does not ship
    or infer an OpenHands/OpenClaw τ² agent implementation.
    """

    name: str
    command_prefix: list[str]


def _require_string(config: Mapping[str, Any], key: str) -> str:
    value = config.get(key)
    if not isinstance(value, str) or not value.strip():
        raise BenchmarkContractError(
            f"τ² execution_config.{key} must be a non-empty string",
            details={"field": key},
        )
    return value.strip()


def _positive_int(config: Mapping[str, Any], key: str, default: int | None = None) -> int:
    value = config.get(key, default)
    if isinstance(value, bool) or not isinstance(value, int) or value <= 0:
        raise BenchmarkContractError(
            f"τ² execution_config.{key} must be a positive integer",
            details={"field": key},
        )
    return value


def _nonnegative_int(config: Mapping[str, Any], key: str, default: int = 0) -> int:
    value = config.get(key, default)
    if isinstance(value, bool) or not isinstance(value, int) or value < 0:
        raise BenchmarkContractError(
            f"τ² execution_config.{key} must be a non-negative integer",
            details={"field": key},
        )
    return value


def _positive_number(config: Mapping[str, Any], key: str, default: float) -> float:
    value = config.get(key, default)
    if isinstance(value, bool) or not isinstance(value, (int, float)) or value <= 0:
        raise BenchmarkContractError(
            f"τ² execution_config.{key} must be a positive number",
            details={"field": key},
        )
    return float(value)


def _command_prefix(value: Any, field: str) -> list[str]:
    if isinstance(value, str) and value.strip():
        return [value.strip()]
    if isinstance(value, list) and value and all(isinstance(item, str) and item for item in value):
        return list(value)
    raise BenchmarkContractError(
        f"τ² execution_config.{field} must be a non-empty string or argv list",
        details={"field": field},
    )


def _validate_config_keys(config: Mapping[str, Any]) -> None:
    unknown = sorted(set(config) - _ALLOWED_CONFIG_KEYS)
    if unknown:
        raise BenchmarkContractError(
            "τ² execution_config contains unsupported fields",
            details={"unsupported_fields": unknown},
        )


def _adapter(config: Mapping[str, Any], executable: list[str]) -> Tau2AgentAdapter:
    value = config.get("agent_adapter")
    if not isinstance(value, Mapping):
        raise BenchmarkContractError(
            "τ² execution_config.agent_adapter must be an object",
            details={"field": "agent_adapter"},
        )
    kind = value.get("kind")
    name = value.get("name")
    if not isinstance(kind, str) or kind not in {"official", "command", "path"}:
        raise BenchmarkContractError(
            "τ² execution_config.agent_adapter.kind must be official, command, or path",
            details={"field": "agent_adapter.kind"},
        )
    if not isinstance(name, str) or not _AGENT_NAME_PATTERN.fullmatch(name):
        raise BenchmarkContractError(
            "τ² execution_config.agent_adapter.name must be a valid τ² agent name",
            details={"field": "agent_adapter.name"},
        )

    allowed = {
        "official": {"kind", "name"},
        "command": {"kind", "name", "command"},
        "path": {"kind", "name", "path", "python_executable"},
    }[kind]
    unknown = sorted(set(value) - allowed)
    if unknown:
        raise BenchmarkContractError(
            "τ² execution_config.agent_adapter contains unsupported fields",
            details={"unsupported_fields": unknown},
        )
    if kind == "official":
        return Tau2AgentAdapter(name=name, command_prefix=executable)
    if kind == "command":
        return Tau2AgentAdapter(
            name=name,
            command_prefix=_command_prefix(value.get("command"), "agent_adapter.command"),
        )

    path_value = value.get("path")
    if not isinstance(path_value, str) or not path_value.strip():
        raise BenchmarkContractError(
            "τ² execution_config.agent_adapter.path must be a non-empty path",
            details={"field": "agent_adapter.path"},
        )
    adapter_path = Path(path_value).expanduser().resolve()
    if not adapter_path.is_file():
        raise BenchmarkContractError(
            "τ² execution_config.agent_adapter.path must reference an existing file",
            details={"field": "agent_adapter.path", "path": str(adapter_path)},
        )
    python_value = value.get("python_executable")
    prefix = (
        _command_prefix(python_value, "agent_adapter.python_executable") if python_value is not None else ["python"]
    )
    return Tau2AgentAdapter(name=name, command_prefix=[*prefix, str(adapter_path)])


def _is_allowed_env_key(key: str) -> bool:
    return key in _ALLOWED_ENV_KEYS or key.startswith(_ALLOWED_ENV_PREFIXES)


def _extra_env(config: Mapping[str, Any]) -> dict[str, str]:
    value = config.get("env", {})
    if value is None:
        return {}
    if not isinstance(value, Mapping):
        raise BenchmarkContractError(
            "τ² execution_config.env must be an object",
            details={"field": "env"},
        )
    result: dict[str, str] = {}
    for key, raw_value in value.items():
        if not isinstance(key, str) or not _ENV_KEY_PATTERN.fullmatch(key):
            raise BenchmarkContractError(
                "τ² execution_config.env contains an invalid environment variable name",
                details={"field": "env"},
            )
        if not _is_allowed_env_key(key):
            raise BenchmarkContractError(
                f"τ² execution_config.env key '{key}' is not allowlisted",
                details={"field": "env", "key": key},
            )
        if not isinstance(raw_value, str) or not raw_value or len(raw_value) > _MAX_ENV_VALUE_LENGTH:
            raise BenchmarkContractError(
                f"τ² execution_config.env.{key} must be a non-empty string within the size limit",
                details={"field": f"env.{key}"},
            )
        result[key] = raw_value
    return result


def _runtime_env() -> dict[str, str]:
    """Read runner-process secrets without persisting them in execution config."""
    result: dict[str, str] = {}
    for key in _RUNTIME_ENV_KEYS:
        value = os.environ.get(key)
        if not value:
            continue
        if "\x00" in value or "\n" in value or "\r" in value or len(value) > _MAX_ENV_VALUE_LENGTH:
            raise BenchmarkContractError(
                f"τ² runtime environment value for '{key}' must be a safe string",
                details={"field": "runtime_environment", "key": key},
            )
        result[key] = value
    return result


def _subprocess_environment() -> dict[str, str]:
    return {key: value for key in _SAFE_CLI_ENV_KEYS if isinstance((value := os.environ.get(key)), str) and value}


def _safe_job_component(value: str) -> str:
    normalized = _SAFE_JOB_NAME_PATTERN.sub("-", value).strip(".-")
    return normalized[:96] or "job"


def _benchmark_context(request: BenchmarkExecutionRequest, trial_index: int) -> dict[str, str]:
    task_id = request.claimed_job.dataset_case_id
    metadata_task_id = (request.claimed_job.case_metadata or {}).get("benchmark_task_id")
    if isinstance(metadata_task_id, str) and metadata_task_id.strip():
        task_id = metadata_task_id.strip()
    return {
        "AGENTSCOPE_EXPERIMENT_ID": request.claimed_job.experiment_id,
        "AGENTSCOPE_EVAL_JOB_ID": request.claimed_job.id,
        "AGENTSCOPE_EVAL_TRIAL_ID": f"{request.claimed_job.id}:{trial_index}",
        "AGENTSCOPE_BENCHMARK_KEY": "tau2",
        "AGENTSCOPE_BENCHMARK_TASK_ID": task_id,
        "AGENTSCOPE_TRIAL_INDEX": str(trial_index),
    }


def _redacted_argv(argv: Iterable[str]) -> list[str]:
    return ["<redacted>" if "key=" in item.lower() or "token=" in item.lower() else item for item in argv]


def build_tau2_command(request: BenchmarkExecutionRequest) -> Tau2Command:
    """Build a strictly validated, shell-free official τ² command."""
    config = request.execution_config
    if not isinstance(config, Mapping):
        raise BenchmarkContractError("τ² execution_config must be an object")
    _validate_config_keys(config)

    executable = _command_prefix(config.get("executable"), "executable")
    working_dir = Path(_require_string(config, "working_dir")).expanduser().resolve()
    if not working_dir.is_dir():
        raise BenchmarkContractError(
            "τ² execution_config.working_dir must reference an existing directory",
            details={"field": "working_dir", "path": str(working_dir)},
        )
    output_dir = Path(_require_string(config, "output_dir")).expanduser().resolve()
    output_dir.mkdir(parents=True, exist_ok=True)
    if not output_dir.is_dir():
        raise BenchmarkContractError(
            "τ² execution_config.output_dir must reference a directory",
            details={"field": "output_dir", "path": str(output_dir)},
        )

    domain = _require_string(config, "domain")
    agent_llm = _require_string(config, "agent_llm")
    user_llm = _require_string(config, "user_llm")
    num_trials = _positive_int(config, "num_trials")
    num_tasks = _positive_int(config, "num_tasks")
    seed = _nonnegative_int(config, "seed") if "seed" in config else None
    timeout_s = _positive_number(config, "timeout_s", 1_800.0)
    trial_index = _nonnegative_int(config, "trial_index")
    adapter = _adapter(config, executable)

    job_dir = output_dir / (
        f"agentscope-tau2-{_safe_job_component(request.claimed_job.id)}-{request.claimed_job.attempts}-{trial_index}"
    )
    job_dir.mkdir(parents=True, exist_ok=True)
    argv = [
        *adapter.command_prefix,
        "run",
        "--domain",
        domain,
        "--agent",
        adapter.name,
        "--agent-llm",
        agent_llm,
        "--user-llm",
        user_llm,
        "--num-trials",
        str(num_trials),
        "--num-tasks",
        str(num_tasks),
        "--max-concurrency",
        "1",
        "--save-to",
        str(job_dir),
    ]
    if seed is not None:
        argv.extend(["--seed", str(seed)])

    env = _subprocess_environment()
    env.update(_extra_env(config))
    env.update(_runtime_env())
    env.update(_benchmark_context(request, trial_index))
    return Tau2Command(argv=argv, cwd=working_dir, env=env, job_dir=job_dir, timeout_s=timeout_s)


def _write_process_logs(job_dir: Path, completed: subprocess.CompletedProcess[str]) -> None:
    job_dir.mkdir(parents=True, exist_ok=True)
    (job_dir / "tau2.stdout.log").write_text(completed.stdout or "", encoding="utf-8")
    (job_dir / "tau2.stderr.log").write_text(completed.stderr or "", encoding="utf-8")


def _read_results(path: Path) -> dict[str, Any]:
    if not path.is_file():
        raise BenchmarkVerifierError(
            "τ² completed but did not write results.json",
            details={"results_path": str(path)},
        )
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise BenchmarkVerifierError(
            "τ² results.json is missing or malformed",
            details={"results_path": str(path), "error": type(exc).__name__},
        ) from exc
    if not isinstance(value, dict):
        raise BenchmarkVerifierError(
            "τ² results.json must contain an object",
            details={"results_path": str(path)},
        )
    return value


def _numeric_reward(simulation: Mapping[str, Any], index: int) -> float:
    reward_info = simulation.get("reward_info")
    if not isinstance(reward_info, Mapping):
        raise BenchmarkVerifierError(
            "τ² simulation is missing official reward_info",
            details={"simulation_index": index},
        )
    reward = reward_info.get("reward")
    if isinstance(reward, bool) or not isinstance(reward, (int, float)):
        raise BenchmarkVerifierError(
            "τ² simulation reward must be numeric",
            details={"simulation_index": index},
        )
    return float(reward)


def _simulation_summaries(
    results: Mapping[str, Any],
    *,
    expected_count: int,
    pass_threshold: float,
) -> list[dict[str, Any]]:
    simulations = results.get("simulations")
    if not isinstance(simulations, list) or not simulations:
        raise BenchmarkVerifierError("τ² results.json contains no simulations")
    if len(simulations) != expected_count:
        raise BenchmarkVerifierError(
            "τ² results.json does not contain the requested number of simulations",
            details={"expected": expected_count, "actual": len(simulations)},
        )
    summaries: list[dict[str, Any]] = []
    for index, simulation in enumerate(simulations):
        if not isinstance(simulation, Mapping):
            raise BenchmarkVerifierError(
                "τ² results.json contains an invalid simulation entry",
                details={"simulation_index": index},
            )
        reward = _numeric_reward(simulation, index)
        summaries.append(
            {
                "id": str(simulation.get("id", "")),
                "task_id": str(simulation.get("task_id", "")),
                "trial": simulation.get("trial"),
                "seed": simulation.get("seed"),
                "reward": reward,
                "passed": reward >= pass_threshold,
                "termination_reason": str(simulation.get("termination_reason", "")),
                "duration": simulation.get("duration"),
                "agent_cost": simulation.get("agent_cost"),
                "user_cost": simulation.get("user_cost"),
            }
        )
    return summaries


def _artifact_kind(path: Path) -> str:
    if path.name == "results.json":
        return "tau2_results"
    if "transcript" in path.name.lower() or path.parent.name == "simulations":
        return "tau2_transcript"
    if path.name in {"tau2.stdout.log", "tau2.stderr.log"} or path.suffix in {".log", ".jsonl"}:
        return "tau2_log"
    return "tau2_artifact"


def _collect_artifacts(job_dir: Path, max_artifacts: int) -> list[BenchmarkArtifact]:
    root = job_dir.resolve()
    artifacts: list[BenchmarkArtifact] = []
    for path in sorted(job_dir.rglob("*")):
        if len(artifacts) >= max_artifacts or not path.is_file():
            continue
        try:
            resolved = path.resolve()
            relative = resolved.relative_to(root)
            size = resolved.stat().st_size
        except (OSError, ValueError):
            continue
        artifacts.append(
            BenchmarkArtifact(
                kind=_artifact_kind(path),
                uri=resolved.as_uri(),
                metadata={"relative_path": relative.as_posix(), "size_bytes": size},
            )
        )
    return artifacts


def parse_tau2_result(job_dir: Path, config: Mapping[str, Any]) -> BenchmarkExecutionResult:
    """Normalize τ²'s official ``results.json`` without recreating evaluation."""
    max_artifacts = _positive_int(config, "max_artifacts", _MAX_ARTIFACTS)
    if max_artifacts > _MAX_ARTIFACTS:
        raise BenchmarkContractError(
            f"τ² execution_config.max_artifacts cannot exceed {_MAX_ARTIFACTS}",
            details={"field": "max_artifacts", "max": _MAX_ARTIFACTS},
        )
    pass_threshold = _positive_number(config, "pass_threshold", 1.0)
    expected_count = _positive_int(config, "num_trials") * _positive_int(config, "num_tasks")
    results = _read_results(job_dir / "results.json")
    simulations = _simulation_summaries(
        results,
        expected_count=expected_count,
        pass_threshold=pass_threshold,
    )
    rewards = [simulation["reward"] for simulation in simulations]
    passed_count = sum(bool(simulation["passed"]) for simulation in simulations)
    official_score = fmean(rewards)
    official_passed = passed_count == len(simulations)
    reason = (
        f"τ² official evaluator completed {len(simulations)} simulation(s): {passed_count}/{len(simulations)} passed"
    )
    return BenchmarkExecutionResult(
        actual_output=json.dumps(
            {"job_dir": str(job_dir), "simulations": simulations},
            ensure_ascii=False,
        ),
        official_score=official_score,
        official_passed=official_passed,
        official_reason=reason,
        official_metrics={
            "harness": "tau2",
            "job_dir": str(job_dir),
            "domain": results.get("info", {}).get("domain") if isinstance(results.get("info"), Mapping) else None,
            "n_simulations": len(simulations),
            "pass_threshold": pass_threshold,
            "avg_reward": official_score,
            "simulations": simulations,
        },
        artifacts=_collect_artifacts(job_dir, max_artifacts),
    )


class Tau2Executor:
    """Run the official τ² CLI and consume its official verifier output."""

    requires_registered_target = False

    def __init__(
        self,
        *,
        subprocess_run: Callable[..., subprocess.CompletedProcess[str]] = subprocess.run,
    ) -> None:
        self._subprocess_run = subprocess_run

    def execute(self, request: BenchmarkExecutionRequest) -> BenchmarkExecutionResult:
        command = build_tau2_command(request)
        try:
            completed = self._subprocess_run(
                command.argv,
                cwd=str(command.cwd),
                env=command.env,
                capture_output=True,
                text=True,
                check=False,
                shell=False,
                timeout=command.timeout_s,
            )
        except subprocess.TimeoutExpired as exc:
            raise BenchmarkHarnessError(
                "τ² CLI timed out before producing an official result",
                details={"timeout_s": command.timeout_s, "command": _redacted_argv(command.argv)},
            ) from exc
        except OSError as exc:
            raise BenchmarkHarnessError(
                "Unable to start the τ² CLI",
                details={"error": type(exc).__name__, "command": _redacted_argv(command.argv)},
            ) from exc

        _write_process_logs(command.job_dir, completed)
        if completed.returncode != 0:
            raise BenchmarkHarnessError(
                "τ² CLI exited with a non-zero status",
                details={
                    "returncode": completed.returncode,
                    "command": _redacted_argv(command.argv),
                    "stdout_tail": (completed.stdout or "")[-4_000:],
                    "stderr_tail": (completed.stderr or "")[-4_000:],
                    "job_dir": str(command.job_dir),
                },
            )
        return parse_tau2_result(command.job_dir, request.execution_config)
