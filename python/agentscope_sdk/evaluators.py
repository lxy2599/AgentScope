"""Rule-based evaluators for deterministic evaluation."""

from __future__ import annotations

import re
from dataclasses import dataclass, field
from typing import Any


class EvaluatorConfigError(Exception):
    """Raised when evaluator configuration is invalid."""


@dataclass
class EvaluationResult:
    """Result of a single evaluation."""

    score: float = 0.0
    passed: bool = False
    reason: str = ""
    details: dict[str, Any] = field(default_factory=dict)


def evaluate_rule(
    config: dict[str, Any],
    actual: Any | None = None,
    expected: Any | None = None,
    metrics: dict[str, Any] | None = None,
) -> EvaluationResult:
    """Evaluate a single rule against actual output.

    Args:
        config: Rule configuration with a ``type`` key and type-specific fields.
        actual: The actual output from the agent.
        expected: The expected output, if applicable.
        metrics: Performance metrics (latency, cost, etc.), if applicable.

    Returns:
        An ``EvaluationResult`` with score, passed, reason, and details.

    Raises:
        EvaluatorConfigError: If the rule configuration is invalid.
    """
    rule_type = config.get("type")
    if not rule_type:
        raise EvaluatorConfigError("Missing 'type' in evaluator config")

    _handlers: dict[str, _RuleHandler] = {
        "exact": _evaluate_exact,
        "contains": _evaluate_contains,
        "not_contains": _evaluate_not_contains,
        "regex": _evaluate_regex,
        "numeric_threshold": _evaluate_numeric_threshold,
        "latency_threshold": _evaluate_latency_threshold,
    }

    handler = _handlers.get(rule_type)
    if handler is None:
        msg = f"Unknown evaluator type: '{rule_type}'"
        raise EvaluatorConfigError(msg)

    return handler(config, actual, expected, metrics)


# ---- Handler implementations ----

type _RuleHandler = Any  # workaround for Python 3.11


def _evaluate_exact(
    config: dict[str, Any],
    actual: Any,
    expected: Any | None,
    metrics: dict[str, Any] | None,
) -> EvaluationResult:
    # If expected is None, try config["expected"]
    exp = expected if expected is not None else config.get("expected")
    if actual == exp:
        return EvaluationResult(
            score=1.0,
            passed=True,
            reason=f"Exact match: '{actual}' == '{exp}'",
        )
    return EvaluationResult(
        score=0.0,
        passed=False,
        reason=f"Exact mismatch: '{actual}' != '{exp}'",
    )


def _evaluate_contains(
    config: dict[str, Any],
    actual: Any,
    expected: Any | None,
    metrics: dict[str, Any] | None,
) -> EvaluationResult:
    value = config.get("value", "")
    str_actual = str(actual) if actual is not None else ""
    if value in str_actual:
        return EvaluationResult(
            score=1.0,
            passed=True,
            reason=f"Contains '{value}' in output",
        )
    return EvaluationResult(
        score=0.0,
        passed=False,
        reason=f"Output does not contain '{value}'",
    )


def _evaluate_not_contains(
    config: dict[str, Any],
    actual: Any,
    expected: Any | None,
    metrics: dict[str, Any] | None,
) -> EvaluationResult:
    value = config.get("value", "")
    str_actual = str(actual) if actual is not None else ""
    if value not in str_actual:
        return EvaluationResult(
            score=1.0,
            passed=True,
            reason=f"Output does not contain '{value}'",
        )
    return EvaluationResult(
        score=0.0,
        passed=False,
        reason=f"Output contains forbidden value '{value}'",
    )


def _evaluate_regex(
    config: dict[str, Any],
    actual: Any,
    expected: Any | None,
    metrics: dict[str, Any] | None,
) -> EvaluationResult:
    pattern = config.get("pattern", "")
    if not pattern:
        raise EvaluatorConfigError("Missing 'pattern' in regex evaluator config")
    try:
        compiled = re.compile(pattern)
    except re.error as e:
        msg = f"Invalid regex pattern '{pattern}': {e}"
        raise EvaluatorConfigError(msg) from e

    str_actual = str(actual) if actual is not None else ""
    if compiled.search(str_actual):
        return EvaluationResult(
            score=1.0,
            passed=True,
            reason=f"Regex '{pattern}' matches output",
        )
    return EvaluationResult(
        score=0.0,
        passed=False,
        reason=f"Regex '{pattern}' does not match output",
    )


def _evaluate_numeric_threshold(
    config: dict[str, Any],
    actual: Any,
    expected: Any | None,
    metrics: dict[str, Any] | None,
) -> EvaluationResult:
    field = config.get("field")
    op = config.get("op", ">=")
    threshold = config.get("value")

    if field is None or threshold is None:
        raise EvaluatorConfigError("Missing 'field' or 'value' in numeric_threshold config")

    if not isinstance(actual, dict):
        return EvaluationResult(
            score=0.0,
            passed=False,
            reason=f"Actual output is not a dict, cannot extract field '{field}'",
        )

    field_value = actual.get(field)
    if field_value is None:
        return EvaluationResult(
            score=0.0,
            passed=False,
            reason=f"Field '{field}' not found in actual output",
        )

    try:
        field_val = float(field_value)
        threshold_val = float(threshold)
    except (ValueError, TypeError) as e:
        msg = f"Cannot compare field '{field}' value {field_value!r} to threshold {threshold!r}: {e}"
        raise EvaluatorConfigError(msg) from e

    passed = _compare_numeric(field_val, op, threshold_val)
    if passed:
        return EvaluationResult(
            score=1.0,
            passed=True,
            reason=f"Field '{field}' = {field_val} {op} {threshold_val}",
        )
    return EvaluationResult(
        score=0.0,
        passed=False,
        reason=f"Field '{field}' = {field_val} fails threshold {op} {threshold_val}",
    )


def _evaluate_latency_threshold(
    config: dict[str, Any],
    actual: Any,
    expected: Any | None,
    metrics: dict[str, Any] | None,
) -> EvaluationResult:
    max_ms = config.get("max_ms")
    if max_ms is None:
        raise EvaluatorConfigError("Missing 'max_ms' in latency_threshold config")

    if metrics is None:
        return EvaluationResult(
            score=0.0,
            passed=False,
            reason="No metrics provided for latency threshold evaluation",
        )

    latency = metrics.get("latency_ms")
    if latency is None:
        return EvaluationResult(
            score=0.0,
            passed=False,
            reason="'latency_ms' not found in metrics",
        )

    try:
        latency_val = float(latency)
        max_ms_val = float(max_ms)
    except (ValueError, TypeError) as e:
        msg = f"Cannot compare latency {latency!r} to max_ms {max_ms!r}: {e}"
        raise EvaluatorConfigError(msg) from e

    if latency_val <= max_ms_val:
        return EvaluationResult(
            score=1.0,
            passed=True,
            reason=f"Latency {latency_val}ms <= {max_ms_val}ms",
        )
    return EvaluationResult(
        score=0.0,
        passed=False,
        reason=f"Latency {latency_val}ms exceeds threshold {max_ms_val}ms",
    )


def _compare_numeric(val: float, op: str, threshold: float) -> bool:
    """Compare two numeric values with the given operator."""
    if op == ">=":
        return val >= threshold
    if op == "<=":
        return val <= threshold
    if op == ">":
        return val > threshold
    if op == "<":
        return val < threshold
    if op == "==":
        return val == threshold
    if op == "!=":
        return val != threshold
    msg = f"Unknown operator '{op}' in numeric_threshold evaluator"
    raise EvaluatorConfigError(msg)
