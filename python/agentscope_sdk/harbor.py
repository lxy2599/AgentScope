"""Official Harbor CLI executor for AgentScope benchmark jobs.

This module deliberately does not import Harbor as a Python library. Harbor
owns its task environments, agent lifecycle, and verifier; AgentScope invokes
the official CLI, passes constrained agent environment variables through
Harbor's ``--agent-env`` mechanism, and normalizes Harbor's on-disk results.
"""

from __future__ import annotations

import json
import math
import os
import re
import subprocess
from collections.abc import Callable, Iterable, Mapping, Sequence
from dataclasses import dataclass
from pathlib import Path
from statistics import fmean
from typing import Any

import yaml

from agentscope_sdk.executors import (
    BenchmarkArtifact,
    BenchmarkContractError,
    BenchmarkExecutionRequest,
    BenchmarkExecutionResult,
    BenchmarkHarnessError,
    BenchmarkTargetError,
    BenchmarkVerifierError,
)

_OFFICIAL_HARBOR_AGENTS = frozenset({"openhands", "openhands-sdk", "openclaw"})
_ENV_KEY_PATTERN = re.compile(r"^[A-Za-z_][A-Za-z0-9_]*$")
_SAFE_JOB_NAME_PATTERN = re.compile(r"[^A-Za-z0-9_.-]+")
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
    "DOCKER_HOST",
    "HTTP_PROXY",
    "HTTPS_PROXY",
    "ALL_PROXY",
    "NO_PROXY",
    "http_proxy",
    "https_proxy",
    "all_proxy",
    "no_proxy",
)
_ALLOWED_AGENT_ENV_KEYS = frozenset(
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
_ALLOWED_AGENT_ENV_PREFIXES = ("LITELLM_", "OPENHANDS_", "OPENCLAW_")
_RUNTIME_AGENT_ENV_KEYS = frozenset(
    {
        "AGENTSCOPE_PROJECT_TOKEN",
        "AGENTSCOPE_SERVER_URL",
        "OPENHANDS_AGENTSCOPE_CAPTURE_LEVEL",
        "OPENHANDS_AGENTSCOPE_ENABLED",
        "OPENAI_API_KEY",
        "OPENAI_BASE_URL",
    }
)


@dataclass(frozen=True)
class HarborCommand:
    """Validated invocation details for one official Harbor job."""

    argv: list[str]
    cwd: Path | None
    env: dict[str, str]
    job_dir: Path
    timeout_s: float


def _require_string(config: Mapping[str, Any], key: str) -> str:
    value = config.get(key)
    if not isinstance(value, str) or not value.strip():
        raise BenchmarkContractError(
            f"Harbor execution_config.{key} must be a non-empty string",
            details={"field": key},
        )
    return value.strip()


def _optional_string(config: Mapping[str, Any], key: str) -> str | None:
    value = config.get(key)
    if value is None:
        return None
    if not isinstance(value, str) or not value.strip():
        raise BenchmarkContractError(
            f"Harbor execution_config.{key} must be a non-empty string when set",
            details={"field": key},
        )
    return value.strip()


def _positive_number(config: Mapping[str, Any], key: str, default: float) -> float:
    value = config.get(key, default)
    if isinstance(value, bool) or not isinstance(value, (int, float)) or value <= 0:
        raise BenchmarkContractError(
            f"Harbor execution_config.{key} must be a positive number",
            details={"field": key},
        )
    return float(value)


def _nonnegative_number(config: Mapping[str, Any], key: str, default: float) -> float:
    value = config.get(key, default)
    if isinstance(value, bool) or not isinstance(value, (int, float)) or value < 0:
        raise BenchmarkContractError(
            f"Harbor execution_config.{key} must be a non-negative number",
            details={"field": key},
        )
    return float(value)


def _positive_int(config: Mapping[str, Any], key: str, default: int) -> int:
    value = config.get(key, default)
    if isinstance(value, bool) or not isinstance(value, int) or value <= 0:
        raise BenchmarkContractError(
            f"Harbor execution_config.{key} must be a positive integer",
            details={"field": key},
        )
    return value


def _command_prefix(config: Mapping[str, Any]) -> list[str]:
    command = config.get("command")
    executable = config.get("executable")
    if command is not None and executable is not None:
        raise BenchmarkContractError(
            "Harbor execution_config may specify only one of command or executable",
            details={"fields": ["command", "executable"]},
        )
    value = command if command is not None else executable
    if isinstance(value, str) and value.strip():
        return [value.strip()]
    if isinstance(value, list) and value and all(isinstance(item, str) and item for item in value):
        return list(value)
    harbor_workdir = os.environ.get("AGENTSCOPE_HARBOR_WORKDIR", "").strip()
    if harbor_workdir:
        return ["uv", "run", "--directory", harbor_workdir, "--frozen", "harbor"]
    return ["harbor"]


def _model(config: Mapping[str, Any]) -> str:
    configured = _optional_string(config, "model")
    if configured:
        return configured
    runtime_default = os.environ.get("AGENTSCOPE_BENCHMARK_MODEL", "").strip()
    if runtime_default:
        return runtime_default
    raise BenchmarkContractError(
        "Harbor execution_config.model is required when AGENTSCOPE_BENCHMARK_MODEL is not configured",
        details={"field": "model"},
    )


def _agent(request: BenchmarkExecutionRequest, config: Mapping[str, Any]) -> str:
    target_name = request.claimed_job.target_name.strip().lower()
    if target_name:
        if target_name not in _OFFICIAL_HARBOR_AGENTS:
            raise BenchmarkContractError(
                "Benchmark target must be one of the official supported Harbor integrations",
                details={"field": "target_name", "allowed": sorted(_OFFICIAL_HARBOR_AGENTS)},
            )
        return target_name

    configured = _require_string(config, "agent").lower()
    if configured not in _OFFICIAL_HARBOR_AGENTS:
        raise BenchmarkContractError(
            "Harbor agent must be one of the official supported integrations",
            details={"field": "agent", "allowed": sorted(_OFFICIAL_HARBOR_AGENTS)},
        )
    return configured


def _task_ids(request: BenchmarkExecutionRequest, config: Mapping[str, Any]) -> list[str]:
    value = config.get("task_ids")
    if value is None:
        case_task_id = request.claimed_job.case_input.strip()
        if not case_task_id:
            raise BenchmarkContractError(
                "Harbor requires execution_config.task_ids or a non-empty dataset case input task ID",
                details={"field": "task_ids"},
            )
        return [case_task_id]
    if not isinstance(value, list) or not value or any(not isinstance(item, str) or not item.strip() for item in value):
        raise BenchmarkContractError(
            "Harbor execution_config.task_ids must be a non-empty list of task names when set",
            details={"field": "task_ids"},
        )
    return [item.strip() for item in value]


def _is_allowed_agent_env_key(key: str) -> bool:
    return key in _ALLOWED_AGENT_ENV_KEYS or key.startswith(_ALLOWED_AGENT_ENV_PREFIXES)


def _extra_agent_env(config: Mapping[str, Any]) -> dict[str, str]:
    value = config.get("extra_env", {})
    if value is None:
        return {}
    if not isinstance(value, Mapping):
        raise BenchmarkContractError(
            "Harbor execution_config.extra_env must be an object of allowlisted variables",
            details={"field": "extra_env"},
        )

    result: dict[str, str] = {}
    for key, env_value in value.items():
        if not isinstance(key, str) or not _ENV_KEY_PATTERN.fullmatch(key):
            raise BenchmarkContractError(
                "Harbor extra_env contains an invalid environment variable name",
                details={"field": "extra_env"},
            )
        if not _is_allowed_agent_env_key(key):
            raise BenchmarkContractError(
                f"Harbor extra_env key '{key}' is not allowlisted",
                details={"field": "extra_env", "key": key},
            )
        if (
            not isinstance(env_value, str)
            or "\x00" in env_value
            or "\n" in env_value
            or "\r" in env_value
            or len(env_value) > _MAX_ENV_VALUE_LENGTH
        ):
            raise BenchmarkContractError(
                f"Harbor extra_env value for '{key}' must be a safe string",
                details={"field": "extra_env", "key": key},
            )
        result[key] = env_value
    return result


def _runtime_agent_env() -> dict[str, str]:
    """Read process-scoped credentials without serializing them into job config."""
    result: dict[str, str] = {}
    for key in _RUNTIME_AGENT_ENV_KEYS:
        value = os.environ.get(key)
        if not value:
            continue
        if "\x00" in value or "\n" in value or "\r" in value or len(value) > _MAX_ENV_VALUE_LENGTH:
            raise BenchmarkContractError(
                f"Harbor runtime environment value for '{key}' must be a safe string",
                details={"field": "runtime_environment", "key": key},
            )
        result[key] = value
    return result


def _safe_job_component(value: str) -> str:
    normalized = _SAFE_JOB_NAME_PATTERN.sub("-", value).strip(".-")
    return normalized[:96] or "job"


def _benchmark_context(request: BenchmarkExecutionRequest, task_ids: Sequence[str]) -> dict[str, str]:
    config = request.execution_config
    trial_index = config.get("trial_index", 0)
    if isinstance(trial_index, bool) or not isinstance(trial_index, int) or trial_index < 0:
        raise BenchmarkContractError(
            "Harbor execution_config.trial_index must be a non-negative integer",
            details={"field": "trial_index"},
        )
    task_id = task_ids[0] if task_ids else str(config.get("task_id", ""))
    benchmark_key = str(config.get("benchmark_key", "harbor")).strip() or "harbor"
    trial_id = str(config.get("trial_id", f"{request.claimed_job.id}:{trial_index}")).strip()
    return {
        "AGENTSCOPE_EXPERIMENT_ID": request.claimed_job.experiment_id,
        "AGENTSCOPE_EVAL_JOB_ID": request.claimed_job.id,
        "AGENTSCOPE_EVAL_TRIAL_ID": trial_id,
        "AGENTSCOPE_BENCHMARK_KEY": benchmark_key,
        "AGENTSCOPE_BENCHMARK_TASK_ID": task_id,
        "AGENTSCOPE_TRIAL_INDEX": str(trial_index),
    }


def _subprocess_environment() -> dict[str, str]:
    """Return the minimal parent environment needed to execute Harbor itself."""
    environment = {key: os.environ[key] for key in _SAFE_CLI_ENV_KEYS if key in os.environ}
    direct_hosts = (
        "hlqxxzsirfrgeqasvaps.supabase.co",
        "github.com",
        "api.github.com",
        "codeload.github.com",
        "raw.githubusercontent.com",
    )
    for key in ("NO_PROXY", "no_proxy"):
        if key in environment:
            missing_hosts = [host for host in direct_hosts if host not in environment[key]]
            if missing_hosts:
                environment[key] = f"{environment[key]},{','.join(missing_hosts)}"
    return environment


def _agent_env_argument(key: str, value: str) -> str:
    """Keep sensitive values out of Harbor's process argument vector."""
    if re.search(r"(KEY|SECRET|TOKEN|PASSWORD|CREDENTIAL|AUTH)", key, re.IGNORECASE):
        return f"${{{key}}}"
    return value


def _redacted_argv(argv: Iterable[str]) -> list[str]:
    redacted: list[str] = []
    redact_next = False
    for item in argv:
        if redact_next:
            key = item.split("=", 1)[0]
            redacted.append(f"{key}=***")
            redact_next = False
            continue
        redacted.append(item)
        if item in {"--ae", "--agent-env"}:
            redact_next = True
    return redacted


def _redact_output(value: str) -> str:
    """Avoid persisting any known agent environment value in failure details."""
    return re.sub(
        r"((?:API[_-]?KEY|TOKEN|SECRET|PASSWORD)[A-Za-z0-9_.-]*=)[^\s]+",
        r"\1***",
        value,
        flags=re.IGNORECASE,
    )


def build_harbor_command(request: BenchmarkExecutionRequest) -> HarborCommand:
    """Validate one job and build a shell-free official Harbor invocation."""
    config = request.execution_config
    if not isinstance(config, Mapping):
        raise BenchmarkContractError("Harbor execution_config must be an object")

    dataset = _require_string(config, "dataset")
    agent = _agent(request, config)
    model = _model(config)
    jobs_dir = Path(_require_string(config, "jobs_dir")).expanduser().resolve()
    working_dir_value = _optional_string(config, "working_dir")
    cwd = Path(working_dir_value).expanduser().resolve() if working_dir_value else None
    if cwd is not None and not cwd.is_dir():
        raise BenchmarkContractError(
            "Harbor execution_config.working_dir must exist and be a directory",
            details={"field": "working_dir", "path": str(cwd)},
        )

    timeout_s = _positive_number(config, "timeout_s", 3_600.0)
    concurrency = _positive_int(config, "concurrency", 1)
    task_ids = _task_ids(request, config)
    context_env = _benchmark_context(request, task_ids)
    extra_env = _extra_agent_env(config)
    agent_env = {**extra_env, **_runtime_agent_env(), **context_env}

    prefix = _safe_job_component(str(config.get("job_name", "agentscope-harbor")))
    job_name = f"{prefix}-{_safe_job_component(request.claimed_job.id)}-{request.claimed_job.attempts}"
    job_dir = jobs_dir / job_name
    argv = _command_prefix(config)
    argv.extend(
        [
            "run",
            "--job-name",
            job_name,
            "--jobs-dir",
            str(jobs_dir),
            "--dataset",
            dataset,
            "--agent",
            agent,
            "--model",
            model,
            "--n-concurrent",
            str(concurrency),
            "--quiet",
        ]
    )
    for task_id in task_ids:
        argv.extend(["--include-task-name", task_id])
    for key in sorted(agent_env):
        argv.extend(["--agent-env", f"{key}={_agent_env_argument(key, agent_env[key])}"])

    subprocess_env = _subprocess_environment()
    subprocess_env.update(agent_env)

    return HarborCommand(
        argv=argv,
        cwd=cwd,
        env=subprocess_env,
        job_dir=job_dir,
        timeout_s=timeout_s,
    )


def _load_document(path: Path) -> dict[str, Any]:
    try:
        content = path.read_text(encoding="utf-8")
        payload = json.loads(content) if path.suffix.lower() == ".json" else yaml.safe_load(content)
    except (OSError, json.JSONDecodeError, yaml.YAMLError) as exc:
        raise BenchmarkVerifierError(
            "Harbor produced a malformed result document",
            details={"path": str(path), "error": type(exc).__name__},
        ) from exc
    if not isinstance(payload, dict):
        raise BenchmarkVerifierError(
            "Harbor result document must contain an object",
            details={"path": str(path)},
        )
    return payload


def _result_path(directory: Path) -> Path | None:
    for name in ("result.json", "result.yaml", "result.yml"):
        candidate = directory / name
        if candidate.is_file():
            return candidate
    return None


def _load_trial_results(job_dir: Path, job_result: dict[str, Any]) -> list[dict[str, Any]]:
    results: list[dict[str, Any]] = []
    embedded = job_result.get("trial_results")
    if isinstance(embedded, list):
        results.extend(item for item in embedded if isinstance(item, dict))

    seen_ids = {str(item.get("id")) for item in results if item.get("id") is not None}
    seen_names = {str(item.get("trial_name")) for item in results if isinstance(item.get("trial_name"), str)}
    for child in sorted(job_dir.iterdir()):
        if not child.is_dir():
            continue
        path = _result_path(child)
        if path is None:
            continue
        trial = _load_document(path)
        trial_id = str(trial.get("id")) if trial.get("id") is not None else None
        trial_name = str(trial.get("trial_name")) if isinstance(trial.get("trial_name"), str) else None
        if (trial_id and trial_id in seen_ids) or (trial_name and trial_name in seen_names):
            continue
        results.append(trial)
        if trial_id:
            seen_ids.add(trial_id)
        if trial_name:
            seen_names.add(trial_name)
    if not results:
        raise BenchmarkVerifierError(
            "Harbor completed without any official trial result",
            details={"job_dir": str(job_dir)},
        )
    return results


def _numeric_rewards(trial: Mapping[str, Any]) -> dict[str, float]:
    verifier = trial.get("verifier_result")
    if not isinstance(verifier, Mapping):
        raise BenchmarkVerifierError(
            "Harbor trial is missing official verifier_result",
            details={"trial_name": trial.get("trial_name", "")},
        )
    rewards = verifier.get("rewards")
    if not isinstance(rewards, Mapping):
        raise BenchmarkVerifierError(
            "Harbor trial verifier did not produce rewards",
            details={"trial_name": trial.get("trial_name", "")},
        )
    numeric: dict[str, float] = {}
    for key, value in rewards.items():
        if isinstance(value, bool) or not isinstance(value, (int, float)):
            continue
        numeric_value = float(value)
        if not math.isfinite(numeric_value):
            raise BenchmarkVerifierError(
                "Harbor trial verifier reward is not finite",
                details={"trial_name": trial.get("trial_name", ""), "reward_key": str(key)},
            )
        numeric[str(key)] = numeric_value
    if not numeric:
        raise BenchmarkVerifierError(
            "Harbor trial verifier rewards contain no numeric score",
            details={"trial_name": trial.get("trial_name", "")},
        )
    return numeric


def _extract_trial_score(
    trial: Mapping[str, Any],
    *,
    score_key: str | None,
    pass_threshold: float,
) -> tuple[float, bool, dict[str, float], str]:
    exception = trial.get("exception_info")
    if isinstance(exception, Mapping):
        raise BenchmarkTargetError(
            "Harbor trial terminated with an agent or environment exception",
            details={
                "trial_name": str(trial.get("trial_name", "")),
                "exception_type": str(exception.get("exception_type", "")),
                "exception_message": str(exception.get("exception_message", "")),
            },
        )
    rewards = _numeric_rewards(trial)
    selected_key = score_key or ("reward" if "reward" in rewards else None)
    if selected_key is None and len(rewards) == 1:
        selected_key = next(iter(rewards))
    if selected_key is None or selected_key not in rewards:
        raise BenchmarkVerifierError(
            "Harbor trial has no unambiguous official score key",
            details={"trial_name": trial.get("trial_name", ""), "reward_keys": sorted(rewards)},
        )
    score = rewards[selected_key]
    return score, score >= pass_threshold, rewards, selected_key


def _artifact_kind(path: Path) -> str:
    if path.name.startswith("result."):
        return "harbor_result"
    if path.name.startswith("config."):
        return "harbor_config"
    if path.suffix.lower() in {".log", ".txt", ".jsonl"}:
        return "harbor_log"
    return "harbor_artifact"


def _collect_artifacts(job_dir: Path, max_artifacts: int) -> list[BenchmarkArtifact]:
    artifacts: list[BenchmarkArtifact] = []
    if not job_dir.exists():
        return artifacts
    root = job_dir.resolve()
    for path in sorted(job_dir.rglob("*")):
        if len(artifacts) >= max_artifacts or not path.is_file():
            continue
        try:
            resolved = path.resolve()
            relative_path = resolved.relative_to(root)
        except (OSError, ValueError):
            continue
        artifacts.append(
            BenchmarkArtifact(
                kind=_artifact_kind(path),
                uri=resolved.as_uri(),
                metadata={"relative_path": relative_path.as_posix(), "size_bytes": resolved.stat().st_size},
            )
        )
    return artifacts


def parse_harbor_result(job_dir: Path, config: Mapping[str, Any]) -> BenchmarkExecutionResult:
    """Normalize official Harbor output into an AgentScope benchmark result."""
    result_path = _result_path(job_dir)
    if result_path is None:
        raise BenchmarkVerifierError(
            "Harbor completed but did not write a job result document",
            details={"job_dir": str(job_dir)},
        )
    job_result = _load_document(result_path)
    score_key = _optional_string(config, "score_key")
    pass_threshold = _nonnegative_number(config, "pass_threshold", 1.0)
    max_artifacts = _positive_int(config, "max_artifacts", _MAX_ARTIFACTS)
    if max_artifacts > _MAX_ARTIFACTS:
        raise BenchmarkContractError(
            f"Harbor execution_config.max_artifacts cannot exceed {_MAX_ARTIFACTS}",
            details={"field": "max_artifacts", "max": _MAX_ARTIFACTS},
        )

    trial_summaries: list[dict[str, Any]] = []
    aggregate_rewards: dict[str, list[float]] = {}
    scores: list[float] = []
    verdicts: list[bool] = []
    selected_score_keys: set[str] = set()
    for trial in _load_trial_results(job_dir, job_result):
        score, passed, rewards, selected_key = _extract_trial_score(
            trial,
            score_key=score_key,
            pass_threshold=pass_threshold,
        )
        scores.append(score)
        verdicts.append(passed)
        selected_score_keys.add(selected_key)
        for key, value in rewards.items():
            aggregate_rewards.setdefault(key, []).append(value)
        trial_summaries.append(
            {
                "trial_name": str(trial.get("trial_name", "")),
                "task_name": str(trial.get("task_name", "")),
                "score": score,
                "passed": passed,
                "rewards": rewards,
            }
        )

    official_score = fmean(scores)
    official_passed = all(verdicts)
    metric_rewards = {key: fmean(values) for key, values in aggregate_rewards.items()}
    passed_count = sum(verdicts)
    reason = (
        f"Harbor official verifier completed {len(trial_summaries)} trial(s): "
        f"{passed_count}/{len(trial_summaries)} passed"
    )
    return BenchmarkExecutionResult(
        actual_output=json.dumps({"job_dir": str(job_dir), "trials": trial_summaries}, ensure_ascii=False),
        official_score=official_score,
        official_passed=official_passed,
        official_reason=reason,
        official_metrics={
            "harness": "harbor",
            "job_dir": str(job_dir),
            "n_trials": len(trial_summaries),
            "score_key": score_key or (next(iter(selected_score_keys)) if len(selected_score_keys) == 1 else "mixed"),
            "pass_threshold": pass_threshold,
            "rewards": metric_rewards,
            "trials": trial_summaries,
        },
        artifacts=_collect_artifacts(job_dir, max_artifacts),
    )


class HarborExecutor:
    """Run Harbor's official CLI and consume its official verifier output."""

    requires_registered_target = False

    def __init__(
        self,
        *,
        subprocess_run: Callable[..., subprocess.CompletedProcess[str]] = subprocess.run,
    ) -> None:
        self._subprocess_run = subprocess_run

    def execute(self, request: BenchmarkExecutionRequest) -> BenchmarkExecutionResult:
        command = build_harbor_command(request)
        command.job_dir.parent.mkdir(parents=True, exist_ok=True)
        try:
            completed = self._subprocess_run(
                command.argv,
                cwd=str(command.cwd) if command.cwd else None,
                env=command.env,
                capture_output=True,
                text=True,
                check=False,
                shell=False,
                timeout=command.timeout_s,
            )
        except subprocess.TimeoutExpired as exc:
            raise BenchmarkHarnessError(
                "Harbor CLI timed out before producing an official result",
                details={"timeout_s": command.timeout_s, "command": _redacted_argv(command.argv)},
            ) from exc
        except OSError as exc:
            raise BenchmarkHarnessError(
                "Unable to start the Harbor CLI",
                details={"error": type(exc).__name__, "command": _redacted_argv(command.argv)},
            ) from exc

        if completed.returncode != 0:
            raise BenchmarkHarnessError(
                "Harbor CLI exited with a non-zero status",
                details={
                    "returncode": completed.returncode,
                    "command": _redacted_argv(command.argv),
                    "stdout_tail": _redact_output((completed.stdout or "")[-4_000:]),
                    "stderr_tail": _redact_output((completed.stderr or "")[-4_000:]),
                },
            )
        return parse_harbor_result(command.job_dir, request.execution_config)
