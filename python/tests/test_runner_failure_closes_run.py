"""Tests for runner failure path that closes the trace run."""

from agentscope_sdk.runner import _build_target_failure_run_error


def test_build_target_failure_run_error_includes_trace_and_error():
    msg = _build_target_failure_run_error(
        trace_id="abcdef1234567890abcdef1234567890",
        error="[glm] HTTP 429",
    )

    assert "abcdef1234567890" in msg
    assert "[glm] HTTP 429" in msg


def test_build_target_failure_run_error_truncates_long_trace_id():
    msg = _build_target_failure_run_error(
        trace_id="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
        error="timeout",
    )

    assert msg.startswith("Target execution failed for trace_id=aaaaaaaaaaaaaaaa...")
    assert "timeout" in msg
