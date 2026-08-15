"""Regression coverage for the one-shot Harbor Worker network contract."""

from __future__ import annotations

import importlib.util
from pathlib import Path
from types import ModuleType


def _load_smoke_module() -> ModuleType:
    path = Path(__file__).parents[1] / "scripts" / "run_real_agent_harbor_smoke.py"
    spec = importlib.util.spec_from_file_location("run_real_agent_harbor_smoke", path)
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_one_shot_worker_uses_compose_control_plane_dns(tmp_path: Path) -> None:
    smoke = _load_smoke_module()

    command = smoke._worker_command(
        root=tmp_path / "catalog",
        work_root=tmp_path / "work",
        worker_image="agentlab/benchmarks-worker:local",
        runner_token="test-token",
        worker_name="harbor-real-smoke-123",
        model_env={"LLM_API_KEY": "not-printed", "LLM_MODEL": "test-model"},
    )

    server_url_index = command.index("--server-url") + 1
    assert command[server_url_index] == "http://agentscope:8080"
    assert "AGENTSCOPE_SERVER_URL=http://agentscope:8080" in command
    assert "AGENTSCOPE_SERVER_URL=http://host.docker.internal:18080" not in command
