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


def test_hermes_demo_requires_real_model_configuration_and_separates_private_credentials():
    content = (ROOT / "scripts" / "demo-hermes-agentops.sh").read_text()

    assert "HERMES_LLM_BASE_URL" in content
    assert "HERMES_LLM_API_KEY" in content
    assert "HERMES_LLM_MODEL" in content
    assert "CCSWITCH_OPENAI_BASE_URL" in content
    assert "NO_PROXY" in content
    assert "127.0.0.1" in content
    assert "hermes_agentops_demo.py" in content
    assert "HERMES_RESULT_FILE" in content
    assert "HERMES_CREDENTIALS_FILE" in content
    assert "--verify-only" in content
    assert "私有凭据文件" in content


def test_hermes_demo_creates_success_and_failure_runs_then_verifies_platform_evidence():
    content = (ROOT / "python" / "examples" / "hermes_agentops_demo.py").read_text()

    for required in (
        "AIAgent",
        "HermesAdapter",
        "agent_init",
        "force_flush",
        "annotation-items",
        "sampling-rules",
        "online-eval-jobs",
        "trace_spans",
        "trace_thoughts",
        "trace_llm_calls",
        "trace_tool_calls",
        "trace_skill_calls",
        "trace_final_answers",
        "memory_tool_calls",
        "failed_run_id",
        "success_run_id",
        "wait_for_online_eval_jobs",
        "annotation_item_id",
        "os.chmod",
        "--verify-only",
        "credentials-file",
    ):
        assert required in content


def test_makefile_exposes_one_command_for_real_hermes_demo():
    content = (ROOT / "Makefile").read_text()

    assert "demo-hermes-agentops:" in content
    assert "./scripts/demo-hermes-agentops.sh" in content


def test_hermes_demo_result_does_not_include_demo_login_credentials():
    content = (ROOT / "python" / "examples" / "hermes_agentops_demo.py").read_text()

    result_block = content[content.index("result = {") : content.index("write_private_json(result_file, result)")]
    assert '"email"' not in result_block
    assert '"password"' not in result_block
    assert "write_private_json(credentials_file, credentials)" in content
