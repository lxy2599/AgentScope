"""Rule-based evaluators for deterministic evaluation."""

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
    metric_name: str = ""
    metric_kind: str = ""
    details: dict[str, Any] = field(default_factory=dict)


def evaluate_rule(
    config: dict[str, Any],
    actual: Any | None = None,
    expected: Any | None = None,
    metrics: dict[str, Any] | None = None,
    *,
    trace: dict[str, Any] | None = None,
) -> EvaluationResult:
    """Evaluate a single rule against actual output.

    Args:
        config: Rule configuration with a ``type`` key and type-specific fields.
        actual: The actual output from the agent.
        expected: The expected output, if applicable.
        metrics: Performance metrics (latency, cost, etc.), if applicable.
        trace: Optional trace/span data for trace-aware evaluators
               (tool_sequence, required_tool_called).

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
        "exact_match": _evaluate_exact,
        "contains": _evaluate_contains,
        "not_contains": _evaluate_not_contains,
        "regex": _evaluate_regex,
        "numeric_threshold": _evaluate_numeric_threshold,
        "latency_threshold": _evaluate_latency_threshold,
        "tool_sequence": _evaluate_tool_sequence,
        "required_tool_called": _evaluate_required_tool_called,
    }

    handler = _handlers.get(rule_type)
    if handler is None:
        msg = f"Unknown evaluator type: '{rule_type}'"
        raise EvaluatorConfigError(msg)

    return handler(config, actual, expected, metrics, trace)


# ---- Handler implementations ----

# Each handler takes (config, actual, expected, metrics) and returns EvaluationResult.
_RuleHandler = Any


def _evaluate_exact(
    config: dict[str, Any],
    actual: Any,
    expected: Any | None,
    metrics: dict[str, Any] | None,
    trace: dict[str, Any] | None = None,
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
    trace: dict[str, Any] | None = None,
) -> EvaluationResult:
    value = config.get("value", "")
    if not value:
        raise EvaluatorConfigError("contains evaluator requires non-empty 'value' field")
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
    trace: dict[str, Any] | None = None,
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
    trace: dict[str, Any] | None = None,
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
    trace: dict[str, Any] | None = None,
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
    trace: dict[str, Any] | None = None,
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


def _evaluate_tool_sequence(
    config: dict[str, Any],
    actual: Any,
    expected: Any | None,
    metrics: dict[str, Any] | None,
    trace: dict[str, Any] | None = None,
) -> EvaluationResult:
    """Evaluate that tool calls follow a required sequence.

    Config:
        tools: list of tool names that must be called in order.
               Example: ["search_tool", "read_tool", "respond_tool"]
    """
    required_tools = config.get("tools", [])
    if not required_tools:
        raise EvaluatorConfigError("Missing 'tools' list in tool_sequence config")

    if trace is None:
        return EvaluationResult(
            score=0.0,
            passed=False,
            reason="No trace data provided for tool_sequence evaluation",
        )

    tool_calls = trace.get("tool_calls", [])
    if not tool_calls:
        return EvaluationResult(
            score=0.0,
            passed=False,
            reason="No tool calls found in trace",
        )

    called_tools = [tc.get("tool_name", "") for tc in tool_calls]

    # Check each required tool appears in the correct order
    idx = 0
    found_count = 0
    for required in required_tools:
        while idx < len(called_tools):
            if called_tools[idx] == required:
                found_count += 1
                idx += 1
                break
            idx += 1
        else:
            # Required tool not found
            return EvaluationResult(
                score=0.0,
                passed=False,
                reason=f"Tool sequence broken: expected '{required}' after tools {called_tools[:idx]}",
                details={"called_tools": called_tools, "required_tools": required_tools, "found_up_to": found_count},
            )

    return EvaluationResult(
        score=1.0,
        passed=True,
        reason=f"Tool sequence correct: {required_tools}",
        details={"called_tools": called_tools, "required_tools": required_tools},
    )


def _evaluate_required_tool_called(
    config: dict[str, Any],
    actual: Any,
    expected: Any | None,
    metrics: dict[str, Any] | None,
    trace: dict[str, Any] | None = None,
) -> EvaluationResult:
    """Evaluate that a required tool was called at least once.

    Config:
        tool_name: name of the required tool.
    """
    required_tool = config.get("tool_name")
    if not required_tool:
        raise EvaluatorConfigError("Missing 'tool_name' in required_tool_called config")

    if trace is None:
        return EvaluationResult(
            score=0.0,
            passed=False,
            reason="No trace data provided for required_tool_called evaluation",
        )

    tool_calls = trace.get("tool_calls", [])
    if not tool_calls:
        return EvaluationResult(
            score=0.0,
            passed=False,
            reason=f"No tool calls found in trace, expected '{required_tool}'",
        )

    called_tools = [tc.get("tool_name", "") for tc in tool_calls]
    if required_tool in called_tools:
        return EvaluationResult(
            score=1.0,
            passed=True,
            reason=f"Required tool '{required_tool}' was called",
            details={"called_tools": called_tools},
        )

    return EvaluationResult(
        score=0.0,
        passed=False,
        reason=f"Required tool '{required_tool}' was not called",
        details={"called_tools": called_tools},
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
