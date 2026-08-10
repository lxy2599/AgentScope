from __future__ import annotations

import asyncio
import json
from dataclasses import dataclass
from types import SimpleNamespace
from urllib.parse import urlsplit

from benchmark_worker.tool_gateway import HarborToolGateway


@dataclass
class FakeExecResult:
    stdout: str | None
    stderr: str | None
    return_code: int


class FakeEnvironment:
    def __init__(self) -> None:
        self.task_env_config = SimpleNamespace(workdir="/task")
        self.calls: list[tuple[str, str | None, int | None]] = []

    async def exec(
        self,
        command: str,
        cwd: str | None = None,
        env: dict[str, str] | None = None,
        timeout_sec: int | None = None,
        user: str | int | None = None,
    ) -> FakeExecResult:
        del env, user
        self.calls.append((command, cwd, timeout_sec))
        return FakeExecResult(stdout="sandbox stdout", stderr="sandbox stderr", return_code=0)


async def _request(endpoint: str, method: str, path: str, *, token: str | None = None, payload: object | None = None) -> tuple[int, dict[str, object]]:
    parsed = urlsplit(endpoint)
    reader, writer = await asyncio.open_connection(parsed.hostname, parsed.port)
    body = b"" if payload is None else json.dumps(payload, separators=(",", ":")).encode("utf-8")
    headers = [
        f"{method} {parsed.path}{path} HTTP/1.1",
        f"Host: {parsed.hostname}",
        "Connection: close",
        f"Content-Length: {len(body)}",
    ]
    if token is not None:
        headers.append(f"X-AgentLab-Tool-Gateway-Token: {token}")
    if body:
        headers.append("Content-Type: application/json")
    writer.write("\r\n".join(headers).encode("ascii") + b"\r\n\r\n" + body)
    await writer.drain()
    raw = await reader.read()
    writer.close()
    await writer.wait_closed()
    head, body = raw.split(b"\r\n\r\n", 1)
    status = int(head.splitlines()[0].split()[1])
    return status, json.loads(body.decode("utf-8"))


def test_gateway_executes_only_through_harbor_environment_and_requires_capability() -> None:
    async def run() -> None:
        env = FakeEnvironment()
        token = "x" * 32
        async with HarborToolGateway(env, capability_token=token, advertised_host="127.0.0.1", max_timeout_seconds=60) as gateway:
            status, denied = await _request(
                gateway.endpoint.endpoint,
                "POST",
                "/terminal/exec",
                payload={"command": "pwd", "timeout_seconds": 5},
            )
            assert status == 401
            assert denied == {"error": {"code": "unauthorized"}}
            assert env.calls == []

            status, result = await _request(
                gateway.endpoint.endpoint,
                "POST",
                "/terminal/exec",
                token=token,
                payload={"command": "printf sandbox", "timeout_seconds": 5},
            )
            assert status == 200
            assert result == {"stdout": "sandbox stdout", "stderr": "sandbox stderr", "return_code": 0}
            assert env.calls == [("printf sandbox", "/task", 5)]

        # The endpoint is closed at end of trial, so the capability cannot reach
        # a verifier phase after TaskRunnerAgent.run returns.
        assert gateway._server is None  # noqa: SLF001 -- lifecycle invariant.

    asyncio.run(run())


def test_gateway_rejects_malformed_or_oversized_sandbox_requests_without_fallback() -> None:
    async def run() -> None:
        env = FakeEnvironment()
        token = "y" * 32
        async with HarborToolGateway(env, capability_token=token, advertised_host="127.0.0.1", max_timeout_seconds=10) as gateway:
            status, malformed = await _request(
                gateway.endpoint.endpoint,
                "POST",
                "/terminal/exec",
                token=token,
                payload={"command": "pwd"},
            )
            assert status == 400
            assert malformed == {"error": {"code": "invalid_request"}}

            status, excessive_timeout = await _request(
                gateway.endpoint.endpoint,
                "POST",
                "/terminal/exec",
                token=token,
                payload={"command": "pwd", "timeout_seconds": 11},
            )
            assert status == 400
            assert excessive_timeout == {"error": {"code": "invalid_request"}}
            assert env.calls == []

    asyncio.run(run())
