"""Regression tests for direct AgentScope SDK calls in Hermes' real loop."""

from __future__ import annotations

import sys
import threading
from pathlib import Path
from types import ModuleType, SimpleNamespace
from unittest.mock import MagicMock, patch

import pytest

from run_agent import AIAgent


class _TraceSession:
    """Record the public SDK lifecycle API used by the owned Hermes source."""

    instances: list["_TraceSession"] = []

    def __init__(self, runtime, tracer, **kwargs) -> None:
        self.runtime = runtime
        self.tracer = tracer
        self.created_kwargs = kwargs
        self.calls: list[tuple[str, tuple, dict]] = []
        type(self).instances.append(self)

    def _record(self, name, *args, **kwargs):
        self.calls.append((name, args, kwargs))
        return True

    start_run = lambda self, *args, **kwargs: self._record("start_run", *args, **kwargs)
    start_turn = lambda self, *args, **kwargs: self._record("start_turn", *args, **kwargs)
    finish_turn = lambda self, *args, **kwargs: self._record("finish_turn", *args, **kwargs)
    start_llm = lambda self, *args, **kwargs: self._record("start_llm", *args, **kwargs)
    finish_llm = lambda self, *args, **kwargs: self._record("finish_llm", *args, **kwargs)
    start_tool = lambda self, *args, **kwargs: self._record("start_tool", *args, **kwargs)
    finish_tool = lambda self, *args, **kwargs: self._record("finish_tool", *args, **kwargs)
    record_final_answer = lambda self, *args, **kwargs: self._record("record_final_answer", *args, **kwargs)
    finish_run = lambda self, *args, **kwargs: self._record("finish_run", *args, **kwargs)
    fail_run = lambda self, *args, **kwargs: self._record("fail_run", *args, **kwargs)


@pytest.fixture(autouse=True)
def direct_sdk(monkeypatch):
    _TraceSession.instances = []
    sdk = ModuleType("agentscope_sdk")
    sdk.RuntimeTraceSession = _TraceSession
    monkeypatch.setitem(sys.modules, "agentscope_sdk", sdk)
    yield


@pytest.fixture()
def agent():
    with (
        patch(
            "run_agent.get_tool_definitions",
            return_value=[
                {
                    "type": "function",
                    "function": {
                        "name": "web_search",
                        "description": "search",
                        "parameters": {"type": "object", "properties": {}},
                    },
                }
            ],
        ),
        patch("run_agent.check_toolset_requirements", return_value={}),
        patch("run_agent.OpenAI"),
    ):
        instance = AIAgent(
            api_key="test-key-1234567890",
            base_url="https://openrouter.ai/api/v1",
            quiet_mode=True,
            skip_context_files=True,
            skip_memory=True,
        )
    instance.client = MagicMock()
    instance._cached_system_prompt = "You are helpful."
    instance._use_prompt_caching = False
    instance.tool_delay = 0
    instance.compression_enabled = False
    instance.save_trajectories = False
    instance._agentscope_runtime = object()
    instance._agentscope_tracer = object()
    instance._agentscope_sessions = {}
    instance._agentscope_sessions_lock = threading.RLock()
    return instance


def _response(content: str) -> SimpleNamespace:
    message = SimpleNamespace(content=content, tool_calls=None)
    choice = SimpleNamespace(message=message, finish_reason="stop")
    return SimpleNamespace(choices=[choice], model="test/model", usage=None)


def _run(agent: AIAgent, message: str):
    with (
        patch.object(agent, "_persist_session"),
        patch.object(agent, "_save_trajectory"),
        patch.object(agent, "_cleanup_task_resources"),
    ):
        return agent.run_conversation(message)


def _only_session() -> _TraceSession:
    assert len(_TraceSession.instances) == 1
    return _TraceSession.instances[0]


def test_real_conversation_loop_calls_direct_sdk_for_full_answer(agent):
    agent.client.chat.completions.create.return_value = _response("Done")

    result = _run(agent, "hello")

    assert result["completed"] is True
    session = _only_session()
    names = [name for name, _, _ in session.calls]
    assert names == [
        "start_run",
        "start_turn",
        "start_llm",
        "finish_llm",
        "record_final_answer",
        "finish_turn",
        "finish_run",
    ]
    run_call = session.calls[0]
    assert run_call[1][0] == "Hermes user turn"
    assert run_call[2]["metadata"]["model_provider"] == agent.provider
    assert run_call[2]["metadata"]["model_name"] == agent.model
    assert run_call[2]["metadata"]["hermes.run.api_mode"] == agent.api_mode
    assert run_call[2]["metadata"]["hermes.run.tool_catalog_size"] == len(agent.tools)
    turn_call = session.calls[1]
    assert turn_call[2]["turn_index"] == 1
    llm_start = session.calls[2]
    llm_finish = session.calls[3]
    assert llm_start[2]["call_id"].endswith(":attempt:1")
    assert llm_start[2]["input_summary"] != "hello"
    assert llm_start[2]["attributes"]["hermes.llm.api_call_index"] == 1
    assert llm_start[2]["attributes"]["hermes.llm.streaming"] is False
    assert "provider" not in llm_finish[2]
    assert "model" not in llm_finish[2]
    assert llm_finish[2]["success"] is True
    assert llm_finish[2]["attributes"]["hermes.llm.error_category"] == "none"
    assert llm_finish[2]["duration_s"] >= 0
    final_call = session.calls[4]
    assert final_call[1][0] == "Done"
    assert session.calls[-1][2]["metrics"]["hermes.run.api_calls"] == 1
    assert final_call[2]["attributes"]["hermes.answer.length"] == len("Done")


def test_real_tool_executor_records_tool_under_active_decision_turn(agent):
    tool_call = SimpleNamespace(
        id="tool-1",
        function=SimpleNamespace(name="web_search", arguments="{}"),
    )
    tool_response = _response("")
    tool_response.choices[0].finish_reason = "tool_calls"
    tool_response.choices[0].message.tool_calls = [tool_call]
    agent.client.chat.completions.create.side_effect = [tool_response, _response("Done")]

    with patch("run_agent.handle_function_call", return_value="search result"):
        result = _run(agent, "search for something")

    assert result["completed"] is True
    session = _only_session()
    names = [name for name, _, _ in session.calls]
    assert names == [
        "start_run",
        "start_turn",
        "start_llm",
        "finish_llm",
        "start_tool",
        "finish_tool",
        "finish_turn",
        "start_turn",
        "start_llm",
        "finish_llm",
        "record_final_answer",
        "finish_turn",
        "finish_run",
    ]
    tool_start = session.calls[4]
    tool_finish = session.calls[5]
    assert tool_start[1][0] == "web_search"
    assert tool_start[2]["tool_id"] == "tool-1"
    assert tool_start[2]["attributes"]["hermes.tool.execution_mode"] == "sequential"
    assert tool_finish[1][1] is None
    assert tool_finish[2]["success"] is True
    assert tool_finish[2]["attributes"]["hermes.tool.failure_category"] == "none"
    assert tool_finish[2]["duration_s"] >= 0


def test_interrupted_real_conversation_loop_fails_direct_sdk_run(agent):
    agent._interrupt_requested = True

    result = _run(agent, "hello")

    assert result["interrupted"] is True
    session = _only_session()
    names = [name for name, _, _ in session.calls]
    assert names == ["start_run", "finish_turn", "fail_run"]
    assert "interrupted_by_user" in session.calls[-1][1][0]


def test_invalid_provider_response_closes_llm_and_fails_direct_sdk_run(agent):
    agent._api_max_retries = 1
    agent.client.chat.completions.create.return_value = SimpleNamespace(choices=[])

    result = _run(agent, "hello")

    assert result["failed"] is True
    session = _only_session()
    names = [name for name, _, _ in session.calls]
    assert names == ["start_run", "start_turn", "start_llm", "finish_llm", "finish_turn", "fail_run"]
    assert session.calls[3][2]["success"] is False
    assert "choices" in session.calls[3][2]["error"]


def test_parallel_tool_batch_records_each_real_worker_under_one_run(agent):
    tool_calls = [
        SimpleNamespace(
            id=f"tool-{index}",
            function=SimpleNamespace(name="web_search", arguments=f'{{"query":"Hermes {index}"}}'),
        )
        for index in (1, 2)
    ]
    tool_response = _response("")
    tool_response.choices[0].finish_reason = "tool_calls"
    tool_response.choices[0].message.tool_calls = tool_calls
    agent.client.chat.completions.create.side_effect = [tool_response, _response("Done")]

    with patch("run_agent.handle_function_call", side_effect=["result one", "result two"]):
        result = _run(agent, "search twice")

    assert result["completed"] is True
    session = _only_session()
    tool_starts = [call for call in session.calls if call[0] == "start_tool"]
    tool_finishes = [call for call in session.calls if call[0] == "finish_tool"]
    assert {call[2]["tool_id"] for call in tool_starts} == {"tool-1", "tool-2"}
    assert {call[2]["tool_id"] for call in tool_finishes} == {"tool-1", "tool-2"}
    assert all(call[2]["success"] is True for call in tool_finishes)
    assert all(call[2]["attributes"]["hermes.tool.execution_mode"] == "concurrent" for call in tool_starts)
    assert all(call[2]["attributes"]["hermes.tool.batch_size"] == 2 for call in tool_finishes)


def test_disabled_agent_does_not_import_or_create_sdk_session(agent, monkeypatch):
    agent._agentscope_runtime = None
    agent._agentscope_tracer = None
    monkeypatch.setitem(sys.modules, "agentscope_sdk", None)
    agent.client.chat.completions.create.return_value = _response("Done")

    result = _run(agent, "hello")

    assert result["completed"] is True
    assert not _TraceSession.instances


def test_source_has_no_bridge_or_adapter_runtime_dependency():
    root = Path(__file__).parents[2]
    production_files = [
        root / "agent" / "agent_init.py",
        root / "agent" / "conversation_loop.py",
        root / "agent" / "tool_executor.py",
    ]
    source = "\n".join(path.read_text() for path in production_files)
    assert "AgentScopeBridge" not in source
    assert "_agentscope_bridge" not in source
    assert "HermesAdapter" not in source
    assert "hermes_agentscope" not in source


def test_sdk_initialization_failure_is_fail_open(monkeypatch):
    monkeypatch.setenv("HERMES_AGENTSCOPE_ENABLED", "true")
    monkeypatch.setenv("AGENTSCOPE_SERVER_URL", "http://agentscope.example")
    monkeypatch.setenv("AGENTSCOPE_PROJECT_TOKEN", "test-project-token")
    monkeypatch.setitem(sys.modules, "agentscope_sdk", None)

    with (
        patch("run_agent.get_tool_definitions", return_value=[]),
        patch("run_agent.check_toolset_requirements", return_value={}),
        patch("run_agent.OpenAI"),
    ):
        instance = AIAgent(
            api_key="test-key-1234567890",
            base_url="https://openrouter.ai/api/v1",
            quiet_mode=True,
            skip_context_files=True,
            skip_memory=True,
        )

    assert instance._agentscope_runtime is None
    assert instance._agentscope_tracer is None


def test_enabled_agent_initializes_public_sdk_once_and_reuses_existing_runtime(monkeypatch):
    calls: list[tuple[str, tuple, dict]] = []
    runtime = object()
    sdk = sys.modules["agentscope_sdk"]

    def configure_tracing(*args, **kwargs):
        calls.append(("configure_tracing", args, kwargs))
        return object()

    def get_runtime():
        calls.append(("get_runtime", (), {}))
        return runtime

    def init(*args, **kwargs):
        calls.append(("init", args, kwargs))
        return object()

    sdk.configure_tracing = configure_tracing
    sdk.get_runtime = get_runtime
    sdk.init = init
    monkeypatch.setenv("HERMES_AGENTSCOPE_ENABLED", "true")
    monkeypatch.setenv("AGENTSCOPE_SERVER_URL", "http://agentscope.example/")
    monkeypatch.setenv("AGENTSCOPE_PROJECT_TOKEN", "test-project-token")
    monkeypatch.setenv("AGENTSCOPE_ENVIRONMENT", "benchmark")

    with (
        patch("run_agent.get_tool_definitions", return_value=[]),
        patch("run_agent.check_toolset_requirements", return_value={}),
        patch("run_agent.OpenAI"),
    ):
        instance = AIAgent(
            api_key="test-key-1234567890",
            base_url="https://openrouter.ai/api/v1",
            quiet_mode=True,
            skip_context_files=True,
            skip_memory=True,
        )

    assert instance._agentscope_runtime is runtime
    assert instance._agentscope_tracer is not None
    assert [name for name, _, _ in calls] == ["configure_tracing", "get_runtime"]
    configure_call = calls[0]
    assert configure_call[1][:2] == ("http://agentscope.example", "test-project-token")
    assert configure_call[2]["service_name"] == "hermes-agent"
    assert configure_call[2]["capture_content"] is False
