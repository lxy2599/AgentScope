"""Harbor-backed execution of one durable AgentScope benchmark job.

This module owns the *worker* lifecycle only.  Harbor remains the authority for
sandbox setup/reset and verifier output; the worker must never manufacture a
pass from an agent HTTP status or a process exit code.

The default command path targets Harbor's supported ``harbor run`` interface
and a small custom agent import path.  The custom agent is given only a
loopback sidecar endpoint and non-secret execution identifiers.  Provider and
AgentScope credentials are injected directly into the immutable agent image by
the worker, never placed in Harbor CLI arguments or catalog YAML.
"""

from __future__ import annotations

import json
import os
import re
import secrets
import shutil
import subprocess
import threading
import time
from dataclasses import dataclass
from urllib.parse import urlparse
from pathlib import Path
from typing import Any, Callable, Mapping, Protocol
from uuid import uuid4

from .contracts import JobResult, LeasedJob, TaskRun, TaskRunState, WorkerBackend
from .harbor_result import HarborVerifierError, HarborVerifierVerdict, parse_official_harbor_verdict
from .sidecar import DockerSidecarLauncher, SidecarError, SidecarHandle, SidecarImageUnavailableError, SidecarSpec
from .task_runner_client import TaskRunnerError, TaskRunnerHTTPAdapter


_SAFE_JOB_COMPONENT = re.compile(r"^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$")
_SAFE_HARBOR_IMPORT_PATH = re.compile(r"^[A-Za-z_][A-Za-z0-9_.]*:[A-Za-z_][A-Za-z0-9_]*$")
_SECRET_KEY = re.compile(r"(?:api[_-]?key|secret|token|password|credential|authorization|cookie)", re.IGNORECASE)


class HarborExecutionError(RuntimeError):
    """Credential-safe failure from the Harbor Worker execution path."""


class HarborHarnessError(HarborExecutionError):
    """Harbor did not complete a usable trial directory."""

    def __init__(self, message: str, *, failure_code: str) -> None:
        self.failure_code = failure_code
        super().__init__(message)


class HarborProcessRunner(Protocol):
    """Small injectable boundary around the Harbor CLI process."""

    def run(self, args: list[str], *, cwd: Path, env: Mapping[str, str], timeout_s: float) -> int: ...


class SubprocessHarborProcessRunner:
    """Run Harbor without a shell and retain only a safe failure category.

    Harbor task output may contain instructions or provider diagnostics. Raw
    streams are never logged, persisted or returned; they are examined only in
    memory to derive a bounded diagnostic category for a failed run. The
    official verifier artifact remains the only accepted score source.
    """

    def __init__(self) -> None:
        self.last_failure_category = "harbor_cli_error"

    def run(self, args: list[str], *, cwd: Path, env: Mapping[str, str], timeout_s: float) -> int:
        try:
            completed = subprocess.run(  # noqa: S603 -- executable/arguments are constructed and validated below.
                args,
                cwd=cwd,
                env=dict(env),
                check=False,
                stdin=subprocess.DEVNULL,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                timeout=timeout_s,
            )
        except subprocess.TimeoutExpired as exc:
            raise HarborHarnessError("Harbor execution timed out", failure_code="timeout") from exc
        except OSError as exc:
            raise HarborHarnessError("Harbor executable could not be started", failure_code="harness_cli_start_error") from exc
        if completed.returncode != 0:
            self.last_failure_category = _harbor_process_failure_category(completed.stdout, completed.stderr)
        return completed.returncode


class HarborHarness(Protocol):
    """Execute one official Harbor task and return its known trial directory."""

    def run(
        self,
        job: LeasedJob,
        *,
        task_runner_endpoint: str,
        job_directory: Path,
        tool_gateway_token: str,
    ) -> Path: ...


class SidecarLauncher(Protocol):
    def start(self, spec: SidecarSpec) -> SidecarHandle: ...

    def stop(self, container_id: str) -> None: ...


@dataclass(frozen=True)
class HarborRuntimeOptions:
    work_root: Path
    harbor_binary: str = "harbor"
    default_agent_import_path: str = "benchmark_worker.harbor_agent:TaskRunnerAgent"
    catalog_root: Path | None = None
    artifact_root: Path | None = None
    cleanup_workspaces: bool = True


class HarborCLIHarness:
    """Build the pinned, per-job Harbor CLI invocation.

    The catalog's immutable ``execution.harbor`` mapping is purposefully
    narrow.  It controls suite identity and safe non-secret transport options;
    it cannot supply arbitrary shell text, credentials, or an unpinned image.
    """

    def __init__(
        self,
        options: HarborRuntimeOptions,
        *,
        process_runner: HarborProcessRunner | None = None,
        environment: Mapping[str, str] | None = None,
    ) -> None:
        self.options = options
        self.process_runner = process_runner or SubprocessHarborProcessRunner()
        self.environment = dict(os.environ if environment is None else environment)

    def run(
        self,
        job: LeasedJob,
        *,
        task_runner_endpoint: str,
        job_directory: Path,
        tool_gateway_token: str,
    ) -> Path:
        config = _harbor_config(job.task.execution_config)
        dataset = _optional_string(config, "dataset")
        task_path = _catalog_task_path(config, self.options.catalog_root)
        task_path_root = _catalog_task_path_root(config, self.options.catalog_root, job.task.task_id)
        if sum(source is not None for source in (dataset, task_path, task_path_root)) != 1:
            raise HarborHarnessError(
                "Harbor execution requires exactly one dataset, task_path, or task_path_root",
                failure_code="harness_task_source_invalid",
            )
        if task_path_root is not None:
            task_path = task_path_root
        import_path = _optional_string(config, "agent_import_path") or self.options.default_agent_import_path
        if not _SAFE_HARBOR_IMPORT_PATH.fullmatch(import_path):
            raise HarborHarnessError("Harbor agent import path is invalid", failure_code="harness_agent_import_invalid")
        if not _is_worker_local_sidecar_endpoint(task_runner_endpoint):
            raise HarborHarnessError("Harbor sidecar endpoint must be worker-loopback or Docker host-gateway", failure_code="harness_sidecar_endpoint_invalid")
        if not isinstance(tool_gateway_token, str) or len(tool_gateway_token) < 24:
            raise HarborHarnessError("tool gateway capability is invalid", failure_code="harness_gateway_capability_invalid")

        job_name = job_directory.name
        if not _SAFE_JOB_COMPONENT.fullmatch(job_name):
            raise HarborHarnessError("Harbor job directory name is invalid", failure_code="harness_job_directory_name_invalid")
        # Harbor receives the local task-runner endpoint as agent config.  It is
        # not a credential and is intentionally scoped to a single task.
        args = [
            self.options.harbor_binary,
            "run",
            "--job-name",
            job_name,
            "--jobs-dir",
            str(job_directory.parent),
            "--agent",
            import_path,
            *( ["--dataset", dataset] if dataset is not None else ["--path", str(task_path)] ),
            "--include-task-name",
            _harbor_task_name_filter(dataset, job.task.task_id),
            "--n-concurrent",
            "1",
            "--n-attempts",
            "1",
            "--quiet",
            "--agent-kwarg",
            f"task_runner_endpoint={task_runner_endpoint}",
            "--agent-kwarg",
            f"task_run_id={job.task.task_run_id}",
            "--agent-kwarg",
            f"task_id={job.task.task_id}",
            "--agent-kwarg",
            f"timeout_seconds={job.task.timeout_seconds}",
            "--agent-kwarg",
            f"experiment_id={job.task.correlation.experiment_id}",
            "--agent-kwarg",
            f"eval_job_id={job.task.correlation.eval_job_id}",
            "--agent-kwarg",
            f"trial_index={job.task.correlation.trial_index}",
        ]
        config_path = _catalog_config_path(config, self.options.catalog_root)
        if config_path is not None:
            args.extend(["--config", str(config_path)])
        timeout_s = _positive_int(config.get("harness_timeout_s"), job.task.timeout_seconds + 300)
        process_env = _harbor_environment(self.environment)
        # Harbor imports TaskRunnerAgent in the trusted Worker container. The
        # digest-pinned sidecar is addressed by a private Docker-network DNS
        # name, so it must bypass the host HTTP proxy. In contrast,
        # host.docker.internal deliberately remains proxied in this deployment.
        _append_private_gateway_no_proxy(process_env, _task_runner_endpoint_host(task_runner_endpoint))
        # The per-job gateway capability is supplied only as a child-process
        # environment variable. It is deliberately absent from YAML, HTTP task
        # payloads, CLI arguments, artifacts, and worker logs.
        process_env["AGENTLAB_TOOL_GATEWAY_TOKEN"] = tool_gateway_token
        returncode = self.process_runner.run(args, cwd=job_directory.parent, env=process_env, timeout_s=timeout_s)
        # A non-zero command may still have a completed official verifier result
        # (for example a terminal failed task).  Parsing below remains source of
        # truth; absence of the artifact is classified as harness failure.
        if not job_directory.is_dir():
            raise HarborHarnessError("Harbor did not create the expected job directory", failure_code="harness_job_directory_missing")
        if returncode != 0 and not _has_result_artifact(job_directory):
            category = getattr(self.process_runner, "last_failure_category", "harbor_cli_error")
            if not isinstance(category, str) or not re.fullmatch(r"[a-z][a-z0-9_]{0,48}", category):
                category = "harbor_cli_error"
            raise HarborHarnessError(
                "Harbor command did not produce an official result",
                failure_code=f"harness_{category}",
            )
        return job_directory


class _HeartbeatLoop:
    """Keep a durable queue lease alive independently of Harbor polling."""

    def __init__(self, backend: WorkerBackend, worker_id: str, job_id: str, interval_s: float) -> None:
        if interval_s <= 0:
            raise ValueError("heartbeat interval must be positive")
        self._backend = backend
        self._worker_id = worker_id
        self._job_id = job_id
        self._interval_s = interval_s
        self._stop = threading.Event()
        self._failure: Exception | None = None
        self._started = False
        self._thread = threading.Thread(target=self._run, name=f"agentlab-heartbeat-{job_id}", daemon=True)

    def start(self) -> None:
        self._backend.heartbeat(self._worker_id, self._job_id)
        self._thread.start()
        self._started = True

    def stop(self) -> None:
        self._stop.set()
        if self._started:
            self._thread.join(timeout=max(self._interval_s * 2, 1.0))

    def check(self) -> None:
        if self._failure is not None:
            raise HarborExecutionError("benchmark lease heartbeat failed") from self._failure

    def _run(self) -> None:
        while not self._stop.wait(self._interval_s):
            try:
                self._backend.heartbeat(self._worker_id, self._job_id)
            except Exception as exc:  # backend failures must stop result submission.
                self._failure = exc
                self._stop.set()
                return


class HarborJobExecutor:
    """Start one pinned sidecar, invoke Harbor, parse official verdict, submit once."""

    def __init__(
        self,
        worker_id: str,
        backend: WorkerBackend,
        *,
        options: HarborRuntimeOptions,
        harness: HarborHarness | None = None,
        sidecars: SidecarLauncher | None = None,
        adapter_factory: Callable[[str], TaskRunnerHTTPAdapter] = TaskRunnerHTTPAdapter,
        heartbeat_interval_s: float = 20.0,
        sidecar_ready_timeout_s: float = 30.0,
        sidecar_environment: Mapping[str, str] | None = None,
    ) -> None:
        self.worker_id = worker_id
        self.backend = backend
        self.options = options
        self.harness = harness or HarborCLIHarness(options)
        self.sidecars = sidecars or DockerSidecarLauncher(endpoint_host=os.environ.get("AGENTLAB_SIDECAR_HOST", "127.0.0.1"))
        self.adapter_factory = adapter_factory
        self.heartbeat_interval_s = heartbeat_interval_s
        if sidecar_ready_timeout_s <= 0:
            raise ValueError("sidecar readiness timeout must be positive")
        self.sidecar_ready_timeout_s = sidecar_ready_timeout_s
        self.sidecar_environment = dict(sidecar_environment or {})

    def process_once(self) -> bool:
        job = self.backend.claim(self.worker_id)
        if job is None:
            return False
        self.execute_claimed(job)
        return True

    def execute_claimed(self, job: LeasedJob) -> None:
        heartbeat = _HeartbeatLoop(self.backend, self.worker_id, job.eval_job_id, self.heartbeat_interval_s)
        handle: SidecarHandle | None = None
        job_directory: Path | None = None
        try:
            job_directory = self._job_directory(job)
            heartbeat.start()
            workspace = job_directory / "sidecar-workspace"
            workspace.mkdir(parents=True, exist_ok=False)
            tool_gateway_token = secrets.token_urlsafe(32)
            handle = self.sidecars.start(self._sidecar_spec(job, workspace, tool_gateway_token))
            # The Worker performs this readiness probe before Harbor is spawned.
            # A managed sidecar name resolves only inside the private Docker
            # network, so it must not be sent through the host proxy. Keep
            # host.docker.internal proxied: direct access to it is intentionally
            # unsupported in this deployment.
            endpoint_host = _task_runner_endpoint_host(handle.endpoint)
            if endpoint_host.startswith("agentlab-sidecar-"):
                _append_private_gateway_no_proxy(os.environ, endpoint_host)
            adapter = self.adapter_factory(handle.endpoint)
            self._wait_for_sidecar(adapter)
            result_directory = self.harness.run(
                job,
                task_runner_endpoint=handle.endpoint,
                job_directory=job_directory,
                tool_gateway_token=tool_gateway_token,
            )
            heartbeat.check()
            try:
                verdict = parse_official_harbor_verdict(result_directory)
            except HarborVerifierError:
                # A Harbor run without a verifier reward is never a score. It
                # can still be diagnosed safely when the sidecar has already
                # published an allowlisted terminal error code. This preserves
                # the official-verifier gate while avoiding an opaque generic
                # agent_error for provider/task-runner failures.
                try:
                    failed_task_run = adapter.get_task(job.task.task_run_id)
                except TaskRunnerError:
                    raise
                if failed_task_run.state is not TaskRunState.SUCCEEDED:
                    raise _TaskRunnerFailure(failed_task_run) from None
                raise
            artifact_manifest = (self._persist_verdict_artifact(job, verdict),)
            try:
                task_run = adapter.get_task(job.task.task_run_id)
            except TaskRunnerError:
                # Official Harbor verifier is authoritative for reward, but an
                # unobservable sidecar means this worker cannot prove which
                # agent instance performed the task.
                raise _AgentFailure(
                    "agent task runner state is unavailable",
                    failure_code="agent_task_runner_state_unavailable",
                ) from None
            heartbeat.check()
            self.backend.submit_result(
                self.worker_id,
                JobResult(
                    eval_job_id=job.eval_job_id,
                    task_run=task_run,
                    verifier_status=verdict.status,
                    score=verdict.score,
                    official_metrics=verdict.metrics,
                    artifact_manifest=artifact_manifest,
                ),
            )
        except _TaskRunnerFailure as exc:
            self._safe_fail(job.eval_job_id, exc.failure_code)
        except _AgentFailure as exc:
            self._safe_fail(job.eval_job_id, exc.failure_code)
        except SidecarImageUnavailableError:
            self._safe_fail(job.eval_job_id, "agent_sidecar_image_unavailable")
        except SidecarError:
            self._safe_fail(job.eval_job_id, "agent_sidecar_lifecycle_error")
        except HarborVerifierError:
            self._safe_fail(job.eval_job_id, "verifier_error")
        except HarborHarnessError as exc:
            error_type = "timeout" if "timed out" in str(exc) else exc.failure_code
            self._safe_fail(job.eval_job_id, error_type)
        except TaskRunnerError:
            self._safe_fail(job.eval_job_id, "agent_task_runner_transport_error")
        except HarborExecutionError:
            # A failed heartbeat makes the claim ownership uncertain.  Do not
            # submit a result or a second state transition.
            return
        except Exception:
            # Unexpected failures are still a harness failure, but no exception
            # content is sent to AgentScope because it could contain task data.
            self._safe_fail(job.eval_job_id, "harness_unexpected_error")
        finally:
            heartbeat.stop()
            if handle is not None:
                try:
                    self.sidecars.stop(handle.container_id)
                except SidecarError:
                    # Result/failure was already settled; cleanup must not emit a
                    # second queue transition or leak Docker diagnostics.
                    pass
            if self.options.cleanup_workspaces and job_directory is not None:
                shutil.rmtree(job_directory, ignore_errors=True)

    def _wait_for_sidecar(self, adapter: TaskRunnerHTTPAdapter) -> None:
        """Wait only for transport readiness after Docker reports a container.

        A digest-pinned sidecar may bind its fixed Task Runner port shortly
        after ``docker run`` returns.  Retry connection-level errors for a
        bounded period, but fail closed immediately if the endpoint answers
        without the required v1 Harbor-gateway capability.
        """

        deadline = time.monotonic() + self.sidecar_ready_timeout_s
        while True:
            try:
                if adapter.healthcheck():
                    return
                raise _AgentFailure("agent sidecar is unhealthy", failure_code="agent_sidecar_contract_error")
            except TaskRunnerError as exc:
                if time.monotonic() >= deadline:
                    raise _AgentFailure(
                        "agent sidecar did not become ready",
                        failure_code="agent_sidecar_startup_timeout",
                    ) from exc
                time.sleep(0.25)


    def _job_directory(self, job: LeasedJob) -> Path:
        safe = _safe_component(job.eval_job_id)
        harbor = _harbor_config(job.task.execution_config)
        jobs_root = _safe_relative_path(_required_string(harbor, "jobs_root"))
        label = _optional_string(harbor, "job_name") or "agentlab"
        root = (self.options.work_root.expanduser().resolve() / jobs_root).resolve()
        work_root = self.options.work_root.expanduser().resolve()
        if root != work_root and work_root not in root.parents:
            raise HarborExecutionError("Harbor jobs root escapes worker root")
        root.mkdir(parents=True, exist_ok=True)
        directory = root / f"{_safe_component(label)}-{safe}-{uuid4().hex}"
        directory.mkdir(mode=0o700)
        return directory

    def _persist_verdict_artifact(self, job: LeasedJob, verdict: HarborVerifierVerdict) -> Mapping[str, str]:
        root = (self.options.artifact_root or (self.options.work_root / "artifacts")).expanduser().resolve()
        root.mkdir(parents=True, exist_ok=True)
        filename = f"{_safe_component(job.eval_job_id)}-{uuid4().hex}-official-verdict.json"
        target = root / filename
        # Persist a tiny allowlisted derivative rather than raw Harbor output:
        # raw result.json can contain task text, logs, or tool/provider data.
        payload = {
            "schema_version": "agentlab.harbor-official-verdict.v1",
            "verifier_status": verdict.status,
            "score": verdict.score,
            "official_metrics": _sanitize_artifact_value(verdict.metrics),
        }
        target.write_text(json.dumps(payload, sort_keys=True, separators=(",", ":")), encoding="utf-8")
        target.chmod(0o600)
        return {
            "kind": "harbor_official_verdict",
            "uri": target.as_uri(),
            "content_type": "application/json",
        }

    def _sidecar_spec(self, job: LeasedJob, workspace: Path, tool_gateway_token: str) -> SidecarSpec:
        artifact = _artifact_config(job.task.execution_config)
        framework = _required_string(artifact, "framework")
        image = _required_string(artifact, "image")
        image_digest = _required_string(artifact, "image_digest")
        network = _sidecar_network(_harbor_config(job.task.execution_config))
        # A sidecar on the worker's private Docker network must contact its
        # per-trial gateway directly. Forwarding this internal hostname through
        # the host HTTP proxy makes the gateway unreachable and incorrectly
        # turns an otherwise valid Harbor task into an agent failure.
        gateway_host = os.environ.get("AGENTLAB_TOOL_GATEWAY_HOST", "") if network else ""
        environment = _sidecar_environment(self.sidecar_environment, no_proxy_host=gateway_host)
        # Model identity is part of the immutable execution snapshot, not a
        # mutable Worker default. In particular, a framework may require a
        # provider-specific literal spelling for the same deployment.
        environment["LLM_MODEL"] = _model_profile_model(job.task)
        # The Task Runner process owns one benchmark task, so its native
        # OpenHands bridge can safely read immutable correlation from its
        # environment. Keep this separate from the HTTP request body, which
        # never accepts AgentScope credentials or process configuration.
        environment.update({
            "AGENTSCOPE_EXPERIMENT_ID": job.task.correlation.experiment_id,
            "AGENTSCOPE_EVAL_JOB_ID": job.task.correlation.eval_job_id,
            "AGENTSCOPE_BENCHMARK_KEY": "harbor",
            "AGENTSCOPE_BENCHMARK_TASK_ID": job.task.task_id,
            "AGENTSCOPE_TRIAL_INDEX": str(job.task.correlation.trial_index),
        })
        # This is a single-task capability, not a project/model credential.
        # It authorizes only the Harbor tool gateway and never enters a request
        # body, image layer, catalog, or log.
        environment["AGENTLAB_TOOL_GATEWAY_TOKEN"] = tool_gateway_token
        return SidecarSpec(
            framework=framework,
            image=image,
            image_digest=image_digest,
            workspace_host_path=workspace,
            workspace_container_path="/workspace",
            environment=environment,
            network=network,
        )

    def _safe_fail(self, job_id: str, error_type: str) -> None:
        try:
            self.backend.fail(self.worker_id, job_id, error_type)
        except Exception:
            # The process is allowed to return to the polling loop.  The job's
            # durable lease will be reconciled by the control plane.
            return


class _AgentFailure(HarborExecutionError):
    """Credential-safe agent/sidecar failure with an allowlisted category."""

    def __init__(self, message: str, *, failure_code: str = "agent_error") -> None:
        self.failure_code = failure_code
        super().__init__(message)


class _TaskRunnerFailure(_AgentFailure):
    """Safe terminal sidecar failure observed after Harbor lacks a verdict."""

    def __init__(self, task_run: TaskRun) -> None:
        failure_code = _task_runner_failure_code(task_run)
        super().__init__(failure_code, failure_code=failure_code)


def _task_runner_failure_code(task_run: TaskRun) -> str:
    code = task_run.error_code
    if isinstance(code, str) and re.fullmatch(r"[a-z][a-z0-9_]{0,48}", code):
        return f"agent_{code}"
    if task_run.state is TaskRunState.TIMED_OUT:
        return "agent_timeout"
    if task_run.state is TaskRunState.CANCELLED:
        return "agent_cancelled"
    return "agent_task_runner_failed"


def _harbor_process_failure_category(stdout: bytes, stderr: bytes) -> str:
    """Classify Harbor output without exposing any of its text."""

    text = (stdout + b"\n" + stderr).decode("utf-8", errors="replace").lower()
    if "no module named" in text or "modulenotfounderror" in text:
        return "runtime_dependency_error"
    if "cannot import" in text or "importerror" in text or "failed to import" in text:
        return "agent_import_error"
    if "task runner" in text or "task_runner" in text:
        return "task_runner_error"
    if "agentlab-sidecar" in text and ("connection" in text or "proxy" in text or "network" in text):
        return "sidecar_connectivity_error"
    if "include-task-name" in text or "task name" in text or "no tasks" in text:
        return "task_selection_error"
    if "unknown option" in text or "unrecognized arguments" in text:
        return "cli_contract_error"
    if "timeout" in text or "timed out" in text:
        return "timeout"
    return "harbor_cli_error"


def _harbor_config(execution_config: Mapping[str, Any] | None) -> Mapping[str, Any]:
    config = execution_config or {}
    execution = config.get("execution")
    if isinstance(execution, Mapping):
        config = execution
    harbor = config.get("harbor")
    if not isinstance(harbor, Mapping):
        raise HarborHarnessError("immutable execution config is missing Harbor settings", failure_code="harness_config_missing")
    return harbor


def _artifact_config(execution_config: Mapping[str, Any] | None) -> Mapping[str, Any]:
    config = execution_config or {}
    artifact = config.get("agent_artifact")
    if not isinstance(artifact, Mapping):
        raise _AgentFailure(
            "immutable execution config is missing agent artifact",
            failure_code="agent_artifact_missing",
        )
    return artifact


def _model_profile_model(task: TaskSpec) -> str:
    """Return the literal runtime model fixed in an execution snapshot.

    The worker must not substitute an ambient ``LLM_MODEL`` value: that would
    make a catalog template non-reproducible and can change OpenAI-compatible
    provider routing. Only the model identifier is copied to the sidecar;
    credentials remain runtime-only worker environment values.
    """

    config = task.execution_config or {}
    profile = config.get("model_profile")
    if not isinstance(profile, Mapping):
        raise _AgentFailure(
            "immutable execution config is missing model profile",
            failure_code="agent_model_profile_missing",
        )
    profile_id = profile.get("id")
    profile_config = profile.get("config")
    if profile_id != task.model_profile_id or not isinstance(profile_config, Mapping):
        raise _AgentFailure(
            "immutable execution model profile is invalid",
            failure_code="agent_model_profile_invalid",
        )
    model = profile_config.get("model")
    if not isinstance(model, str) or not model.strip() or "\x00" in model or len(model) > 512:
        raise _AgentFailure(
            "immutable execution model identifier is invalid",
            failure_code="agent_model_profile_invalid",
        )
    return model.strip()


def _required_string(values: Mapping[str, Any], key: str) -> str:
    value = values.get(key)
    if not isinstance(value, str) or not value.strip():
        raise HarborHarnessError(f"required Harbor setting {key} is missing", failure_code="harness_config_required_setting_missing")
    if "\x00" in value:
        raise HarborHarnessError(f"Harbor setting {key} is invalid", failure_code="harness_config_setting_invalid")
    return value.strip()


def _optional_string(values: Mapping[str, Any], key: str) -> str | None:
    value = values.get(key)
    if value is None:
        return None
    if not isinstance(value, str) or not value.strip() or "\x00" in value:
        raise HarborHarnessError(f"Harbor setting {key} is invalid", failure_code="harness_config_setting_invalid")
    return value.strip()


def _positive_int(value: Any, default: int) -> int:
    if value is None:
        return default
    if isinstance(value, bool) or not isinstance(value, int) or value <= 0:
        raise HarborHarnessError("Harbor timeout must be a positive integer", failure_code="harness_timeout_invalid")
    return value


def _task_runner_endpoint_host(value: str) -> str:
    parsed = urlparse(value)
    return parsed.hostname or ""


def _is_worker_local_sidecar_endpoint(value: str) -> bool:
    return bool(
        re.fullmatch(
            r"http://(?:127\.0\.0\.1|host\.docker\.internal|agentlab-sidecar-[a-f0-9]{24}):[1-9][0-9]{0,4}",
            value,
        )
    )


def _safe_component(value: str) -> str:
    compact = re.sub(r"[^A-Za-z0-9_.-]+", "-", value).strip(".-")
    if not compact:
        raise HarborHarnessError("eval job id is invalid", failure_code="harness_job_id_invalid")
    return compact[:96]


def _safe_relative_path(value: str) -> Path:
    path = Path(value)
    if path.is_absolute() or any(part in {"", ".", ".."} for part in path.parts):
        raise HarborHarnessError("Harbor jobs root is unsafe", failure_code="harness_relative_path_invalid")
    return path


def _catalog_task_path(config: Mapping[str, Any], catalog_root: Path | None) -> Path | None:
    raw = _optional_string(config, "task_path")
    if raw is None:
        return None
    if catalog_root is None:
        raise HarborHarnessError("Harbor task_path requires a configured catalog root", failure_code="harness_task_catalog_root_missing")
    candidate = (catalog_root.expanduser().resolve() / _safe_relative_path(raw)).resolve()
    root = catalog_root.expanduser().resolve()
    if candidate != root and root not in candidate.parents:
        raise HarborHarnessError("Harbor task path escapes catalog root", failure_code="harness_task_path_escape")
    if not candidate.is_dir():
        raise HarborHarnessError("Harbor task path does not exist", failure_code="harness_task_path_missing")
    return candidate


def _harbor_task_name_filter(dataset: str | None, task_id: str) -> str:
    """Match Harbor package task names while keeping local paths unchanged."""

    if dataset is None or "/" not in dataset:
        return task_id
    dataset_name = dataset.split("@", 1)[0].rstrip("/")
    task_namespace = dataset_name.split("/", 1)[0]
    if task_id.startswith(task_namespace + "/"):
        return task_id
    return f"{task_namespace}/{task_id}"


def _catalog_task_path_root(config: Mapping[str, Any], catalog_root: Path | None, task_id: str) -> Path | None:
    raw = _optional_string(config, "task_path_root")
    if raw is None:
        return None
    if catalog_root is None:
        raise HarborHarnessError("Harbor task_path_root requires a configured catalog root", failure_code="harness_task_catalog_root_missing")
    if not _SAFE_JOB_COMPONENT.fullmatch(task_id):
        raise HarborHarnessError("Harbor task id is invalid for task_path_root", failure_code="harness_task_id_invalid")
    root = catalog_root.expanduser().resolve()
    candidate_root = (root / _safe_relative_path(raw)).resolve()
    candidate = (candidate_root / task_id).resolve()
    if candidate_root != root and root not in candidate_root.parents:
        raise HarborHarnessError("Harbor task path root escapes catalog root", failure_code="harness_task_path_escape")
    if candidate != candidate_root and candidate_root not in candidate.parents:
        raise HarborHarnessError("Harbor task path escapes configured task root", failure_code="harness_task_path_escape")
    if not candidate.is_dir():
        raise HarborHarnessError("Harbor task path does not exist", failure_code="harness_task_path_missing")
    return candidate


def _catalog_config_path(config: Mapping[str, Any], catalog_root: Path | None) -> Path | None:
    raw = _optional_string(config, "config_path")
    if raw is None:
        return None
    if catalog_root is None:
        raise HarborHarnessError("Harbor config_path requires a configured catalog root", failure_code="harness_config_catalog_root_missing")
    candidate = (catalog_root.expanduser().resolve() / _safe_relative_path(raw)).resolve()
    root = catalog_root.expanduser().resolve()
    if candidate != root and root not in candidate.parents:
        raise HarborHarnessError("Harbor config path escapes catalog root", failure_code="harness_config_path_escape")
    if not candidate.is_file():
        raise HarborHarnessError("Harbor config path does not exist", failure_code="harness_config_path_missing")
    return candidate


def _has_result_artifact(directory: Path) -> bool:
    return (directory / "result.json").is_file() or (directory / "verifier" / "reward.json").is_file() or (directory / "verifier" / "reward.txt").is_file()



def _sanitize_artifact_value(value: Any) -> Any:
    """Return a bounded, secret-free JSON-compatible metrics copy."""

    if isinstance(value, Mapping):
        return {
            str(key): _sanitize_artifact_value(child)
            for key, child in value.items()
            if not _SECRET_KEY.search(str(key))
        }
    if isinstance(value, list):
        return [_sanitize_artifact_value(child) for child in value]
    if isinstance(value, tuple):
        return [_sanitize_artifact_value(child) for child in value]
    if isinstance(value, (str, int, float, bool)) or value is None:
        return value
    return str(value)


def _sidecar_network(config: Mapping[str, Any]) -> str | None:
    # Network identity is a local deployment detail, never a catalog-provided
    # command or environment map.  Compose workers set this to agentlab_default
    # so an ephemeral sidecar can reach benchmarks-worker's in-process gateway.
    configured = _optional_string(config, "sidecar_network")
    if configured is not None:
        return configured
    raw = os.environ.get("AGENTLAB_SIDECAR_NETWORK", "").strip()
    if not raw:
        return None
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,127}", raw):
        raise HarborHarnessError("sidecar network is invalid", failure_code="harness_sidecar_network_invalid")
    return raw


def _sidecar_environment(overrides: Mapping[str, str], *, no_proxy_host: str = "") -> dict[str, str]:
    """Pick runtime-only agent values from the Worker environment.

    Catalog data never influences this list.  Keeping it narrow prevents a
    user-provided template from smuggling process-wide settings or Docker
    credentials into an agent container.
    """

    allowed = {
        "AGENTSCOPE_SERVER_URL",
        "AGENTSCOPE_PROJECT_TOKEN",
        "AGENTSCOPE_RUNTIME_TOKEN",
        # Hermes owns the AgentScope SDK initialization in its native source.
        # These non-secret controls must survive the Worker → sidecar boundary
        # so benchmark runs register and emit their correlated native traces.
        "HERMES_AGENTSCOPE_ENABLED",
        "HERMES_AGENTSCOPE_AGENT_NAME",
        "AGENTSCOPE_CAPTURE_CONTENT",
        "HERMES_TASK_RUNNER_DISABLE_STREAMING",
        # OpenHands is opt-in. Forward its explicit Worker setting so its
        # native Conversation bridge observes task-runner benchmark work.
        "OPENHANDS_AGENTSCOPE_ENABLED",
        "AGENTSCOPE_ENVIRONMENT",
        "ARK_API_KEY",
        "ARK_BASE_URL",
        "LLM_API_KEY",
        "LLM_MODEL",
        "LLM_BASE_URL",
        "OPENAI_API_KEY",
        "OPENAI_BASE_URL",
        "HTTP_PROXY",
        "HTTPS_PROXY",
        "NO_PROXY",
    }
    merged = {key: value for key, value in os.environ.items() if key in allowed and value}
    for key, value in overrides.items():
        if key not in allowed or not isinstance(value, str) or not value:
            continue
        merged[key] = value
    # AgentLab's local .env historically uses ARK_API_KEY.  Owned agent forks
    # consume the provider-neutral LLM_* variables, so translate at the
    # process boundary without exposing either value in CLI/config/logs.
    if "LLM_API_KEY" not in merged and merged.get("ARK_API_KEY"):
        merged["LLM_API_KEY"] = merged["ARK_API_KEY"]
    if "LLM_BASE_URL" not in merged and merged.get("ARK_BASE_URL"):
        merged["LLM_BASE_URL"] = merged["ARK_BASE_URL"]
    # Some owned runtimes accept OpenAI-compatible environment names while
    # OpenHands and Hermes consume LLM_*. Derive the aliases only inside the
    # private sidecar environment; neither name is placed in catalog YAML, CLI
    # arguments, artifacts, or browser responses.
    if "OPENAI_API_KEY" not in merged and merged.get("LLM_API_KEY"):
        merged["OPENAI_API_KEY"] = merged["LLM_API_KEY"]
    if "OPENAI_BASE_URL" not in merged and merged.get("LLM_BASE_URL"):
        merged["OPENAI_BASE_URL"] = merged["LLM_BASE_URL"]
    _append_private_gateway_no_proxy(merged, no_proxy_host)
    return merged


def _append_private_gateway_no_proxy(environment: dict[str, str], host: str) -> None:
    """Bypass HTTP proxies only for a validated Worker-network gateway host."""

    host = host.strip()
    if not host or host == "host.docker.internal":
        return
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9.-]{0,252}", host):
        raise HarborHarnessError("tool gateway host is invalid", failure_code="harness_tool_gateway_host_invalid")
    existing = [item.strip() for item in environment.get("NO_PROXY", "").split(",") if item.strip()]
    if host not in existing:
        existing.append(host)
    environment["NO_PROXY"] = ",".join(existing)


def _harbor_environment(source: Mapping[str, str]) -> dict[str, str]:
    """Minimal Harbor process environment; do not forward project/model secrets."""

    safe = {key: value for key, value in source.items() if not _SECRET_KEY.search(key)}
    # Harbor must preserve PATH/home/temp settings to run, but no provider or
    # AgentScope credential is required by the worker-side custom agent.
    return safe
