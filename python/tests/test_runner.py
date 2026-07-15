"""Tests for runner execution orchestration."""

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
