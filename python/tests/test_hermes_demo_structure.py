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


def test_hermes_documentation_distinguishes_native_fork_from_adapter_demo():
    integration_doc = (ROOT / "docs" / "integrations" / "hermes-agent.md").read_text()
    readme = (ROOT / "README.md").read_text()
    native_plan = (
        ROOT / "docs" / "superpowers" / "plans" / "2026-07-30-hermes-native-sdk-integration.md"
    ).read_text()

    assert "Hermes 原生 SDK 路径（已实现）" in integration_doc
    assert "02a438eb5b04009bebfc3c23c60d03b347e6b133" in integration_doc
    assert "integrations/hermes-agent/" in integration_doc
    assert (ROOT / "integrations" / "hermes-agent" / "hermes_agentscope" / "bridge.py").is_file()
    assert "make demo-hermes-agentops` 验证此路径" in integration_doc
    assert "外置 `HermesAdapter` 兼容路径" in integration_doc
    assert "受控 Hermes fork（源码位于 `integrations/hermes-agent/`）的原生 SDK 埋点" in readme
    assert "原生 fork 源码实现完成" in native_plan
    assert "原生平台 E2E 证据待单独归档" in native_plan
