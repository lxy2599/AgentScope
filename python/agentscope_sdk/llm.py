"""OpenAI-compatible LLM client for AgentScope.

Supports DeepSeek, GLM, and any provider with an OpenAI-compatible
Chat Completions API.

Configuration via environment variables (no OPENAI_ prefix):

- ``AGENTSCOPE_LLM_PROVIDER`` — Provider name (e.g. ``deepseek``, ``glm``).
- ``AGENTSCOPE_LLM_BASE_URL`` — Base URL for the API (e.g. ``https://api.deepseek.com/v1``).
- ``AGENTSCOPE_LLM_API_KEY`` — API key for authentication.
- ``AGENTSCOPE_LLM_MODEL`` — Model name (e.g. ``deepseek-chat``, ``glm-4.7-flash``).
"""

from __future__ import annotations

import os
import time
from dataclasses import dataclass
from typing import Any

import httpx

# ---- Exceptions ----


class LLMConfigError(Exception):
    """Raised when LLM configuration is missing or invalid."""


class LLMResponseError(Exception):
    """Raised when the LLM API returns a non-2xx response or is unreachable.

    Error messages do **not** contain the API key.
    """


# ---- Domain types ----


@dataclass
class LLMResponse:
    """A successful response from an LLM call."""

    content: str
    provider: str
    model: str
    latency_ms: int
    prompt_tokens: int = 0
    completion_tokens: int = 0
    total_tokens: int = 0


# ---- LLM Client ----


class LLMClient:
    """Client for OpenAI-compatible Chat Completions API.

    Reads configuration from environment variables.
    Does **not** log, persist, or transmit the API key.

    Args:
        provider: Provider name (``deepseek``, ``glm``, etc.).
            Defaults to ``AGENTSCOPE_LLM_PROVIDER`` env var.
        base_url: API base URL.
            Defaults to ``AGENTSCOPE_LLM_BASE_URL`` env var.
        api_key: API key.
            Defaults to ``AGENTSCOPE_LLM_API_KEY`` env var.
        model: Model name.
            Defaults to ``AGENTSCOPE_LLM_MODEL`` env var.
        timeout: HTTP request timeout in seconds.
    """

    def __init__(
        self,
        provider: str | None = None,
        base_url: str | None = None,
        api_key: str | None = None,
        model: str | None = None,
        timeout: float = 60.0,
    ) -> None:
        self._provider = provider or os.environ.get("AGENTSCOPE_LLM_PROVIDER", "")
        self._base_url = (base_url or os.environ.get("AGENTSCOPE_LLM_BASE_URL", "")).rstrip("/")
        self._api_key = api_key or os.environ.get("AGENTSCOPE_LLM_API_KEY", "")
        self._model = model or os.environ.get("AGENTSCOPE_LLM_MODEL", "")

        missing: list[str] = []
        if not self._provider:
            missing.append("AGENTSCOPE_LLM_PROVIDER")
        if not self._base_url:
            missing.append("AGENTSCOPE_LLM_BASE_URL")
        if not self._api_key:
            missing.append("AGENTSCOPE_LLM_API_KEY")
        if not self._model:
            missing.append("AGENTSCOPE_LLM_MODEL")

        if missing:
            raise LLMConfigError(
                "LLM configuration incomplete. Set environment variables:\n"
                + "\n".join(f"  export {v}=<value>" for v in missing)
                + "\n\nExample (DeepSeek):\n"
                "  export AGENTSCOPE_LLM_PROVIDER=deepseek\n"
                "  export AGENTSCOPE_LLM_BASE_URL=https://api.deepseek.com/v1\n"
                "  export AGENTSCOPE_LLM_API_KEY=sk-...\n"
                "  export AGENTSCOPE_LLM_MODEL=deepseek-chat\n"
                "\nExample (GLM):\n"
                "  export AGENTSCOPE_LLM_PROVIDER=glm\n"
                "  export AGENTSCOPE_LLM_BASE_URL=https://open.bigmodel.cn/api/paas/v4\n"
                "  export AGENTSCOPE_LLM_API_KEY=your-key\n"
                "  export AGENTSCOPE_LLM_MODEL=glm-4.7-flash"
            )

        self._timeout = timeout

    @property
    def provider(self) -> str:
        return self._provider

    @property
    def model(self) -> str:
        return self._model

    @property
    def base_url(self) -> str:
        return self._base_url

    def generate_response(self, messages: list[dict[str, str]], **kwargs: Any) -> LLMResponse:
        """Send a Chat Completions request and return the response.

        Args:
            messages: List of message dicts with ``role`` and ``content`` keys.
            **kwargs: Extra parameters passed to the API (e.g. ``temperature``).

        Returns:
            An ``LLMResponse`` with the generated content and metadata.

        Raises:
            LLMResponseError: If the API returns a non-2xx status or is unreachable.
                The error message contains the HTTP status and provider name only,
                **not** the API key.
        """
        url = f"{self._base_url}/chat/completions"
        headers = {
            "Authorization": f"Bearer {self._api_key}",
            "Content-Type": "application/json",
        }

        payload: dict[str, Any] = {
            "model": self._model,
            "messages": messages,
        }
        if kwargs:
            payload.update(kwargs)

        start_time = time.monotonic()
        try:
            response = httpx.post(url, headers=headers, json=payload, timeout=self._timeout)
        except httpx.TimeoutException as exc:
            raise LLMResponseError(f"[{self._provider}] Request timed out after {self._timeout}s") from exc
        except httpx.RequestError as e:
            raise LLMResponseError(f"[{self._provider}] Connection failed: {e}") from e

        elapsed_ms = int((time.monotonic() - start_time) * 1000)

        if not response.is_success:
            status = response.status_code
            detail = _safe_error_detail(response)
            raise LLMResponseError(f"[{self._provider}] HTTP {status}: {detail}")

        data = response.json()
        choice = data.get("choices", [{}])[0]
        content = (choice.get("message") or {}).get("content", "")

        usage = data.get("usage", {}) or {}
        prompt_tokens = usage.get("prompt_tokens", 0) or 0
        completion_tokens = usage.get("completion_tokens", 0) or 0
        total_tokens = usage.get("total_tokens", 0) or 0

        return LLMResponse(
            content=content,
            provider=self._provider,
            model=self._model,
            latency_ms=elapsed_ms,
            prompt_tokens=prompt_tokens,
            completion_tokens=completion_tokens,
            total_tokens=total_tokens,
        )


def _safe_error_detail(response: httpx.Response) -> str:
    """Extract a short error detail without leaking the API key."""
    try:
        body = response.json()
        detail = body.get("error", {}).get("message", str(response.reason_phrase))
    except Exception:
        detail = str(response.reason_phrase)
    return detail
