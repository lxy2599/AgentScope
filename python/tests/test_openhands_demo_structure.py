from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
PINNED_COMMIT = "a375c66aaf29c8d3b6553c9164e65ed917149e81"


def test_openhands_setup_uses_one_pinned_official_commit():
    setup = (ROOT / "scripts" / "setup-openhands-agent.sh").read_text()
    docs = (ROOT / "docs" / "integrations" / "openhands.md").read_text()
    adapter = (ROOT / "python" / "agentscope_sdk" / "adapters" / "openhands.py").read_text()

    assert "github.com/OpenHands/software-agent-sdk.git" in setup
    assert PINNED_COMMIT in setup
    assert PINNED_COMMIT in docs
    assert PINNED_COMMIT in adapter
    assert ".external/openhands" in setup
    assert "OpenHands 原生 fork 路径（目标实现）" in docs
    assert "外置 Adapter 兼容路径（当前已实现）" in docs
    assert "hermes_agentscope/bridge.py" in docs
    assert "RuntimeTraceSession" in docs
    assert "callbacks" in docs and "token_callbacks" in docs
    assert "agent.turn" in docs
    assert "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT" in docs


def test_openhands_demo_runs_real_upstream_and_verifies_all_platform_stores():
    demo = (ROOT / "python" / "examples" / "openhands_agentops_demo.py").read_text()
    shell = (ROOT / "scripts" / "demo-openhands-agentops.sh").read_text()
    verify = (ROOT / "scripts" / "verify-openhands-data.sh").read_text()

    assert "from openhands.sdk import" in demo
    assert "OpenHandsAdapter" in demo
    assert "TerminalTool" in demo
    assert "FileEditorTool" in demo
    assert "callbacks=adapter.callbacks" in demo
    assert "token_callbacks=adapter.token_callbacks" in demo
    assert 'events/stream"' in demo
    assert 'event.get("type") == "run.semantic.appended"' in demo
    assert '"sse_semantic_before_finish": True' in demo
    assert 'run_detail.get("trace_id")' in demo
    assert "GLM_API_KEY" in shell
    assert "DEEPSEEK_API_KEY" in shell
    assert "trace_spans" in verify
    assert "trace_thoughts" in verify
    assert "trace_llm_calls" in verify
    assert "trace_tool_calls" in verify
    assert "trace_final_answers" in verify
    assert "run_snapshots" in verify
    assert "realtime_outbox" in verify
    assert "redis-cli" in verify
    assert 'git -C "${OPENHANDS_DIR}" status --porcelain' in verify


def test_makefile_exposes_real_openhands_setup_and_demo():
    makefile = (ROOT / "Makefile").read_text()
    assert "setup-openhands-agent:" in makefile
    assert "demo-openhands-agentops:" in makefile
    assert "./scripts/setup-openhands-agent.sh" in makefile
    assert "./scripts/demo-openhands-agentops.sh" in makefile
