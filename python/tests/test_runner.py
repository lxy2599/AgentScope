"""Tests for runner execution orchestration."""

from __future__ import annotations

import unittest
from unittest.mock import MagicMock, PropertyMock, patch

import pytest


@pytest.fixture(autouse=True)
def reset_target_registry():
    """Clean the target registry before each test."""
    from agentscope_sdk.target import _clear_registry, _save_registry, _restore_registry

    saved = _save_registry()
    _clear_registry()
    yield
    _clear_registry()
    _restore_registry(saved)


def _mock_client(claim_job_return):
    """Build a mock RunnerClient with a controllable claim_job."""
    client = MagicMock()
    client.claim_job.return_value = claim_job_return
    client._base_url = "http://test-server:8080"
    client._project_token = "ascope_testtoken"
    return client


def test_run_once_no_job_returns_false():
    """If claim_job returns None, run_once should return False."""
    from agentscope_sdk.runner import run_once

    client = _mock_client(claim_job_return=None)
    result = run_once(client)
    assert result is False
    # No submit or fail should be called
    client.submit_result.assert_not_called()
    client.fail_job.assert_not_called()


def test_run_once_target_success_submits_result():
    """When a target executes successfully, submit_result should be called."""
    from agentscope_sdk import target
    from agentscope_sdk.runner import run_once

    @target(name="success-agent", version="1.0")
    def my_agent(case_input, context):
        return f"Hello {case_input}"

    from agentscope_sdk.client import ClaimedJob

    claimed = ClaimedJob(
        id="job-1",
        experiment_id="exp-1",
        dataset_case_id="case-1",
        case_input="World",
        target_name="success-agent",
        target_version="1.0",
    )
    client = _mock_client(claim_job_return=claimed)
    client.submit_result = MagicMock()

    result = run_once(client)

    assert result is True
    # Verify submit_result was called with correct payload
    client.submit_result.assert_called_once()
    call_args = client.submit_result.call_args
    assert call_args[0][0] == "job-1"  # job_id
    payload = call_args[0][1]
    assert payload.actual_output == "Hello World"
    assert payload.passed is True
    assert payload.score == 1.0
    assert payload.latency_ms >= 0  # runner now measures latency


def test_run_once_target_raises_exception_calls_fail():
    """When a target raises an exception, fail_job should be called."""
    from agentscope_sdk import target
    from agentscope_sdk.runner import run_once

    @target(name="crash-agent", version="1.0")
    def crashing_agent(case_input, context):
        raise ValueError("something went wrong")

    from agentscope_sdk.client import ClaimedJob

    claimed = ClaimedJob(
        id="job-2",
        experiment_id="exp-1",
        dataset_case_id="case-2",
        case_input="test",
        target_name="crash-agent",
        target_version="1.0",
    )
    client = _mock_client(claim_job_return=claimed)
    client.fail_job = MagicMock()

    result = run_once(client)

    assert result is True
    client.fail_job.assert_called_once()
    call_args = client.fail_job.call_args
    assert call_args[0][0] == "job-2"  # job_id
    assert call_args[1]["error_type"] == "target_error"


def test_run_once_target_not_found_calls_fail():
    """When no target is registered, fail_job with target_not_found should be called."""
    from agentscope_sdk.runner import run_once

    from agentscope_sdk.client import ClaimedJob

    claimed = ClaimedJob(
        id="job-3",
        experiment_id="exp-1",
        dataset_case_id="case-3",
        case_input="test",
        target_name="non-existent-agent",
        target_version="1.0",
    )
    client = _mock_client(claim_job_return=claimed)
    client.fail_job = MagicMock()

    result = run_once(client)

    assert result is True
    client.fail_job.assert_called_once()
    call_args = client.fail_job.call_args
    assert call_args[0][0] == "job-3"
    assert call_args[1]["error_type"] == "target_not_found"


def test_run_once_evaluator_config_error_calls_fail():
    """When evaluator config is invalid, fail_job with evaluator_config_error should be called."""
    from agentscope_sdk import target
    from agentscope_sdk.runner import run_once

    @target(name="eval-agent", version="1.0")
    def my_agent(case_input, context):
        return "output"

    from agentscope_sdk.client import ClaimedJob

    claimed = ClaimedJob(
        id="job-4",
        experiment_id="exp-1",
        dataset_case_id="case-4",
        case_input="test",
        target_name="eval-agent",
        target_version="1.0",
        eval_config={"type": "unknown_type"},
    )
    client = _mock_client(claim_job_return=claimed)
    client.fail_job = MagicMock()
    client.submit_result = MagicMock()

    result = run_once(client)

    assert result is True
    # Should call fail, not submit
    client.submit_result.assert_not_called()
    client.fail_job.assert_called_once()
    call_args = client.fail_job.call_args
    assert call_args[1]["error_type"] == "evaluator_config_error"


def test_run_once_submits_latency_ms():
    """Submitted payload should include non-zero latency_ms and details."""
    from agentscope_sdk import target
    from agentscope_sdk.runner import run_once

    @target(name="latency-agent", version="1.0")
    def my_agent(case_input, context):
        import time
        time.sleep(0.01)  # ensure measurable latency
        return "done"

    from agentscope_sdk.client import ClaimedJob

    claimed = ClaimedJob(
        id="job-lat",
        experiment_id="exp-1",
        dataset_case_id="case-lat",
        case_input="test",
        target_name="latency-agent",
        target_version="1.0",
        eval_config={"type": "exact"},
        case_expected="done",
    )
    client = _mock_client(claim_job_return=claimed)
    client.submit_result = MagicMock()

    result = run_once(client)

    assert result is True
    client.submit_result.assert_called_once()
    payload = client.submit_result.call_args[0][1]
    assert payload.latency_ms > 0, "latency_ms should be > 0"
    assert payload.actual_output == "done"
    assert payload.passed is True


def test_run_once_latency_evaluator_passes():
    """Latency evaluator should pass when execution is fast enough."""
    from agentscope_sdk import target
    from agentscope_sdk.runner import run_once

    @target(name="fast-agent", version="1.0")
    def fast_agent(case_input, context):
        import time
        time.sleep(0.005)  # ensure measurable latency
        return "result"

    from agentscope_sdk.client import ClaimedJob

    claimed = ClaimedJob(
        id="job-fast",
        experiment_id="exp-1",
        dataset_case_id="case-fast",
        case_input="test",
        target_name="fast-agent",
        target_version="1.0",
        eval_config={"type": "latency_threshold", "max_ms": 30000},
    )
    client = _mock_client(claim_job_return=claimed)
    client.submit_result = MagicMock()

    result = run_once(client)

    assert result is True
    client.submit_result.assert_called_once()
    payload = client.submit_result.call_args[0][1]
    assert payload.passed is True
    assert payload.latency_ms > 0
    assert payload.score == 1.0


def test_run_once_submits_with_run_id():
    """Submitted payload should include run_id set by tracing lookup."""
    from agentscope_sdk import target
    from agentscope_sdk.runner import run_once

    @target(name="runid-agent", version="1.0")
    def my_agent(case_input, context):
        return "output"

    from agentscope_sdk.client import ClaimedJob

    claimed = ClaimedJob(
        id="job-runid",
        experiment_id="exp-1",
        dataset_case_id="case-runid",
        case_input="test",
        target_name="runid-agent",
        target_version="1.0",
    )
    client = _mock_client(claim_job_return=claimed)
    client.submit_result = MagicMock()
    client.get_run_id_by_trace = MagicMock(return_value="pg-run-uuid-123")

    with (
        patch("agentscope_sdk.runner.configure_tracing") as mock_ct,
        patch("agentscope_sdk.runner.force_flush") as mock_ff,
        patch("agentscope_sdk.runner.shutdown_tracing") as mock_st,
    ):
        mock_tracer = MagicMock()
        mock_ct.return_value = mock_tracer
        # Make the context manager return a mock span
        mock_span = MagicMock()
        mock_tracer.start_as_current_span.return_value.__enter__.return_value = mock_span

        result = run_once(client)

    assert result is True
    client.submit_result.assert_called_once()
    payload = client.submit_result.call_args[0][1]
    assert payload.run_id == "pg-run-uuid-123"
    assert payload.actual_output == "output"


def test_run_once_tracing_configured_with_correct_server_url():
    """Tracing should be configured with the client's base URL and project token."""
    from agentscope_sdk import target
    from agentscope_sdk.runner import run_once

    @target(name="trace-cfg-agent", version="2.0")
    def my_agent(case_input, context):
        return "ok"

    from agentscope_sdk.client import ClaimedJob

    claimed = ClaimedJob(
        id="job-tracecfg",
        experiment_id="exp-1",
        dataset_case_id="case-tracecfg",
        case_input="test",
        target_name="trace-cfg-agent",
        target_version="2.0",
    )
    client = _mock_client(claim_job_return=claimed)
    client.submit_result = MagicMock()
    client.get_run_id_by_trace = MagicMock(return_value="pg-run-456")

    with (
        patch("agentscope_sdk.runner.configure_tracing") as mock_ct,
        patch("agentscope_sdk.runner.force_flush") as mock_ff,
        patch("agentscope_sdk.runner.shutdown_tracing") as mock_st,
    ):
        mock_tracer = MagicMock()
        mock_ct.return_value = mock_tracer
        mock_span = MagicMock()
        mock_tracer.start_as_current_span.return_value.__enter__.return_value = mock_span

        run_once(client)

    # Verify configure_tracing was called with the right arguments
    mock_ct.assert_called_once()
    call_kwargs = mock_ct.call_args.kwargs
    assert call_kwargs["server_url"] == client._base_url
    assert call_kwargs["project_token"] == client._project_token
    assert call_kwargs["service_name"] == "agentscope-runner"
    # Verify resource attributes include agentscope keys
    res_attrs = call_kwargs.get("resource_attributes", {})
    assert "agentscope.run.id" in res_attrs
    assert "agentscope.agent.name" in res_attrs
    assert "agentscope.agent.version" in res_attrs
    assert "agentscope.eval.job_id" in res_attrs
    assert "agentscope.dataset.case_id" in res_attrs


def test_run_once_span_attributes_include_agentscope_keys():
    """Span attributes should include agentscope.run.id and other required keys."""
    from agentscope_sdk import target
    from agentscope_sdk.runner import run_once

    @target(name="span-attrs-agent", version="3.0")
    def my_agent(case_input, context):
        return "result"

    from agentscope_sdk.client import ClaimedJob

    claimed = ClaimedJob(
        id="job-spanattrs",
        experiment_id="exp-1",
        dataset_case_id="case-spanattrs",
        case_input="test",
        target_name="span-attrs-agent",
        target_version="3.0",
    )
    client = _mock_client(claim_job_return=claimed)
    client.submit_result = MagicMock()
    client.get_run_id_by_trace = MagicMock(return_value="pg-run-789")

    with (
        patch("agentscope_sdk.runner.configure_tracing") as mock_ct,
        patch("agentscope_sdk.runner.force_flush") as mock_ff,
        patch("agentscope_sdk.runner.shutdown_tracing") as mock_st,
    ):
        mock_tracer = MagicMock()
        mock_ct.return_value = mock_tracer
        mock_span = MagicMock()
        mock_tracer.start_as_current_span.return_value.__enter__.return_value = mock_span

        run_once(client)

    # Verify span.set_attribute was called with agentscope keys
    required_attrs = [
        "agentscope.run.id",
        "agentscope.agent.name",
        "agentscope.agent.version",
        "agentscope.eval.job_id",
        "agentscope.dataset.case_id",
    ]
    for attr in required_attrs:
        mock_span.set_attribute.assert_any_call(attr, unittest.mock.ANY)


def test_run_once_get_run_id_by_trace_called():
    """After OTLP flush, get_run_id_by_trace should be called with the trace_id."""
    from agentscope_sdk import target
    from agentscope_sdk.runner import run_once

    @target(name="trace-lookup-agent", version="1.0")
    def my_agent(case_input, context):
        return "ok"

    from agentscope_sdk.client import ClaimedJob

    claimed = ClaimedJob(
        id="job-tracelookup",
        experiment_id="exp-1",
        dataset_case_id="case-tracelookup",
        case_input="test",
        target_name="trace-lookup-agent",
        target_version="1.0",
    )
    client = _mock_client(claim_job_return=claimed)
    client.submit_result = MagicMock()
    client.get_run_id_by_trace = MagicMock(return_value="pg-run-999")

    with (
        patch("agentscope_sdk.runner.configure_tracing") as mock_ct,
        patch("agentscope_sdk.runner.force_flush") as mock_ff,
        patch("agentscope_sdk.runner.shutdown_tracing") as mock_st,
    ):
        mock_tracer = MagicMock()
        mock_ct.return_value = mock_tracer
        mock_span = MagicMock()
        mock_tracer.start_as_current_span.return_value.__enter__.return_value = mock_span

        run_once(client)

    # force_flush should have been called before the trace lookup
    mock_ff.assert_called_once()
    # get_run_id_by_trace should have been called
    client.get_run_id_by_trace.assert_called_once()
