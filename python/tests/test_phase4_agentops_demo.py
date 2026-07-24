from __future__ import annotations

import importlib.util
import json
from pathlib import Path

import pytest


def load_demo_module():
    path = Path(__file__).parents[1] / "examples" / "phase4_agentops_demo.py"
    spec = importlib.util.spec_from_file_location("phase4_agentops_demo", path)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def test_load_openhands_result_requires_closed_loop_identity(tmp_path: Path) -> None:
    demo = load_demo_module()
    result_path = tmp_path / "openhands.json"
    result_path.write_text(
        json.dumps(
            {
                "email": "demo@example.com",
                "password": "secret",
                "project_id": "project-1",
                "agent_id": "agent-1",
                "run_id": "run-1",
                "trace_id": "trace-1",
                "snapshot_id": "snapshot-1",
                "sse_semantic_before_finish": True,
            }
        ),
        encoding="utf-8",
    )

    result = demo.load_openhands_result(result_path)

    assert result["run_id"] == "run-1"
    result_path.write_text("{}", encoding="utf-8")
    with pytest.raises(ValueError, match="missing fields"):
        demo.load_openhands_result(result_path)


def test_build_judge_configs_reference_env_without_secret(monkeypatch) -> None:
    demo = load_demo_module()
    monkeypatch.setenv("OPENHANDS_LLM_API_KEY", "super-secret-value")
    monkeypatch.setenv("OPENHANDS_LLM_BASE_URL", "https://judge.example/v1")
    monkeypatch.setenv("OPENHANDS_LLM_MODEL", "openai/demo-model")

    configs = demo.build_judge_configs()
    encoded = json.dumps(configs)

    assert len(configs) == 2
    assert all(item["api_key_env"] == "OPENHANDS_LLM_API_KEY" for item in configs)
    assert "super-secret-value" not in encoded
    assert {item["prompt_version"] for item in configs} == {
        "phase4-primary-v1",
        "phase4-reviewer-v1",
    }


def test_public_result_rejects_credentials() -> None:
    demo = load_demo_module()

    with pytest.raises(ValueError, match="credential"):
        demo.validate_public_result(
            {
                "project_id": "project-1",
                "project_token": "ascope_secret",
            }
        )

    demo.validate_public_result(
        {
            "email": "demo@example.com",
            "project_id": "project-1",
            "run_id": "run-1",
        }
    )
