"""Native benchmark-only client for the AgentLab Harbor Tool Gateway.

The gateway is deliberately a narrow terminal capability.  It is attached to a
Hermes agent only by the owned Task Runner, so benchmark tool calls cannot fall
back to the sidecar's local shell or workspace.
"""

from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.parse import urlparse
from urllib.request import Request, urlopen

_TOKEN_HEADER = "x-agentlab-tool-gateway-token"
_MAX_COMMAND_CHARS = 200_000
_MAX_OUTPUT_CHARS = 256_000


@dataclass(frozen=True)
class HarborToolGateway:
    """Task-scoped capability client owned by Hermes' native tool path."""

    endpoint: str
    capability_token: str
    max_timeout_seconds: int

    def __post_init__(self) -> None:
        parsed = urlparse(self.endpoint)
        if parsed.scheme not in {"http", "https"} or not parsed.netloc:
            raise ValueError("tool gateway endpoint must be an absolute HTTP(S) URL")
        if parsed.username is not None or parsed.password is not None or parsed.query or parsed.fragment:
            raise ValueError("tool gateway endpoint is invalid")
        if not isinstance(self.capability_token, str) or len(self.capability_token) < 24:
            raise ValueError("tool gateway capability is invalid")
        if isinstance(self.max_timeout_seconds, bool) or not isinstance(self.max_timeout_seconds, int):
            raise ValueError("tool gateway timeout is invalid")
        if not 1 <= self.max_timeout_seconds <= 3600:
            raise ValueError("tool gateway timeout is invalid")

    def execute(self, function_name: str, function_args: dict[str, Any]) -> str:
        """Execute a supported benchmark tool with no local fallback."""
        if function_name != "terminal":
            return _error("benchmark_tool_gateway_only_supports_terminal")
        if not isinstance(function_args, dict):
            return _error("benchmark_tool_gateway_invalid_request")
        command = function_args.get("command")
        if not isinstance(command, str) or not command or len(command) > _MAX_COMMAND_CHARS or "\x00" in command:
            return _error("benchmark_tool_gateway_invalid_command")
        if function_args.get("background"):
            return _error("benchmark_tool_gateway_background_not_supported")
        timeout = function_args.get("timeout", self.max_timeout_seconds)
        if isinstance(timeout, bool) or not isinstance(timeout, int) or not 1 <= timeout <= self.max_timeout_seconds:
            return _error("benchmark_tool_gateway_invalid_timeout")

        request = Request(
            f"{self.endpoint.rstrip('/')}/terminal/exec",
            data=json.dumps(
                {"command": command, "timeout_seconds": timeout},
                sort_keys=True,
                separators=(",", ":"),
            ).encode("utf-8"),
            headers={"Content-Type": "application/json", _TOKEN_HEADER: self.capability_token},
            method="POST",
        )
        try:
            with urlopen(request, timeout=timeout + 5) as response:  # noqa: S310 -- endpoint is worker-created and capability-gated.
                raw = response.read()
                status = response.status
        except HTTPError:
            return _error("benchmark_tool_gateway_rejected")
        except URLError:
            return _error("benchmark_tool_gateway_unreachable")
        except Exception:
            return _error("benchmark_tool_gateway_transport_failed")
        if status != 200:
            return _error("benchmark_tool_gateway_rejected")
        try:
            payload = json.loads(raw.decode("utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError):
            return _error("benchmark_tool_gateway_invalid_response")
        if not isinstance(payload, dict):
            return _error("benchmark_tool_gateway_invalid_response")
        return_code = payload.get("return_code")
        stdout = payload.get("stdout", "")
        stderr = payload.get("stderr", "")
        if isinstance(return_code, bool) or not isinstance(return_code, int):
            return _error("benchmark_tool_gateway_invalid_response")
        if not isinstance(stdout, str) or not isinstance(stderr, str):
            return _error("benchmark_tool_gateway_invalid_response")
        result: dict[str, Any] = {
            "output": (stdout + (f"\n{stderr}" if stderr else ""))[:_MAX_OUTPUT_CHARS],
            "exit_code": return_code,
        }
        if return_code != 0:
            result["error"] = "sandbox command returned a non-zero exit code"
        return json.dumps(result, ensure_ascii=False)


def _error(code: str) -> str:
    return json.dumps({"error": code}, ensure_ascii=False)
