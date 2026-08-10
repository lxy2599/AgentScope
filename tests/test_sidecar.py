from __future__ import annotations

import json
import subprocess
from dataclasses import dataclass, field
from pathlib import Path

import pytest

from benchmark_worker.sidecar import DockerSidecarLauncher, SidecarError, SidecarSpec


@dataclass
class FakeDocker:
    results: list[subprocess.CompletedProcess[str]]
    calls: list[list[str]] = field(default_factory=list)

    def run(self, args: list[str], *, timeout_s: float) -> subprocess.CompletedProcess[str]:
        del timeout_s
        self.calls.append(args)
        return self.results.pop(0)


def completed(args: list[str], stdout: str = "", code: int = 0) -> subprocess.CompletedProcess[str]:
    return subprocess.CompletedProcess(args, code, stdout=stdout, stderr="ignored")


def spec(workspace: Path) -> SidecarSpec:
    return SidecarSpec(
        framework="openhands", image="local/openhands:dev", image_digest="sha256:" + "a" * 64,
        workspace_host_path=workspace, workspace_container_path="/workspace",
        environment={"LLM_API_KEY": "not-printed", "HTTP_PROXY": "http://host.docker.internal:7897"}, network="agentlab_default",
    )


def test_sidecar_launcher_pins_digest_and_uses_managed_network_endpoint(tmp_path: Path) -> None:
    container = "a" * 64
    docker = FakeDocker([completed(["run"], stdout=container + "\n")])
    handle = DockerSidecarLauncher(docker).start(spec(tmp_path))
    assert handle.endpoint.startswith("http://agentlab-sidecar-")
    assert handle.endpoint.endswith(":8001")
    run_args = docker.calls[0]
    assert "local/openhands:dev@sha256:" + "a" * 64 in run_args
    assert "127.0.0.1::8001" in run_args
    assert "--add-host" in run_args and "host.docker.internal:host-gateway" in run_args
    assert "--name" in run_args
    assert any(item == "LLM_API_KEY=not-printed" for item in run_args)


def test_sidecar_launcher_rejects_unpinned_or_missing_workspace(tmp_path: Path) -> None:
    bad = spec(tmp_path / "missing")
    with pytest.raises(SidecarError, match="workspace"):
        DockerSidecarLauncher(FakeDocker([])).start(bad)
    bad_digest = SidecarSpec(**{**spec(tmp_path).__dict__, "image_digest": "latest"})
    with pytest.raises(SidecarError, match="digest"):
        DockerSidecarLauncher(FakeDocker([])).start(bad_digest)


def test_sidecar_launcher_does_not_include_docker_stderr_in_error(tmp_path: Path) -> None:
    docker = FakeDocker([completed(["run"], code=1)])
    with pytest.raises(SidecarError) as caught:
        DockerSidecarLauncher(docker).start(spec(tmp_path))
    assert "ignored" not in str(caught.value)


def test_sidecar_launcher_uses_artifact_entrypoint_without_runtime_source_commands(tmp_path: Path) -> None:
    container = "b" * 64
    docker = FakeDocker([completed(["run"], stdout=container + "\n")])

    DockerSidecarLauncher(docker).start(spec(tmp_path))

    run_args = docker.calls[0]
    image_index = run_args.index("local/openhands:dev@sha256:" + "a" * 64)
    assert run_args[image_index + 1 :] == []
    assert "pnpm" not in run_args
    assert "openhands-task-runner" not in run_args


def test_sidecar_launcher_can_return_host_gateway_endpoint_for_containerized_worker(tmp_path: Path) -> None:
    container = "c" * 64
    docker = FakeDocker([
        completed(["run"], stdout=container + "\n"),
        completed(["inspect"], stdout=json.dumps({"8001/tcp": [{"HostIp": "127.0.0.1", "HostPort": "43125"}]})),
    ])
    local = SidecarSpec(**{**spec(tmp_path).__dict__, "network": None})

    handle = DockerSidecarLauncher(docker, endpoint_host="host.docker.internal").start(local)

    assert handle.endpoint == "http://host.docker.internal:43125"
