"""Judge provider abstraction and response parsing."""

from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any


class JudgeResponseError(Exception):
    """Raised when a judge response cannot be parsed."""


@dataclass(frozen=True)
class JudgeResult:
    """Parsed result from a judge evaluation."""

    score: float
    passed: bool
    reason: str


@dataclass
class JudgeRequest:
    """A request to judge an agent's output."""

    input: str
    rubric: str
    expected: str | None = None
    context: dict[str, Any] | None = None


class JudgeProvider:
    """Base class for judge providers."""

    def evaluate(self, request: JudgeRequest) -> JudgeResult:
        """Evaluate an agent's output and return a structured result.

        Args:
            request: The judge request containing input and rubric.

        Returns:
            A JudgeResult with score, pass/fail, and reason.

        Raises:
            NotImplementedError: Subclasses must implement this method.
        """
        raise NotImplementedError("Subclasses must implement evaluate()")


def parse_judge_response(text: str) -> JudgeResult:
    """Parse a JSON judge response into a JudgeResult.

    The response must contain:
    - ``score``: a float between 0.0 and 1.0
    - ``passed`` or ``pass``: a boolean indicating pass/fail
    - ``reason``: a string explanation

    If both ``pass`` and ``passed`` are present, ``passed`` takes precedence.

    Args:
        text: The raw JSON string from the judge.

    Returns:
        A ``JudgeResult`` with parsed values.

    Raises:
        JudgeResponseError: If the JSON is invalid or required fields are
            missing or out of range.
    """
    if not text or not text.strip():
        raise JudgeResponseError("Empty judge response")

    try:
        data = json.loads(text)
    except json.JSONDecodeError as e:
        raise JudgeResponseError(f"Invalid JSON in judge response: {e}") from e

    if not isinstance(data, dict):
        raise JudgeResponseError("Judge response must be a JSON object")

    # Validate score
    score = data.get("score")
    if score is None:
        raise JudgeResponseError("Missing 'score' in judge response")
    try:
        score = float(score)
    except (ValueError, TypeError) as e:
        raise JudgeResponseError(
            f"Invalid 'score' value in judge response: {e}"
        ) from e
    if score < 0.0 or score > 1.0:
        raise JudgeResponseError(
            f"'score' must be between 0.0 and 1.0, got {score}"
        )

    # Validate pass/passed
    passed = data.get("passed")
    if passed is None:
        passed = data.get("pass")
    if passed is None:
        raise JudgeResponseError(
            "Missing 'pass' or 'passed' field in judge response"
        )
    if not isinstance(passed, bool):
        raise JudgeResponseError(
            f"'pass'/'passed' must be a boolean, got {type(passed).__name__}"
        )

    # Validate reason
    reason = data.get("reason")
    if reason is None:
        raise JudgeResponseError("Missing 'reason' in judge response")
    if not isinstance(reason, str):
        raise JudgeResponseError(
            f"'reason' must be a string, got {type(reason).__name__}"
        )

    return JudgeResult(score=score, passed=passed, reason=reason)
