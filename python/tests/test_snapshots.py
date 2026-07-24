from agentscope_sdk.snapshots import RunSnapshotBuilder


def test_snapshot_builder_captures_runtime_contract() -> None:
    snapshot = (
        RunSnapshotBuilder()
        .framework("openhands", version="1.2.3", upstream_commit="abc123")
        .model("glm", "glm-4.7-flash", temperature=0.2)
        .prompt("system", version="v2", content_hash="prompt-hash")
        .capability("tool", "terminal", version="1")
        .capability("skill", "research", version="2")
        .capability("mcp", "filesystem")
        .capability("knowledge_base", "docs", version="2026-07")
        .dependency("openhands-ai", "1.2.3")
        .flags(feature_flags={"streaming": True}, ablation_flags={"memory": False})
        .capture_policy(thoughts="summary", tool_arguments="redacted")
        .input_hash("hello")
        .build()
    )

    assert snapshot["runtime_contract"]["schema_version"] == "runtime-contract-v2"
    assert snapshot["framework"]["upstream_commit"] == "abc123"
    assert snapshot["models"][0]["parameters"]["temperature"] == 0.2
    assert snapshot["tools"][0]["name"] == "terminal"
    assert snapshot["skills"][0]["name"] == "research"
    assert len(snapshot["input_hash"]) == 64


def test_snapshot_builder_returns_defensive_copy() -> None:
    builder = RunSnapshotBuilder().flags(feature_flags={"nested": {"enabled": True}})
    first = builder.build()
    first["feature_flags"]["nested"]["enabled"] = False
    assert builder.build()["feature_flags"]["nested"]["enabled"] is True
