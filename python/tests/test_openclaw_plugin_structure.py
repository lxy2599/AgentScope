"""Regression checks for the OpenClaw native observability plugin contract."""

from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
PLUGIN = ROOT / "integrations" / "openclaw" / "agentscope-observability"


def test_openclaw_plugin_uses_public_hooks_and_otlp_exporter() -> None:
    source = (PLUGIN / "src" / "index.ts").read_text()
    for hook in (
        "before_agent_run",
        "model_call_started",
        "model_call_ended",
        "llm_input",
        "llm_output",
        "before_tool_call",
        "after_tool_call",
        "agent_end",
        "gateway_start",
        "gateway_stop",
    ):
        assert f'api.on("{hook}"' in source
    assert "OTLPTraceExporter" in source
    assert "/v1/traces" in source
    assert "/api/sdk/runs/start" in source


def test_openclaw_plugin_manifest_requires_agent_scope_configuration() -> None:
    manifest = (PLUGIN / "openclaw.plugin.json").read_text()
    package = (PLUGIN / "package.json").read_text()
    assert '"projectToken"' in manifest
    assert '"agentName"' in manifest
    assert '"openclaw"' in package
