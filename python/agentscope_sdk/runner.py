"""Runner execution orchestration."""

from __future__ import annotations

import time
from typing import Any

from agentscope_sdk.client import ClaimedJob, JobResultPayload, RunnerClient
from agentscope_sdk.evaluators import EvaluatorConfigError, evaluate_rule
from agentscope_sdk.target import TargetNotFoundError, get_target
from agentscope_sdk.tracing import configure_tracing, force_flush, shutdown_tracing

# Max retries and delay when looking up a PostgreSQL run_id by OTLP trace ID.
_TRACE_LOOKUP_RETRIES = 5
_TRACE_LOOKUP_DELAY_S = 0.2


# ---- Public API ----


def run_once(client: RunnerClient) -> bool:
    """Claim and execute a single job.

    This function performs one iteration of the runner loop:
    1. Claim a job from the server.
    2. If no jobs are available, return ``False``.
    3. Look up the registered target by name.
    4. Execute the target with the case input and context.
    5. Run the evaluator if a config is provided.
    6. Submit the result (or fail the job on error).

    Args:
        client: A ``RunnerClient`` connected to the AgentScope server.

    Returns:
        ``True`` if a job was processed, ``False`` if no jobs were available.
    """
    claimed = client.claim_job()
    if claimed is None:
        return False

    _execute_job(client, claimed)
    return True


def run_loop(
    client: RunnerClient,
    *,
    once: bool = False,
    poll_interval: float = 2.0,
) -> None:
    """Run the runner loop, claiming and executing jobs.

    Args:
        client: A ``RunnerClient`` connected to the AgentScope server.
        once: If ``True``, execute only one job and exit.
        poll_interval: Seconds to wait between claim attempts when idle.
    """
    try:
        while True:
            processed = run_once(client)
            if once:
                break
            if not processed:
                time.sleep(poll_interval)
    finally:
        # Shut down the global TracerProvider once at loop exit.
        shutdown_tracing()


# ---- Internal helpers ----


def _execute_job(client: RunnerClient, claimed: ClaimedJob) -> None:
    """Execute a claimed job: look up target, trace, evaluate, submit/fail.

    The execution trace is linked to the backend via:

    1.  An OTLP trace sent to the backend via the OpenTelemetry exporter.
    2.  The trace's auto-generated ``trace_id``, extracted from the root span
        context *after* the span is created.
    3.  A ``GET /runner/runs/by-trace/{actual_trace_id}`` lookup that returns
        the PostgreSQL ``run_id``.
    4.  The ``run_id`` is included in the submit-result payload.

    If the trace lookup fails after retries, the job is failed rather than
    submitting an orphaned result.

    For multi-dimensional metrics that need trace data (tool_sequence,
    required_tool_called), the runner fetches real trace data from the
    backend after obtaining the PostgreSQL run_id.

    Fail scenarios:
    - Target not found -> fail_job(error_type="target_not_found")
    - Target execution error -> fail_job(error_type="target_error")
    - Trace lookup timeout -> fail_job(error_type="trace_lookup_failed")
    - Evaluator config error -> fail_job(error_type="evaluator_config_error")
    """
    job_id = claimed.id

    # ---- 1. Look up target ----
    try:
        target_func = get_target(claimed.target_name).func
    except TargetNotFoundError:
        client.fail_job(
            job_id,
            error_type="target_not_found",
            message=f"Target '{claimed.target_name}' is not registered",
        )
        return

    # ---- 2. Build execution context ----
    context: dict[str, Any] = {
        "job_id": claimed.id,
        "case_id": claimed.dataset_case_id,
        "metadata": claimed.case_metadata or {},
        "agent_version": claimed.target_version,
        "expected": claimed.case_expected,
    }

    # ---- 3. Configure tracing (idempotent -- creates TracerProvider once) ----
    tracer = configure_tracing(
        server_url=client._base_url,
        project_token=client._project_token,
        service_name="agentscope-runner",
    )

    actual_output: Any = None
    latency_ms = 0
    target_error: str | None = None
    backend_run_id: str | None = None

    # ---- 4. Execute target within a root span ----
    try:
        with tracer.start_as_current_span("agent.run") as root_span:
            # Extract the *real* OTLP trace_id from the span context.
            actual_trace_id = _format_trace_id(root_span.get_span_context().trace_id)

            # Make the trace_id available to child spans via the context.
            context["run_id"] = actual_trace_id
            context["trace_id"] = actual_trace_id

            root_span.set_attribute("agentscope.run.id", actual_trace_id)
            root_span.set_attribute("agentscope.agent.name", claimed.target_name)
            root_span.set_attribute("agentscope.agent.version", claimed.target_version or "")
            root_span.set_attribute("agentscope.eval.job_id", job_id)
            root_span.set_attribute("agentscope.dataset.case_id", claimed.dataset_case_id)
            root_span.set_attribute("agentscope.trace.id", actual_trace_id)

            start_time = time.monotonic()
            try:
                actual_output = target_func(claimed.case_input, context)
            except Exception as e:
                target_error = str(e)
                root_span.set_status(
                    trace_api.Status(trace_api.StatusCode.ERROR, target_error)
                )
                raise
            finally:
                end_time = time.monotonic()
                latency_ms = int((end_time - start_time) * 1000)
                root_span.set_attribute("latency_ms", latency_ms)
    except Exception as e:
        if target_error is None:
            target_error = str(e)
        client.fail_job(
            job_id,
            error_type="target_error",
            message=target_error,
        )
        force_flush()
        return

    # ---- 5. Evaluate single evaluator (no trace needed, backwards compatible) ----
    metrics = {"latency_ms": latency_ms}
    eval_config = claimed.eval_config
    eval_result = None
    eval_scores_list: list[dict[str, Any]] = []
    has_multi_metrics = False

    if eval_config:
        try:
            multi_metrics = eval_config.get("multi_metrics", [])
            has_multi_metrics = bool(multi_metrics)
            if not has_multi_metrics:
                # Single evaluator (backwards compatible, no trace needed)
                eval_result = evaluate_rule(
                    config=eval_config,
                    actual=actual_output,
                    expected=claimed.case_expected,
                    metrics=metrics,
                )
        except EvaluatorConfigError as e:
            client.fail_job(
                job_id,
                error_type="evaluator_config_error",
                message=str(e),
            )
            force_flush()
            return

    if eval_result is None:
        eval_result = type(
            "EvalResult",
            (),
            {
                "score": 1.0,
                "passed": True,
                "reason": "Agent executed successfully; no evaluator configured, score defaults to 1.0",
                "details": {},
            },
        )()

    # ---- 6. Flush spans to the backend so the run is created ----
    force_flush()

    # ---- 7. Look up the PostgreSQL run_id from the real trace_id (with retry) ----
    for attempt in range(1, _TRACE_LOOKUP_RETRIES + 1):
        backend_run_id = client.get_run_id_by_trace(actual_trace_id)
        if backend_run_id is not None:
            break
        if attempt < _TRACE_LOOKUP_RETRIES:
            time.sleep(_TRACE_LOOKUP_DELAY_S)

    if backend_run_id is None:
        error_msg = (
            f"Trace lookup failed after {_TRACE_LOOKUP_RETRIES} retries "
            f"for trace_id={actual_trace_id[:16]}... "
            "OTLP export may have failed or the backend ingestion is slow."
        )
        client.fail_job(
            job_id,
            error_type="trace_lookup_failed",
            message=error_msg,
        )
        force_flush()
        return

    # ---- 7b. For multi-dimensional metrics, fetch real trace data ----
    if has_multi_metrics and eval_config:
        trace_data: dict[str, Any] | None = None
        try:
            run_detail = client.get_run_detail(backend_run_id)
            if run_detail:
                trace_data = {
                    "tool_calls": [
                        {
                            "tool_name": tc.get("tool_name", ""),
                            "arguments_summary": tc.get("arguments_summary", ""),
                            "result_summary": tc.get("result_summary", ""),
                            "success": tc.get("success", False),
                            "error": tc.get("error"),
                            "span_id": tc.get("span_id", ""),
                            "duration_ms": tc.get("duration_ms"),
                        }
                        for tc in (run_detail.get("tool_calls") or [])
                    ],
                    "retrieval_calls": [
                        {
                            "knowledge_base": rc.get("knowledge_base", ""),
                            "query_text": rc.get("query_text", ""),
                            "document_ids": rc.get("document_ids", []),
                            "scores": rc.get("scores", []),
                            "duration_ms": rc.get("duration_ms"),
                        }
                        for rc in (run_detail.get("retrieval_calls") or [])
                    ],
                    "spans": [
                        {
                            "name": s.get("name", ""),
                            "span_kind": s.get("kind", ""),
                            "status_code": s.get("status_code", ""),
                            "duration_ms": s.get("duration_ms"),
                        }
                        for s in (run_detail.get("spans") or [])
                    ],
                }
        except Exception:
            trace_data = None

        try:
            for i, metric_cfg in enumerate(multi_metrics):
                metric_result = evaluate_rule(
                    config=metric_cfg,
                    actual=actual_output,
                    expected=claimed.case_expected,
                    metrics=metrics,
                    trace=trace_data,
                )
                metric_name = metric_cfg.get("metric_name", metric_cfg.get("name", f"metric-{i}"))
                metric_kind = metric_cfg.get("metric_kind", metric_cfg.get("kind", ""))
                eval_scores_list.append({
                    "metric_name": metric_name,
                    "metric_kind": metric_kind,
                    "score": metric_result.score,
                    "passed": metric_result.passed,
                    "reason": metric_result.reason,
                    "details": metric_result.details if metric_result.details else {},
                    "trace_id": actual_trace_id,
                })
                # Use the first metric as the primary result
                if i == 0:
                    eval_result = metric_result
            if not eval_result:
                eval_result = type(
                    "EvalResult",
                    (),
                    {
                        "score": 1.0,
                        "passed": True,
                        "reason": "No metrics evaluated",
                        "details": {},
                    },
                )()
        except EvaluatorConfigError as e:
            client.fail_job(
                job_id,
                error_type="evaluator_config_error",
                message=str(e),
            )
            force_flush()
            return

    # ---- 8. Submit result with the real PostgreSQL run_id ----
    output_str = str(actual_output) if actual_output is not None else ""
    payload = JobResultPayload(
        run_id=backend_run_id,
        score=eval_result.score,
        passed=eval_result.passed,
        reason=eval_result.reason,
        actual_output=output_str,
        latency_ms=latency_ms,
        details=getattr(eval_result, "details", None),
        eval_scores=eval_scores_list if eval_scores_list else None,
    )
    client.submit_result(job_id, payload)

    # Flush after submit so any remaining span events are sent
    force_flush()


def _format_trace_id(trace_id: int) -> str:
    """Format a 128-bit OTel trace ID as a zero-padded 32-char hex string."""
    return format(trace_id, "032x")


# Import trace API for status codes (kept at bottom to avoid circular issues)
from opentelemetry import trace as trace_api  # noqa: E402
