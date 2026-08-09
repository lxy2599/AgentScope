"""Runner execution orchestration."""

from __future__ import annotations

import logging
import threading
import time
from asyncio import CancelledError as AsyncCancelledError
from collections.abc import Iterator, Sequence
from concurrent.futures import CancelledError as FutureCancelledError
from contextlib import contextmanager
from dataclasses import asdict, dataclass, field
from datetime import UTC, datetime
from typing import Any

from agentscope_sdk.client import ClaimedJob, JobResultPayload, RunnerClient
from agentscope_sdk.evaluators import EvaluatorConfigError, evaluate_rule
from agentscope_sdk.executors import (
    BenchmarkErrorType,
    BenchmarkExecutionError,
    BenchmarkExecutionRequest,
    BenchmarkExecutionResult,
    ExecutorRegistry,
    get_executor_registry,
)
from agentscope_sdk.multi_judges import (
    MultiJudgeError,
    aggregate_scores_with_veto,
    evaluate_judge,
    evaluate_judges,
)
from agentscope_sdk.target import TargetNotFoundError, get_target
from agentscope_sdk.tracing import configure_tracing, force_flush, shutdown_tracing
from agentscope_sdk.tracing_contract import SpanAttribute, SpanName

logger = logging.getLogger(__name__)

# Max retries and delay when looking up a PostgreSQL run_id by OTLP trace ID.
_TRACE_LOOKUP_RETRIES = 5
_TRACE_LOOKUP_DELAY_S = 0.2

# Renew at one third of the remaining lease, leaving a two-thirds safety
# margin. The fallback covers older servers that omit lease_expires_at.
_LEASE_HEARTBEAT_FRACTION = 1.0 / 3.0
_DEFAULT_HEARTBEAT_INTERVAL_S = 30.0
_MIN_HEARTBEAT_INTERVAL_S = 1.0

_CANCELLED_ERRORS = (AsyncCancelledError, FutureCancelledError)


# ---- Harness Metadata ----


@dataclass
class HarnessMetadata:
    """Metadata for experiment reproducibility and comparison.

    Captures the full Harness identity of an experiment or run, enabling
    model_swap, ablation, and general version comparisons. Every field is
    optional — only the values that are set will be sent to the server.
    """

    model_provider: str = ""
    model_name: str = ""
    model_version: str = ""
    prompt_version: str = ""
    harness_version: str = ""
    dataset_version: str = ""
    toolset_version: str = ""
    skillset_version: str = ""
    rag_config_version: str = ""
    feature_flags: dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> dict[str, Any]:
        """Convert to a plain dict for JSON serialization."""
        return asdict(self)


# ---- Public API ----


def run_once(
    client: RunnerClient,
    *,
    executor_registry: ExecutorRegistry | None = None,
    execution_kinds: Sequence[str] | None = None,
) -> bool:
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
    claimed = client.claim_job(execution_kinds=execution_kinds)
    if claimed is None:
        return False

    with _maintain_job_lease(client, claimed):
        if executor_registry is None:
            _execute_job(client, claimed)
        else:
            _execute_job(client, claimed, executor_registry=executor_registry)
    return True


def run_loop(
    client: RunnerClient,
    *,
    once: bool = False,
    poll_interval: float = 2.0,
    executor_registry: ExecutorRegistry | None = None,
    execution_kinds: Sequence[str] | None = None,
    exit_when_idle: bool = False,
) -> None:
    """Run the runner loop, claiming and executing jobs.

    Args:
        client: A ``RunnerClient`` connected to the AgentScope server.
        once: If ``True``, execute only one job and exit.
        poll_interval: Seconds to wait between claim attempts when idle.
        executor_registry: Optional execution strategy registry.
        execution_kinds: Execution kinds this runner may claim.
        exit_when_idle: Exit after the first empty claim instead of polling indefinitely.
    """
    try:
        while True:
            try:
                processed = run_once(
                    client,
                    executor_registry=executor_registry,
                    execution_kinds=execution_kinds,
                )
            except _CANCELLED_ERRORS:
                raise
            except Exception as exc:
                if once:
                    raise
                logger.warning(
                    "Runner iteration failed with %s; retrying",
                    type(exc).__name__,
                )
                time.sleep(poll_interval)
                continue

            if once:
                break
            if not processed:
                if exit_when_idle:
                    return
                time.sleep(poll_interval)
    finally:
        # Shut down the global TracerProvider once at loop exit.
        shutdown_tracing()


# ---- Internal helpers ----


def _lease_heartbeat_interval(
    lease_expires_at: str | None,
    *,
    now: datetime | None = None,
) -> float:
    """Calculate a safe heartbeat interval from the current job lease."""
    if not lease_expires_at:
        return _DEFAULT_HEARTBEAT_INTERVAL_S

    try:
        normalized_expiry = lease_expires_at.strip()
        if normalized_expiry.endswith(("Z", "z")):
            normalized_expiry = f"{normalized_expiry[:-1]}+00:00"
        expiry = datetime.fromisoformat(normalized_expiry)
        if expiry.tzinfo is None:
            expiry = expiry.replace(tzinfo=UTC)

        current = now or datetime.now(UTC)
        if current.tzinfo is None:
            current = current.replace(tzinfo=UTC)
        remaining_seconds = (expiry.astimezone(UTC) - current.astimezone(UTC)).total_seconds()
    except (AttributeError, TypeError, ValueError):
        logger.warning(
            "Claimed job contained an invalid lease expiry; using the default heartbeat interval",
        )
        return _DEFAULT_HEARTBEAT_INTERVAL_S

    return max(
        _MIN_HEARTBEAT_INTERVAL_S,
        remaining_seconds * _LEASE_HEARTBEAT_FRACTION,
    )


def _send_job_heartbeat(client: RunnerClient, job_id: str) -> None:
    """Renew a job lease, tolerating ordinary transient request failures."""
    try:
        client.heartbeat(job_id)
    except _CANCELLED_ERRORS:
        raise
    except Exception as exc:
        logger.warning(
            "Job heartbeat failed with %s; the next interval will retry",
            type(exc).__name__,
        )


def _job_heartbeat_loop(
    client: RunnerClient,
    job_id: str,
    interval: float,
    stop_event: threading.Event,
) -> None:
    """Renew a claimed job until its execution finishes."""
    while not stop_event.wait(interval):
        _send_job_heartbeat(client, job_id)


@contextmanager
def _maintain_job_lease(
    client: RunnerClient,
    claimed: ClaimedJob,
) -> Iterator[None]:
    """Keep a claimed job's lease alive for the full execution lifecycle."""
    interval = _lease_heartbeat_interval(claimed.lease_expires_at)
    stop_event = threading.Event()

    worker = threading.Thread(
        target=_job_heartbeat_loop,
        args=(client, claimed.id, interval, stop_event),
        name=f"agentscope-job-heartbeat-{claimed.id}",
        daemon=True,
    )
    try:
        worker.start()
    except BaseException:
        stop_event.set()
        if worker.is_alive():
            worker.join()
        raise

    try:
        yield
    finally:
        stop_event.set()
        worker.join()


def _build_target_failure_run_error(*, trace_id: str, error: str) -> str:
    """Build an error message for a target execution failure that includes
    the OTLP trace ID so operators can cross-reference the trace."""
    return f"Target execution failed for trace_id={trace_id[:16]}...: {error}"


def _fail_trace_run(client: RunnerClient, trace_id: str, error_msg: str) -> None:
    """Look up a run by its OTLP trace_id and mark it as failed.

    This is called after ``force_flush()`` so the trace spans have been
    sent to the backend.  Retries the trace lookup (same pattern as the
    success-path trace lookup) in case ingestion hasn't completed yet.

    **Best-effort only** — all exceptions are logged and swallowed.
    The caller is responsible for calling ``client.fail_job`` regardless.
    """
    try:
        for attempt in range(1, _TRACE_LOOKUP_RETRIES + 1):
            try:
                run_id = client.get_run_id_by_trace(trace_id)
            except Exception:
                logger.warning(
                    "fail_trace_run: get_run_id_by_trace threw (attempt %d/%d)",
                    attempt,
                    _TRACE_LOOKUP_RETRIES,
                    exc_info=True,
                )
                run_id = None

            if run_id is not None:
                try:
                    client.fail_run(run_id=run_id, error=error_msg)
                    return
                except Exception:
                    logger.warning(
                        "fail_trace_run: fail_run call failed for run_id=%s, trace_id=%s...",
                        run_id,
                        trace_id[:16],
                        exc_info=True,
                    )
                    return
            if attempt < _TRACE_LOOKUP_RETRIES:
                time.sleep(_TRACE_LOOKUP_DELAY_S)

        logger.warning(
            "fail_trace_run: no run found for trace_id=%s... "
            "after %d retries (ingestion may be slow or OTLP export failed)",
            trace_id[:16],
            _TRACE_LOOKUP_RETRIES,
        )
    except Exception:
        logger.warning(
            "fail_trace_run: unexpected error for trace_id=%s... (swallowed)",
            trace_id[:16],
            exc_info=True,
        )


def _execute_job(
    client: RunnerClient,
    claimed: ClaimedJob,
    *,
    executor_registry: ExecutorRegistry | None = None,
) -> None:
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
    - Benchmark environment/harness errors -> classified fail_job errors
    - Unsupported execution kind -> fail_job(error_type="unsupported_execution_kind")
    - Trace lookup timeout -> fail_job(error_type="trace_lookup_failed")
    - Evaluator config error -> fail_job(error_type="evaluator_config_error")
    """
    job_id = claimed.id

    # ---- 1. Resolve executor and target requirements ----
    try:
        executor = (executor_registry or get_executor_registry()).resolve(claimed.execution_kind)
    except BenchmarkExecutionError as exc:
        client.fail_job(
            job_id,
            error_type=exc.error_type.value,
            message=str(exc),
            details=exc.details,
        )
        return

    registered_target = None
    if getattr(executor, "requires_registered_target", True):
        try:
            registered_target = get_target(claimed.target_name)
        except TargetNotFoundError:
            client.fail_job(
                job_id,
                error_type="target_not_found",
                message=f"Target '{claimed.target_name}' is not registered",
            )
            return

        if claimed.target_version and registered_target.version != claimed.target_version:
            client.fail_job(
                job_id,
                error_type="target_version_mismatch",
                message=(
                    f"Target '{claimed.target_name}' version mismatch: "
                    f"expected '{claimed.target_version}', registered '{registered_target.version}'"
                ),
            )
            return

    # ---- 2. Build execution context ----
    context: dict[str, Any] = {
        "job_id": claimed.id,
        "case_id": claimed.dataset_case_id,
        "metadata": claimed.case_metadata or {},
        "agent_version": claimed.target_version,
        "expected": claimed.case_expected,
        "execution_kind": claimed.execution_kind,
        "execution_config": claimed.execution_config,
    }

    # ---- 3. Configure tracing (idempotent -- creates TracerProvider once) ----
    tracer = configure_tracing(
        server_url=client._base_url,
        project_token=client._project_token,
        service_name="agentscope-runner",
    )

    actual_output: Any = None
    execution_result: BenchmarkExecutionResult | None = None
    latency_ms = 0
    target_error: str | None = None
    backend_run_id: str | None = None
    actual_trace_id = ""  # Initialized before the tracing try so the except
    # path can safely check it before calling _fail_trace_run.

    # ---- 4. Execute target within a root span ----
    try:
        with tracer.start_as_current_span(SpanName.RUN) as root_span:
            # Extract the *real* OTLP trace_id from the span context.
            actual_trace_id = _format_trace_id(root_span.get_span_context().trace_id)

            # Make the trace_id available to child spans via the context.
            context["trace_id"] = actual_trace_id

            root_span.set_attribute(SpanAttribute.TRACE_ID, actual_trace_id)
            root_span.set_attribute("agentscope.agent.name", claimed.target_name)
            root_span.set_attribute("agentscope.agent.version", claimed.target_version or "")
            root_span.set_attribute("agentscope.eval.job_id", job_id)
            root_span.set_attribute("agentscope.dataset.case_id", claimed.dataset_case_id)
            root_span.set_attribute(SpanAttribute.TRACE_ID, actual_trace_id)

            start_time = time.monotonic()
            try:
                raw_execution_result = executor.execute(
                    BenchmarkExecutionRequest(
                        claimed_job=claimed,
                        target=registered_target,
                        context=context,
                    ),
                )
                if not isinstance(raw_execution_result, BenchmarkExecutionResult):
                    raise BenchmarkExecutionError(
                        BenchmarkErrorType.CONTRACT_ERROR,
                        "executors must return BenchmarkExecutionResult",
                        details={"returned_type": type(raw_execution_result).__name__},
                    )
                execution_result = raw_execution_result
                if not execution_result.valid:
                    raise BenchmarkExecutionError(
                        execution_result.error_type or BenchmarkErrorType.CONTRACT_ERROR,
                        execution_result.error_message or "Benchmark execution returned an invalid result",
                        details={"benchmark": execution_result.to_details()},
                    )
                actual_output = execution_result.actual_output
                # If the target function created a runtime run, capture its run_id
                # and link the OTLP trace to it so the backend reuses the existing run
                # instead of creating a duplicate via OTLP ingestion.
                target_run_id = context.get("run_id", "")
                if target_run_id:
                    root_span.set_attribute(SpanAttribute.RUN_ID, target_run_id)
                    backend_run_id = target_run_id
            except _CANCELLED_ERRORS:
                raise
            except BenchmarkExecutionError:
                raise
            except Exception as e:
                target_error = str(e)
                root_span.set_status(trace_api.Status(trace_api.StatusCode.ERROR, target_error))
                raise
            finally:
                end_time = time.monotonic()
                latency_ms = int((end_time - start_time) * 1000)
                root_span.set_attribute("latency_ms", latency_ms)
    except _CANCELLED_ERRORS:
        raise
    except BenchmarkExecutionError as exc:
        force_flush()
        if actual_trace_id:
            _fail_trace_run(
                client,
                actual_trace_id,
                _build_target_failure_run_error(trace_id=actual_trace_id, error=str(exc)),
            )
        client.fail_job(
            job_id,
            error_type=exc.error_type.value,
            message=str(exc),
            details=exc.details,
        )
        return
    except Exception as e:
        if target_error is None:
            target_error = str(e)
        force_flush()
        # Attempt to fail the trace-created run (best-effort).
        # Only try if we successfully created a trace (actual_trace_id is set).
        # This is wrapped in try/except so that any exception here
        # does NOT prevent client.fail_job from being called below.
        if actual_trace_id:
            try:
                _fail_trace_run(
                    client,
                    actual_trace_id,
                    _build_target_failure_run_error(
                        trace_id=actual_trace_id,
                        error=target_error,
                    ),
                )
            except Exception:
                logger.warning(
                    "fail_trace_run threw unexpectedly (swallowed to preserve fail_job)",
                    exc_info=True,
                )
        client.fail_job(
            job_id,
            error_type="target_error",
            message=target_error,
        )
        return

    # ---- 5. Evaluate single evaluator (no trace needed, backwards compatible) ----
    metrics = {"latency_ms": latency_ms}
    eval_config = claimed.eval_config
    eval_result = None
    eval_scores_list: list[dict[str, Any]] = []
    has_multi_metrics = False

    if execution_result and execution_result.has_official_verdict:
        eval_result = type(
            "EvalResult",
            (),
            {
                "score": execution_result.official_score,
                "passed": execution_result.official_passed,
                "reason": execution_result.official_reason,
                "details": {"benchmark": execution_result.to_details()},
            },
        )()
    elif eval_config:
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
    # If the target function already provided a run_id via context["run_id"],
    # skip the OTLP-based trace lookup entirely.
    if backend_run_id is None:
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
            aggregation_cfg = eval_config.get("aggregation", "weighted_average")
            global_pass_threshold = eval_config.get("pass_threshold", 0.7)
            metric_results_for_composite: list[dict[str, Any]] = []
            payload_metric_positions: list[int] = []

            for i, metric_cfg in enumerate(multi_metrics):
                metric_name = metric_cfg.get("metric_name", metric_cfg.get("name", f"metric-{i}"))
                metric_kind = metric_cfg.get("metric_kind", metric_cfg.get("kind", ""))
                metric_weight = metric_cfg.get("weight", 1.0)
                metric_threshold = metric_cfg.get("pass_threshold", global_pass_threshold)
                metric_veto = metric_cfg.get("veto", False)
                metric_essential = metric_cfg.get("essential", metric_veto)
                judge_configs = metric_cfg.get("judges") or []

                if judge_configs:
                    trace_summary = ""
                    if trace_data:
                        trace_summary = (
                            f"Tool calls: {len(trace_data.get('tool_calls', []))}, "
                            f"Retrievals: {len(trace_data.get('retrieval_calls', []))}"
                        )
                    votes = evaluate_judges(
                        list(judge_configs),
                        input_text=str(claimed.case_input),
                        output_text=str(actual_output),
                        expected=claimed.case_expected,
                        trace_summary=trace_summary,
                    )
                    for vote in votes:
                        if vote.get("error"):
                            continue
                        if metric_essential and not vote.get("passed", False):
                            vote["essential_failed"] = True
                        if metric_veto and not vote.get("passed", False):
                            vote["veto_triggered"] = True
                            vote["veto_reason"] = vote.get("reason") or metric_name

                    judge_policy = metric_cfg.get("judge_aggregation", {})
                    aggregate_record = client.submit_judge_votes(
                        backend_run_id,
                        metric_name=metric_name,
                        metric_kind=metric_kind,
                        evaluator_id=str(eval_config.get("evaluator_id", "")),
                        rubric_version_id=str(metric_cfg.get("rubric_version_id", "")),
                        votes=votes,
                        aggregation={
                            "method": judge_policy.get("method", "mean"),
                            "min_valid_votes": int(judge_policy.get("min_valid_votes", len(judge_configs))),
                            "pass_threshold": metric_threshold,
                            "trim_proportion": float(judge_policy.get("trim_proportion", 0.1)),
                        },
                    )
                    aggregate = aggregate_record["result"]
                    metric_summary = {
                        "metric_name": metric_name,
                        "metric_kind": metric_kind,
                        "score": float(aggregate["score"]),
                        "passed": bool(aggregate["passed"]),
                        "reason": (f"{aggregate['method']} of {aggregate['valid_vote_count']} valid Judge votes"),
                        "weight": metric_weight,
                        "pass_threshold": metric_threshold,
                        "veto": metric_veto,
                        "details": {
                            **aggregate,
                            "aggregation_id": aggregate_record.get("id", ""),
                        },
                        "trace_id": actual_trace_id,
                    }
                    metric_results_for_composite.append(metric_summary)
                    continue

                metric_result: Any
                judge_config = metric_cfg.get("judge")
                if judge_config:
                    try:
                        trace_summary = ""
                        if trace_data:
                            trace_summary = (
                                f"Tool calls: {len(trace_data.get('tool_calls', []))}, "
                                f"Retrievals: {len(trace_data.get('retrieval_calls', []))}"
                            )
                        metric_result = evaluate_judge(
                            config=judge_config,
                            input_text=str(claimed.case_input),
                            output_text=str(actual_output),
                            expected=claimed.case_expected,
                            trace_summary=trace_summary,
                        )
                    except Exception:
                        metric_result = evaluate_rule(
                            config=metric_cfg,
                            actual=actual_output,
                            expected=claimed.case_expected,
                            metrics=metrics,
                            trace=trace_data,
                            case_metadata=claimed.case_metadata,
                        )
                else:
                    metric_result = evaluate_rule(
                        config=metric_cfg,
                        actual=actual_output,
                        expected=claimed.case_expected,
                        metrics=metrics,
                        trace=trace_data,
                        case_metadata=claimed.case_metadata,
                    )

                # Build rich details for this metric score
                score_details = {
                    "weight": metric_weight,
                    "veto": metric_veto,
                }
                if "pass_threshold" in metric_cfg:
                    score_details["pass_threshold"] = metric_cfg["pass_threshold"]

                # Merge in judge or rule details
                result_details = getattr(metric_result, "details", None)
                if isinstance(result_details, dict):
                    for field in (
                        "dimension_scores",
                        "dimension_reasons",
                        "evidence",
                        "veto_triggered",
                        "veto_reason",
                        "raw_response",
                        "judge_model",
                        "judge_provider",
                        "raw_payload",
                    ):
                        if field in result_details:
                            score_details[field] = result_details[field]

                metric_summary = {
                    "metric_name": metric_name,
                    "metric_kind": metric_kind,
                    "score": metric_result.score,
                    "passed": metric_result.passed,
                    "reason": metric_result.reason,
                    "weight": metric_weight,
                    "veto": metric_veto,
                    "details": score_details,
                    "trace_id": actual_trace_id,
                }
                if "pass_threshold" in metric_cfg:
                    metric_summary["pass_threshold"] = metric_cfg["pass_threshold"]
                payload_metric_positions.append(len(metric_results_for_composite))
                metric_results_for_composite.append(metric_summary)

            # ---- Aggregate with weighted + veto support ----
            agg_result = aggregate_scores_with_veto(
                metric_results_for_composite,
                default_pass_threshold=global_pass_threshold,
            )
            composite_metric_scores = agg_result["metric_scores"]
            eval_scores_list = [composite_metric_scores[position] for position in payload_metric_positions]
            global_pass_threshold = agg_result["pass_threshold"]
            for metric_score in composite_metric_scores:
                aggregated_details = metric_score.get("details")
                if isinstance(aggregated_details, dict):
                    aggregated_details["weight"] = metric_score["weight"]
                    aggregated_details["veto"] = metric_score["veto"]
                    if "pass_threshold" in metric_score:
                        aggregated_details["pass_threshold"] = metric_score["pass_threshold"]
                    else:
                        aggregated_details.pop("pass_threshold", None)

            composite_score = agg_result["composite_score"]
            composite_passed = agg_result["passed"]
            veto_triggered = agg_result["veto_triggered"]
            veto_reason = agg_result["veto_reason"]

            composite_reason_parts = [
                f"Weighted average: {composite_score:.4f} "
                f"(threshold={global_pass_threshold}, metrics={len(multi_metrics)})"
            ]
            if veto_triggered:
                composite_reason_parts.append(f"VETO: {veto_reason}")
            composite_reason = "; ".join(composite_reason_parts)

            eval_result = type(
                "EvalResult",
                (),
                {
                    "score": composite_score,
                    "passed": composite_passed,
                    "reason": composite_reason,
                    "details": {
                        "aggregation": aggregation_cfg,
                        "pass_threshold": global_pass_threshold,
                        "num_metrics": len(multi_metrics),
                        "total_weight": sum(s.get("weight", 1.0) for s in composite_metric_scores),
                        "veto_triggered": veto_triggered,
                        "veto_reason": veto_reason,
                        "metric_scores": composite_metric_scores,
                        "dimension_breakdown": agg_result.get("dimension_breakdown", {}),
                    },
                },
            )()
        except (
            EvaluatorConfigError,
            MultiJudgeError,
            OverflowError,
            TypeError,
            ValueError,
        ) as e:
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
