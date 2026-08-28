from __future__ import annotations

import json
from pathlib import Path

from benchmarkctl.cli import main

ROOT = Path(__file__).resolve().parents[1]


def test_validate_cli(capsys) -> None:
    assert main(["validate", str(ROOT)]) == 0
    assert "validated catalog: 8 run template(s)" in capsys.readouterr().out


def test_catalog_sync_writes_manifest(tmp_path: Path) -> None:
    output = tmp_path / "catalog.json"
    assert main(["catalog-sync", str(ROOT), "--output", str(output)]) == 0
    payload = json.loads(output.read_text(encoding="utf-8"))
    assert payload["schema_version"] == "v1"
    assert payload["catalog_sha256"]


def test_run_compiles_request(capsys) -> None:
    assert main(["run", str(ROOT), "--template", "openhands-terminal-bench-smoke"]) == 0
    payload = json.loads(capsys.readouterr().out)
    assert payload["execution"] == "not_submitted"
    assert payload["effective_run"]["template_id"] == "openhands-terminal-bench-smoke"
    assert payload["effective_run"]["execution"]["task_timeout_s"] == 900


def test_catalog_sync_remote_cli_reads_cookie_only_from_environment(monkeypatch, capsys) -> None:
    import benchmark_worker.platform as platform

    calls: list[tuple[str, dict, str]] = []

    class FakePlatformClient:
        def __init__(self, server_url: str) -> None:
            assert server_url == "http://platform.test"

        def sync_catalog(self, project_id: str, manifest: dict, cookie: str) -> dict:
            calls.append((project_id, manifest, cookie))
            return {"status": "ok"}

    cookie = "session=only-from-environment"
    monkeypatch.setenv("AGENTSCOPE_SESSION_COOKIE", cookie)
    monkeypatch.setattr(platform, "PlatformClient", FakePlatformClient)

    assert main(["catalog-sync", str(ROOT), "--server-url", "http://platform.test", "--project-id", "project-1"]) == 0
    captured = capsys.readouterr()
    assert calls[0][0] == "project-1"
    assert calls[0][1]["schema_version"] == "v1"
    assert calls[0][2] == cookie
    assert cookie not in captured.out
    assert cookie not in captured.err
