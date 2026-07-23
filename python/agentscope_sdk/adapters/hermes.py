"""Non-invasive AgentScope instrumentation for NousResearch Hermes Agent."""

from __future__ import annotations

import logging
from typing import Any, Callable

from .base import AdapterSession

HERMES_UPSTREAM_COMMIT = "d9165d7a678d4105f42921a7fc1886df3804531b"

_logger = logging.getLogger(__name__)


class HermesAdapter:
    """Bind AgentScope semantic tracing to Hermes' documented agent callbacks.

    This does not import or patch Hermes. Any object exposing the callback
    attributes and ``run_conversation`` method can be bound, keeping the SDK
    dependency optional and keeping compatibility failures local to this
    adapter.
    """

    def __init__(self, session: AdapterSession, *, capture_reasoning: bool = False) -> None:
        self.session = session
        self.capture_reasoning = capture_reasoning
        self._llm_call_name = ""
        self._streamed_output: list[str] = []

    def bind(self, agent: Any) -> Any:
        """Attach callbacks while preserving callbacks already installed by Hermes hosts."""
        self.session.metadata.setdefault("adapter_name", "hermes")
        self.session.metadata.setdefault("adapter_version", "1")
        self.session.metadata.setdefault("framework", "hermes")
        self.session.metadata.setdefault("hermes.upstream_commit", HERMES_UPSTREAM_COMMIT)

        self._chain(agent, "tool_progress_callback", self._on_tool_progress)
        self._chain(agent, "reasoning_callback", self._on_reasoning)
        self._chain(agent, "step_callback", self._on_step)
        self._chain(agent, "stream_delta_callback", self._on_stream_delta)
        self._chain(agent, "event_callback", self._on_event)
        return agent

    def run(self, agent: Any, prompt: str, *, task_id: str = "", metadata: dict[str, Any] | None = None) -> str:
        """Run one real Hermes turn and mirror its lifecycle into AgentScope."""
        self.bind(agent)
        session_id = str(getattr(agent, "session_id", "") or "")
        provider = str(getattr(agent, "provider", "") or "")
        model = str(getattr(agent, "model", "") or "")
        self.session.metadata.update({"model_provider": provider, "model_name": model})
        self.session.start_run(prompt, session_id=session_id, conversation_id=session_id, task_id=task_id, metadata=metadata)
        try:
            result = agent.run_conversation(prompt, task_id=task_id or None)
            output = self._final_output(result)
            self._finish_llm(output)
            self.session.finish_run(output)
            return output
        except Exception as exc:
            self._finish_llm("", success=False, error=str(exc))
            self.session.fail_run(str(exc), attributes={"framework": "hermes"})
            raise

    def _chain(self, agent: Any, attribute: str, observer: Callable[..., None]) -> None:
        previous = getattr(agent, attribute, None)

        def callback(*args: Any, **kwargs: Any) -> None:
            try:
                observer(agent, *args, **kwargs)
            except Exception:
                _logger.debug("AgentScope Hermes callback failed: %s", attribute, exc_info=True)
            if previous is not None:
                previous(*args, **kwargs)

        setattr(agent, attribute, callback)

    def _on_step(self, agent: Any, api_call_count: int, prev_tools: Any = None) -> None:
        self._finish_llm("\n".join(self._streamed_output))
        self._streamed_output.clear()
        self._llm_call_name = f"hermes.call.{api_call_count}"
        previous_summary = "" if not prev_tools else f"previous tools: {prev_tools}"
        self.session.on_llm_start(
            self._llm_call_name,
            input_summary=previous_summary,
            provider=str(getattr(agent, "provider", "") or ""),
            model=str(getattr(agent, "model", "") or ""),
        )

    def _on_reasoning(self, agent: Any, reasoning: str) -> None:
        if reasoning:
            self.session.on_thought("reasoning", reasoning, capture_content=self.capture_reasoning)

    def _on_stream_delta(self, agent: Any, delta: str | None) -> None:
        if delta:
            self._streamed_output.append(str(delta))

    def _on_tool_progress(self, agent: Any, event_type: str, name: str | None = None, preview: str | None = None, args: Any = None, **kwargs: Any) -> None:
        tool_name = str(name or "unknown")
        if event_type == "tool.started":
            self.session.on_tool_start(tool_name, args, tool_id=str(kwargs.get("tool_call_id") or tool_name), tool_type="hermes")
            return
        if event_type == "tool.completed":
            success = not bool(kwargs.get("is_error", False))
            result = kwargs.get("result")
            tool_id = str(kwargs.get("tool_call_id") or tool_name)
            self.session.on_tool_finish(tool_name, result, tool_id=tool_id, success=success, error="tool failed" if not success else "", duration_s=kwargs.get("duration"))
            self._record_tool_semantics(tool_name, args, result, success)
            return
        if event_type == "tool.output_risk":
            risk = kwargs.get("risk_metadata") or {}
            level = str(risk.get("risk") or "unknown") if isinstance(risk, dict) else "unknown"
            self.session.on_guardrail("tool_output_risk", level, arguments={"tool": tool_name, "risk": risk}, blocked=False)

    def _record_tool_semantics(self, name: str, args: Any, result: Any, success: bool) -> None:
        if name == "memory":
            action = str((args or {}).get("action", "access")) if isinstance(args, dict) else "access"
            target = str((args or {}).get("target", "memory")) if isinstance(args, dict) else "memory"
            if action in {"write", "set", "append", "delete", "forget"}:
                self.session.on_memory_write(target, result, arguments=args, success=success)
            else:
                self.session.on_memory_read(target, result, arguments=args, success=success)
        elif name in {"skill_view", "skills_list", "skill_manage"}:
            skill_name = str((args or {}).get("name") or name) if isinstance(args, dict) else name
            self.session.on_skill_call(skill_name, result, arguments=args, success=success)

    def _on_event(self, agent: Any, event_type: str, context: dict[str, Any]) -> None:
        if event_type == "session:compress":
            self.session.on_thought("context_compression", "Hermes compressed the session context", capture_content=False)

    def _finish_llm(self, output: str, *, success: bool = True, error: str = "") -> None:
        if self._llm_call_name:
            self.session.on_llm_finish(self._llm_call_name, output_summary=output, success=success, error=error)
            self._llm_call_name = ""

    def _final_output(self, result: Any) -> str:
        if isinstance(result, dict):
            return str(result.get("final_response") or result.get("response") or "".join(self._streamed_output))
        return str(result or "".join(self._streamed_output))
