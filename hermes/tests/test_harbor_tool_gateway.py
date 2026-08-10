"""Contract tests for Hermes' native Harbor Tool Gateway client."""

from __future__ import annotations

import json
import threading
from collections.abc import Iterator
from contextlib import contextmanager
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any

from agent.harbor_tool_gateway import HarborToolGateway


class _GatewayServer(ThreadingHTTPServer):
    received: list[dict[str, Any]]
    response_status: int
    response_payload: bytes


@contextmanager
def _gateway_server(
    *,
    status: int = 200,
    payload: dict[str, Any] | bytes | None = None,
) -> Iterator[_GatewayServer]:
    class Handler(BaseHTTPRequestHandler):
        def do_POST(self) -> None:  # noqa: N802 -- stdlib HTTP hook.
            body = self.rfile.read(int(self.headers.get("Content-Length", "0")))
            self.server.received.append(  # type: ignore[attr-defined]
                {
                    "path": self.path,
                    "token": self.headers.get("x-agentlab-tool-gateway-token"),
                    "body": body,
                }
            )
            self.send_response(self.server.response_status)  # type: ignore[attr-defined]
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(self.server.response_payload)  # type: ignore[attr-defined]

        def log_message(self, format: str, *args: object) -> None:  # noqa: A003
            del format, args

    server = _GatewayServer(("127.0.0.1", 0), Handler)
    server.received = []
    server.response_status = status
    if payload is None:
        payload = {"return_code": 0, "stdout": "ok", "stderr": ""}
    server.response_payload = payload if isinstance(payload, bytes) else json.dumps(payload).encode("utf-8")
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield server
    finally:
        server.shutdown()
        thread.join(timeout=2)
        server.server_close()


def _client(server: _GatewayServer) -> HarborToolGateway:
    host, port = server.server_address
    return HarborToolGateway(
        endpoint=f"http://{host}:{port}/v1",
        capability_token="capability-token-at-least-24-characters",
        max_timeout_seconds=30,
    )


def test_terminal_request_uses_capability_and_returns_sandbox_result() -> None:
    with _gateway_server(payload={"return_code": 0, "stdout": "sandbox output", "stderr": ""}) as server:
        result = json.loads(_client(server).execute("terminal", {"command": "pwd", "timeout": 12}))

    assert result == {"output": "sandbox output", "exit_code": 0}
    assert len(server.received) == 1
    received = server.received[0]
    assert received["path"] == "/v1/terminal/exec"
    assert received["token"] == "capability-token-at-least-24-characters"
    assert json.loads(received["body"]) == {"command": "pwd", "timeout_seconds": 12}


def test_non_zero_sandbox_exit_is_returned_to_the_agent() -> None:
    with _gateway_server(payload={"return_code": 17, "stdout": "partial", "stderr": "failed"}) as server:
        result = json.loads(_client(server).execute("terminal", {"command": "false"}))

    assert result == {
        "output": "partial\nfailed",
        "exit_code": 17,
        "error": "sandbox command returned a non-zero exit code",
    }


def test_gateway_rejection_and_invalid_response_fail_closed() -> None:
    with _gateway_server(status=403, payload={"detail": "denied"}) as server:
        rejected = json.loads(_client(server).execute("terminal", {"command": "pwd"}))
    with _gateway_server(payload=b"not-json") as server:
        malformed = json.loads(_client(server).execute("terminal", {"command": "pwd"}))

    assert rejected == {"error": "benchmark_tool_gateway_rejected"}
    assert malformed == {"error": "benchmark_tool_gateway_invalid_response"}


def test_unsupported_or_invalid_tool_never_uses_local_fallback() -> None:
    gateway = HarborToolGateway(
        endpoint="http://127.0.0.1:1/v1",
        capability_token="capability-token-at-least-24-characters",
        max_timeout_seconds=30,
    )

    assert json.loads(gateway.execute("read_file", {"path": "/workspace/secret"})) == {
        "error": "benchmark_tool_gateway_only_supports_terminal"
    }
    assert json.loads(gateway.execute("terminal", {"command": "pwd", "background": True})) == {
        "error": "benchmark_tool_gateway_background_not_supported"
    }
    assert json.loads(gateway.execute("terminal", {"command": "pwd", "timeout": 31})) == {
        "error": "benchmark_tool_gateway_invalid_timeout"
    }
