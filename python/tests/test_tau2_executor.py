"""Focused tests for the official τ²-bench executor."""

from __future__ import annotations

import json
import subprocess
from pathlib import Path

import pytest

from agentscope_sdk.client import ClaimedJob
from agentscope_sdk.executors import (
    BenchmarkContractError,
    BenchmarkExecutionRequest,
    BenchmarkHarnessError,
    BenchmarkVerifierError,
    get_executor_registry,
)
from agentscope_sdk.tau2 import Tau2Executor, build_tau2_command, parse_tau2_result


def _request(tmp_path: Path, **overrides: object) -> BenchmarkExecutionRequest:
    config: dict[str, object] = {
        "executable": ["uv", "run", "tau2"],
        "working_dir": str(tmp_path),
        "output_dir": str(tmp_path / "outputs"),
        "domain": "airline",
        "agent_llm": "openai/gpt-4.1",
        "user_llm": "openai/gpt-4.1-mini",
        "num_trials": 1,
        "num_tasks": 2,
        "seed": 73,
        "timeout_s": 120,
        "trial_index": 3,
        "agent_adapter": {"kind": "official", "name": "llm_agent"},
        "env": {"OPENAI_API_KEY": "secret-key", "OPENHANDS_MODE": "benchmark"},
    }
    config.update(overrides)
    return BenchmarkExecutionRequest(
        claimed_job=ClaimedJob(
            id="tau2/job with unsafe chars",
            experiment_id="experiment-1",
            dataset_case_id="case-1",
            case_input="ignored by tau2",
            case_metadata={"benchmark_task_id": "airline-task-1"},
            target_name="unused-official-agent",
            execution_kind="tau2",
            execution_config=config,
            attempts=2,
        ),
        target=None,
        context={},
    )


def _write_results(job_dir: Path, rewards: list[float]) -> None:
    job_dir.mkdir(parents=True, exist_ok=True)
    simulations = []
    for index, reward in enumerate(rewards):
        simulations.append(
            {
                "id": f"simulation-{index}",
                "task_id": f"task-{index}",
                "trial": 0,
                "seed": 73 + index,
                "duration": 1.5,
                "termination_reason": "agent_stop",
                "agent_cost": 0.02,
                "user_cost": 0.01,
                "reward_info": {"reward": reward},
            }
        )
    (job_dir / "results.json").write_text(
        json.dumps({"info": {"domain": "airline"}, "simulations": simulations}),
        encoding="utf-8",
    )
    (job_dir / "agent-transcript.jsonl").write_text("{}\n", encoding="utf-8")


def test_default_registry_registers_official_tau2_executor():
    assert isinstance(get_executor_registry().resolve("tau2"), Tau2Executor)


def test_build_tau2_command_uses_official_flags_safe_env_and_correlation_context(tmp_path: Path):
    command = build_tau2_command(_request(tmp_path))

    assert command.argv[:4] == ["uv", "run", "tau2", "run"]
    assert command.argv[command.argv.index("--domain") + 1] == "airline"
    assert command.argv[command.argv.index("--agent") + 1] == "llm_agent"
    assert command.argv[command.argv.index("--num-trials") + 1] == "1"
    assert command.argv[command.argv.index("--num-tasks") + 1] == "2"
    assert command.argv[command.argv.index("--seed") + 1] == "73"
    assert command.argv[command.argv.index("--save-to") + 1] == str(command.job_dir)
    assert command.job_dir.name == "agentscope-tau2-tau2-job-with-unsafe-chars-2-3"
    assert command.env["OPENAI_API_KEY"] == "secret-key"
    assert command.env["AGENTSCOPE_EXPERIMENT_ID"] == "experiment-1"
    assert command.env["AGENTSCOPE_EVAL_JOB_ID"] == "tau2/job with unsafe chars"
    assert command.env["AGENTSCOPE_EVAL_TRIAL_ID"] == "tau2/job with unsafe chars:3"
    assert command.env["AGENTSCOPE_BENCHMARK_TASK_ID"] == "airline-task-1"
    assert command.env["AGENTSCOPE_TRIAL_INDEX"] == "3"


def test_build_tau2_command_supports_explicit_openhands_or_openclaw_adapter_bootstrap(tmp_path: Path):
    request = _request(
        tmp_path,
        agent_adapter={
            "kind": "command",
            "name": "openhands",
            "command": ["python", "/opt/agents/openhands_tau2_adapter.py"],
        },
    )
    command = build_tau2_command(request)

    assert command.argv[:3] == ["python", "/opt/agents/openhands_tau2_adapter.py", "run"]
    assert command.argv[command.argv.index("--agent") + 1] == "openhands"

    openclaw = build_tau2_command(
        _request(
            tmp_path,
            agent_adapter={
                "kind": "command",
                "name": "openclaw",
                "command": ["python", "/opt/agents/openclaw_tau2_adapter.py"],
            },
        )
    )
    assert openclaw.argv[openclaw.argv.index("--agent") + 1] == "openclaw"


def test_build_tau2_command_rejects_unknown_configuration_and_unallowlisted_env(tmp_path: Path):
    with pytest.raises(BenchmarkContractError, match="unsupported fields"):
        build_tau2_command(_request(tmp_path, unexpected=True))
    with pytest.raises(BenchmarkContractError, match="not allowlisted"):
        build_tau2_command(_request(tmp_path, env={"DATABASE_URL": "postgres://secret"}))


def test_build_tau2_command_uses_runtime_credentials_without_persisting_them(tmp_path: Path, monkeypatch):
    monkeypatch.setenv("OPENAI_API_KEY", "runtime-secret")
    monkeypatch.setenv("OPENAI_BASE_URL", "https://example.invalid/v1")
    monkeypatch.setenv("AGENTSCOPE_PROJECT_TOKEN", "ascope_runtime_token")
    monkeypatch.setenv("AGENTSCOPE_SERVER_URL", "http://127.0.0.1:8080")

    command = build_tau2_command(_request(tmp_path, env={}))

    assert command.env["OPENAI_API_KEY"] == "runtime-secret"
    assert command.env["OPENAI_BASE_URL"] == "https://example.invalid/v1"
    assert command.env["AGENTSCOPE_PROJECT_TOKEN"] == "ascope_runtime_token"
    assert command.env["AGENTSCOPE_SERVER_URL"] == "http://127.0.0.1:8080"


def test_parse_tau2_result_marks_completed_agent_failure_valid_and_collects_artifacts(tmp_path: Path):
    job_dir = tmp_path / "result"
    _write_results(job_dir, [1.0, 0.0])

    result = parse_tau2_result(job_dir, _request(tmp_path).execution_config)

    assert result.valid is True
    assert result.official_score == 0.5
    assert result.official_passed is False
    assert result.official_metrics["n_simulations"] == 2
    assert "1/2 passed" in result.official_reason
    assert {artifact.kind for artifact in result.artifacts} >= {"tau2_results", "tau2_transcript"}


def test_parse_tau2_result_rejects_missing_or_malformed_official_output(tmp_path: Path):
    with pytest.raises(BenchmarkVerifierError, match=r"did not write results\.json"):
        parse_tau2_result(tmp_path / "missing", _request(tmp_path).execution_config)

    job_dir = tmp_path / "malformed"
    job_dir.mkdir()
    (job_dir / "results.json").write_text("not json", encoding="utf-8")
    with pytest.raises(BenchmarkVerifierError, match="malformed"):
        parse_tau2_result(job_dir, _request(tmp_path).execution_config)


def test_tau2_executor_runs_without_shell_and_parses_official_success(tmp_path: Path):
    request = _request(tmp_path)

    def successful_run(argv, **kwargs):
        assert kwargs["shell"] is False
        job_dir = Path(argv[argv.index("--save-to") + 1])
        _write_results(job_dir, [1.0, 1.0])
        return subprocess.CompletedProcess(argv, 0, stdout="official output", stderr="")

    result = Tau2Executor(subprocess_run=successful_run).execute(request)

    assert result.official_score == 1.0
    assert result.official_passed is True
    assert any(artifact.metadata["relative_path"] == "tau2.stdout.log" for artifact in result.artifacts)


def test_tau2_executor_classifies_timeout_and_cli_failure(tmp_path: Path):
    request = _request(tmp_path)

    def timeout_run(argv, **kwargs):
        raise subprocess.TimeoutExpired(argv, kwargs["timeout"])

    with pytest.raises(BenchmarkHarnessError, match="timed out"):
        Tau2Executor(subprocess_run=timeout_run).execute(request)

    def failed_run(argv, **kwargs):
        return subprocess.CompletedProcess(argv, 9, stdout="", stderr="agent bootstrap failed")

    with pytest.raises(BenchmarkHarnessError, match="non-zero") as exc_info:
        Tau2Executor(subprocess_run=failed_run).execute(request)
    assert exc_info.value.details["returncode"] == 9
