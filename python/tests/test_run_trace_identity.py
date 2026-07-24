"""Test Run/Trace identity separation."""

from agentscope_sdk.runtime import build_run_resource_attributes


def test_build_run_resource_attributes_separates_run_id_and_trace_id():
    attrs = build_run_resource_attributes(
        run_id="run-123",
        trace_id="trace-abc",
        agent_name="support-agent",
    )

    assert attrs["agentscope.run.id"] == "run-123"
    assert attrs["agentscope.trace.id"] == "trace-abc"
    assert attrs["agentscope.agent.name"] == "support-agent"


def test_build_run_resource_attributes_includes_governance_dimensions():
    attrs = build_run_resource_attributes(
        run_id="run-123",
        trace_id="trace-abc",
        agent_name="support-agent",
        agent_version="2.4.0",
        environment="prod",
    )

    assert attrs["agentscope.agent.version"] == "2.4.0"
    assert attrs["agentscope.environment"] == "prod"
