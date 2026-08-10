#!/usr/bin/env python3
"""Deterministic Task Runner v1 fixture used only by Harbor sentinel E2E.

It accepts the same narrow Task Runner v1 request that OpenHands/Hermes use,
then invokes the supplied Harbor Tool Gateway once.  It never mounts task state,
Docker, the host filesystem, or installs dependencies.  The successful terminal
response is intentionally not treated as the benchmark score: Harbor's task
verifier writes the official reward.
"""

from __future__ import annotations

import json
import os
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

_PROTOCOL = "agentlab.harbor-tool-gateway.v1"
_TOKEN_HEADER = "X-AgentLab-Tool-Gateway-Token"
_SENTINEL_COMMAND = "printf '%s\\n' agentlab-harbor-sentinel-v1 > /app/.agentlab-harbor-sentinel"


class _State:
    def __init__(self) -> None:
        self.lock = threading.Lock()
        self.runs: dict[str, dict[str, Any]] = {}

    def start(self, payload: object) -> dict[str, Any]:
        if not isinstance(payload, dict):
            raise ValueError("invalid payload")
        task_run_id = payload.get("task_run_id")
        gateway = payload.get("tool_gateway")
        task = payload.get("task")
        if not isinstance(task_run_id, str) or not task_run_id:
            raise ValueError("invalid run")
        if not isinstance(gateway, dict) or gateway.get("protocol_version") != _PROTOCOL:
            raise ValueError("invalid gateway")
        endpoint = gateway.get("endpoint")
        if not isinstance(endpoint, str) or not endpoint.startswith("http://"):
            raise ValueError("invalid endpoint")
        if not isinstance(task, dict) or task.get("task_id") != "harbor-sentinel":
            raise ValueError("unexpected task")
        with self.lock:
            existing = self.runs.get(task_run_id)
            if existing is not None:
                return dict(existing)
            run = {"task_run_id": task_run_id, "state": "running", "final_output": None}
            self.runs[task_run_id] = run
        try:
            _execute_gateway(endpoint)
        except _GatewayFailure as exc:
            completed = {"task_run_id": task_run_id, "state": "failed", "final_output": None, "error": {"code": exc.code}}
        except Exception:
            completed = {"task_run_id": task_run_id, "state": "failed", "final_output": None, "error": {"code": "gateway_execution_failed"}}
        else:
            completed = {"task_run_id": task_run_id, "state": "succeeded", "final_output": "sentinel command submitted"}
        with self.lock:
            self.runs[task_run_id] = completed
        return dict(completed)

    def get(self, task_run_id: str) -> dict[str, Any] | None:
        with self.lock:
            value = self.runs.get(task_run_id)
            return dict(value) if value is not None else None


class _GatewayFailure(RuntimeError):
    def __init__(self, code: str) -> None:
        self.code = code
        super().__init__(code)


def _execute_gateway(endpoint: str) -> None:
    token = os.environ.get("AGENTLAB_TOOL_GATEWAY_TOKEN", "")
    if len(token) < 24:
        raise _GatewayFailure("gateway_capability_missing")
    request = Request(
        endpoint.rstrip("/") + "/terminal/exec",
        data=json.dumps({"command": _SENTINEL_COMMAND, "timeout_seconds": 20}, separators=(",", ":")).encode("utf-8"),
        headers={"Content-Type": "application/json", _TOKEN_HEADER: token},
        method="POST",
    )
    try:
        with urlopen(request, timeout=25) as response:  # noqa: S310 -- endpoint is worker-created, token-gated gateway.
            raw = response.read()
            status = response.status
    except HTTPError as exc:
        raise _GatewayFailure("gateway_http_error") from exc
    except URLError as exc:
        raise _GatewayFailure("gateway_unreachable") from exc
    if status != 200:
        raise _GatewayFailure("gateway_rejected")
    try:
        result = json.loads(raw.decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as exc:
        raise _GatewayFailure("gateway_response_invalid") from exc
    if not isinstance(result, dict) or result.get("return_code") != 0:
        raise _GatewayFailure("sandbox_command_failed")


class _Handler(BaseHTTPRequestHandler):
    state: _State

    def do_GET(self) -> None:  # noqa: N802
        if self.path == "/healthz":
            self._json(200, {"ok": True, "protocol_version": "v1", "tool_gateway_protocols": [_PROTOCOL]})
            return
        prefix = "/v1/task-runs/"
        if self.path.startswith(prefix):
            task_run_id = self.path.removeprefix(prefix)
            if not task_run_id or "/" in task_run_id:
                self._json(404, {"error": {"code": "not_found"}})
                return
            result = self.state.get(task_run_id)
            self._json(200, result) if result is not None else self._json(404, {"error": {"code": "not_found"}})
            return
        self._json(404, {"error": {"code": "not_found"}})

    def do_POST(self) -> None:  # noqa: N802
        if self.path != "/v1/task-runs":
            self._json(404, {"error": {"code": "not_found"}})
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if not 0 < length <= 128 * 1024:
                raise ValueError("invalid length")
            payload = json.loads(self.rfile.read(length).decode("utf-8"))
            result = self.state.start(payload)
        except (ValueError, UnicodeDecodeError, json.JSONDecodeError):
            self._json(400, {"error": {"code": "invalid_request"}})
            return
        self._json(200, result)

    def log_message(self, _format: str, *args: object) -> None:
        del args  # Task data and capability-bearing requests must not enter logs.

    def _json(self, status: int, payload: dict[str, Any]) -> None:
        body = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)


def main() -> None:
    token = os.environ.get("AGENTLAB_TOOL_GATEWAY_TOKEN", "")
    if len(token) < 24:
        raise SystemExit("AGENTLAB_TOOL_GATEWAY_TOKEN is required")
    state = _State()
    handler = type("SentinelHandler", (_Handler,), {"state": state})
    server = ThreadingHTTPServer(("0.0.0.0", 8082), handler)
    server.serve_forever()


if __name__ == "__main__":
    main()
