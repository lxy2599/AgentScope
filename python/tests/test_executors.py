"""Tests for the pluggable benchmark executor contract."""

from __future__ import annotations

from unittest.mock import MagicMock, patch

import pytest

from agentscope_sdk import target
from agentscope_sdk.client import ClaimedJob
from agentscope_sdk.executors import (
    BenchmarkArtifact,
    BenchmarkContractError,
    BenchmarkEnvironmentError,
    BenchmarkErrorType,
    BenchmarkExecutionError,
    BenchmarkExecutionResult,
    BenchmarkTargetError,
    ExecutorRegistry,
    FunctionExecutor,
)


@pytest.fixture(autouse=True)
def reset_target_registry():
    """Keep process-wide target registration isolated between tests."""
    from agentscope_sdk.target import _clear_registry, _restore_registry, _save_registry

    saved = _save_registry()
    _clear_registry()
    yield
    _clear_registry()
    _restore_registry(saved)


def _make_claimed_job(*, execution_kind: str = "function") -> ClaimedJob:
    return ClaimedJob(
        id="job-1",
        experiment_id="exp-1",
        dataset_case_id="case-1",
        case_input="calculate the total",
        target_name="benchmark-target",
        target_version="1.0",
        execution_kind=execution_kind,
        execution_config={"benchmark": "native-contract", "task_id": "task-1"},
    )


def _make_mock_client(claimed: ClaimedJob) -> MagicMock:
    client = MagicMock()
    client.claim_job.return_value = claimed
    client._base_url = "http://test-server:8080"
    client._project_token = "ascope_testtoken"
    client.get_run_id_by_trace.return_value = "run-1"
    return client


def _make_mock_tracer() -> MagicMock:
    span = MagicMock()
    span.get_span_context.return_value.trace_id = 0x1234
    tracer = MagicMock()
    tracer.start_as_current_span.return_value.__enter__.return_value = span
    return tracer


def test_benchmark_execution_result_serializes_official_metrics_and_artifacts():
    result = BenchmarkExecutionResult(
        actual_output="done",
        official_score=1.0,
        official_passed=True,
        official_reason="official verifier passed",
        official_metrics={"task_success": 1.0},
        artifacts=[
            BenchmarkArtifact(
                kind="verifier_report",
                uri="file:///tmp/report.json",
                metadata={"sha256": "abc"},
            ),
        ],
    )

    assert result.to_details() == {
        "valid": True,
        "official_score": 1.0,
        "official_passed": True,
        "official_reason": "official verifier passed",
        "official_metrics": {"task_success": 1.0},
        "artifacts": [
            {
                "kind": "verifier_report",
                "uri": "file:///tmp/report.json",
                "metadata": {"sha256": "abc"},
            },
        ],
    }


def test_invalid_benchmark_result_requires_classified_error():
    with pytest.raises(BenchmarkContractError, match="requires an error type"):
        BenchmarkExecutionResult(valid=False, error_message="environment failed")

    with pytest.raises(BenchmarkContractError, match="must be a BenchmarkErrorType"):
        BenchmarkExecutionResult(valid=False, error_type="environment_error", error_message="environment failed")


def test_registry_rejects_unknown_execution_kind():
    registry = ExecutorRegistry()
    registry.register("function", FunctionExecutor())

    with pytest.raises(BenchmarkExecutionError, match="Unsupported execution kind 'harbor'") as exc_info:
        registry.resolve("harbor")

    assert exc_info.value.error_type == BenchmarkErrorType.UNSUPPORTED_EXECUTION_KIND


def test_native_benchmark_executor_submits_official_verdict_and_artifacts():
    from agentscope_sdk.runner import run_once

    @target(name="benchmark-target", version="1.0")
    def benchmark_target(case_input, context):
        assert case_input == "calculate the total"
        assert context["execution_kind"] == "native_benchmark"
        assert context["execution_config"]["task_id"] == "task-1"
        return BenchmarkExecutionResult(
            actual_output="42",
            official_score=1.0,
            official_passed=True,
            official_reason="official verifier passed",
            official_metrics={"task_success": 1.0, "steps": 3},
            artifacts=[BenchmarkArtifact(kind="log", uri="file:///tmp/agent.log")],
        )

    client = _make_mock_client(_make_claimed_job(execution_kind="native_benchmark"))
    with (
        patch("agentscope_sdk.runner.configure_tracing", return_value=_make_mock_tracer()),
        patch("agentscope_sdk.runner.force_flush"),
    ):
        assert run_once(client) is True

    client.fail_job.assert_not_called()
    payload = client.submit_result.call_args.args[1]
    assert payload.score == 1.0
    assert payload.passed is True
    assert payload.reason == "official verifier passed"
    assert payload.actual_output == "42"
    assert payload.details == {
        "benchmark": {
            "valid": True,
            "official_score": 1.0,
            "official_passed": True,
            "official_reason": "official verifier passed",
            "official_metrics": {"task_success": 1.0, "steps": 3},
            "artifacts": [{"kind": "log", "uri": "file:///tmp/agent.log"}],
        },
    }


def test_runner_dispatches_to_an_injected_executor_registry():
    from agentscope_sdk.runner import run_once

    class RecordingExecutor:
        def __init__(self):
            self.requests = []

        def execute(self, request):
            self.requests.append(request)
            return BenchmarkExecutionResult(
                actual_output="from custom executor",
                official_score=0.5,
                official_passed=False,
                official_reason="official verifier found an incomplete task",
            )

    @target(name="benchmark-target", version="1.0")
    def benchmark_target(case_input, context):
        raise AssertionError("custom executor should not invoke the decorated target")

    executor = RecordingExecutor()
    registry = ExecutorRegistry()
    registry.register("custom_harness", executor)
    client = _make_mock_client(_make_claimed_job(execution_kind="custom_harness"))
    with (
        patch("agentscope_sdk.runner.configure_tracing", return_value=_make_mock_tracer()),
        patch("agentscope_sdk.runner.force_flush"),
    ):
        assert run_once(client, executor_registry=registry) is True

    assert len(executor.requests) == 1
    assert executor.requests[0].execution_config["task_id"] == "task-1"
    payload = client.submit_result.call_args.args[1]
    assert payload.score == 0.5
    assert payload.passed is False


def test_runner_rejects_a_custom_executor_with_a_nonstandard_result():
    from agentscope_sdk.runner import run_once

    class InvalidExecutor:
        def execute(self, request):
            return "not a benchmark result"

    @target(name="benchmark-target", version="1.0")
    def benchmark_target(case_input, context):
        raise AssertionError("invalid executor should not invoke the decorated target")

    registry = ExecutorRegistry()
    registry.register("invalid", InvalidExecutor())
    client = _make_mock_client(_make_claimed_job(execution_kind="invalid"))
    with (
        patch("agentscope_sdk.runner.configure_tracing", return_value=_make_mock_tracer()),
        patch("agentscope_sdk.runner.force_flush"),
        patch("agentscope_sdk.runner._fail_trace_run"),
    ):
        assert run_once(client, executor_registry=registry) is True

    assert client.fail_job.call_args.kwargs["error_type"] == "benchmark_contract_error"
    assert client.fail_job.call_args.kwargs["details"] == {"returned_type": "str"}


def test_invalid_environment_result_fails_job_without_submitting_capability_score():
    from agentscope_sdk.runner import run_once

    @target(name="benchmark-target", version="1.0")
    def benchmark_target(case_input, context):
        return BenchmarkExecutionResult.invalid(
            BenchmarkErrorType.ENVIRONMENT_ERROR,
            "benchmark container did not start",
            error_details={"container_id": "abc123"},
        )

    client = _make_mock_client(_make_claimed_job(execution_kind="benchmark_contract"))
    with (
        patch("agentscope_sdk.runner.configure_tracing", return_value=_make_mock_tracer()),
        patch("agentscope_sdk.runner.force_flush"),
        patch("agentscope_sdk.runner._fail_trace_run"),
    ):
        assert run_once(client) is True

    client.submit_result.assert_not_called()
    assert client.fail_job.call_args.kwargs["error_type"] == "environment_error"
    assert client.fail_job.call_args.kwargs["details"] == {
        "benchmark": {
            "valid": False,
            "official_metrics": {},
            "artifacts": [],
            "error_type": "environment_error",
            "error_message": "benchmark container did not start",
            "error_details": {"container_id": "abc123"},
        },
    }


def test_native_target_error_uses_target_error_taxonomy():
    from agentscope_sdk.runner import run_once

    @target(name="benchmark-target", version="1.0")
    def benchmark_target(case_input, context):
        raise BenchmarkTargetError("agent process exited unexpectedly", details={"exit_code": 137})

    client = _make_mock_client(_make_claimed_job(execution_kind="native_benchmark"))
    with (
        patch("agentscope_sdk.runner.configure_tracing", return_value=_make_mock_tracer()),
        patch("agentscope_sdk.runner.force_flush"),
        patch("agentscope_sdk.runner._fail_trace_run"),
    ):
        assert run_once(client) is True

    client.submit_result.assert_not_called()
    assert client.fail_job.call_args.kwargs["error_type"] == "target_error"
    assert client.fail_job.call_args.kwargs["details"] == {"exit_code": 137}


def test_native_executor_rejects_plain_target_output_as_contract_error():
    from agentscope_sdk.runner import run_once

    @target(name="benchmark-target", version="1.0")
    def benchmark_target(case_input, context):
        return "completed"

    client = _make_mock_client(_make_claimed_job(execution_kind="native_benchmark"))
    with (
        patch("agentscope_sdk.runner.configure_tracing", return_value=_make_mock_tracer()),
        patch("agentscope_sdk.runner.force_flush"),
        patch("agentscope_sdk.runner._fail_trace_run"),
    ):
        assert run_once(client) is True

    assert client.fail_job.call_args.kwargs["error_type"] == "benchmark_contract_error"


def test_environment_error_class_retains_details():
    error = BenchmarkEnvironmentError("failed to reset state", details={"attempt": 2})
    assert error.error_type == BenchmarkErrorType.ENVIRONMENT_ERROR
    assert error.details == {"attempt": 2}
