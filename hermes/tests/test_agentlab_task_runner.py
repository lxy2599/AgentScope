"""Contract tests for Hermes' owned Task Runner HTTP v1."""

from __future__ import annotations

import asyncio
import sys
from types import SimpleNamespace
from uuid import UUID

import httpx
import pytest

from hermes_cli.agentlab_task_runner import (
    HARBOR_TOOL_GATEWAY_PROTOCOL,
    CreateTaskRunRequest,
    HermesConversationExecutor,
    TaskExecutionError,
    TaskExecutionResult,
    create_task_runner_app,
)


class _BlockingExecutor:
    name = "blocking"

    def __init__(self) -> None:
        self.started = asyncio.Event()

    async def execute(self, request, cancellation):
        del request
        self.started.set()
        await cancellation.wait()
        raise TaskExecutionError("cancelled")

    async def cancel(self, task_run_id):
        del task_run_id


class _FailingExecutor:
    name = "failing"

    async def execute(self, request, cancellation):
        del request, cancellation
        raise RuntimeError("provider payload must never reach the HTTP response")

    async def cancel(self, task_run_id):
        del task_run_id


def _payload(task_run_id: str = "00000000-0000-0000-0000-000000000001") -> dict:
    return {
        "task_run_id": task_run_id,
        "task": {
            "task_id": "hermes-contract-task",
            "instruction": "do the benchmark task",
            "workspace": "/workspace",
            "timeout_seconds": 30,
        },
        "tool_gateway": {
            "protocol_version": HARBOR_TOOL_GATEWAY_PROTOCOL,
            "endpoint": "http://agentlab-sidecar-example:8080/v1",
        },
        "profiles": {"agent_profile": "harbor", "model_profile": "harbor"},
        "correlation": {"experiment_id": "experiment-1", "eval_job_id": "job-1", "trial_index": 0},
    }


@pytest.mark.asyncio
async def test_health_and_fake_task_runner_contract() -> None:
    app = create_task_runner_app()
    # The default executor does not matter here: use a fake explicitly to pin
    # the sidecar HTTP contract independently of model credentials.
    from hermes_cli.agentlab_task_runner import FakeTaskExecutor

    app = create_task_runner_app(FakeTaskExecutor())
    transport = httpx.ASGITransport(app=app)
    async with httpx.AsyncClient(transport=transport, base_url="http://runner") as client:
        health = await client.get("/healthz")
        assert health.status_code == 200
        assert health.json() == {
            "ok": True,
            "protocol_version": "v1",
            "tool_gateway_protocols": [HARBOR_TOOL_GATEWAY_PROTOCOL],
            "executor": "fake",
        }

        created = await client.post("/v1/task-runs", json=_payload())
        assert created.status_code == 202
        assert created.json()["state"] == "queued"
        assert created.json()["final_output"] is None

        await asyncio.sleep(0)
        fetched = await client.get("/v1/task-runs/00000000-0000-0000-0000-000000000001")
        assert fetched.status_code == 200
        assert fetched.json()["state"] == "succeeded"
        assert fetched.json()["final_output"] == "fake completed hermes-contract-task"


@pytest.mark.asyncio
async def test_cancellation_is_cooperative_and_exposes_only_public_state() -> None:
    executor = _BlockingExecutor()
    app = create_task_runner_app(executor)
    transport = httpx.ASGITransport(app=app)
    run_id = "00000000-0000-0000-0000-000000000002"
    async with httpx.AsyncClient(transport=transport, base_url="http://runner") as client:
        created = await client.post("/v1/task-runs", json=_payload(run_id))
        assert created.status_code == 202
        await executor.started.wait()
        cancelled = await client.post(f"/v1/task-runs/{run_id}/cancel", json={})
        assert cancelled.status_code == 200
        await asyncio.sleep(0)
        fetched = await client.get(f"/v1/task-runs/{run_id}")
        assert fetched.json()["state"] == "cancelled"
        assert fetched.json()["error"] == {"code": "cancelled"}


@pytest.mark.asyncio
async def test_failure_response_does_not_echo_exception_text() -> None:
    app = create_task_runner_app(_FailingExecutor())
    transport = httpx.ASGITransport(app=app)
    async with httpx.AsyncClient(transport=transport, base_url="http://runner") as client:
        created = await client.post(
            "/v1/task-runs",
            json=_payload("00000000-0000-0000-0000-000000000003"),
        )
        assert created.status_code == 202
        await asyncio.sleep(0)
        fetched = await client.get("/v1/task-runs/00000000-0000-0000-0000-000000000003")
        assert fetched.json()["state"] == "failed"
        assert fetched.json()["error"] == {"code": "agent_execution_failed"}
        assert "provider payload" not in fetched.text


def test_invalid_gateway_capability_is_rejected_without_local_fallback() -> None:
    from agent.harbor_tool_gateway import HarborToolGateway

    with pytest.raises(ValueError, match="capability"):
        HarborToolGateway("http://gateway.example/v1", "too-short", 30)

@pytest.mark.asyncio
async def test_native_executor_uses_hermes_agent_and_attaches_task_scoped_gateway(monkeypatch) -> None:
    constructed: list[object] = []

    class CapturingAgent:
        def __init__(self, **kwargs) -> None:
            self.kwargs = kwargs
            constructed.append(self)

        def run_conversation(self, instruction: str, *, task_id: str):
            assert instruction == "do the benchmark task"
            assert task_id == "hermes-contract-task"
            return {"completed": True, "interrupted": False, "final_response": "native result"}

    monkeypatch.setitem(sys.modules, "run_agent", SimpleNamespace(AIAgent=CapturingAgent))
    request = CreateTaskRunRequest.model_validate(_payload())
    executor = HermesConversationExecutor(
        {
            "AGENTLAB_TOOL_GATEWAY_TOKEN": "capability-token-at-least-24-characters",
            "HERMES_MODEL": "test-model",
            "HERMES_API_KEY": "test-key",
            "HERMES_BASE_URL": "http://provider.invalid/v1",
            "HERMES_TASK_RUNNER_MAX_ITERATIONS": "17",
            "AGENTSCOPE_BENCHMARK_KEY": "harbor",
        }
    )

    result = await executor.execute(request, asyncio.Event())

    assert result == TaskExecutionResult(final_output="native result")
    assert len(constructed) == 1
    agent = constructed[0]
    assert agent.kwargs == {
        "api_key": "test-key",
        "base_url": "http://provider.invalid/v1",
        "model": "test-model",
        "max_iterations": 17,
        "enabled_toolsets": ["terminal"],
        "quiet_mode": True,
        "skip_context_files": True,
        "skip_memory": True,
        "save_trajectories": False,
    }
    assert agent._agentlab_harbor_tool_gateway.endpoint == "http://agentlab-sidecar-example:8080/v1"
    assert agent._agentscope_benchmark_context == {
        "experiment_id": "experiment-1",
        "eval_job_id": "job-1",
        "eval_trial_id": "job-1:0",
        "benchmark_key": "harbor",
        "benchmark_task_id": "hermes-contract-task",
        "trial_index": 0,
    }


@pytest.mark.asyncio
async def test_task_run_timeout_cancels_executor_and_exposes_timed_out_state() -> None:
    class TimeoutExecutor:
        name = "timeout"

        def __init__(self) -> None:
            self.cancelled = asyncio.Event()

        async def execute(self, request, cancellation):
            del request, cancellation
            await asyncio.Event().wait()
            raise AssertionError("unreachable")

        async def cancel(self, task_run_id):
            del task_run_id
            self.cancelled.set()

    executor = TimeoutExecutor()
    app = create_task_runner_app(executor)
    transport = httpx.ASGITransport(app=app)
    payload = _payload("00000000-0000-0000-0000-000000000004")
    payload["task"]["timeout_seconds"] = 1
    async with httpx.AsyncClient(transport=transport, base_url="http://runner") as client:
        created = await client.post("/v1/task-runs", json=payload)
        assert created.status_code == 202
        await asyncio.wait_for(executor.cancelled.wait(), timeout=2)
        fetched = await client.get("/v1/task-runs/00000000-0000-0000-0000-000000000004")

    assert fetched.json()["state"] == "timed_out"
    assert fetched.json()["error"] == {"code": "timeout"}

