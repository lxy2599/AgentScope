"""Strict HTTP client for the owned OpenHands/OpenClaw Task Runner v1 contract."""

from __future__ import annotations

import json
from dataclasses import dataclass
from typing import Any, Mapping, Protocol
from urllib.error import HTTPError, URLError
from urllib.parse import quote
from urllib.request import Request, urlopen

from .contracts import HARBOR_TOOL_GATEWAY_PROTOCOL, TaskRun, TaskRunState, TaskSpec


class TaskRunnerError(RuntimeError):
    """Public, credential-safe Task Runner transport/protocol failure."""

    def __init__(self, operation: str, message: str, status_code: int | None = None) -> None:
        self.operation = operation
        self.status_code = status_code
        super().__init__(f"task runner {operation}: {message}")


@dataclass(frozen=True)
class TaskRunnerHTTPResponse:
    status_code: int
    body: bytes


class TaskRunnerHTTPTransport(Protocol):
    def request(self, method: str, url: str, headers: Mapping[str, str], body: bytes | None, timeout_s: float) -> TaskRunnerHTTPResponse: ...


class UrllibTaskRunnerTransport:
    def request(self, method: str, url: str, headers: Mapping[str, str], body: bytes | None, timeout_s: float) -> TaskRunnerHTTPResponse:
        request = Request(url, data=body, headers=dict(headers), method=method)
        try:
            with urlopen(request, timeout=timeout_s) as response:  # noqa: S310 -- endpoint is local Worker-controlled sidecar.
                return TaskRunnerHTTPResponse(response.status, response.read())
        except HTTPError as exc:
            return TaskRunnerHTTPResponse(exc.code, exc.read())
        except URLError as exc:
            # Do not retain request headers or provider/server response text.
            raise TaskRunnerError(method, "network error") from exc


class TaskRunnerHTTPAdapter:
    """Adapter used by a Worker after it starts one immutable agent sidecar.

    Requests carry task data and correlation only. Model credentials and project
    tokens stay in the sidecar's runtime environment, never in this API body.
    """

    def __init__(self, base_url: str, transport: TaskRunnerHTTPTransport | None = None, timeout_s: float = 15.0) -> None:
        normalized = base_url.rstrip("/")
        if not normalized.startswith(("http://", "https://")):
            raise ValueError("task runner URL must start with http:// or https://")
        self.base_url = normalized
        self.transport = transport or UrllibTaskRunnerTransport()
        self.timeout_s = timeout_s

    def healthcheck(self) -> bool:
        payload = self._request_json("GET", "/healthz")
        protocols = payload.get("tool_gateway_protocols")
        return (
            payload.get("ok") is True
            and payload.get("protocol_version") == "v1"
            and isinstance(protocols, list)
            and HARBOR_TOOL_GATEWAY_PROTOCOL in protocols
        )

    def start_task(self, task: TaskSpec) -> TaskRun:
        if task.tool_gateway_protocol != HARBOR_TOOL_GATEWAY_PROTOCOL:
            raise TaskRunnerError("start task", "unsupported Harbor tool gateway protocol")
        payload = {
            "task_run_id": task.task_run_id,
            "task": {
                "task_id": task.task_id,
                "instruction": task.instruction,
                "workspace": task.workspace,
                "timeout_seconds": task.timeout_seconds,
            },
            "tool_gateway": {"protocol_version": task.tool_gateway_protocol, "endpoint": task.tool_gateway_endpoint},
            "profiles": {"agent_profile": task.agent_profile_id, "model_profile": task.model_profile_id},
            "correlation": {
                "experiment_id": task.correlation.experiment_id,
                "eval_job_id": task.correlation.eval_job_id,
                "trial_index": task.correlation.trial_index,
            },
        }
        return self._task_run(self._request_json("POST", "/v1/task-runs", payload))

    def get_task(self, task_run_id: str) -> TaskRun:
        return self._task_run(self._request_json("GET", f"/v1/task-runs/{quote(task_run_id, safe='')}"))

    def cancel_task(self, task_run_id: str) -> TaskRun:
        return self._task_run(self._request_json("POST", f"/v1/task-runs/{quote(task_run_id, safe='')}" + "/cancel", {}))

    def _request_json(self, method: str, path: str, payload: Mapping[str, Any] | None = None) -> dict[str, Any]:
        body = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8") if payload is not None else None
        headers = {"Accept": "application/json"}
        if body is not None:
            headers["Content-Type"] = "application/json"
        response = self.transport.request(method, f"{self.base_url}{path}", headers, body, self.timeout_s)
        if not 200 <= response.status_code < 300:
            # Sidecars intentionally return only public allowlisted codes. Never
            # relay a raw response because an upstream framework may accidentally
            # include task content or provider metadata in it.
            raise TaskRunnerError(method, "sidecar returned a non-success status", response.status_code)
        try:
            decoded = json.loads(response.body.decode("utf-8"))
        except (UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise TaskRunnerError(method, "invalid JSON response", response.status_code) from exc
        if not isinstance(decoded, dict):
            raise TaskRunnerError(method, "JSON response must be an object", response.status_code)
        return decoded

    @staticmethod
    def _task_run(payload: Mapping[str, Any]) -> TaskRun:
        task_run_id = payload.get("task_run_id")
        state = payload.get("state")
        if not isinstance(task_run_id, str) or not task_run_id:
            raise TaskRunnerError("decode task run", "response is missing task_run_id")
        try:
            parsed_state = TaskRunState(state)
        except (TypeError, ValueError) as exc:
            raise TaskRunnerError("decode task run", "response has an invalid state") from exc
        final_output = payload.get("final_output")
        if final_output is not None and not isinstance(final_output, str):
            raise TaskRunnerError("decode task run", "response final_output must be string or null")
        error_code: str | None = None
        error = payload.get("error")
        if isinstance(error, Mapping) and isinstance(error.get("code"), str):
            error_code = error["code"]
        elif error is not None:
            raise TaskRunnerError("decode task run", "response error must contain a public code")
        run_id = payload.get("run_id")
        if run_id is not None and (not isinstance(run_id, str) or not run_id or len(run_id) > 512):
            raise TaskRunnerError("decode task run", "response has an invalid run_id")
        return TaskRun(task_run_id, parsed_state, final_text=final_output, error_code=error_code, run_id=run_id)
