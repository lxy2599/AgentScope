from __future__ import annotations

from pathlib import Path

from benchmark_worker.cli import main


def test_worker_dry_run_requires_runtime_token_and_never_prints_it(monkeypatch, capsys) -> None:
    token = "runtime-project-token"
    monkeypatch.setenv("AGENTSCOPE_PROJECT_TOKEN", token)

    assert main(["--server-url", "http://platform.test", "--worker-id", "harbor-local", "--dry-run"]) == 0
    captured = capsys.readouterr()
    assert "benchmark-worker dry-run ready: worker=harbor-local execution_kind=harbor" in captured.out
    assert token not in captured.out
    assert token not in captured.err


def test_worker_registers_then_uses_real_harbor_executor(monkeypatch, capsys, tmp_path) -> None:
    import benchmark_worker.cli as worker_cli

    token = "runtime-project-token"
    monkeypatch.setenv("AGENTSCOPE_PROJECT_TOKEN", token)
    events: list[tuple[str, str]] = []

    class FakePlatformClient:
        def __init__(self, server_url: str) -> None:
            assert server_url == "http://platform.test"

        def register_benchmark_worker(self, project_token: str, runner_name: str) -> dict:
            assert project_token == token
            events.append(("register", runner_name))
            return {"status": "registered"}

    created: dict[str, object] = {}

    class FakeExecutor:
        def __init__(self, worker_id: str, backend: object, *, options: object, heartbeat_interval_s: float) -> None:
            created.update(worker_id=worker_id, backend=backend, options=options, heartbeat_interval_s=heartbeat_interval_s)

        def process_once(self) -> bool:
            events.append(("process", "harbor-local"))
            return True

    monkeypatch.setattr(worker_cli, "PlatformClient", FakePlatformClient)
    monkeypatch.setattr(worker_cli, "HarborJobExecutor", FakeExecutor)

    assert worker_cli.main([
        "--server-url", "http://platform.test", "--worker-id", "harbor-local", "--work-root", str(tmp_path),
        "--heartbeat-interval-s", "17", "--once",
    ]) == 0
    captured = capsys.readouterr()
    assert events == [("register", "harbor-local"), ("process", "harbor-local")]
    assert created["worker_id"] == "harbor-local"
    assert created["heartbeat_interval_s"] == 17
    assert created["options"].catalog_root is None
    assert created["options"].artifact_root == tmp_path / "artifacts"
    assert "Harbor execution is not implemented" not in captured.out
    assert "execution_kind=harbor" in captured.out
    assert token not in captured.out
    assert token not in captured.err


def test_worker_passes_configured_host_visible_catalog_root(monkeypatch, tmp_path) -> None:
    import benchmark_worker.cli as worker_cli

    monkeypatch.setenv("AGENTSCOPE_PROJECT_TOKEN", "runtime-project-token")
    monkeypatch.setenv("AGENTLAB_BENCHMARK_CATALOG_ROOT", str(tmp_path))
    created: dict[str, object] = {}

    class FakePlatformClient:
        def __init__(self, server_url: str) -> None:
            del server_url

        def register_benchmark_worker(self, project_token: str, runner_name: str) -> dict:
            del project_token, runner_name
            return {"status": "registered"}

    class FakeExecutor:
        def __init__(self, worker_id: str, backend: object, *, options: object, heartbeat_interval_s: float) -> None:
            del worker_id, backend, heartbeat_interval_s
            created["options"] = options

        def process_once(self) -> bool:
            return False

    monkeypatch.setattr(worker_cli, "PlatformClient", FakePlatformClient)
    monkeypatch.setattr(worker_cli, "HarborJobExecutor", FakeExecutor)

    assert worker_cli.main(["--server-url", "http://platform.test", "--worker-id", "harbor-local", "--once"]) == 0
    assert created["options"].catalog_root == tmp_path.resolve()
    assert created["options"].artifact_root == Path(".agentlab/benchmark-worker") / "artifacts"


def test_worker_rejects_nonexistent_catalog_root(monkeypatch, tmp_path, capsys) -> None:
    monkeypatch.setenv("AGENTSCOPE_PROJECT_TOKEN", "runtime-project-token")
    monkeypatch.setenv("AGENTLAB_BENCHMARK_CATALOG_ROOT", str(tmp_path / "missing"))

    # Validate this deployment path before a runner registers or claims work.
    assert main(["--server-url", "http://platform.test", "--worker-id", "harbor-local", "--dry-run"]) == 2
    captured = capsys.readouterr()
    assert "AGENTLAB_BENCHMARK_CATALOG_ROOT" in captured.err
