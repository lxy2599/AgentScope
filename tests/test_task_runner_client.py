from __future__ import annotations

import json
from dataclasses import dataclass, field
from typing import Mapping

import pytest

from benchmark_worker.contracts import Correlation, TaskRunState, TaskSpec
from benchmark_worker.task_runner_client import TaskRunnerError, TaskRunnerHTTPAdapter, TaskRunnerHTTPResponse


@dataclass
class FakeTransport:
    responses: list[TaskRunnerHTTPResponse]
    calls: list[tuple[str, str, dict[str, str], bytes | None]] = field(default_factory=list)

    def request(self, method: str, url: str, headers: Mapping[str, str], body: bytes | None, timeout_s: float) -> TaskRunnerHTTPResponse:
        del timeout_s
        self.calls.append((method, url, dict(headers), body))
        return self.responses.pop(0)


def response(payload: dict, status: int = 200) -> TaskRunnerHTTPResponse:
    return TaskRunnerHTTPResponse(status, json.dumps(payload).encode())


def task() -> TaskSpec:
    return TaskSpec("01234567-89ab-cdef-0123-456789abcdef", "terminal-bench/task-1", "fix it", "/workspace", 30, "http://gateway.local/v1", "agentlab.harbor-tool-gateway.v1", "agent", "model", Correlation("exp", "job", 0))


def test_task_runner_http_adapter_uses_only_v1_contract() -> None:
    transport = FakeTransport([
        response({"ok": True, "protocol_version": "v1", "tool_gateway_protocols": ["agentlab.harbor-tool-gateway.v1"]}),
        response({"task_run_id": task().task_run_id, "state": "queued", "created_at": "now", "final_output": None}),
        response({"task_run_id": task().task_run_id, "state": "succeeded", "created_at": "now", "completed_at": "later", "final_output": "done"}),
        response({"task_run_id": task().task_run_id, "state": "cancelled", "created_at": "now", "completed_at": "later", "final_output": None, "error": {"code": "cancelled"}}),
    ])
    adapter = TaskRunnerHTTPAdapter("http://sidecar.test", transport=transport)

    assert adapter.healthcheck()
    assert adapter.start_task(task()).state is TaskRunState.QUEUED
    assert adapter.get_task(task().task_run_id).final_text == "done"
    assert adapter.cancel_task(task().task_run_id).error_code == "cancelled"

    assert transport.calls[0][0:2] == ("GET", "http://sidecar.test/healthz")
    start = transport.calls[1]
    assert start[0:2] == ("POST", "http://sidecar.test/v1/task-runs")
    payload = json.loads(start[3] or b"{}")
    assert payload["task"]["workspace"] == "/workspace"
    assert payload["tool_gateway"] == {"protocol_version": "agentlab.harbor-tool-gateway.v1", "endpoint": "http://gateway.local/v1"}
    assert payload["correlation"] == {"experiment_id": "exp", "eval_job_id": "job", "trial_index": 0}


def test_task_runner_http_adapter_does_not_expose_error_body() -> None:
    transport = FakeTransport([TaskRunnerHTTPResponse(500, b'{"error":"api_key=should-not-leak"}')])
    with pytest.raises(TaskRunnerError) as caught:
        TaskRunnerHTTPAdapter("http://sidecar.test", transport=transport).healthcheck()
    assert "api_key" not in str(caught.value)
    assert "should-not-leak" not in str(caught.value)


def test_task_runner_http_adapter_rejects_sidecar_without_required_harbor_gateway() -> None:
    transport = FakeTransport([response({"ok": True, "protocol_version": "v1", "tool_gateway_protocols": []})])
    assert not TaskRunnerHTTPAdapter("http://sidecar.test", transport=transport).healthcheck()


def test_task_runner_http_adapter_rejects_legacy_gateway_protocol_before_sidecar_request() -> None:
    legacy = TaskSpec(
        "01234567-89ab-cdef-0123-456789abcdef", "terminal-bench/task-1", "fix it",
        "/workspace", 30, "http://gateway.local/v1", "v1", "agent", "model", Correlation("exp", "job", 0),
    )
    transport = FakeTransport([])
    with pytest.raises(TaskRunnerError, match="unsupported Harbor tool gateway protocol"):
        TaskRunnerHTTPAdapter("http://sidecar.test", transport=transport).start_task(legacy)
    assert transport.calls == []
