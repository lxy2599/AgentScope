"""Runner execution orchestration."""

from __future__ import annotations

import time
from typing import Any

from agentscope_sdk.client import ClaimedJob, JobResultPayload, RunnerClient
from agentscope_sdk.evaluators import EvaluatorConfigError, evaluate_rule
from agentscope_sdk.target import TargetNotFoundError, get_target


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
    """Execute a claimed job: look up target, run it, evaluate, submit/ fail.

    Fail scenarios:
    - Target not found -> fail_job(error_type="target_not_found")
    - Target execution error -> fail_job(error_type="target_error")
    - Evaluator config error -> fail_job(error_type="evaluator_config_error")
    """
    job_id = claimed.id

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

    # 2. Build execution context
    context: dict[str, Any] = {
        "job_id": claimed.id,
        "case_id": claimed.dataset_case_id,
        "metadata": claimed.case_metadata or {},
        "agent_version": claimed.target_version,
    }

    # 3. Execute target
    try:
        start_time = time.monotonic()
        actual_output = target_func(claimed.case_input, context)
        end_time = time.monotonic()
        latency_ms = int((end_time - start_time) * 1000)
    except Exception as e:
        client.fail_job(
            job_id,
            error_type="target_error",
            message=str(e),
        )
        return

    # 4. Evaluate result
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
            return
    else:
        # No evaluator: treat execution success as a pass
        eval_result = type(
            "EvalResult",
            (),
            {"score": 1.0, "passed": True, "reason": "Executed successfully, no evaluator configured", "details": {}},
        )()

    # 5. Submit result
    output_str = str(actual_output) if actual_output is not None else ""
    payload = JobResultPayload(
        score=eval_result.score,
        passed=eval_result.passed,
        reason=eval_result.reason,
        actual_output=output_str,
        latency_ms=latency_ms,
        details=getattr(eval_result, "details", None),
    )
    client.submit_result(job_id, payload)
