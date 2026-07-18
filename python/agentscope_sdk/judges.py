"""Judge provider abstraction and response parsing."""

from __future__ import annotations

import json
import os
from dataclasses import dataclass, field
from typing import Any

import httpx


class JudgeResponseError(Exception):
    """Raised when a judge response cannot be parsed."""


@dataclass(frozen=True)
class JudgeResult:
    """Parsed result from a judge evaluation.

    Attributes:
        score: Numeric score between 0.0 and 1.0.
        passed: Whether the response passed the evaluation.
        reason: Explanation of the evaluation.
        raw_text: The raw response text from the judge API (before parsing).
    """

    score: float
    passed: bool
    reason: str
    raw_text: str = ""


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
        A ``JudgeResult`` with parsed values and the original raw text.

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

    return JudgeResult(score=score, passed=passed, reason=reason, raw_text=text)


# ---- Providers ----


def _resolve_api_key(api_key_env: str | None) -> str:
    """Resolve API key from the named environment variable.

    Args:
        api_key_env: Name of the environment variable containing the key.

    Returns:
        The resolved API key string.

    Raises:
        ValueError: If the key cannot be resolved.
    """
    if not api_key_env or not api_key_env.strip():
        raise ValueError("'api_key_env' must be provided and non-empty")

    resolved = os.environ.get(api_key_env)
    if not resolved:
        raise ValueError(
            "API key environment variable is not set or is empty"
        )
    return resolved


class OpenAICompatibleProvider(JudgeProvider):
    """Judge provider for OpenAI-compatible chat completion APIs.

    Sends requests to ``/chat/completions`` with ``response_format`` set
    to ``json_object`` to request structured output.
    """

    def __init__(
        self,
        model: str,
        base_url: str,
        api_key_env: str,
        rubric: str | None = None,
        temperature: float = 0.0,
        max_tokens: int = 1024,
        transport: httpx.BaseTransport | None = None,
    ) -> None:
        self.model = model
        self.base_url = base_url.rstrip("/")
        self._api_key_env = api_key_env
        self.rubric = rubric
        self.temperature = temperature
        self.max_tokens = max_tokens
        self._transport = transport

        if not self._api_key_env.strip():
            raise ValueError("'api_key_env' must not be empty")

    def evaluate(self, request: JudgeRequest) -> JudgeResult:
        """Evaluate the request against the OpenAI-compatible API.

        Args:
            request: The judge request.

        Returns:
            A parsed JudgeResult.

        Raises:
            ValueError: If the API key cannot be resolved.
            httpx.HTTPStatusError: If the API returns a non-2xx response.
        """
        api_key = _resolve_api_key(self._api_key_env)
        rubric = request.rubric if request.rubric else (self.rubric or "")
        prompt = (
            f"Evaluate the following output based on this rubric: {rubric}\n\n"
            f"Output to evaluate:\n{request.input}"
        )
        if request.expected:
            prompt += f"\n\nExpected answer:\n{request.expected}"

        body = {
            "model": self.model,
            "messages": [
                {
                    "role": "system",
                    "content": (
                        "You are a judge evaluating agent outputs. "
                        "Respond in JSON with fields: "
                        '"score" (float 0.0-1.0), '
                        '"passed" (bool), '
                        '"reason" (string). '
                        "Do not include any other text outside the JSON."
                    ),
                },
                {"role": "user", "content": prompt},
            ],
            "temperature": self.temperature,
            "max_tokens": self.max_tokens,
            "response_format": {"type": "json_object"},
        }

        client = httpx.Client(transport=self._transport)
        try:
            response = client.post(
                f"{self.base_url}/chat/completions",
                json=body,
                headers={
                    "Authorization": f"Bearer {api_key}",
                    "Content-Type": "application/json",
                },
                timeout=60.0,
            )
            response.raise_for_status()
            data = response.json()
            content = data["choices"][0]["message"]["content"]
            return parse_judge_response(content)
        finally:
            client.close()


class AnthropicCompatibleProvider(JudgeProvider):
    """Judge provider for Anthropic-compatible Messages API.

    Sends requests to ``/messages`` with the Anthropic-style request format.
    Uses ``x-api-key`` header for authentication.
    """

    def __init__(
        self,
        model: str,
        base_url: str,
        api_key_env: str,
        rubric: str | None = None,
        temperature: float = 0.0,
        max_tokens: int = 1024,
        transport: httpx.BaseTransport | None = None,
    ) -> None:
        self.model = model
        self.base_url = base_url.rstrip("/")
        self._api_key_env = api_key_env
        self.rubric = rubric
        self.temperature = temperature
        self.max_tokens = max_tokens
        self._transport = transport

        if not self._api_key_env.strip():
            raise ValueError("'api_key_env' must not be empty")

    def evaluate(self, request: JudgeRequest) -> JudgeResult:
        """Evaluate the request against the Anthropic-compatible API.

        Args:
            request: The judge request.

        Returns:
            A parsed JudgeResult.

        Raises:
            ValueError: If the API key cannot be resolved.
            httpx.HTTPStatusError: If the API returns a non-2xx response.
        """
        api_key = _resolve_api_key(self._api_key_env)
        rubric = request.rubric if request.rubric else (self.rubric or "")
        prompt = (
            f"Evaluate the following output based on this rubric: {rubric}\n\n"
            f"Output to evaluate:\n{request.input}"
        )
        if request.expected:
            prompt += f"\n\nExpected answer:\n{request.expected}"

        body = {
            "model": self.model,
            "max_tokens": self.max_tokens,
            "temperature": self.temperature,
            "system": (
                "You are a judge evaluating agent outputs. "
                "Respond in JSON with fields: "
                '"score" (float 0.0-1.0), '
                '"passed" (bool), '
                '"reason" (string). '
                "Do not include any other text outside the JSON."
            ),
            "messages": [
                {"role": "user", "content": prompt},
            ],
        }

        client = httpx.Client(transport=self._transport)
        try:
            response = client.post(
                f"{self.base_url}/messages",
                json=body,
                headers={
                    "x-api-key": api_key,
                    "anthropic-version": "2023-06-01",
                    "Content-Type": "application/json",
                },
                timeout=60.0,
            )
            response.raise_for_status()
            data = response.json()
            content_blocks = data.get("content", [])
            content = ""
            for block in content_blocks:
                if block.get("type") == "text":
                    content = block.get("text", "")
                    break
            return parse_judge_response(content)
        finally:
            client.close()


class FakeProvider(JudgeProvider):
    """Fake judge provider for testing.

    Returns a deterministic response without calling any API.
    """

    def __init__(self, score: float = 1.0, passed: bool = True, reason: str = "Fake judge evaluation") -> None:
        self._score = score
        self._passed = passed
        self._reason = reason

    def evaluate(self, request: JudgeRequest) -> JudgeResult:
        """Return a deterministic JudgeResult."""
        return JudgeResult(
            score=self._score,
            passed=self._passed,
            reason=self._reason,
            raw_text=self._reason,
        )
