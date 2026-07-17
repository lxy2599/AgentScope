"""Runner execution orchestration."""

from __future__ import annotations

import time
import uuid
from typing import Any

from agentscope_sdk.client import ClaimedJob, JobResultPayload, RunnerClient
from agentscope_sdk.evaluators import EvaluatorConfigError, evaluate_rule
from agentscope_sdk.target import TargetNotFoundError, get_target
from agentscope_sdk.tracing import configure_tracing, force_flush, shutdown_tracing


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
    while True:
        processed = run_once(client)
        if once:
            break
        if not processed:
            time.sleep(poll_interval)


def _execute_job(client: RunnerClient, claimed: ClaimedJob) -> None:
    """Execute a claimed job: look up target, run it with tracing, evaluate, submit/fail.

    A stable ``run_id`` is generated from the job ID and communicated to the
    backend via:

    1.  OpenTelemetry span attributes (``agentscope.run.id``) on every span.
    2.  The ``run_id`` field in the submit-result payload.

    After sending OTLP traces, the runner looks up the backend's PostgreSQL
    run ID by trace ID so the eval result is correctly linked to the trace.

    Fail scenarios:
    - Target not found -> fail_job(error_type="target_not_found")
    - Target execution error -> fail_job(error_type="target_error")
    - Evaluator config error -> fail_job(error_type="evaluator_config_error")
    """
    job_id = claimed.id
    run_id = f"run-{job_id}"  # stable external ID used across spans & result
    trace_id = uuid.uuid4().hex  # unique trace per job execution

    # 1. Look up target
    try:
        target_func = get_target(claimed.target_name).func
    except TargetNotFoundError:
        client.fail_job(
            job_id,
            error_type="target_not_found",
            message=f"Target '{claimed.target_name}' is not registered",
        )
        return

    # 2. Build execution context (shared with target via ``context``)
    context: dict[str, Any] = {
        "job_id": claimed.id,
        "case_id": claimed.dataset_case_id,
        "metadata": claimed.case_metadata or {},
        "agent_version": claimed.target_version,
        "run_id": run_id,
        "trace_id": trace_id,
    }

    # 3. Configure tracing for this job
    tracer = configure_tracing(
        server_url=client._base_url,
        project_token=client._project_token,
        service_name="agentscope-runner",
        resource_attributes={
            "agentscope.run.id": run_id,
            "agentscope.agent.name": claimed.target_name,
            "agentscope.agent.version": claimed.target_version or "",
            "agentscope.eval.job_id": job_id,
            "agentscope.dataset.case_id": claimed.dataset_case_id,
            "agentscope.trace.id": trace_id,
        },
    )

    actual_output = None
    latency_ms = 0
    target_error: str | None = None

    try:
        # 4. Execute target within a root span
        with tracer.start_as_current_span("agent.run") as root_span:
            root_span.set_attribute("agentscope.run.id", run_id)
            root_span.set_attribute("agentscope.agent.name", claimed.target_name)
            root_span.set_attribute("agentscope.agent.version", claimed.target_version or "")
            root_span.set_attribute("agentscope.eval.job_id", job_id)
            root_span.set_attribute("agentscope.dataset.case_id", claimed.dataset_case_id)

            start_time = time.monotonic()
            try:
                actual_output = target_func(claimed.case_input, context)
            except Exception as e:
                target_error = str(e)
                root_span.set_status(trace_api.Status(trace_api.StatusCode.ERROR, target_error))
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
        shutdown_tracing()
        return

    # 5. Evaluate result
    metrics = {"latency_ms": latency_ms}
    eval_config = claimed.eval_config
    if eval_config:
        try:
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
            shutdown_tracing()
            return
    else:
        # No evaluator: treat execution success as a pass
        eval_result = type(
            "EvalResult",
            (),
            {"score": 1.0, "passed": True, "reason": "Executed successfully, no evaluator configured", "details": {}},
        )()

    # 6. Flush traces to ensure they reach the backend before we look up the run
    force_flush()

    # 7. Look up the backend PostgreSQL run ID from the trace
    backend_run_id = client.get_run_id_by_trace(trace_id)
    if backend_run_id is None:
        # Last resort: use the external run_id as a fallback so results are
        # not orphaned even if the trace export is delayed.
        backend_run_id = run_id

    # 8. Submit result with the real run_id
    output_str = str(actual_output) if actual_output is not None else ""
    payload = JobResultPayload(
        run_id=backend_run_id,
        score=eval_result.score,
        passed=eval_result.passed,
        reason=eval_result.reason,
        actual_output=output_str,
        latency_ms=latency_ms,
        details=getattr(eval_result, "details", None),
    )
    client.submit_result(job_id, payload)

    # 9. Clean up tracing
    shutdown_tracing()


# Import trace API for status codes
from opentelemetry import trace as trace_api  # noqa: E402, F811
