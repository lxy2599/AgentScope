"""Docker sidecar lifecycle used by the Harbor Worker.

The launcher accepts only an immutable artifact image/digest selected by the
queued execution snapshot. It deliberately has no npm/pip install path.
"""

from __future__ import annotations

import json
import re
import secrets
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping, Protocol


_DIGEST = re.compile(r"^sha256:[a-f0-9]{64}$")
_SAFE_IMAGE = re.compile(r"^[A-Za-z0-9][A-Za-z0-9._/:+-]{0,511}$")
_SAFE_CONTAINER_ID = re.compile(r"^[a-f0-9]{12,128}$")


class SidecarError(RuntimeError):
    """Safe lifecycle error; command stdout/stderr and environment are excluded."""


class DockerCommandRunner(Protocol):
    def run(self, args: list[str], *, timeout_s: float) -> subprocess.CompletedProcess[str]: ...


class SubprocessDockerRunner:
    def run(self, args: list[str], *, timeout_s: float) -> subprocess.CompletedProcess[str]:
        try:
            return subprocess.run(["docker", *args], check=False, capture_output=True, text=True, timeout=timeout_s)  # noqa: S603 -- fixed executable; all arguments validated/owned.
        except (OSError, subprocess.TimeoutExpired) as exc:
            raise SidecarError("docker command could not be completed") from exc


@dataclass(frozen=True)
class SidecarSpec:
    framework: str
    image: str
    image_digest: str
    workspace_host_path: Path
    workspace_container_path: str
    environment: Mapping[str, str]
    network: str | None = None
    container_port: int | None = None

    @property
    def image_ref(self) -> str:
        return f"{self.image}@{self.image_digest}"


@dataclass(frozen=True)
class SidecarHandle:
    container_id: str
    endpoint: str


class DockerSidecarLauncher:
    """Start/stop one fixed image Task Runner sidecar without shell invocation."""

    def __init__(
        self,
        runner: DockerCommandRunner | None = None,
        *,
        timeout_s: float = 30.0,
        endpoint_host: str = "127.0.0.1",
    ) -> None:
        if endpoint_host not in {"127.0.0.1", "host.docker.internal"}:
            raise ValueError("sidecar endpoint host must be a loopback or Docker host-gateway address")
        self.runner = runner or SubprocessDockerRunner()
        self.timeout_s = timeout_s
        self.endpoint_host = endpoint_host

    def start(self, spec: SidecarSpec) -> SidecarHandle:
        self._validate(spec)
        port = spec.container_port or _task_runner_port(spec.framework)
        managed_name = _managed_sidecar_name() if spec.network else None
        args = [
            "run", "--detach", "--rm", "--init",
            "--label", "agentlab.managed=true",
            "--label", f"agentlab.framework={spec.framework}",
            "--mount", f"type=bind,src={spec.workspace_host_path},dst={spec.workspace_container_path}",
            "--publish", f"127.0.0.1::{port}",
            "--add-host", "host.docker.internal:host-gateway",
        ]
        if spec.network:
            # A worker in a Docker container cannot reach a sibling's port that
            # is deliberately published only to host loopback. Give the
            # per-trial sidecar an unguessable managed DNS name on the Worker
            # network instead; no external port is exposed by this endpoint.
            args.extend(["--network", spec.network, "--name", managed_name])
        for name, value in sorted(spec.environment.items()):
            if not name or "=" in name or "\x00" in value:
                raise SidecarError("invalid sidecar environment declaration")
            args.extend(["--env", f"{name}={value}"])
        # Artifact images define their own immutable Task Runner ENTRYPOINT/CMD.
        # Supplying a framework command here would override that image contract
        # (and would regress OpenClaw back to pnpm/source execution).
        args.append(spec.image_ref)
        result = self.runner.run(args, timeout_s=self.timeout_s)
        if result.returncode != 0:
            raise SidecarError("sidecar start failed")
        container_id = result.stdout.strip()
        if not _SAFE_CONTAINER_ID.fullmatch(container_id):
            raise SidecarError("docker returned an invalid sidecar container id")
        try:
            endpoint = self._endpoint(container_id, port, managed_name)
        except Exception:
            self.stop(container_id)
            raise
        return SidecarHandle(container_id=container_id, endpoint=endpoint)

    def stop(self, container_id: str) -> None:
        if not _SAFE_CONTAINER_ID.fullmatch(container_id):
            raise SidecarError("invalid sidecar container id")
        result = self.runner.run(["rm", "--force", container_id], timeout_s=self.timeout_s)
        if result.returncode != 0:
            raise SidecarError("sidecar cleanup failed")

    def _endpoint(self, container_id: str, port: int, managed_name: str | None) -> str:
        if managed_name is not None:
            return f"http://{managed_name}:{port}"
        result = self.runner.run(["inspect", "--format", "{{json .NetworkSettings.Ports}}", container_id], timeout_s=self.timeout_s)
        if result.returncode != 0:
            raise SidecarError("could not inspect sidecar endpoint")
        try:
            ports = json.loads(result.stdout)
            bindings = ports[f"{port}/tcp"]
            binding = bindings[0]
            host_ip, host_port = binding["HostIp"], binding["HostPort"]
        except (KeyError, IndexError, TypeError, json.JSONDecodeError) as exc:
            raise SidecarError("sidecar does not expose its task runner port") from exc
        if host_ip not in {"127.0.0.1", "0.0.0.0", "::"} or not isinstance(host_port, str) or not host_port.isdigit():
            raise SidecarError("sidecar task runner port binding is unsafe")
        return f"http://{self.endpoint_host}:{host_port}"

    @staticmethod
    def _validate(spec: SidecarSpec) -> None:
        if spec.framework not in {"openhands", "openclaw", "hermes", "harbor-sentinel"}:
            raise SidecarError("unsupported artifact framework")
        if not _SAFE_IMAGE.fullmatch(spec.image) or "@" in spec.image:
            raise SidecarError("artifact image must be a safe tag reference without a digest")
        if not _DIGEST.fullmatch(spec.image_digest):
            raise SidecarError("artifact image digest must be a sha256 digest")
        if not spec.workspace_host_path.is_absolute() or not spec.workspace_host_path.is_dir():
            raise SidecarError("workspace host path must be an existing absolute directory")
        if not spec.workspace_container_path.startswith("/"):
            raise SidecarError("workspace container path must be absolute")
        if spec.container_port is not None and (
            isinstance(spec.container_port, bool) or not isinstance(spec.container_port, int) or not 1 <= spec.container_port <= 65535
        ):
            raise SidecarError("sidecar task runner port is invalid")


def _managed_sidecar_name() -> str:
    return f"agentlab-sidecar-{secrets.token_hex(12)}"


def _task_runner_port(framework: str) -> int:
    if framework == "openhands":
        return 8001
    if framework == "openclaw":
        return 8091
    if framework == "hermes":
        return 8001
    return 8082
