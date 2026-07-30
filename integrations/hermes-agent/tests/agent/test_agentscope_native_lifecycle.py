"""Regression tests for native AgentScope lifecycle calls in Hermes' real loop."""

from __future__ import annotations

from types import SimpleNamespace
from unittest.mock import MagicMock, patch

import pytest

from run_agent import AIAgent


class _Bridge:
    def __init__(self) -> None:
        self.calls: list[tuple[str, tuple, dict]] = []

    def _record(self, name, *args, **kwargs):
        self.calls.append((name, args, kwargs))
        return True

    on_user_turn_started = lambda self, *args, **kwargs: self._record("run_started", *args, **kwargs)
    on_decision_turn_started = lambda self, *args, **kwargs: self._record("turn_started", *args, **kwargs)
    on_decision_turn_finished = lambda self, *args, **kwargs: self._record("turn_finished", *args, **kwargs)
    on_llm_started = lambda self, *args, **kwargs: self._record("llm_started", *args, **kwargs)
    on_llm_finished = lambda self, *args, **kwargs: self._record("llm_finished", *args, **kwargs)
    on_tool_started = lambda self, *args, **kwargs: self._record("tool_started", *args, **kwargs)
    on_tool_finished = lambda self, *args, **kwargs: self._record("tool_finished", *args, **kwargs)
    on_final_answer = lambda self, *args, **kwargs: self._record("run_finished", *args, **kwargs)
    on_turn_failed = lambda self, *args, **kwargs: self._record("run_failed", *args, **kwargs)


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


def test_real_conversation_loop_starts_run_turn_and_finishes_with_answer(agent):
    bridge = _Bridge()
    agent._agentscope_bridge = bridge
    agent.client.chat.completions.create.return_value = _response("Done")

    result = _run(agent, "hello")

    assert result["completed"] is True
    names = [name for name, _, _ in bridge.calls]
    assert names == ["run_started", "turn_started", "llm_started", "llm_finished", "run_finished"]
    run_call = bridge.calls[0]
    assert run_call[1][1] == "Hermes user turn"
    assert run_call[2]["metadata"]["model_provider"] == agent.provider
    assert run_call[2]["metadata"]["model_name"] == agent.model
    assert run_call[2]["metadata"]["hermes.run.api_mode"] == agent.api_mode
    assert run_call[2]["metadata"]["hermes.run.tool_catalog_size"] == len(agent.tools)
    turn_call = bridge.calls[1]
    assert turn_call[2]["turn_index"] == 1
    llm_start = bridge.calls[2]
    llm_finish = bridge.calls[3]
    assert llm_start[2]["call_id"].endswith(":attempt:1")
    assert llm_start[2]["input_summary"] != "hello"
    assert llm_start[2]["attributes"]["hermes.llm.api_call_index"] == 1
    assert llm_start[2]["attributes"]["hermes.llm.streaming"] is False
    assert "provider" not in llm_finish[2]
    assert "model" not in llm_finish[2]
    assert llm_finish[2]["success"] is True
    assert llm_finish[2]["attributes"]["hermes.llm.error_category"] == "none"
    assert llm_finish[2]["duration_s"] >= 0
    final_call = bridge.calls[-1]
    assert final_call[1][-1] == "Done"
    assert final_call[2]["metrics"]["hermes.run.api_calls"] == 1
    assert final_call[2]["answer_attributes"]["hermes.answer.length"] == len("Done")


def test_real_tool_executor_records_tool_under_the_active_decision_turn(agent):
    bridge = _Bridge()
    agent._agentscope_bridge = bridge
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
    names = [name for name, _, _ in bridge.calls]
    assert names == [
        "run_started",
        "turn_started",
        "llm_started",
        "llm_finished",
        "tool_started",
        "tool_finished",
        "turn_finished",
        "turn_started",
        "llm_started",
        "llm_finished",
        "run_finished",
    ]
    tool_start = bridge.calls[4]
    tool_finish = bridge.calls[5]
    assert tool_start[1][1] == "web_search"
    assert tool_start[2]["tool_id"] == "tool-1"
    assert tool_start[2]["attributes"]["hermes.tool.execution_mode"] == "sequential"
    assert tool_finish[1][2] is None
    assert tool_finish[2]["success"] is True
    assert tool_finish[2]["attributes"]["hermes.tool.failure_category"] == "none"
    assert tool_finish[2]["duration_s"] >= 0


def test_interrupted_real_conversation_loop_fails_observability_run(agent):
    bridge = _Bridge()
    agent._agentscope_bridge = bridge
    agent._interrupt_requested = True

    result = _run(agent, "hello")

    assert result["interrupted"] is True
    names = [name for name, _, _ in bridge.calls]
    assert names == ["run_started", "run_failed"]
    assert "interrupted_by_user" in bridge.calls[-1][1][-1]


def test_invalid_provider_response_closes_llm_and_fails_native_run(agent):
    bridge = _Bridge()
    agent._agentscope_bridge = bridge
    agent._api_max_retries = 1
    agent.client.chat.completions.create.return_value = SimpleNamespace(choices=[])

    result = _run(agent, "hello")

    assert result["failed"] is True
    names = [name for name, _, _ in bridge.calls]
    assert names == ["run_started", "turn_started", "llm_started", "llm_finished", "run_failed"]
    assert bridge.calls[3][2]["success"] is False
    assert "choices" in bridge.calls[3][2]["error"]


def test_parallel_tool_batch_records_each_real_worker_under_one_run(agent):
    bridge = _Bridge()
    agent._agentscope_bridge = bridge
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
    tool_starts = [call for call in bridge.calls if call[0] == "tool_started"]
    tool_finishes = [call for call in bridge.calls if call[0] == "tool_finished"]
    assert {call[2]["tool_id"] for call in tool_starts} == {"tool-1", "tool-2"}
    assert {call[2]["tool_id"] for call in tool_finishes} == {"tool-1", "tool-2"}
    assert all(call[2]["success"] is True for call in tool_finishes)
    assert all(call[2]["attributes"]["hermes.tool.execution_mode"] == "concurrent" for call in tool_starts)
    assert all(call[2]["attributes"]["hermes.tool.batch_size"] == 2 for call in tool_finishes)
    run_key = bridge.calls[0][1][0]
    assert {call[1][0] for call in tool_starts + tool_finishes} == {run_key}
