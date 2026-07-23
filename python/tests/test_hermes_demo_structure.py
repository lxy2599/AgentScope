"""Contract tests for the real Hermes AgentScope demonstration entrypoint."""

from __future__ import annotations

from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]


def test_hermes_setup_pins_upstream_commit_and_installs_both_projects():
    content = (ROOT / "scripts" / "setup-hermes-agent.sh").read_text()

    assert "d9165d7a678d4105f42921a7fc1886df3804531b" in content
    assert "git clone" in content
    assert "checkout --detach" in content
    assert '"${ROOT_DIR}/python"' in content


def test_hermes_demo_requires_real_model_configuration_and_emits_single_result_file():
    content = (ROOT / "scripts" / "demo-hermes-agentops.sh").read_text()

    assert "HERMES_LLM_BASE_URL" in content
    assert "HERMES_LLM_API_KEY" in content
    assert "HERMES_LLM_MODEL" in content
    assert "CCSWITCH_OPENAI_BASE_URL" in content
    assert "hermes_agentops_demo.py" in content
    assert "HERMES_RESULT_FILE" in content


def test_hermes_demo_creates_success_and_failure_runs_then_verifies_platform_evidence():
    content = (ROOT / "python" / "examples" / "hermes_agentops_demo.py").read_text()

    for required in (
        "AIAgent",
        "HermesAdapter",
        "agent_init",
        "force_flush",
        "annotation-items",
        "trace_spans",
        "trace_llm_calls",
        "trace_tool_calls",
        "trace_final_answers",
        "failed_run_id",
        "success_run_id",
    ):
        assert required in content


def test_makefile_exposes_one_command_for_real_hermes_demo():
    content = (ROOT / "Makefile").read_text()

    assert "demo-hermes-agentops:" in content
    assert "./scripts/demo-hermes-agentops.sh" in content
