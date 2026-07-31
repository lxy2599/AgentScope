"""Static regression checks for the controlled, native-only OpenHands integration."""

from __future__ import annotations

from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
FORK = ROOT / "integrations" / "openhands-agent"
BRIDGE = FORK / "openhands-sdk" / "openhands" / "sdk" / "agentscope" / "bridge.py"
CONFIG = FORK / "openhands-sdk" / "openhands" / "sdk" / "agentscope" / "config.py"


def test_openhands_native_fork_has_provenance_bridge_and_real_execution_hooks():
    provenance = (FORK / "AGENTSCOPE_FORK.md").read_text()
    bridge = BRIDGE.read_text()
    config = CONFIG.read_text()
    conversation = (
        FORK / "openhands-sdk" / "openhands" / "sdk" / "conversation" / "impl" / "local_conversation.py"
    ).read_text()
    agent = (FORK / "openhands-sdk" / "openhands" / "sdk" / "agent" / "agent.py").read_text()
    telemetry = (FORK / "openhands-sdk" / "openhands" / "sdk" / "llm" / "utils" / "telemetry.py").read_text()

    assert "a375c66aaf29c8d3b6553c9164e65ed917149e81" in provenance
    assert "OPENHANDS_AGENTSCOPE_ENABLED" in config
    assert "RuntimeTraceSession" in bridge
    assert "on_run_started" in conversation
    assert "on_turn_started" in conversation
    assert "_agentscope_execute_action_event" in agent
    assert "on_llm_started" in telemetry
    assert "on_first_token" in telemetry


def test_openhands_demo_and_makefile_use_native_project_level_path_only():
    demo = (ROOT / "python" / "examples" / "openhands_agentops_demo.py").read_text()
    makefile = (ROOT / "Makefile").read_text()

    assert "agentscope_sdk" not in demo
    assert "callbacks=" not in demo
    assert "token_callbacks=" not in demo
    assert "conversation.send_message(prompt)" in demo
    assert "conversation.run()" in demo
    assert "demo-openhands-native-agentops:" in makefile
    assert "demo-openhands-agentops:" not in makefile
    assert not (ROOT / "python" / "agentscope_sdk" / "adapters" / "openhands.py").exists()
