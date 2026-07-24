"""Judge provider abstraction and response parsing."""

from __future__ import annotations

import json
import math
import os
import re
from dataclasses import dataclass
from typing import Any
from urllib.parse import urlsplit

import httpx

_API_KEY_ENV_PATTERN = re.compile(r"^[A-Z_][A-Z0-9_]*$")
_EXTRA_API_KEY_ENVS_VAR = "AGENTSCOPE_JUDGE_ALLOWED_API_KEY_ENVS"
_EXTRA_BASE_URLS_VAR = "AGENTSCOPE_JUDGE_ALLOWED_BASE_URLS"
_ALLOW_INSECURE_HTTP_VAR = "AGENTSCOPE_JUDGE_ALLOW_INSECURE_HTTP"

_OPENAI_API_KEY_ENV = "OPENAI_API_KEY"
_OPENAI_ALLOWED_ORIGINS = frozenset({"https://api.openai.com"})
_ANTHROPIC_API_KEY_ENV = "ANTHROPIC_API_KEY"
_ANTHROPIC_ALLOWED_ORIGINS = frozenset({"https://api.anthropic.com"})


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
    except (ValueError, RecursionError) as e:
        raise JudgeResponseError(f"Invalid JSON in judge response: {e}") from e

    if not isinstance(data, dict):
        raise JudgeResponseError("Judge response must be a JSON object")

    # Validate score
    score = data.get("score")
    if score is None:
        raise JudgeResponseError("Missing 'score' in judge response")
    if isinstance(score, bool):
        raise JudgeResponseError("'score' must be a number, not a boolean")
    try:
        score = float(score)
    except (ValueError, TypeError) as e:
        raise JudgeResponseError(f"Invalid 'score' value in judge response: {e}") from e
    if not math.isfinite(score) or score < 0.0 or score > 1.0:
        raise JudgeResponseError(f"'score' must be between 0.0 and 1.0, got {score}")

    # Validate pass/passed
    passed = data.get("passed")
    if passed is None:
        passed = data.get("pass")
    if passed is None:
        raise JudgeResponseError("Missing 'pass' or 'passed' field in judge response")
    if not isinstance(passed, bool):
        raise JudgeResponseError(f"'pass'/'passed' must be a boolean, got {type(passed).__name__}")

    # Validate reason
    reason = data.get("reason")
    if reason is None:
        raise JudgeResponseError("Missing 'reason' in judge response")
    if not isinstance(reason, str):
        raise JudgeResponseError(f"'reason' must be a string, got {type(reason).__name__}")

    return JudgeResult(score=score, passed=passed, reason=reason, raw_text=text)


# ---- Providers ----


def _csv_env_values(name: str) -> list[str]:
    """Read a comma-separated operator allowlist without accepting blanks."""
    raw = os.environ.get(name, "")
    if not raw:
        return []
    return [value.strip() for value in raw.split(",") if value.strip()]


def _validate_api_key_env(
    api_key_env: str | None,
    *,
    default_api_key_env: str,
) -> str:
    """Apply the runner's fail-closed API-key environment policy."""
    if not isinstance(api_key_env, str) or not api_key_env.strip():
        raise ValueError("'api_key_env' must be provided and non-empty")

    normalized = api_key_env.strip()
    if not _API_KEY_ENV_PATTERN.fullmatch(normalized):
        raise ValueError("'api_key_env' must be a valid environment variable name")

    extra_names = _csv_env_values(_EXTRA_API_KEY_ENVS_VAR)
    if any(not _API_KEY_ENV_PATTERN.fullmatch(name) for name in extra_names):
        raise ValueError("judge API-key environment allowlist is invalid")

    if normalized not in {default_api_key_env, *extra_names}:
        raise ValueError("'api_key_env' is not allowed by the runner security policy")
    return normalized


def _origin(value: str) -> tuple[str, str]:
    """Return a normalized origin and scheme for an absolute HTTP(S) URL."""
    try:
        parsed = urlsplit(value)
        # Validate and canonicalize with the parser that will actually send
        # the request, avoiding security-policy/request-parser differentials.
        request_url = httpx.URL(value)
    except (TypeError, ValueError, httpx.InvalidURL) as exc:
        raise ValueError("judge base URL is invalid") from exc

    if (
        request_url.scheme not in {"http", "https"}
        or not request_url.host
        or parsed.username is not None
        or parsed.password is not None
        or request_url.userinfo
        or "?" in value
        or "#" in value
        or parsed.query
        or parsed.fragment
        or request_url.query
        or request_url.fragment
    ):
        raise ValueError("judge base URL must be an absolute HTTP(S) URL")

    host = request_url.host.lower()
    if ":" in host:
        host = f"[{host}]"
    port_suffix = f":{request_url.port}" if request_url.port is not None else ""
    return (
        f"{request_url.scheme}://{host}{port_suffix}",
        request_url.scheme,
    )


def _validate_base_url(
    base_url: str,
    *,
    default_allowed_origins: frozenset[str],
) -> str:
    """Restrict credential-bearing requests to operator-approved origins."""
    if not isinstance(base_url, str) or not base_url.strip():
        raise ValueError("judge base URL must not be empty")

    normalized = base_url.strip().rstrip("/")
    origin, scheme = _origin(normalized)

    extra_origins: set[str] = set()
    for allowed_url in _csv_env_values(_EXTRA_BASE_URLS_VAR):
        allowed_origin, _ = _origin(allowed_url)
        extra_origins.add(allowed_origin)

    if origin not in default_allowed_origins and origin not in extra_origins:
        raise ValueError("judge base URL origin is not allowed by the runner security policy")

    allow_insecure = os.environ.get(_ALLOW_INSECURE_HTTP_VAR, "").strip().lower() == "true"
    if scheme != "https" and not (origin in extra_origins and allow_insecure):
        raise ValueError("judge base URL must use HTTPS unless insecure HTTP is explicitly enabled")
    return normalized


def _resolve_api_key(
    api_key_env: str | None,
    *,
    default_api_key_env: str,
) -> str:
    """Resolve API key from the named environment variable.

    Args:
        api_key_env: Name of the environment variable containing the key.

    Returns:
        The resolved API key string.

    Raises:
        ValueError: If the key cannot be resolved.
    """
    normalized = _validate_api_key_env(
        api_key_env,
        default_api_key_env=default_api_key_env,
    )

    resolved = os.environ.get(normalized)
    if resolved is None or not resolved.strip():
        raise ValueError("API key environment variable is not set or is empty")
    return resolved


def _validate_finite_float(value: Any, field_name: str) -> float:
    """Return a finite float suitable for a provider request."""
    if isinstance(value, bool):
        raise ValueError(f"{field_name} must be a finite number")
    try:
        number = float(value)
    except (TypeError, ValueError) as exc:
        raise ValueError(f"{field_name} must be a finite number") from exc
    if not math.isfinite(number):
        raise ValueError(f"{field_name} must be a finite number")
    return number


def _validate_positive_int(value: Any, field_name: str) -> int:
    """Return a strictly positive integer without silently truncating values."""
    if isinstance(value, bool):
        raise ValueError(f"{field_name} must be a positive integer")
    try:
        number = int(value)
        numeric_value = float(value)
    except (OverflowError, TypeError, ValueError) as exc:
        raise ValueError(f"{field_name} must be a positive integer") from exc
    if not math.isfinite(numeric_value) or numeric_value != number or number <= 0:
        raise ValueError(f"{field_name} must be a positive integer")
    return number


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
        self.base_url = _validate_base_url(
            base_url,
            default_allowed_origins=_OPENAI_ALLOWED_ORIGINS,
        )
        self._api_key_env = _validate_api_key_env(
            api_key_env,
            default_api_key_env=_OPENAI_API_KEY_ENV,
        )
        self.rubric = rubric
        self.temperature = _validate_finite_float(temperature, "temperature")
        self.max_tokens = _validate_positive_int(max_tokens, "max_tokens")
        self._transport = transport

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
        base_url = _validate_base_url(
            self.base_url,
            default_allowed_origins=_OPENAI_ALLOWED_ORIGINS,
        )
        api_key = _resolve_api_key(
            self._api_key_env,
            default_api_key_env=_OPENAI_API_KEY_ENV,
        )
        temperature = _validate_finite_float(self.temperature, "temperature")
        max_tokens = _validate_positive_int(self.max_tokens, "max_tokens")
        rubric = request.rubric if request.rubric else (self.rubric or "")
        prompt = f"Evaluate the following output based on this rubric: {rubric}\n\nOutput to evaluate:\n{request.input}"
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
            "temperature": temperature,
            "max_tokens": max_tokens,
            "response_format": {"type": "json_object"},
        }

        client = httpx.Client(transport=self._transport)
        try:
            response = client.post(
                f"{base_url}/chat/completions",
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
        self.base_url = _validate_base_url(
            base_url,
            default_allowed_origins=_ANTHROPIC_ALLOWED_ORIGINS,
        )
        self._api_key_env = _validate_api_key_env(
            api_key_env,
            default_api_key_env=_ANTHROPIC_API_KEY_ENV,
        )
        self.rubric = rubric
        self.temperature = _validate_finite_float(temperature, "temperature")
        self.max_tokens = _validate_positive_int(max_tokens, "max_tokens")
        self._transport = transport

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
        base_url = _validate_base_url(
            self.base_url,
            default_allowed_origins=_ANTHROPIC_ALLOWED_ORIGINS,
        )
        api_key = _resolve_api_key(
            self._api_key_env,
            default_api_key_env=_ANTHROPIC_API_KEY_ENV,
        )
        temperature = _validate_finite_float(self.temperature, "temperature")
        max_tokens = _validate_positive_int(self.max_tokens, "max_tokens")
        rubric = request.rubric if request.rubric else (self.rubric or "")
        prompt = f"Evaluate the following output based on this rubric: {rubric}\n\nOutput to evaluate:\n{request.input}"
        if request.expected:
            prompt += f"\n\nExpected answer:\n{request.expected}"

        body = {
            "model": self.model,
            "max_tokens": max_tokens,
            "temperature": temperature,
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
                f"{base_url}/messages",
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
