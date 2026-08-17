from __future__ import annotations

import ast
import os
import inspect
import json
from dataclasses import dataclass, field, replace
from pathlib import Path
from typing import Mapping

import pytest

from benchmark_worker.contracts import Correlation, JobResult, LeasedJob, TaskRun, TaskRunState, TaskSpec, WorkerBackend
from benchmark_worker.harbor_executor import HarborHarnessError, HarborJobExecutor, HarborRuntimeOptions, _sidecar_environment
from benchmark_worker.sidecar import SidecarHandle, SidecarSpec


@dataclass
class FakeBackend(WorkerBackend):
    queued: LeasedJob | None
    heartbeats: list[tuple[str, str]] = field(default_factory=list)
    results: list[JobResult] = field(default_factory=list)
    failures: list[tuple[str, str, str]] = field(default_factory=list)

    def claim(self, worker_id: str) -> LeasedJob | None:
        del worker_id
        job, self.queued = self.queued, None
        return job

    def heartbeat(self, worker_id: str, eval_job_id: str) -> None:
        self.heartbeats.append((worker_id, eval_job_id))

    def submit_result(self, worker_id: str, result: JobResult) -> None:
        assert worker_id == "worker-1"
        self.results.append(result)

    def fail(self, worker_id: str, eval_job_id: str, error_type: str) -> None:
        self.failures.append((worker_id, eval_job_id, error_type))


@dataclass
class FakeSidecars:
    started: list[SidecarSpec] = field(default_factory=list)
    stopped: list[str] = field(default_factory=list)

    def start(self, spec: SidecarSpec) -> SidecarHandle:
        self.started.append(spec)
        return SidecarHandle("a" * 64, "http://127.0.0.1:43123")

    def stop(self, container_id: str) -> None:
        self.stopped.append(container_id)


class FakeAdapter:
    def __init__(self, endpoint: str, task_run: TaskRun | None = None, healthy: bool = True) -> None:
        assert endpoint == "http://127.0.0.1:43123"
        self.task_run = task_run or TaskRun("task-run-1", TaskRunState.SUCCEEDED)
        self.healthy = healthy

    def healthcheck(self) -> bool:
        return self.healthy

    def get_task(self, task_run_id: str) -> TaskRun:
        assert task_run_id == "task-run-1"
        return self.task_run


class FakeHarness:
    def __init__(self, *, reward: float | None, creates_result: bool = True) -> None:
        self.reward = reward
        self.creates_result = creates_result
        self.calls: list[tuple[LeasedJob, str, Path, str]] = []

    def run(
        self,
        job: LeasedJob,
        *,
        task_runner_endpoint: str,
        job_directory: Path,
        tool_gateway_token: str,
    ) -> Path:
        self.calls.append((job, task_runner_endpoint, job_directory, tool_gateway_token))
        if self.creates_result:
            payload: dict[str, object] = {"verifier_result": {"rewards": {"reward": self.reward}}}
            (job_directory / "result.json").write_text(json.dumps(payload), encoding="utf-8")
        return job_directory


def job() -> LeasedJob:
    return LeasedJob(
        "job-1",
        TaskSpec(
            task_run_id="task-run-1",
            task_id="smoke-task-001",
            instruction="fix it",
            workspace="/workspace",
            timeout_seconds=30,
            tool_gateway_endpoint="http://gateway.local/v1",
            tool_gateway_protocol="agentlab.harbor-tool-gateway.v1",
            agent_profile_id="openhands-default",
            model_profile_id="model-fixed",
            correlation=Correlation("experiment-1", "job-1", 0),
            execution_config={
                "agent_artifact": {
                    "framework": "openhands",
                    "image": "local/openhands:dev",
                    "image_digest": "sha256:" + "a" * 64,
                },
                "model_profile": {
                    "id": "model-fixed",
                    "config": {
                        "provider": "openai-compatible",
                        "model": "openai/model-fixed",
                        "parameters": {},
                    },
                },
                "execution": {"harbor": {
                    "version": "0.20.0",
                    "dataset": "terminal-bench@2.0",
                    "agent_import_path": "benchmark_worker.harbor_agent:TaskRunnerAgent",
                    "jobs_root": "harbor-jobs",
                }},
            },
        ),
    )


def executor(
    tmp_path: Path,
    backend: FakeBackend,
    harness: FakeHarness,
    sidecars: FakeSidecars,
    *,
    task_run: TaskRun | None = None,
    healthy: bool = True,
    sidecar_environment: Mapping[str, str] | None = None,
) -> HarborJobExecutor:
    return HarborJobExecutor(
        "worker-1",
        backend,
        options=HarborRuntimeOptions(work_root=tmp_path),
        harness=harness,
        sidecars=sidecars,
        adapter_factory=lambda endpoint: FakeAdapter(endpoint, task_run=task_run, healthy=healthy),
        heartbeat_interval_s=60,
        sidecar_environment=sidecar_environment,
    )


def test_worker_runs_pinned_sidecar_and_submits_only_official_harbor_verdict(tmp_path: Path) -> None:
    backend = FakeBackend(job())
    harness = FakeHarness(reward=1.0)
    sidecars = FakeSidecars()

    assert executor(tmp_path, backend, harness, sidecars).process_once() is True

    assert backend.heartbeats == [("worker-1", "job-1")]
    assert backend.failures == []
    assert len(backend.results) == 1
    result = backend.results[0]
    assert result.verifier_status == "passed"
    assert result.score == 1.0
    assert result.official_metrics == {"reward": 1.0}
    assert result.artifact_manifest[0]["kind"] == "harbor_official_verdict"
    artifact_path = Path(result.artifact_manifest[0]["uri"].removeprefix("file://"))
    assert json.loads(artifact_path.read_text(encoding="utf-8"))["score"] == 1.0
    assert sidecars.started[0].image_ref == "local/openhands:dev@sha256:" + "a" * 64
    assert sidecars.started[0].workspace_container_path == "/workspace"
    capability = sidecars.started[0].environment.get("AGENTLAB_TOOL_GATEWAY_TOKEN", "")
    assert isinstance(capability, str) and len(capability) >= 24
    assert harness.calls[0][3] == capability
    assert sidecars.stopped == ["a" * 64]
    assert not list((tmp_path / "harbor-jobs").iterdir())
    assert len(list((tmp_path / "artifacts").glob("*-official-verdict.json"))) == 1


def test_worker_binds_catalog_model_profile_over_ambient_worker_default(tmp_path: Path) -> None:
    backend = FakeBackend(job())
    harness = FakeHarness(reward=1.0)
    sidecars = FakeSidecars()

    assert executor(
        tmp_path,
        backend,
        harness,
        sidecars,
        sidecar_environment={"LLM_MODEL": "ambient-model-must-not-win"},
    ).process_once() is True

    assert sidecars.started[0].environment["LLM_MODEL"] == "openai/model-fixed"


def test_worker_rejects_missing_immutable_model_profile(tmp_path: Path) -> None:
    queued = job()
    config = dict(queued.task.execution_config or {})
    config.pop("model_profile")
    invalid = LeasedJob(queued.eval_job_id, replace(queued.task, execution_config=config))
    backend = FakeBackend(invalid)
    harness = FakeHarness(reward=1.0)
    sidecars = FakeSidecars()

    assert executor(tmp_path, backend, harness, sidecars).process_once() is True

    assert harness.calls == []
    assert sidecars.started == []
    assert backend.failures == [("worker-1", "job-1", "agent_model_profile_missing")]


def test_worker_submits_official_failed_verdict_instead_of_synthesizing_agent_failure(tmp_path: Path) -> None:
    backend = FakeBackend(job())
    harness = FakeHarness(reward=0.0)
    sidecars = FakeSidecars()

    executor(tmp_path, backend, harness, sidecars, task_run=TaskRun("task-run-1", TaskRunState.FAILED, error_code="agent_execution_failed")).process_once()

    assert backend.failures == []
    assert backend.results[0].verifier_status == "failed"
    assert backend.results[0].score == 0.0


def test_worker_reports_verifier_error_if_harbor_lacks_official_evidence(tmp_path: Path) -> None:
    backend = FakeBackend(job())
    harness = FakeHarness(reward=None, creates_result=False)
    sidecars = FakeSidecars()

    executor(tmp_path, backend, harness, sidecars).process_once()

    assert backend.results == []
    assert backend.failures == [("worker-1", "job-1", "verifier_error")]
    assert sidecars.stopped == ["a" * 64]


def test_worker_preserves_safe_task_runner_code_when_harbor_has_no_verdict(tmp_path: Path) -> None:
    backend = FakeBackend(job())
    harness = FakeHarness(reward=None, creates_result=False)
    sidecars = FakeSidecars()

    executor(
        tmp_path,
        backend,
        harness,
        sidecars,
        task_run=TaskRun("task-run-1", TaskRunState.FAILED, error_code="agent_execution_failed"),
    ).process_once()

    assert backend.results == []
    assert backend.failures == [("worker-1", "job-1", "agent_agent_execution_failed")]


class StructuralFailureHarness:
    """Small deterministic harness used only to test safe platform reporting."""

    def run(
        self,
        job: LeasedJob,
        *,
        task_runner_endpoint: str,
        job_directory: Path,
        tool_gateway_token: str,
    ) -> Path:
        del job, task_runner_endpoint, job_directory, tool_gateway_token
        raise HarborHarnessError("structural Harbor configuration rejected", failure_code="harness_config_missing")


def test_worker_reports_structural_harbor_failure_code_without_exception_text(tmp_path: Path) -> None:
    backend = FakeBackend(job())
    sidecars = FakeSidecars()

    executor(tmp_path, backend, StructuralFailureHarness(), sidecars).process_once()

    assert backend.results == []
    assert backend.failures == [("worker-1", "job-1", "harness_config_missing")]
    assert sidecars.stopped == ["a" * 64]


def test_worker_reports_unsafe_job_path_as_fixed_harness_failure(tmp_path: Path) -> None:
    original = job()
    execution_config = dict(original.task.execution_config or {})
    execution = dict(execution_config["execution"])
    harbor = dict(execution["harbor"])
    harbor["jobs_root"] = "../unsafe"
    execution["harbor"] = harbor
    execution_config["execution"] = execution
    invalid_job = replace(original, task=replace(original.task, execution_config=execution_config))
    backend = FakeBackend(invalid_job)
    harness = FakeHarness(reward=1.0)
    sidecars = FakeSidecars()

    executor(tmp_path, backend, harness, sidecars).process_once()

    assert harness.calls == []
    assert backend.failures == [("worker-1", "job-1", "harness_relative_path_invalid")]
    assert sidecars.started == []


def test_every_harbor_harness_raise_requires_a_fixed_failure_code() -> None:
    """Keep structural failures reportable without reading Harbor/provider text."""

    import benchmark_worker.harbor_executor as module

    tree = ast.parse(inspect.getsource(module))
    missing_codes = [
        node.lineno
        for node in ast.walk(tree)
        if isinstance(node, ast.Call)
        and isinstance(node.func, ast.Name)
        and node.func.id == "HarborHarnessError"
        and not any(keyword.arg == "failure_code" for keyword in node.keywords)
    ]
    assert missing_codes == []
    with pytest.raises(TypeError):
        HarborHarnessError("failure code is required")




def test_worker_adds_private_sidecar_host_to_no_proxy_before_readiness(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    class PrivateSidecars(FakeSidecars):
        def start(self, spec: SidecarSpec) -> SidecarHandle:
            self.started.append(spec)
            return SidecarHandle(container_id="a" * 64, endpoint="http://agentlab-sidecar-0123456789abcdef01234567:8001")

    monkeypatch.delenv("NO_PROXY", raising=False)
    backend = FakeBackend(job())
    harness = FakeHarness(reward=1.0)
    sidecars = PrivateSidecars()

    executor(tmp_path, backend, harness, sidecars).process_once()

    assert "agentlab-sidecar-0123456789abcdef01234567" in os.environ.get("NO_PROXY", "")

def test_worker_reports_agent_error_for_unhealthy_sidecar_without_running_harbor(tmp_path: Path) -> None:
    backend = FakeBackend(job())
    harness = FakeHarness(reward=1.0)
    sidecars = FakeSidecars()

    executor(tmp_path, backend, harness, sidecars, healthy=False).process_once()

    assert harness.calls == []
    assert backend.results == []
    assert backend.failures == [("worker-1", "job-1", "agent_sidecar_contract_error")]
    assert sidecars.stopped == ["a" * 64]


class CapturingHarborProcess:
    def __init__(self) -> None:
        self.args: list[str] | None = None
        self.env: Mapping[str, str] | None = None

    def run(self, args: list[str], *, cwd: Path, env: Mapping[str, str], timeout_s: float) -> int:
        del cwd, timeout_s
        self.args = list(args)
        self.env = dict(env)
        return 0


def test_harbor_cli_keeps_gateway_capability_out_of_cli_arguments(tmp_path: Path) -> None:
    from benchmark_worker.harbor_executor import HarborCLIHarness

    process = CapturingHarborProcess()
    harness = HarborCLIHarness(
        HarborRuntimeOptions(work_root=tmp_path),
        process_runner=process,
        environment={"PATH": "/usr/bin", "LLM_API_KEY": "test-model-key"},
    )
    directory = tmp_path / "harbor-job"
    directory.mkdir()
    (directory / "result.json").write_text(
        json.dumps({"verifier_result": {"rewards": {"reward": 1.0}}}),
        encoding="utf-8",
    )
    capability = "z" * 32

    assert harness.run(
        job(),
        task_runner_endpoint="http://127.0.0.1:43123",
        job_directory=directory,
        tool_gateway_token=capability,
    ) == directory

    assert process.args is not None and process.env is not None
    assert capability not in process.args
    assert not any("tool_gateway_endpoint" in item for item in process.args)
    assert process.env["AGENTLAB_TOOL_GATEWAY_TOKEN"] == capability
    assert "LLM_API_KEY" not in process.env


def test_harbor_cli_bypasses_proxy_for_private_task_runner_sidecar(tmp_path: Path) -> None:
    from benchmark_worker.harbor_executor import HarborCLIHarness

    process = CapturingHarborProcess()
    harness = HarborCLIHarness(
        HarborRuntimeOptions(work_root=tmp_path),
        process_runner=process,
        environment={"PATH": "/usr/bin", "NO_PROXY": "localhost", "HTTP_PROXY": "http://proxy.invalid:7897"},
    )
    directory = tmp_path / "harbor-job"
    directory.mkdir()
    (directory / "result.json").write_text(
        json.dumps({"verifier_result": {"rewards": {"reward": 1.0}}}),
        encoding="utf-8",
    )

    harness.run(
        job(),
        task_runner_endpoint="http://agentlab-sidecar-" + "a" * 24 + ":43123",
        job_directory=directory,
        tool_gateway_token="z" * 32,
    )

    assert process.env is not None
    assert process.env["NO_PROXY"].split(",") == ["localhost", "agentlab-sidecar-" + "a" * 24]


def test_private_sidecar_gateway_bypasses_proxy_without_bypassing_host_gateway(monkeypatch) -> None:
    monkeypatch.setenv("HTTP_PROXY", "http://proxy.invalid:7897")
    monkeypatch.setenv("HTTPS_PROXY", "http://proxy.invalid:7897")
    monkeypatch.setenv("NO_PROXY", "localhost,127.0.0.1")

    environment = _sidecar_environment({}, no_proxy_host="agentlab-harbor-sentinel-worker")

    assert environment["NO_PROXY"].split(",") == ["localhost", "127.0.0.1", "agentlab-harbor-sentinel-worker"]
    host_gateway_environment = _sidecar_environment({}, no_proxy_host="host.docker.internal")
    assert host_gateway_environment["NO_PROXY"] == "localhost,127.0.0.1"


def test_sidecar_preserves_hermes_native_observability_controls(monkeypatch) -> None:
    monkeypatch.setenv("HERMES_AGENTSCOPE_ENABLED", "true")
    monkeypatch.setenv("HERMES_AGENTSCOPE_AGENT_NAME", "hermes-benchmark")
    monkeypatch.setenv("AGENTSCOPE_ENVIRONMENT", "development")

    environment = _sidecar_environment({})

    assert environment["HERMES_AGENTSCOPE_ENABLED"] == "true"
    assert environment["HERMES_AGENTSCOPE_AGENT_NAME"] == "hermes-benchmark"
    assert environment["AGENTSCOPE_ENVIRONMENT"] == "development"


def test_worker_forwards_openhands_benchmark_observability_context(tmp_path: Path, monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("OPENHANDS_AGENTSCOPE_ENABLED", "true")
    monkeypatch.setenv("AGENTSCOPE_SERVER_URL", "http://agentscope:8080")
    monkeypatch.setenv("AGENTSCOPE_PROJECT_TOKEN", "project-token")
    backend = FakeBackend(job())
    harness = FakeHarness(reward=1.0)
    sidecars = FakeSidecars()

    executor(tmp_path, backend, harness, sidecars).process_once()

    environment = sidecars.started[0].environment
    assert environment["OPENHANDS_AGENTSCOPE_ENABLED"] == "true"
    assert environment["AGENTSCOPE_EXPERIMENT_ID"] == "experiment-1"
    assert environment["AGENTSCOPE_EVAL_JOB_ID"] == "job-1"
    assert environment["AGENTSCOPE_BENCHMARK_KEY"] == "harbor"
    assert environment["AGENTSCOPE_BENCHMARK_TASK_ID"] == "smoke-task-001"
    assert environment["AGENTSCOPE_TRIAL_INDEX"] == "0"


def test_private_sidecar_gateway_host_must_be_safe(monkeypatch) -> None:
    monkeypatch.setenv("NO_PROXY", "localhost")

    import pytest
    from benchmark_worker.harbor_executor import HarborHarnessError

    with pytest.raises(HarborHarnessError, match="tool gateway host is invalid"):
        _sidecar_environment({}, no_proxy_host="bad host")
