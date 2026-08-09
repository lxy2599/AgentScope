"""Focused tests for the official Harbor CLI executor."""

from __future__ import annotations

import json
import subprocess
from pathlib import Path

import pytest

from agentscope_sdk import harbor as harbor_module
from agentscope_sdk.client import ClaimedJob
from agentscope_sdk.executors import (
    BenchmarkContractError,
    BenchmarkExecutionRequest,
    BenchmarkHarnessError,
    BenchmarkVerifierError,
    get_executor_registry,
)
from agentscope_sdk.harbor import HarborExecutor, build_harbor_command, parse_harbor_result


def _request(tmp_path: Path, **overrides: object) -> BenchmarkExecutionRequest:
    config: dict[str, object] = {
        "executable": "harbor",
        "dataset": "terminal-bench@2.0",
        "agent": "openhands-sdk",
        "model": "openai/gpt-5",
        "task_ids": ["terminal-bench-task-1"],
        "jobs_dir": str(tmp_path / "jobs"),
        "concurrency": 1,
        "timeout_s": 120,
        "trial_index": 2,
        "extra_env": {"OPENAI_API_KEY": "secret-key", "OPENHANDS_VERSION": "1.2.3"},
    }
    config.update(overrides)
    return BenchmarkExecutionRequest(
        claimed_job=ClaimedJob(
            id="job/with unsafe chars",
            experiment_id="experiment-1",
            dataset_case_id="case-1",
            case_input="ignored by Harbor",
            target_name="openhands-sdk",
            execution_kind="harbor",
            execution_config=config,
            attempts=3,
        ),
        target=None,
        context={},
    )


def _write_result(job_dir: Path, *, reward: float, suffix: str = ".json") -> None:
    job_dir.mkdir(parents=True)
    payload = {
        "id": "00000000-0000-0000-0000-000000000001",
        "n_total_trials": 1,
        "stats": {},
        "trial_results": [
            {
                "id": "00000000-0000-0000-0000-000000000002",
                "task_name": "terminal-bench-task-1",
                "trial_name": "terminal-bench-task-1__openhands-sdk__0",
                "verifier_result": {"rewards": {"reward": reward, "secondary": reward / 2}},
            }
        ],
    }
    target = job_dir / f"result{suffix}"
    if suffix == ".json":
        target.write_text(json.dumps(payload), encoding="utf-8")
    else:
        target.write_text(
            "id: 00000000-0000-0000-0000-000000000001\n"
            "n_total_trials: 1\n"
            "stats: {}\n"
            "trial_results:\n"
            "  - task_name: terminal-bench-task-1\n"
            "    trial_name: terminal-bench-task-1__openhands-sdk__0\n"
            "    verifier_result:\n"
            "      rewards:\n"
            f"        reward: {reward}\n",
            encoding="utf-8",
        )
    (job_dir / "job.log").write_text("official Harbor log", encoding="utf-8")


def test_default_registry_registers_official_harbor_executor():
    assert isinstance(get_executor_registry().resolve("harbor"), HarborExecutor)


def test_build_harbor_command_uses_official_flags_and_allowlisted_agent_env(tmp_path: Path):
    request = _request(
        tmp_path,
        executable=None,
        command=["uv", "tool", "run", "harbor"],
    )
    command = build_harbor_command(request)

    assert command.argv[:5] == ["uv", "tool", "run", "harbor", "run"]
    assert command.argv[command.argv.index("--dataset") + 1] == "terminal-bench@2.0"
    assert command.argv[command.argv.index("--agent") + 1] == "openhands-sdk"
    assert command.argv[command.argv.index("--model") + 1] == "openai/gpt-5"
    assert command.argv[command.argv.index("--include-task-name") + 1] == "terminal-bench-task-1"
    assert command.argv[command.argv.index("--n-concurrent") + 1] == "1"
    assert command.job_dir.parent == (tmp_path / "jobs").resolve()
    assert command.job_dir.name.startswith("agentscope-harbor-job-with-unsafe-chars-3")

    env_flags = [command.argv[index + 1] for index, item in enumerate(command.argv) if item == "--agent-env"]
    assert "OPENAI_API_KEY=${OPENAI_API_KEY}" in env_flags
    assert "OPENHANDS_VERSION=1.2.3" in env_flags
    assert "AGENTSCOPE_EXPERIMENT_ID=experiment-1" in env_flags
    assert "AGENTSCOPE_EVAL_JOB_ID=job/with unsafe chars" in env_flags
    assert "AGENTSCOPE_EVAL_TRIAL_ID=job/with unsafe chars:2" in env_flags
    assert "AGENTSCOPE_BENCHMARK_TASK_ID=terminal-bench-task-1" in env_flags
    assert "AGENTSCOPE_TRIAL_INDEX=2" in env_flags
    assert command.env["OPENAI_API_KEY"] == "secret-key"


def test_build_harbor_command_rejects_nonofficial_target_and_unallowlisted_env(tmp_path: Path):
    request = _request(tmp_path)
    request.claimed_job.target_name = "custom-agent"
    with pytest.raises(BenchmarkContractError, match="official supported Harbor integrations"):
        build_harbor_command(request)

    with pytest.raises(BenchmarkContractError, match="not allowlisted"):
        build_harbor_command(_request(tmp_path, extra_env={"DATABASE_URL": "postgres://secret"}))


def test_build_harbor_command_uses_runner_defaults_for_command_and_model(tmp_path: Path, monkeypatch):
    monkeypatch.setenv("AGENTSCOPE_HARBOR_WORKDIR", "/opt/harbor")
    monkeypatch.setenv("AGENTSCOPE_BENCHMARK_MODEL", "ark-runtime-model")

    command = build_harbor_command(_request(tmp_path, executable=None, agent=None, model=None))

    assert command.argv[:7] == ["uv", "run", "--directory", "/opt/harbor", "--frozen", "harbor", "run"]
    assert command.argv[command.argv.index("--agent") + 1] == "openhands-sdk"
    assert command.argv[command.argv.index("--model") + 1] == "ark-runtime-model"


def test_build_harbor_command_uses_selected_openclaw_target(tmp_path: Path):
    request = _request(tmp_path, agent="openhands-sdk")
    request.claimed_job.target_name = "openclaw"

    command = build_harbor_command(request)

    assert command.argv[command.argv.index("--agent") + 1] == "openclaw"


def test_build_harbor_command_normalizes_openclaw_model_without_forcing_openhands_source(tmp_path: Path):
    openclaw = _request(tmp_path, model="deepseek-v4-flash")
    openclaw.claimed_job.target_name = "openclaw"

    openclaw_command = build_harbor_command(openclaw)
    openclaw_kwargs = [
        openclaw_command.argv[index + 1] for index, item in enumerate(openclaw_command.argv) if item == "--agent-kwarg"
    ]

    assert openclaw_command.argv[openclaw_command.argv.index("--model") + 1] == "openai/deepseek-v4-flash"
    assert "thinking=off" in openclaw_kwargs

    openhands = _request(tmp_path, agent="openhands", model="deepseek-v4-flash")
    openhands.claimed_job.target_name = "openhands"
    openhands_command = build_harbor_command(openhands)
    kwargs = [
        openhands_command.argv[index + 1]
        for index, item in enumerate(openhands_command.argv)
        if item == "--agent-kwarg"
    ]

    assert not any(value.startswith("git_version=") for value in kwargs)

    pinned_openhands = _request(
        tmp_path,
        agent="openhands",
        model="openai/deepseek-v4-flash",
        agent_kwargs={"git_version": "explicit-ref"},
    )
    pinned_openhands.claimed_job.target_name = "openhands"
    pinned_command = build_harbor_command(pinned_openhands)
    pinned_kwargs = [
        pinned_command.argv[index + 1] for index, item in enumerate(pinned_command.argv) if item == "--agent-kwarg"
    ]

    assert "git_version=explicit-ref" in pinned_kwargs


def test_build_harbor_command_uses_dataset_case_input_as_single_task_slice(tmp_path: Path):
    request = _request(tmp_path, task_ids=None)
    request.claimed_job.case_input = "cancel-async-tasks"

    command = build_harbor_command(request)

    assert command.argv[command.argv.index("--include-task-name") + 1] == "cancel-async-tasks"


def test_build_harbor_command_supports_agent_setup_timeout_multiplier(tmp_path: Path):
    command = build_harbor_command(_request(tmp_path, agent_setup_timeout_multiplier=3))

    assert command.argv[command.argv.index("--agent-setup-timeout-multiplier") + 1] == "3.0"


def test_build_harbor_command_uses_runtime_credentials_without_persisting_them(tmp_path: Path, monkeypatch):
    monkeypatch.setenv("OPENAI_API_KEY", "runtime-secret")
    monkeypatch.setenv("OPENAI_BASE_URL", "https://example.invalid/v1")
    monkeypatch.setenv("AGENTSCOPE_PROJECT_TOKEN", "ascope_runtime_token")
    monkeypatch.setenv("AGENTSCOPE_SERVER_URL", "http://127.0.0.1:8080")
    monkeypatch.setenv("OPENHANDS_AGENTSCOPE_ENABLED", "true")
    monkeypatch.setenv("HTTPS_PROXY", "http://127.0.0.1:7897")
    monkeypatch.setenv("NO_PROXY", "localhost,127.0.0.1")

    command = build_harbor_command(_request(tmp_path, extra_env={}))
    env_flags = [command.argv[index + 1] for index, item in enumerate(command.argv) if item == "--agent-env"]

    assert "OPENAI_API_KEY=${OPENAI_API_KEY}" in env_flags
    assert "OPENAI_BASE_URL=https://example.invalid/v1" in env_flags
    assert "AGENTSCOPE_PROJECT_TOKEN=${AGENTSCOPE_PROJECT_TOKEN}" in env_flags
    assert "AGENTSCOPE_SERVER_URL=http://127.0.0.1:8080" in env_flags
    assert "OPENHANDS_AGENTSCOPE_ENABLED=true" in env_flags
    assert "HTTPS_PROXY=${AGENTSCOPE_HARBOR_AGENT_HTTPS_PROXY}" in env_flags
    assert "NO_PROXY=${AGENTSCOPE_HARBOR_AGENT_NO_PROXY}" in env_flags
    assert command.env["OPENAI_API_KEY"] == "runtime-secret"
    assert command.env["HTTPS_PROXY"] == ""
    assert command.env["NO_PROXY"] == ""
    assert command.env["AGENTSCOPE_HARBOR_AGENT_HTTPS_PROXY"] == "http://127.0.0.1:7897"
    assert command.env["AGENTSCOPE_HARBOR_AGENT_NO_PROXY"] == "localhost,127.0.0.1"
    assert "http://127.0.0.1:7897" not in " ".join(command.argv)


def test_harbor_cli_preserves_configured_no_proxy(tmp_path: Path, monkeypatch):
    monkeypatch.setenv("NO_PROXY", "localhost,127.0.0.1")

    command = build_harbor_command(_request(tmp_path, extra_env={}))

    assert command.env["AGENTSCOPE_HARBOR_AGENT_NO_PROXY"] == "localhost,127.0.0.1"


def test_parse_harbor_result_marks_official_success_and_collects_artifacts(tmp_path: Path):
    job_dir = tmp_path / "jobs" / "job-1"
    _write_result(job_dir, reward=1.0)

    result = parse_harbor_result(job_dir, {})

    assert result.valid is True
    assert result.official_score == 1.0
    assert result.official_passed is True
    assert result.official_metrics["rewards"] == {"reward": 1.0, "secondary": 0.5}
    assert "1/1 passed" in result.official_reason
    assert {artifact.kind for artifact in result.artifacts} == {"harbor_result", "harbor_log"}
    assert all(artifact.uri.startswith("file://") for artifact in result.artifacts)


def test_parse_harbor_result_marks_official_verifier_failure_as_valid_result(tmp_path: Path):
    job_dir = tmp_path / "jobs" / "job-2"
    _write_result(job_dir, reward=0.0, suffix=".yaml")

    result = parse_harbor_result(job_dir, {})

    assert result.valid is True
    assert result.official_score == 0.0
    assert result.official_passed is False
    assert "0/1 passed" in result.official_reason


def test_parse_harbor_result_rejects_malformed_official_output(tmp_path: Path):
    job_dir = tmp_path / "jobs" / "job-3"
    job_dir.mkdir(parents=True)
    (job_dir / "result.json").write_text("{not JSON", encoding="utf-8")

    with pytest.raises(BenchmarkVerifierError, match="malformed result"):
        parse_harbor_result(job_dir, {})


def test_harbor_executor_runs_official_cli_without_shell_and_parses_result(tmp_path: Path):
    request = _request(tmp_path, agent="openhands-sdk")
    request.claimed_job.target_name = "openclaw"
    captured: dict[str, object] = {}

    def fake_run(argv, **kwargs):
        captured["argv"] = argv
        captured.update(kwargs)
        jobs_dir = Path(argv[argv.index("--jobs-dir") + 1])
        job_name = argv[argv.index("--job-name") + 1]
        _write_result(jobs_dir / job_name, reward=1.0)
        return subprocess.CompletedProcess(argv, 0, stdout="done", stderr="")

    result = HarborExecutor(subprocess_run=fake_run).execute(request)

    assert result.official_passed is True
    assert captured["shell"] is False
    assert captured["check"] is False
    assert captured["timeout"] == 120.0
    assert captured["env"] is not None
    assert captured["env"]["OPENAI_API_KEY"] == "secret-key"
    assert "secret-key" not in " ".join(captured["argv"])
    assert captured["argv"][captured["argv"].index("--agent") + 1] == "openclaw"


def test_harbor_executor_accepts_an_already_patched_openclaw_source(tmp_path: Path, monkeypatch):
    harbor_dir = tmp_path / "harbor"
    source_path = harbor_dir / "src/harbor/agents/installed/openclaw.py"
    source_path.parent.mkdir(parents=True)
    source_path.write_text("def _install_prebuilt_node22() -> str:\n", encoding="utf-8")
    monkeypatch.setenv("HTTPS_PROXY", "http://127.0.0.1:7897")
    request = _request(tmp_path, agent="openclaw", working_dir=str(harbor_dir))
    request.claimed_job.target_name = "openclaw"

    def fake_run(argv, **kwargs):
        jobs_dir = Path(argv[argv.index("--jobs-dir") + 1])
        job_name = argv[argv.index("--job-name") + 1]
        _write_result(jobs_dir / job_name, reward=1.0)
        return subprocess.CompletedProcess(argv, 0, stdout="done", stderr="")

    result = HarborExecutor(subprocess_run=fake_run).execute(request)

    assert result.official_passed is True


def test_harbor_executor_uses_uv_directory_as_openclaw_source_checkout(tmp_path: Path, monkeypatch):
    harbor_dir = tmp_path / "harbor"
    source_path = harbor_dir / "src/harbor/agents/installed/openclaw.py"
    source_path.parent.mkdir(parents=True)
    source_path.write_text("def _install_prebuilt_node22() -> str:\n", encoding="utf-8")
    monkeypatch.setenv("HTTPS_PROXY", "http://127.0.0.1:7897")
    request = _request(tmp_path, agent="openclaw", executable=None, working_dir=None)
    request.claimed_job.target_name = "openclaw"
    monkeypatch.setenv("AGENTSCOPE_HARBOR_WORKDIR", str(harbor_dir))

    def fake_run(argv, **kwargs):
        jobs_dir = Path(argv[argv.index("--jobs-dir") + 1])
        job_name = argv[argv.index("--job-name") + 1]
        _write_result(jobs_dir / job_name, reward=1.0)
        return subprocess.CompletedProcess(argv, 0, stdout="done", stderr="")

    result = HarborExecutor(subprocess_run=fake_run).execute(request)

    assert result.official_passed is True


def test_harbor_executor_upgrades_the_legacy_openclaw_proxy_patch(tmp_path: Path, monkeypatch):
    harbor_dir = tmp_path / "harbor"
    source_path = harbor_dir / "src/harbor/agents/installed/openclaw.py"
    source_path.parent.mkdir(parents=True)
    source_path.write_text('$1 == "00000000"\n', encoding="utf-8")
    monkeypatch.setenv("HTTPS_PROXY", "http://127.0.0.1:7897")
    request = _request(tmp_path, agent="openclaw", working_dir=str(harbor_dir))
    request.claimed_job.target_name = "openclaw"

    def fake_apply(applied_harbor_dir, patch_path, *, error_message):
        assert applied_harbor_dir == harbor_dir
        assert patch_path.name == "harbor_openclaw_apt_proxy_upgrade.patch"
        source_path.write_text(
            '$2 == "00000000"\nasync def _ensure_system_dependencies(\n',
            encoding="utf-8",
        )

    monkeypatch.setattr(harbor_module, "_apply_harbor_patch", fake_apply)

    def fake_run(argv, **kwargs):
        jobs_dir = Path(argv[argv.index("--jobs-dir") + 1])
        job_name = argv[argv.index("--job-name") + 1]
        _write_result(jobs_dir / job_name, reward=1.0)
        return subprocess.CompletedProcess(argv, 0, stdout="done", stderr="")

    result = HarborExecutor(subprocess_run=fake_run).execute(request)

    assert result.official_passed is True
    assert '$2 == "00000000"' in source_path.read_text(encoding="utf-8")
    assert "async def _ensure_system_dependencies(" in source_path.read_text(encoding="utf-8")


def test_harbor_executor_applies_openclaw_patch_for_a_rewritten_proxy(tmp_path: Path, monkeypatch):
    harbor_dir = tmp_path / "harbor"
    source_path = harbor_dir / "src/harbor/agents/installed/openclaw.py"
    source_path.parent.mkdir(parents=True)
    source_path.write_text("unpatched Harbor source\n", encoding="utf-8")
    monkeypatch.setenv("HTTPS_PROXY", "http://127.0.0.1:7897")
    request = _request(tmp_path, agent="openclaw", working_dir=str(harbor_dir))
    request.claimed_job.target_name = "openclaw"
    applied_patch_names: list[str] = []

    def fake_apply(applied_harbor_dir, patch_path, *, error_message):
        assert applied_harbor_dir == harbor_dir
        applied_patch_names.append(patch_path.name)
        source_path.write_text("def _install_prebuilt_node22() -> str:\n", encoding="utf-8")

    monkeypatch.setattr(harbor_module, "_apply_harbor_patch", fake_apply)

    def fake_run(argv, **kwargs):
        jobs_dir = Path(argv[argv.index("--jobs-dir") + 1])
        job_name = argv[argv.index("--job-name") + 1]
        _write_result(jobs_dir / job_name, reward=1.0)
        return subprocess.CompletedProcess(argv, 0, stdout="done", stderr="")

    result = HarborExecutor(subprocess_run=fake_run).execute(request)

    assert result.official_passed is True
    assert applied_patch_names == ["harbor_openclaw_dynamic_proxy.patch"]


def test_harbor_executor_applies_openhands_patch_for_a_rewritten_proxy(tmp_path: Path, monkeypatch):
    harbor_dir = tmp_path / "harbor"
    source_path = harbor_dir / "src/harbor/agents/installed/openhands.py"
    source_path.parent.mkdir(parents=True)
    source_path.write_text("unpatched Harbor source\n", encoding="utf-8")
    monkeypatch.setenv("HTTPS_PROXY", "http://127.0.0.1:7897")
    request = _request(tmp_path, agent="openhands", working_dir=str(harbor_dir))
    request.claimed_job.target_name = "openhands"
    applied_patch_names: list[str] = []

    def fake_apply(applied_harbor_dir, patch_path, *, error_message):
        assert applied_harbor_dir == harbor_dir
        applied_patch_names.append(patch_path.name)
        source_path.write_text("def _container_proxy_command(cmd: str) -> str:\n", encoding="utf-8")

    monkeypatch.setattr(harbor_module, "_apply_harbor_patch", fake_apply)

    def fake_run(argv, **kwargs):
        jobs_dir = Path(argv[argv.index("--jobs-dir") + 1])
        job_name = argv[argv.index("--job-name") + 1]
        _write_result(jobs_dir / job_name, reward=1.0)
        return subprocess.CompletedProcess(argv, 0, stdout="done", stderr="")

    result = HarborExecutor(subprocess_run=fake_run).execute(request)

    assert result.official_passed is True
    assert applied_patch_names == ["harbor_openhands_dynamic_proxy.patch"]


def test_harbor_executor_upgrades_existing_openhands_proxy_patch(tmp_path: Path, monkeypatch):
    harbor_dir = tmp_path / "harbor"
    source_path = harbor_dir / "src/harbor/agents/installed/openhands.py"
    source_path.parent.mkdir(parents=True)
    source_path.write_text("def _container_proxy_command(cmd: str) -> str:\n", encoding="utf-8")
    monkeypatch.setenv("HTTPS_PROXY", "http://127.0.0.1:7897")
    request = _request(tmp_path, agent="openhands", working_dir=str(harbor_dir))
    request.claimed_job.target_name = "openhands"
    applied_patch_names: list[str] = []

    def fake_apply(applied_harbor_dir, patch_path, *, error_message):
        assert applied_harbor_dir == harbor_dir
        applied_patch_names.append(patch_path.name)
        source_path.write_text(
            'OPENHANDS_MCP_COMPATIBLE_PACKAGE_VERSION = "1.29.0"\n',
            encoding="utf-8",
        )

    monkeypatch.setattr(harbor_module, "_apply_harbor_patch", fake_apply)

    def fake_run(argv, **kwargs):
        jobs_dir = Path(argv[argv.index("--jobs-dir") + 1])
        job_name = argv[argv.index("--job-name") + 1]
        _write_result(jobs_dir / job_name, reward=1.0)
        return subprocess.CompletedProcess(argv, 0, stdout="done", stderr="")

    result = HarborExecutor(subprocess_run=fake_run).execute(request)

    assert result.official_passed is True
    assert applied_patch_names == ["harbor_openhands_dependency_upgrade.patch"]


def test_openclaw_compatibility_patch_avoids_github_hosted_nvm_bootstrap():
    patch_path = Path(harbor_module.__file__).with_name("harbor_openclaw_dynamic_proxy.patch")
    patch = patch_path.read_text(encoding="utf-8")
    added_lines = [line for line in patch.splitlines() if line.startswith("+")]

    assert not any("raw.githubusercontent.com/nvm-sh/nvm" in line for line in added_lines)
    assert any("https://nodejs.org/dist/${node_version}/${archive}" in line for line in added_lines)
    assert any("def _node22(cmd: str)" in line for line in added_lines)
    assert any("urllib.request.urlopen" in line for line in added_lines)
    assert not any("apt-get" in line for line in added_lines)
    assert any("env -u HTTP_PROXY -u HTTPS_PROXY" in line for line in added_lines)


def test_openhands_compatibility_patch_uses_direct_https_apt_bootstrap():
    patch_path = Path(harbor_module.__file__).with_name("harbor_openhands_dynamic_proxy.patch")
    added_lines = [line for line in patch_path.read_text(encoding="utf-8").splitlines() if line.startswith("+")]

    assert any("https://deb.debian.org" in line for line in added_lines)
    assert any("Acquire::https::Timeout=30" in line for line in added_lines)
    assert any("Acquire::https::Proxy=${configured_proxy}" in line for line in added_lines)
    assert any('OPENHANDS_COMPATIBLE_PACKAGE_VERSION = "1.4.0"' in line for line in added_lines)
    assert any('OPENHANDS_MCP_COMPATIBLE_PACKAGE_VERSION = "1.29.0"' in line for line in added_lines)
    assert any("mcp=={OPENHANDS_MCP_COMPATIBLE_PACKAGE_VERSION} pydantic-settings" in line for line in added_lines)
    assert not any("uv pip install pydantic-settings &&" in line for line in added_lines)
    assert any("def _container_proxy_command(cmd: str)" in line for line in added_lines)


def test_harbor_executor_rejects_incompatible_openclaw_source_with_loopback_proxy(tmp_path: Path, monkeypatch):
    harbor_dir = tmp_path / "harbor"
    source_path = harbor_dir / "src/harbor/agents/installed/openclaw.py"
    source_path.parent.mkdir(parents=True)
    source_path.write_text("unpatched Harbor source\n", encoding="utf-8")
    monkeypatch.setenv("HTTPS_PROXY", "http://127.0.0.1:7897")
    request = _request(tmp_path, agent="openclaw", working_dir=str(harbor_dir))
    request.claimed_job.target_name = "openclaw"

    with pytest.raises(BenchmarkHarnessError, match="incompatible"):
        HarborExecutor().execute(request)


def test_harbor_executor_classifies_timeout_and_nonzero_exit(tmp_path: Path):
    request = _request(tmp_path)

    def timeout_run(*args, **kwargs):
        raise subprocess.TimeoutExpired(args[0], kwargs["timeout"])

    with pytest.raises(BenchmarkHarnessError, match="timed out"):
        HarborExecutor(subprocess_run=timeout_run).execute(request)

    def failed_run(argv, **kwargs):
        return subprocess.CompletedProcess(argv, 17, stdout="", stderr="failed with OPENAI_API_KEY=secret-key")

    with pytest.raises(BenchmarkHarnessError, match="non-zero") as exc_info:
        HarborExecutor(subprocess_run=failed_run).execute(request)

    details = exc_info.value.details
    assert details["returncode"] == 17
    assert "secret-key" not in str(details["command"])
    assert "secret-key" not in details["stderr_tail"]
    assert "OPENAI_API_KEY=***" in details["stderr_tail"]
