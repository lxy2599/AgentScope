"""Private per-trial HTTP bridge from an Agent sidecar to Harbor's sandbox.

The gateway lives in the Harbor custom-agent process, where it owns the real
``BaseEnvironment`` object.  Agent sidecars never receive a Docker socket or a
host workspace; terminal actions are forwarded only through
``BaseEnvironment.exec`` and therefore run in the same sandbox later verified
by Harbor.

This module purposefully implements a tiny dependency-free HTTP surface rather
than a general proxy.  It accepts one capability-token authenticated terminal
operation and never logs request bodies, commands, responses, or tokens.
"""

from __future__ import annotations

import asyncio
import json
import os
import re
from collections.abc import Mapping
from dataclasses import dataclass
from typing import Any, Protocol
from urllib.parse import urlsplit


_MAX_HEADER_BYTES = 16 * 1024
_MAX_BODY_BYTES = 512 * 1024
_MAX_OUTPUT_CHARS = 256 * 1024
_SAFE_ADVERTISED_HOST = re.compile(r"^(?:[A-Za-z0-9][A-Za-z0-9.-]{0,252}|\[[0-9A-Fa-f:.]+\])$")
_TOKEN_HEADER = "x-agentlab-tool-gateway-token"


class ToolGatewayError(RuntimeError):
    """Public, credential-safe tool-gateway setup failure."""


class SandboxEnvironment(Protocol):
    task_env_config: Any

    async def exec(
        self,
        command: str,
        cwd: str | None = None,
        env: dict[str, str] | None = None,
        timeout_sec: int | None = None,
        user: str | int | None = None,
    ) -> Any: ...


@dataclass(frozen=True)
class ToolGatewayEndpoint:
    """A sidecar-facing endpoint.  The capability stays out of the URL."""

    endpoint: str


class HarborToolGateway:
    """Serve the narrow v1 terminal bridge for exactly one Harbor environment."""

    def __init__(
        self,
        environment: SandboxEnvironment,
        *,
        capability_token: str,
        advertised_host: str | None = None,
        max_timeout_seconds: int,
    ) -> None:
        if not isinstance(capability_token, str) or len(capability_token) < 24 or "\x00" in capability_token:
            raise ToolGatewayError("tool gateway capability is invalid")
        if isinstance(max_timeout_seconds, bool) or not isinstance(max_timeout_seconds, int) or max_timeout_seconds <= 0:
            raise ToolGatewayError("tool gateway timeout is invalid")
        host = (advertised_host or os.environ.get("AGENTLAB_TOOL_GATEWAY_HOST", "host.docker.internal")).strip()
        if not _SAFE_ADVERTISED_HOST.fullmatch(host):
            raise ToolGatewayError("tool gateway advertised host is invalid")
        self._environment = environment
        self._capability_token = capability_token
        self._advertised_host = host
        self._max_timeout_seconds = min(max_timeout_seconds, 3600)
        self._server: asyncio.AbstractServer | None = None
        self._lock = asyncio.Lock()
        self._endpoint: ToolGatewayEndpoint | None = None

    @property
    def endpoint(self) -> ToolGatewayEndpoint:
        if self._endpoint is None:
            raise ToolGatewayError("tool gateway is not running")
        return self._endpoint

    async def __aenter__(self) -> "HarborToolGateway":
        # In a compose Worker the sidecar joins the same internal Docker network
        # and resolves ``benchmarks-worker``.  A host Worker uses
        # host.docker.internal.  The random port and mandatory capability token
        # prevent this from becoming a generic LAN-exposed shell endpoint.
        self._server = await asyncio.start_server(self._handle, host="0.0.0.0", port=0, limit=_MAX_BODY_BYTES + _MAX_HEADER_BYTES)
        sockets = self._server.sockets or []
        if len(sockets) != 1:
            await self.aclose()
            raise ToolGatewayError("tool gateway did not bind exactly one socket")
        port = sockets[0].getsockname()[1]
        if not isinstance(port, int) or not 1 <= port <= 65535:
            await self.aclose()
            raise ToolGatewayError("tool gateway allocated an invalid port")
        self._endpoint = ToolGatewayEndpoint(f"http://{self._advertised_host}:{port}/v1")
        return self

    async def __aexit__(self, exc_type: object, exc: object, traceback: object) -> None:
        del exc_type, exc, traceback
        await self.aclose()

    async def aclose(self) -> None:
        server, self._server = self._server, None
        self._endpoint = None
        if server is not None:
            server.close()
            await server.wait_closed()

    async def _handle(self, reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        try:
            method, target, headers, body = await _read_request(reader)
            if target == "/v1/healthz" and method == "GET":
                await _write_json(writer, 200, {"ok": True, "protocol_version": "v1"})
                return
            if target != "/v1/terminal/exec" or method != "POST":
                await _write_json(writer, 404, {"error": {"code": "not_found"}})
                return
            if headers.get(_TOKEN_HEADER) != self._capability_token:
                await _write_json(writer, 401, {"error": {"code": "unauthorized"}})
                return
            payload = _parse_terminal_payload(body, self._max_timeout_seconds)
            async with self._lock:
                result = await self._environment.exec(
                    payload.command,
                    cwd=_sandbox_workdir(self._environment),
                    timeout_sec=payload.timeout_seconds,
                )
            await _write_json(
                writer,
                200,
                {
                    "stdout": _bounded_text(getattr(result, "stdout", None)),
                    "stderr": _bounded_text(getattr(result, "stderr", None)),
                    "return_code": _return_code(getattr(result, "return_code", None)),
                },
            )
        except _HTTPError as exc:
            await _write_json(writer, exc.status, {"error": {"code": exc.code}})
        except Exception:
            # Environment/provider errors may contain task or infrastructure
            # details.  The sidecar only receives an allowlisted error code.
            await _write_json(writer, 502, {"error": {"code": "sandbox_execution_failed"}})
        finally:
            writer.close()
            try:
                await writer.wait_closed()
            except Exception:
                pass


@dataclass(frozen=True)
class _TerminalRequest:
    command: str
    timeout_seconds: int


@dataclass(frozen=True)
class _HTTPError(Exception):
    status: int
    code: str


def _sandbox_workdir(environment: SandboxEnvironment) -> str | None:
    config = getattr(environment, "task_env_config", None)
    value = getattr(config, "workdir", None)
    return value if isinstance(value, str) and value.startswith("/") else None


def _bounded_text(value: object) -> str:
    if not isinstance(value, str):
        return ""
    return value[:_MAX_OUTPUT_CHARS]


def _return_code(value: object) -> int:
    return value if isinstance(value, int) and not isinstance(value, bool) else 1


async def _read_request(reader: asyncio.StreamReader) -> tuple[str, str, dict[str, str], bytes]:
    try:
        head = await reader.readuntil(b"\r\n\r\n")
    except (asyncio.IncompleteReadError, asyncio.LimitOverrunError) as exc:
        raise _HTTPError(400, "invalid_request") from exc
    if len(head) > _MAX_HEADER_BYTES:
        raise _HTTPError(413, "request_too_large")
    try:
        lines = head.decode("ascii").split("\r\n")
        method, raw_target, version = lines[0].split(" ")
    except (UnicodeDecodeError, ValueError) as exc:
        raise _HTTPError(400, "invalid_request") from exc
    if version != "HTTP/1.1" or method not in {"GET", "POST"}:
        raise _HTTPError(400, "invalid_request")
    parsed = urlsplit(raw_target)
    if parsed.scheme or parsed.netloc or parsed.query or parsed.fragment:
        raise _HTTPError(400, "invalid_request")
    headers: dict[str, str] = {}
    for line in lines[1:]:
        if not line:
            continue
        if ":" not in line:
            raise _HTTPError(400, "invalid_request")
        key, value = line.split(":", 1)
        normalized = key.strip().lower()
        if not normalized or normalized in headers:
            raise _HTTPError(400, "invalid_request")
        headers[normalized] = value.strip()
    raw_length = headers.get("content-length", "0")
    try:
        length = int(raw_length)
    except ValueError as exc:
        raise _HTTPError(400, "invalid_request") from exc
    if length < 0 or length > _MAX_BODY_BYTES:
        raise _HTTPError(413, "request_too_large")
    body = await reader.readexactly(length) if length else b""
    return method, parsed.path, headers, body


def _parse_terminal_payload(body: bytes, max_timeout_seconds: int) -> _TerminalRequest:
    try:
        payload = json.loads(body.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise _HTTPError(400, "invalid_request") from exc
    if not isinstance(payload, Mapping) or set(payload) != {"command", "timeout_seconds"}:
        raise _HTTPError(400, "invalid_request")
    command = payload.get("command")
    timeout = payload.get("timeout_seconds")
    if not isinstance(command, str) or not command or len(command) > 200_000 or "\x00" in command:
        raise _HTTPError(400, "invalid_request")
    if isinstance(timeout, bool) or not isinstance(timeout, int) or not 1 <= timeout <= max_timeout_seconds:
        raise _HTTPError(400, "invalid_request")
    return _TerminalRequest(command, timeout)


async def _write_json(writer: asyncio.StreamWriter, status: int, payload: Mapping[str, Any]) -> None:
    body = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8")
    reason = {200: "OK", 400: "Bad Request", 401: "Unauthorized", 404: "Not Found", 413: "Payload Too Large", 502: "Bad Gateway"}.get(status, "Error")
    writer.write(
        f"HTTP/1.1 {status} {reason}\r\nContent-Type: application/json\r\nContent-Length: {len(body)}\r\nConnection: close\r\n\r\n".encode("ascii") + body
    )
    await writer.drain()
