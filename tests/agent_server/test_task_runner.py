"""Contract tests for the OpenHands Benchmark Task Runner HTTP v1."""

from __future__ import annotations

import asyncio
from types import SimpleNamespace
from uuid import uuid4

import httpx
import pytest

from openhands.agent_server.task_runner import (
    CreateTaskRunRequest,
    FakeTaskExecutor,
    GatewayTerminalExecutor,
    OpenHandsConversationExecutor,
    TaskExecutionError,
    TaskExecutionResult,
    create_task_runner_app,
)
from openhands.sdk import Tool



def _request_payload(task_run_id: str | None = None) -> dict[str, object]:
    return {
        "task_run_id": task_run_id or str(uuid4()),
        "task": {
            "task_id": "terminal-bench-smoke",
            "instruction": "Create a small file in the assigned workspace.",
            "workspace": "/workspace",
            "timeout_seconds": 60,
        },
        "tool_gateway": {
            "protocol_version": "agentlab.harbor-tool-gateway.v1",
            "endpoint": "http://tool-gateway.internal:8080",
        },
        "profiles": {
            "agent_profile": "openhands-default",
            "model_profile": "test-model",
        },
        "correlation": {
            "experiment_id": "experiment-1",
            "eval_job_id": "job-1",
            "trial_index": 0,
        },
    }


async def _client_for(executor):
    app = create_task_runner_app(executor=executor)
    transport = httpx.ASGITransport(app=app)
    client = httpx.AsyncClient(transport=transport, base_url="http://task-runner.test")
    return client


@pytest.mark.asyncio
async def test_healthz_and_fake_task_lifecycle() -> None:
    client = await _client_for(FakeTaskExecutor())
    try:
        health = await client.get("/healthz")
        assert health.status_code == 200
        assert health.json() == {
            "ok": True,
            "protocol_version": "v1",
            "tool_gateway_protocols": ["agentlab.harbor-tool-gateway.v1"],
            "executor": "fake",
        }

        payload = _request_payload()
        created = await client.post("/v1/task-runs", json=payload)
        assert created.status_code == 202
        assert created.json()["state"] == "queued"
        assert created.json()["final_output"] is None

        task: httpx.Response | None = None
        for _ in range(10):
            await asyncio.sleep(0)
            task = await client.get(f"/v1/task-runs/{payload['task_run_id']}")
            if task.json()["state"] != "queued":
                break
        assert task is not None
        assert task.status_code == 200
        assert task.json()["state"] == "succeeded"
        assert task.json()["completed_at"] is not None
        assert task.json()["final_output"] == "fake completed terminal-bench-smoke"
        assert task.json()["error"] is None
    finally:
        await client.aclose()


class BlockingExecutor:
    name = "blocking-test"

    def __init__(self) -> None:
        self.started = asyncio.Event()
        self.release = asyncio.Event()
        self.cancelled: list[str] = []

    async def execute(
        self,
        request: CreateTaskRunRequest,
        cancellation: asyncio.Event,
    ) -> TaskExecutionResult:
        self.started.set()
        await self.release.wait()
        if cancellation.is_set():
            raise RuntimeError("secret-bearing executor error must stay private")
        return TaskExecutionResult(final_output="unexpected success")

    async def cancel(self, task_run_id) -> None:
        self.cancelled.append(str(task_run_id))
        self.release.set()


@pytest.mark.asyncio
async def test_cancel_wins_over_late_executor_result_and_keeps_errors_private() -> None:
    executor = BlockingExecutor()
    client = await _client_for(executor)
    try:
        payload = _request_payload()
        created = await client.post("/v1/task-runs", json=payload)
        assert created.status_code == 202
        await asyncio.wait_for(executor.started.wait(), timeout=1)

        cancelled = await client.post(f"/v1/task-runs/{payload['task_run_id']}/cancel")
        assert cancelled.status_code == 200
        assert cancelled.json()["state"] == "cancelled"
        assert cancelled.json()["error"] == {"code": "cancelled"}
        assert str(payload["task_run_id"]) in executor.cancelled

        await asyncio.sleep(0)
        task = await client.get(f"/v1/task-runs/{payload['task_run_id']}")
        assert task.json()["state"] == "cancelled"
        assert "secret-bearing" not in task.text
    finally:
        await client.aclose()


class ProviderStyleFailureExecutor:
    name = "provider-style-failure"

    async def execute(
        self, request: CreateTaskRunRequest, cancellation: asyncio.Event
    ) -> TaskExecutionResult:
        del request, cancellation

        class ProviderFailure(Exception):
            pass

        ProviderFailure.__module__ = "litellm.exceptions"
        raise ProviderFailure("provider response must never appear")

    async def cancel(self, task_run_id) -> None:
        del task_run_id


@pytest.mark.asyncio
async def test_task_runner_classifies_provider_failure_without_serializing_error_text() -> None:
    client = await _client_for(ProviderStyleFailureExecutor())
    try:
        payload = _request_payload()
        created = await client.post("/v1/task-runs", json=payload)
        assert created.status_code == 202
        for _ in range(20):
            await asyncio.sleep(0)
            task = await client.get(f"/v1/task-runs/{payload['task_run_id']}")
            if task.json()["state"] == "failed":
                break
        assert task.json()["state"] == "failed"
        assert task.json()["error"] == {"code": "model_provider_error"}
        assert "provider response" not in task.text
    finally:
        await client.aclose()


@pytest.mark.asyncio
async def test_rejects_invalid_workspace_and_duplicate_task_run_ids() -> None:
    client = await _client_for(FakeTaskExecutor())
    try:
        invalid = _request_payload()
        invalid["task"]["workspace"] = "relative/workspace"  # type: ignore[index]
        invalid_response = await client.post("/v1/task-runs", json=invalid)
        assert invalid_response.status_code == 422

        credentialed_gateway = _request_payload()
        credentialed_gateway["tool_gateway"]["endpoint"] = (  # type: ignore[index]
            "http://user:secret@tool-gateway.internal:8080"
        )
        credentialed_response = await client.post(
            "/v1/task-runs", json=credentialed_gateway
        )
        assert credentialed_response.status_code == 422
        assert "secret@" not in credentialed_response.text

        query_gateway = _request_payload()
        query_gateway["tool_gateway"]["endpoint"] = (  # type: ignore[index]
            "http://tool-gateway.internal:8080?credential=hidden"
        )
        query_response = await client.post("/v1/task-runs", json=query_gateway)
        assert query_response.status_code == 422
        assert "credential=hidden" not in query_response.text

        legacy_gateway = _request_payload()
        legacy_gateway["tool_gateway"]["protocol_version"] = "v1"  # type: ignore[index]
        legacy_response = await client.post("/v1/task-runs", json=legacy_gateway)
        assert legacy_response.status_code == 422
        assert "agentlab.harbor-tool-gateway.v1" not in legacy_response.text

        task_run_id = str(uuid4())
        first = await client.post("/v1/task-runs", json=_request_payload(task_run_id))
        duplicate = await client.post(
            "/v1/task-runs", json=_request_payload(task_run_id)
        )
        assert first.status_code == 202
        assert duplicate.status_code == 409
        assert duplicate.json() == {"detail": "task_run_already_exists"}
    finally:
        await client.aclose()


@pytest.mark.asyncio
async def test_not_found_task_routes() -> None:
    client = await _client_for(FakeTaskExecutor())
    try:
        unknown = uuid4()
        assert (await client.get(f"/v1/task-runs/{unknown}")).status_code == 404
        assert (await client.post(f"/v1/task-runs/{unknown}/cancel")).status_code == 404
    finally:
        await client.aclose()


def test_tool_spec_omits_process_local_executor_from_conversation_serialization() -> None:
    runtime_executor = object()

    serialized = Tool(
        name="TerminalTool",
        params={"executor": runtime_executor, "terminal_type": "subprocess"},
    ).model_dump(mode="json")

    assert serialized == {"name": "TerminalTool", "params": {"terminal_type": "subprocess"}}


@pytest.mark.asyncio
async def test_native_executor_hides_missing_llm_key_runtime_values(tmp_path) -> None:
    client = await _client_for(
        OpenHandsConversationExecutor(
            env={
                "AGENTSCOPE_PROJECT_TOKEN": "must-never-appear-in-http-response",
                "AGENTLAB_TOOL_GATEWAY_TOKEN": "gateway-token-must-not-appear",
            }
        )
    )
    try:
        payload = _request_payload()
        payload["task"]["workspace"] = str(tmp_path)  # type: ignore[index]
        created = await client.post("/v1/task-runs", json=payload)
        assert created.status_code == 202

        task: httpx.Response | None = None
        for _ in range(10):
            await asyncio.sleep(0)
            task = await client.get(f"/v1/task-runs/{payload['task_run_id']}")
            if task.json()["state"] == "failed":
                break
        assert task is not None
        assert task.json()["state"] == "failed"
        assert task.json()["error"] == {"code": "missing_llm_api_key"}
        assert "must-never-appear" not in task.text
    finally:
        await client.aclose()



def _gateway_client(handler) -> httpx.Client:
    return httpx.Client(transport=httpx.MockTransport(handler))


def test_gateway_terminal_executor_posts_only_the_bounded_command_contract() -> None:
    captured: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        captured["url"] = str(request.url)
        captured["token"] = request.headers.get("X-AgentLab-Tool-Gateway-Token")
        captured["json"] = request.content
        return httpx.Response(
            200,
            json={
                "stdout": "sandbox stdout",
                "stderr": "sandbox stderr",
                "return_code": 7,
            },
        )

    token = "gateway-test-token-not-for-output"
    executor = GatewayTerminalExecutor(
        endpoint="http://gateway.test/base/",
        token=token,
        task_timeout_seconds=30,
        client=_gateway_client(handler),
    )
    try:
        from openhands.tools.terminal import TerminalAction

        observation = executor(TerminalAction(command="pwd", timeout=1.2))
    finally:
        executor.close()

    assert captured["url"] == "http://gateway.test/base/terminal/exec"
    assert captured["token"] == token
    assert captured["json"] == b'{"command":"pwd","timeout_seconds":2}'
    assert observation.is_error is False
    assert observation.exit_code == 7
    assert observation.text == "sandbox stdout\n[stderr]\nsandbox stderr"
    assert token not in observation.text


@pytest.mark.parametrize(
    ("action_kwargs", "expected_code"),
    [
        ({"command": "C-c", "is_input": True}, "interactive_mode_not_supported"),
        ({"command": "ignored", "reset": True}, "interactive_mode_not_supported"),
        ({"command": ""}, "empty_command_not_supported"),
    ],
)
def test_gateway_terminal_executor_rejects_non_gateway_terminal_modes(
    action_kwargs: dict[str, object], expected_code: str
) -> None:
    invoked = False

    def handler(_request: httpx.Request) -> httpx.Response:
        nonlocal invoked
        invoked = True
        return httpx.Response(200, json={"stdout": "", "stderr": "", "return_code": 0})

    executor = GatewayTerminalExecutor(
        endpoint="http://gateway.test",
        token="gateway-test-token",
        task_timeout_seconds=30,
        client=_gateway_client(handler),
    )
    try:
        from openhands.tools.terminal import TerminalAction

        observation = executor(TerminalAction(**action_kwargs))
    finally:
        executor.close()

    assert invoked is False
    assert observation.is_error is True
    assert expected_code in observation.text


def test_gateway_terminal_executor_hides_gateway_response_details_and_token() -> None:
    token = "gateway-test-token-must-not-leak"

    def handler(_request: httpx.Request) -> httpx.Response:
        return httpx.Response(502, text=f"upstream failure {token}")

    executor = GatewayTerminalExecutor(
        endpoint="http://gateway.test",
        token=token,
        task_timeout_seconds=30,
        client=_gateway_client(handler),
    )
    try:
        from openhands.tools.terminal import TerminalAction

        observation = executor(TerminalAction(command="echo test"))
    finally:
        executor.close()

    assert observation.is_error is True
    assert observation.text == (
        "terminal_gateway_execution_failed: "
        "the benchmark terminal gateway did not return a valid command result."
    )
    assert token not in observation.text


@pytest.mark.asyncio
async def test_native_executor_requires_gateway_token_before_model_or_workspace_access(
) -> None:
    token = "gateway-token-must-not-appear"
    request = CreateTaskRunRequest.model_validate(_request_payload())
    request.task.workspace = "/not-mounted-in-sidecar"
    executor = OpenHandsConversationExecutor(
        env={"LLM_API_KEY": "model-key-must-not-appear"}
    )

    with pytest.raises(TaskExecutionError) as raised:
        await executor.execute(request, asyncio.Event())

    assert raised.value.code == "tool_gateway_not_configured"
    assert token not in str(raised.value)


@pytest.mark.asyncio
async def test_native_executor_skips_mounted_task_workspace() -> None:
    token = "gateway-token-must-not-appear"
    request = CreateTaskRunRequest.model_validate(_request_payload())
    request.task.workspace = "/not-mounted-in-sidecar"
    executor = OpenHandsConversationExecutor(env={"AGENTLAB_TOOL_GATEWAY_TOKEN": token})

    with pytest.raises(TaskExecutionError) as raised:
        await executor.execute(request, asyncio.Event())

    # Missing model configuration is reached after gateway validation, proving
    # the request's host workspace is not opened or required by the executor.
    assert raised.value.code == "missing_llm_api_key"
    assert token not in str(raised.value)



def test_terminal_tool_accepts_gateway_executor_without_local_task_workspace() -> None:
    from openhands.tools.terminal import TerminalTool

    executor = GatewayTerminalExecutor(
        endpoint="http://gateway.test",
        token="gateway-test-token",
        task_timeout_seconds=30,
        client=_gateway_client(
            lambda _request: httpx.Response(
                200, json={"stdout": "", "stderr": "", "return_code": 0}
            )
        ),
    )
    conv_state = SimpleNamespace(
        workspace=SimpleNamespace(working_dir="/not-mounted-in-sidecar"),
        agent=SimpleNamespace(llm=SimpleNamespace(vision_is_active=lambda: False)),
        env_observation_persistence_dir=None,
    )
    try:
        tool = TerminalTool.create(conv_state, executor=executor)[0]
    finally:
        executor.close()

    assert tool.executor is executor
