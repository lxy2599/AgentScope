"""End-to-end runner protocol coverage for native benchmark execution."""

from __future__ import annotations

import json
from unittest.mock import MagicMock, patch

import pytest

from agentscope_sdk import BenchmarkExecutionResult, target
from agentscope_sdk.client import RunnerClient


@pytest.fixture(autouse=True)
def reset_target_registry():
    """Keep process-wide target registration isolated between tests."""
    from agentscope_sdk.target import _clear_registry, _restore_registry, _save_registry

    saved = _save_registry()
    _clear_registry()
    yield
    _clear_registry()
    _restore_registry(saved)


def _claim_response() -> dict[str, object]:
    return {
        "job": {
            "id": "job-benchmark-1",
            "experiment_id": "experiment-1",
            "dataset_case_id": "case-1",
            "status": "running",
            "attempts": 1,
        },
        "status": "running",
        "case_input": "Run the fixed benchmark task.",
        "target_name": "native-harness",
        "target_version": "1.0",
        "execution_kind": "native_benchmark",
        "execution_config": {"benchmark": "contract-smoke", "task_id": "task-1"},
    }


def _mock_tracer() -> MagicMock:
    span = MagicMock()
    span.get_span_context.return_value.trace_id = 0xABCD
    tracer = MagicMock()
    tracer.start_as_current_span.return_value.__enter__.return_value = span
    return tracer


def test_native_benchmark_claim_to_result_uses_existing_runner_protocol(httpx_mock):
    """A benchmark job follows claim -> trace lookup -> result with no new queue/API."""
    import httpx

    submitted_payloads: list[dict[str, object]] = []

    @target(name="native-harness", version="1.0")
    def native_harness(case_input, context):
        assert case_input == "Run the fixed benchmark task."
        assert context["execution_config"] == {"benchmark": "contract-smoke", "task_id": "task-1"}
        return BenchmarkExecutionResult(
            actual_output="task complete",
            official_score=1.0,
            official_passed=True,
            official_reason="official verifier passed",
            official_metrics={"task_success": 1.0},
        )

    def capture_result(request):
        submitted_payloads.append(json.loads(request.content))
        return httpx.Response(200, json={"status": "ok"}, request=request)

    httpx_mock.add_response(
        method="GET",
        url="http://localhost:8080/runner/jobs/claim?runner_name=benchmark-runner",
        json=_claim_response(),
    )
    httpx_mock.add_response(
        method="GET",
        url="http://localhost:8080/runner/runs/by-trace/0000000000000000000000000000abcd",
        json={"run_id": "run-1"},
    )
    httpx_mock.add_callback(
        capture_result,
        method="POST",
        url="http://localhost:8080/runner/jobs/job-benchmark-1/results",
    )

    client = RunnerClient(
        server_url="http://localhost:8080",
        project_token="test-token",
        runner_name="benchmark-runner",
    )
    with (
        patch("agentscope_sdk.runner.configure_tracing", return_value=_mock_tracer()),
        patch("agentscope_sdk.runner.force_flush"),
    ):
        from agentscope_sdk.runner import run_once

        assert run_once(client) is True

    assert len(submitted_payloads) == 1
    payload = submitted_payloads[0]
    assert payload["runner_name"] == "benchmark-runner"
    assert payload["run_id"] == "run-1"
    assert payload["score"] == 1.0
    assert payload["passed"] is True
    assert payload["actual_output"] == "task complete"
    assert payload["details"] == {
        "benchmark": {
            "valid": True,
            "official_score": 1.0,
            "official_passed": True,
            "official_reason": "official verifier passed",
            "official_metrics": {"task_success": 1.0},
            "artifacts": [],
        },
    }
