"""Tests for runner execution orchestration."""

from __future__ import annotations

import unittest
from unittest.mock import MagicMock, patch

import pytest


# ---- Helpers ----

FAKE_TRACE_ID = 0xABCDEF0123456789ABCDEF0123456789  # 128-bit int
FAKE_TRACE_ID_HEX = format(FAKE_TRACE_ID, "032x")


@pytest.fixture(autouse=True)
def reset_target_registry():
    """Clean the target registry before each test."""
    from agentscope_sdk.target import _clear_registry, _save_registry, _restore_registry

    saved = _save_registry()
    _clear_registry()
    yield
    _clear_registry()
    _restore_registry(saved)


def _make_mock_span():
    """Build a mock span whose ``get_span_context().trace_id`` returns
    ``FAKE_TRACE_ID`` so the runner can extract the real OTel trace ID."""
    span = MagicMock()
    span.get_span_context.return_value.trace_id = FAKE_TRACE_ID
    return span


def _make_mock_tracer(return_span=None):
    """Build a mock tracer whose ``start_as_current_span`` context manager
    returns *return_span* (a new mock span if not provided)."""
    tracer = MagicMock()
    span = return_span or _make_mock_span()
    tracer.start_as_current_span.return_value.__enter__.return_value = span
    return tracer


def _mock_client(claim_job_return):
    """Build a mock RunnerClient with a controllable claim_job."""
    client = MagicMock()
    client.claim_job.return_value = claim_job_return
    client._base_url = "http://test-server:8080"
    client._project_token = "ascope_testtoken"
    return client


# ---- Tests ----


def test_run_once_no_job_returns_false():
    """If claim_job returns None, run_once should return False."""
    from agentscope_sdk.runner import run_once

    client = _mock_client(claim_job_return=None)
    result = run_once(client)
    assert result is False
    client.submit_result.assert_not_called()
    client.fail_job.assert_not_called()


class TestTargetExecution:
    """Tests for target lookup, execution, and evaluation."""

    def test_target_success_submits_result(self):
        """When a target executes successfully, submit_result should be called."""
        from agentscope_sdk import target
        from agentscope_sdk.runner import run_once
        from agentscope_sdk.client import ClaimedJob

        @target(name="success-agent", version="1.0")
        def my_agent(case_input, context):
            return f"Hello {case_input}"

        claimed = ClaimedJob(
            id="job-1", experiment_id="exp-1", dataset_case_id="case-1",
            case_input="World", target_name="success-agent", target_version="1.0",
        )
        client = _mock_client(claim_job_return=claimed)
        client.submit_result = MagicMock()
        client.get_run_id_by_trace = MagicMock(return_value="pg-run-uuid")

        with (
            patch("agentscope_sdk.runner.configure_tracing") as mock_ct,
            patch("agentscope_sdk.runner.force_flush") as mock_ff,
        ):
            mock_ct.return_value = _make_mock_tracer()
            result = run_once(client)

        assert result is True
        client.submit_result.assert_called_once()
        call_args = client.submit_result.call_args
        assert call_args[0][0] == "job-1"
        payload = call_args[0][1]
        assert payload.actual_output == "Hello World"
        assert payload.passed is True
        assert payload.score == 1.0
        assert payload.latency_ms >= 0

    def test_target_raises_exception_calls_fail(self):
        """When a target raises an exception, fail_job should be called."""
        from agentscope_sdk import target
        from agentscope_sdk.runner import run_once
        from agentscope_sdk.client import ClaimedJob

        @target(name="crash-agent", version="1.0")
        def crashing_agent(case_input, context):
            raise ValueError("something went wrong")

        claimed = ClaimedJob(
            id="job-2", experiment_id="exp-1", dataset_case_id="case-2",
            case_input="test", target_name="crash-agent", target_version="1.0",
        )
        client = _mock_client(claim_job_return=claimed)
        client.fail_job = MagicMock()

        with (
            patch("agentscope_sdk.runner.configure_tracing") as mock_ct,
            patch("agentscope_sdk.runner.force_flush") as mock_ff,
        ):
            mock_ct.return_value = _make_mock_tracer()
            result = run_once(client)

        assert result is True
        client.fail_job.assert_called_once()
        call_args = client.fail_job.call_args
        assert call_args[0][0] == "job-2"
        assert call_args[1]["error_type"] == "target_error"

    def test_target_not_found_calls_fail(self):
        """When no target is registered, fail_job with target_not_found."""
        from agentscope_sdk.runner import run_once
        from agentscope_sdk.client import ClaimedJob

        claimed = ClaimedJob(
            id="job-3", experiment_id="exp-1", dataset_case_id="case-3",
            case_input="test", target_name="non-existent-agent", target_version="1.0",
        )
        client = _mock_client(claim_job_return=claimed)
        client.fail_job = MagicMock()

        result = run_once(client)

        assert result is True
        client.fail_job.assert_called_once()
        call_args = client.fail_job.call_args
        assert call_args[0][0] == "job-3"
        assert call_args[1]["error_type"] == "target_not_found"

    def test_evaluator_config_error_calls_fail(self):
        """When evaluator config is invalid, fail_job with evaluator_config_error."""
        from agentscope_sdk import target
        from agentscope_sdk.runner import run_once
        from agentscope_sdk.client import ClaimedJob

        @target(name="eval-agent", version="1.0")
        def my_agent(case_input, context):
            return "output"

        claimed = ClaimedJob(
            id="job-4", experiment_id="exp-1", dataset_case_id="case-4",
            case_input="test", target_name="eval-agent", target_version="1.0",
            eval_config={"type": "unknown_type"},
        )
        client = _mock_client(claim_job_return=claimed)
        client.fail_job = MagicMock()
        client.submit_result = MagicMock()

        with (
            patch("agentscope_sdk.runner.configure_tracing") as mock_ct,
            patch("agentscope_sdk.runner.force_flush") as mock_ff,
        ):
            mock_ct.return_value = _make_mock_tracer()
            result = run_once(client)

        assert result is True
        client.submit_result.assert_not_called()
        client.fail_job.assert_called_once()
        call_args = client.fail_job.call_args
        assert call_args[1]["error_type"] == "evaluator_config_error"

    def test_submits_latency_ms(self):
        """Submitted payload should include non-zero latency_ms."""
        from agentscope_sdk import target
        from agentscope_sdk.runner import run_once
        from agentscope_sdk.client import ClaimedJob

        @target(name="latency-agent", version="1.0")
        def my_agent(case_input, context):
            import time
            time.sleep(0.01)
            return "done"

        claimed = ClaimedJob(
            id="job-lat", experiment_id="exp-1", dataset_case_id="case-lat",
            case_input="test", target_name="latency-agent", target_version="1.0",
            eval_config={"type": "exact"}, case_expected="done",
        )
        client = _mock_client(claim_job_return=claimed)
        client.submit_result = MagicMock()
        client.get_run_id_by_trace = MagicMock(return_value="pg-run-uuid")

        with (
            patch("agentscope_sdk.runner.configure_tracing") as mock_ct,
            patch("agentscope_sdk.runner.force_flush") as mock_ff,
        ):
            mock_ct.return_value = _make_mock_tracer()
            result = run_once(client)

        assert result is True
        client.submit_result.assert_called_once()
        payload = client.submit_result.call_args[0][1]
        assert payload.latency_ms > 0
        assert payload.actual_output == "done"
        assert payload.passed is True

    def test_latency_evaluator_passes(self):
        """Latency evaluator should pass when execution is fast enough."""
        from agentscope_sdk import target
        from agentscope_sdk.runner import run_once
        from agentscope_sdk.client import ClaimedJob

        @target(name="fast-agent", version="1.0")
        def fast_agent(case_input, context):
            import time
            time.sleep(0.005)
            return "result"

        claimed = ClaimedJob(
            id="job-fast", experiment_id="exp-1", dataset_case_id="case-fast",
            case_input="test", target_name="fast-agent", target_version="1.0",
            eval_config={"type": "latency_threshold", "max_ms": 30000},
        )
        client = _mock_client(claim_job_return=claimed)
        client.submit_result = MagicMock()
        client.get_run_id_by_trace = MagicMock(return_value="pg-run-uuid")

        with (
            patch("agentscope_sdk.runner.configure_tracing") as mock_ct,
            patch("agentscope_sdk.runner.force_flush") as mock_ff,
        ):
            mock_ct.return_value = _make_mock_tracer()
            result = run_once(client)

        assert result is True
        client.submit_result.assert_called_once()
        payload = client.submit_result.call_args[0][1]
        assert payload.passed is True
        assert payload.latency_ms > 0
        assert payload.score == 1.0

    def test_target_context_contains_expected(self):
        """The context passed to the target should include 'expected'."""
        from agentscope_sdk import target
        from agentscope_sdk.runner import run_once
        from agentscope_sdk.client import ClaimedJob

        captured_context = {}

        @target(name="ctx-agent", version="1.0")
        def my_agent(case_input, context):
            captured_context.update(context)
            return "output"

        claimed = ClaimedJob(
            id="job-ctx", experiment_id="exp-1", dataset_case_id="case-ctx",
            case_input="test", target_name="ctx-agent", target_version="1.0",
            case_expected="expected-output",
        )
        client = _mock_client(claim_job_return=claimed)
        client.submit_result = MagicMock()
        client.get_run_id_by_trace = MagicMock(return_value="pg-run-uuid")

        with (
            patch("agentscope_sdk.runner.configure_tracing") as mock_ct,
            patch("agentscope_sdk.runner.force_flush") as mock_ff,
        ):
            mock_ct.return_value = _make_mock_tracer()
            run_once(client)

        assert "expected" in captured_context
        assert captured_context["expected"] == "expected-output"


class TestTracingIntegration:
    """Tests for the tracing integration in _execute_job."""

    def test_real_trace_id_used_for_lookup(self):
        """The real OTel trace_id from the root span should be passed to
        ``get_run_id_by_trace``."""
        from agentscope_sdk import target
        from agentscope_sdk.runner import run_once
        from agentscope_sdk.client import ClaimedJob

        @target(name="traceid-agent", version="1.0")
        def my_agent(case_input, context):
            return "ok"

        claimed = ClaimedJob(
            id="job-traceid", experiment_id="exp-1", dataset_case_id="case-traceid",
            case_input="test", target_name="traceid-agent", target_version="1.0",
        )
        client = _mock_client(claim_job_return=claimed)
        client.submit_result = MagicMock()
        client.get_run_id_by_trace = MagicMock(return_value="pg-run-uuid")

        with (
            patch("agentscope_sdk.runner.configure_tracing") as mock_ct,
            patch("agentscope_sdk.runner.force_flush") as mock_ff,
        ):
            mock_ct.return_value = _make_mock_tracer()
            run_once(client)

        # get_run_id_by_trace should have been called with the hex
        # representation of FAKE_TRACE_ID.
        client.get_run_id_by_trace.assert_any_call(FAKE_TRACE_ID_HEX)

    def test_trace_lookup_retries_on_none(self):
        """When get_run_id_by_trace returns None, the runner should retry
        and eventually call fail_job."""
        from agentscope_sdk import target
        from agentscope_sdk.runner import run_once
        from agentscope_sdk.client import ClaimedJob

        @target(name="retry-agent", version="1.0")
        def my_agent(case_input, context):
            return "never-gets-run-id"

        claimed = ClaimedJob(
            id="job-retry", experiment_id="exp-1", dataset_case_id="case-retry",
            case_input="test", target_name="retry-agent", target_version="1.0",
        )
        client = _mock_client(claim_job_return=claimed)
        client.submit_result = MagicMock()
        client.fail_job = MagicMock()
        client.get_run_id_by_trace = MagicMock(return_value=None)

        with (
            patch("agentscope_sdk.runner.configure_tracing") as mock_ct,
            patch("agentscope_sdk.runner.force_flush") as mock_ff,
            patch("agentscope_sdk.runner.time") as mock_time,
        ):
            mock_ct.return_value = _make_mock_tracer()
            result = run_once(client)

        assert result is True
        # Should NOT submit without a run_id
        client.submit_result.assert_not_called()
        # Should call fail_job with trace_lookup_failed
        client.fail_job.assert_called_once()
        call_args = client.fail_job.call_args
        assert call_args[1]["error_type"] == "trace_lookup_failed"
        assert "retries" in call_args[1]["message"].lower()

    def test_submits_with_real_run_id(self):
        """Submitted payload should contain the PostgreSQL run_id from lookup."""
        from agentscope_sdk import target
        from agentscope_sdk.runner import run_once
        from agentscope_sdk.client import ClaimedJob

        @target(name="runid-agent", version="1.0")
        def my_agent(case_input, context):
            return "output"

        claimed = ClaimedJob(
            id="job-runid", experiment_id="exp-1", dataset_case_id="case-runid",
            case_input="test", target_name="runid-agent", target_version="1.0",
        )
        client = _mock_client(claim_job_return=claimed)
        client.submit_result = MagicMock()
        client.get_run_id_by_trace = MagicMock(return_value="pg-run-uuid-123")

        with (
            patch("agentscope_sdk.runner.configure_tracing") as mock_ct,
            patch("agentscope_sdk.runner.force_flush") as mock_ff,
        ):
            mock_ct.return_value = _make_mock_tracer()
            result = run_once(client)

        assert result is True
        client.submit_result.assert_called_once()
        payload = client.submit_result.call_args[0][1]
        assert payload.run_id == "pg-run-uuid-123"
        assert payload.actual_output == "output"

    def test_configure_tracing_called_with_correct_args(self):
        """configure_tracing should be called with server_url, project_token,
        and service_name — but NOT with resource_attributes."""
        from agentscope_sdk import target
        from agentscope_sdk.runner import run_once
        from agentscope_sdk.client import ClaimedJob

        @target(name="cfg-agent", version="2.0")
        def my_agent(case_input, context):
            return "ok"

        claimed = ClaimedJob(
            id="job-cfg", experiment_id="exp-1", dataset_case_id="case-cfg",
            case_input="test", target_name="cfg-agent", target_version="2.0",
        )
        client = _mock_client(claim_job_return=claimed)
        client.submit_result = MagicMock()
        client.get_run_id_by_trace = MagicMock(return_value="pg-run-456")

        with (
            patch("agentscope_sdk.runner.configure_tracing") as mock_ct,
            patch("agentscope_sdk.runner.force_flush") as mock_ff,
        ):
            mock_ct.return_value = _make_mock_tracer()
            run_once(client)

        mock_ct.assert_called_once()
        call_kwargs = mock_ct.call_args.kwargs
        assert call_kwargs["server_url"] == client._base_url
        assert call_kwargs["project_token"] == client._project_token
        assert call_kwargs["service_name"] == "agentscope-runner"
        # resource_attributes should NOT be passed
        assert "resource_attributes" not in call_kwargs

    def test_span_attributes_include_agentscope_keys(self):
        """Root span attributes should include agentscope.* keys."""
        from agentscope_sdk import target
        from agentscope_sdk.runner import run_once
        from agentscope_sdk.client import ClaimedJob

        @target(name="attrs-agent", version="3.0")
        def my_agent(case_input, context):
            context["run_id"] = "runtime-run-123"
            return "result"

        claimed = ClaimedJob(
            id="job-attrs", experiment_id="exp-1", dataset_case_id="case-attrs",
            case_input="test", target_name="attrs-agent", target_version="3.0",
        )
        client = _mock_client(claim_job_return=claimed)
        client.submit_result = MagicMock()
        client.get_run_id_by_trace = MagicMock(return_value="pg-run-789")

        mock_span = _make_mock_span()

        with (
            patch("agentscope_sdk.runner.configure_tracing") as mock_ct,
            patch("agentscope_sdk.runner.force_flush") as mock_ff,
        ):
            mock_ct.return_value = _make_mock_tracer(return_span=mock_span)
            run_once(client)

        required = ["agentscope.run.id", "agentscope.agent.name",
                     "agentscope.agent.version", "agentscope.eval.job_id",
                     "agentscope.dataset.case_id"]
        for attr in required:
            mock_span.set_attribute.assert_any_call(attr, unittest.mock.ANY)

    def test_trace_id_is_agentscope_trace_id(self):
        """agentscope.trace.id should equal the formatted OTel trace_id."""
        from agentscope_sdk import target
        from agentscope_sdk.runner import run_once
        from agentscope_sdk.client import ClaimedJob

        @target(name="traceattr-agent", version="1.0")
        def my_agent(case_input, context):
            return "ok"

        claimed = ClaimedJob(
            id="job-traceattr", experiment_id="exp-1",
            dataset_case_id="case-traceattr",
            case_input="test", target_name="traceattr-agent", target_version="1.0",
        )
        client = _mock_client(claim_job_return=claimed)
        client.submit_result = MagicMock()
        client.get_run_id_by_trace = MagicMock(return_value="pg-run")

        mock_span = _make_mock_span()

        with (
            patch("agentscope_sdk.runner.configure_tracing") as mock_ct,
            patch("agentscope_sdk.runner.force_flush") as mock_ff,
        ):
            mock_ct.return_value = _make_mock_tracer(return_span=mock_span)
            run_once(client)

        mock_span.set_attribute.assert_any_call("agentscope.trace.id", FAKE_TRACE_ID_HEX)

    def test_two_consecutive_jobs_have_distinct_traces(self):
        """Two consecutive calls to run_once with different jobs should
        create separate traces and use distinct run_ids."""
        from agentscope_sdk import target
        from agentscope_sdk.runner import run_once
        from agentscope_sdk.client import ClaimedJob

        all_trace_ids = []

        @target(name="multi-job-agent", version="1.0")
        def my_agent(case_input, context):
            all_trace_ids.append(context.get("trace_id"))
            return context.get("trace_id", "missing")

        first_job = ClaimedJob(
            id="job-a", experiment_id="exp-1", dataset_case_id="case-a",
            case_input="first", target_name="multi-job-agent", target_version="1.0",
        )
        second_job = ClaimedJob(
            id="job-b", experiment_id="exp-1", dataset_case_id="case-b",
            case_input="second", target_name="multi-job-agent", target_version="1.0",
        )

        # Use two different trace IDs for the two jobs
        trace_a = 0x11111111111111111111111111111111
        trace_b = 0x22222222222222222222222222222222

        client = _mock_client(claim_job_return=None)
        client.submit_result = MagicMock()
        client.get_run_id_by_trace = MagicMock(side_effect=lambda t: f"pg-{t[:16]}")

        with (
            patch("agentscope_sdk.runner.configure_tracing") as mock_ct,
            patch("agentscope_sdk.runner.force_flush") as mock_ff,
        ):
            # ---- Job 1 ----
            client.claim_job.return_value = first_job
            span1 = _make_mock_span()
            span1.get_span_context.return_value.trace_id = trace_a
            mock_ct.return_value = _make_mock_tracer(return_span=span1)
            run_once(client)

            # ---- Job 2 ----
            client.claim_job.return_value = second_job
            span2 = _make_mock_span()
            span2.get_span_context.return_value.trace_id = trace_b
            mock_ct.return_value = _make_mock_tracer(return_span=span2)
            run_once(client)

        # The two jobs should have received different trace IDs in context
        assert len(all_trace_ids) == 2
        assert all_trace_ids[0] != all_trace_ids[1]
        assert all_trace_ids[0] == format(trace_a, "032x")
        assert all_trace_ids[1] == format(trace_b, "032x")

    def test_tracer_start_as_current_span_throws_still_calls_fail_job(self):
        """When tracer.start_as_current_span() throws, run_once must NOT
        propagate the exception and MUST call client.fail_job."""
        from agentscope_sdk import target
        from agentscope_sdk.runner import run_once
        from agentscope_sdk.client import ClaimedJob

        @target(name="throw-agent", version="1.0")
        def my_agent(case_input, context):
            return "should never reach here"

        claimed = ClaimedJob(
            id="job-throw-tracer", experiment_id="exp-1", dataset_case_id="case-tracer",
            case_input="test", target_name="throw-agent", target_version="1.0",
        )
        client = _mock_client(claim_job_return=claimed)
        client.submit_result = MagicMock()
        client.get_run_id_by_trace = MagicMock()
        client.fail_job = MagicMock()  # Track whether fail_job was called

        with patch("agentscope_sdk.runner.configure_tracing") as mock_ct:
            # Make start_as_current_span throw immediately
            bad_tracer = MagicMock()
            bad_tracer.start_as_current_span.side_effect = RuntimeError("tracer crashed")
            mock_ct.return_value = bad_tracer

            # Must not raise
            run_once(client)

            # fail_job must have been called with target_error
            client.fail_job.assert_called_once()
            call_kwargs = client.fail_job.call_args
            # First positional arg is the error_type
            assert "target_error" in str(call_kwargs), (
                f"expected target_error in fail_job call, got {call_kwargs}"
            )


class TestRunnerLoop:
    """Tests for the run_loop function."""

    def test_run_loop_calls_shutdown_tracing_at_exit(self):
        """run_loop should call shutdown_tracing when the loop exits."""
        from agentscope_sdk.runner import run_loop

        client = _mock_client(claim_job_return=None)  # no jobs available

        with (
            patch("agentscope_sdk.runner.shutdown_tracing") as mock_shutdown,
            patch("agentscope_sdk.runner.time") as mock_time,
        ):
            mock_time.sleep.side_effect = StopIteration  # break the loop

            try:
                run_loop(client, once=False)
            except StopIteration:
                pass

        mock_shutdown.assert_called_once()


class TestMultiMetricsTraceAware:
    """Tests for multi_metrics evaluation with trace data."""

    def test_required_tool_called_with_trace_passes(self):
        """When run_detail includes the required tool, tool_correctness passes."""
        from agentscope_sdk import target
        from agentscope_sdk.runner import run_once
        from agentscope_sdk.client import ClaimedJob

        @target(name="tool-agent", version="1.0")
        def my_agent(case_input, context):
            return "output"

        claimed = ClaimedJob(
            id="job-tool-pass", experiment_id="exp-1", dataset_case_id="case-tool",
            case_input="test", target_name="tool-agent", target_version="1.0",
            eval_config={
                "type": "exact",
                "multi_metrics": [
                    {
                        "type": "required_tool_called",
                        "tool_name": "search",
                        "metric_name": "tool-usage",
                        "metric_kind": "tool_correctness",
                    },
                    {
                        "type": "exact",
                        "metric_name": "final-answer",
                        "metric_kind": "final_answer",
                    },
                ],
            },
            case_expected="output",
        )
        client = _mock_client(claim_job_return=claimed)
        client.submit_result = MagicMock()
        client.get_run_id_by_trace = MagicMock(return_value="pg-run-tool")
        # Fake get_run_detail returns a run detail containing search tool call
        client.get_run_detail = MagicMock(return_value={
            "tool_calls": [
                {
                    "tool_name": "search",
                    "arguments_summary": "query: test",
                    "result_summary": "found results",
                    "success": True,
                    "span_id": "span-001",
                },
            ],
            "retrieval_calls": [],
            "spans": [],
        })

        with (
            patch("agentscope_sdk.runner.configure_tracing") as mock_ct,
            patch("agentscope_sdk.runner.force_flush") as mock_ff,
        ):
            mock_ct.return_value = _make_mock_tracer()
            result = run_once(client)

        assert result is True
        client.submit_result.assert_called_once()
        payload = client.submit_result.call_args[0][1]
        assert payload.passed is True
        assert payload.run_id == "pg-run-tool"
        # Verify eval_scores were submitted with correct metric_kind
        assert payload.eval_scores is not None
        tool_scores = [es for es in payload.eval_scores if es["metric_kind"] == "tool_correctness"]
        assert len(tool_scores) == 1
        assert tool_scores[0]["passed"] is True
        assert "search" in tool_scores[0]["reason"]

        # Assert get_run_detail was called with the correct backend_run_id
        client.get_run_detail.assert_called_once_with("pg-run-tool")

    def test_required_tool_called_without_trace_fails(self):
        """When run_detail has no tool calls, tool_correctness fails with reason."""
        from agentscope_sdk import target
        from agentscope_sdk.runner import run_once
        from agentscope_sdk.client import ClaimedJob

        @target(name="no-tool-agent", version="1.0")
        def my_agent(case_input, context):
            return "output"

        claimed = ClaimedJob(
            id="job-tool-fail", experiment_id="exp-1", dataset_case_id="case-notool",
            case_input="test", target_name="no-tool-agent", target_version="1.0",
            eval_config={
                "type": "exact",
                "multi_metrics": [
                    {
                        "type": "required_tool_called",
                        "tool_name": "search",
                        "metric_name": "tool-usage",
                        "metric_kind": "tool_correctness",
                    },
                ],
            },
            case_expected="output",
        )
        client = _mock_client(claim_job_return=claimed)
        client.submit_result = MagicMock()
        client.get_run_id_by_trace = MagicMock(return_value="pg-run-notool")
        # Fake get_run_detail returns empty tool_calls
        client.get_run_detail = MagicMock(return_value={
            "tool_calls": [],
            "retrieval_calls": [],
            "spans": [],
        })

        with (
            patch("agentscope_sdk.runner.configure_tracing") as mock_ct,
            patch("agentscope_sdk.runner.force_flush") as mock_ff,
        ):
            mock_ct.return_value = _make_mock_tracer()
            result = run_once(client)

        assert result is True
        client.submit_result.assert_called_once()
        payload = client.submit_result.call_args[0][1]
        assert payload.passed is False
        # Verify the eval_score for tool_correctness is not passed with clear reason
        assert payload.eval_scores is not None
        tool_scores = [es for es in payload.eval_scores if es["metric_kind"] == "tool_correctness"]
        assert len(tool_scores) == 1
        assert tool_scores[0]["passed"] is False
        # Reason should indicate no tool calls found
        assert "no tool calls" in tool_scores[0]["reason"].lower()

        # Assert get_run_detail was called with the correct backend_run_id
        client.get_run_detail.assert_called_once_with("pg-run-notool")

    def test_multiple_judges_use_server_aggregate(self):
        from agentscope_sdk import target
        from agentscope_sdk.client import ClaimedJob
        from agentscope_sdk.runner import run_once

        @target(name="multi-judge-agent", version="1.0")
        def my_agent(case_input, context):
            return "answer"

        claimed = ClaimedJob(
            id="job-judges",
            experiment_id="exp-1",
            dataset_case_id="case-1",
            case_input="question",
            target_name="multi-judge-agent",
            target_version="1.0",
            eval_config={
                "multi_metrics": [{
                    "metric_name": "quality",
                    "metric_kind": "final_answer",
                    "weight": 1,
                    "judges": [
                        {"provider": "fake-a", "model": "a"},
                        {"provider": "fake-b", "model": "b"},
                    ],
                    "judge_aggregation": {
                        "method": "median",
                        "min_valid_votes": 2,
                    },
                }],
            },
        )
        client = _mock_client(claim_job_return=claimed)
        client.get_run_id_by_trace.return_value = "run-judges"
        client.get_run_detail.return_value = {
            "tool_calls": [], "retrieval_calls": [], "spans": [],
        }
        client.submit_judge_votes.return_value = {
            "id": "agg-1",
            "result": {
                "method": "median",
                "score": 0.85,
                "passed": True,
                "valid_vote_count": 2,
                "veto_triggered": False,
            },
        }

        with (
            patch("agentscope_sdk.runner.configure_tracing") as mock_ct,
            patch("agentscope_sdk.runner.force_flush"),
            patch("agentscope_sdk.runner.evaluate_judges") as mock_judges,
        ):
            mock_ct.return_value = _make_mock_tracer()
            mock_judges.return_value = [
                {"provider": "fake-a", "score": 0.8, "passed": True},
                {"provider": "fake-b", "score": 0.9, "passed": True},
            ]
            assert run_once(client) is True

        client.submit_judge_votes.assert_called_once()
        payload = client.submit_result.call_args[0][1]
        assert payload.score == 0.85
        assert payload.passed is True
        assert payload.eval_scores is None
