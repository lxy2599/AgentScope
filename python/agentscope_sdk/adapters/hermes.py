"""Non-invasive AgentScope instrumentation for NousResearch Hermes Agent."""

from __future__ import annotations

import logging
import json
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
        self._observer_installed = False

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
        self._observer_installed = self._install_builtin_observer_hooks()
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
        if self._observer_installed:
            return
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
        if self._observer_installed:
            return
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

    def register_observer_hooks(self, context: Any) -> None:
        """Register against Hermes' documented read-only observer contract."""
        context.register_hook("pre_api_request", self._observer_pre_api_request)
        context.register_hook("post_api_request", self._observer_post_api_request)
        context.register_hook("api_request_error", self._observer_api_request_error)
        context.register_hook("pre_tool_call", self._observer_pre_tool_call)
        context.register_hook("post_tool_call", self._observer_post_tool_call)
        context.register_hook("pre_approval_request", self._observer_pre_approval_request)
        context.register_hook("post_approval_response", self._observer_post_approval_response)

    def _install_builtin_observer_hooks(self) -> bool:
        """Install the observer through Hermes' plugin context when available."""
        if self._observer_installed:
            return True
        try:
            from hermes_cli.plugins import PluginContext, PluginManifest, get_plugin_manager

            context = PluginContext(
                PluginManifest(name="agentscope-observability", version="1", provides_hooks=[]),
                get_plugin_manager(),
            )
            self.register_observer_hooks(context)
        except (ImportError, AttributeError):
            return False
        self._observer_installed = True
        return True

    def _observer_pre_api_request(self, **kwargs: Any) -> None:
        if self.session.run is None:
            return
        request_id = str(kwargs.get("api_request_id") or kwargs.get("api_call_count") or "unknown")
        request = kwargs.get("request") or {}
        self.session.on_llm_start(
            f"hermes.api.{request_id}",
            call_id=request_id,
            input_summary=self._payload_summary(request),
            provider=str(kwargs.get("provider") or ""),
            model=str(kwargs.get("model") or ""),
        )

    def _observer_post_api_request(self, **kwargs: Any) -> None:
        if self.session.run is None:
            return
        request_id = str(kwargs.get("api_request_id") or kwargs.get("api_call_count") or "unknown")
        usage = kwargs.get("usage") or {}
        self.session.on_llm_finish(
            f"hermes.api.{request_id}",
            call_id=request_id,
            output_summary=self._payload_summary(kwargs.get("response") or {}),
            input_tokens=self._token_count(usage, "prompt_tokens", "input_tokens"),
            output_tokens=self._token_count(usage, "completion_tokens", "output_tokens"),
        )

    def _observer_api_request_error(self, **kwargs: Any) -> None:
        if self.session.run is None:
            return
        request_id = str(kwargs.get("api_request_id") or kwargs.get("api_call_count") or "unknown")
        error = kwargs.get("error") or kwargs.get("reason") or "Hermes API request failed"
        self.session.on_llm_finish(f"hermes.api.{request_id}", call_id=request_id, success=False, error=self._payload_summary(error))

    def _observer_pre_tool_call(self, **kwargs: Any) -> None:
        if self.session.run is None:
            return
        name = str(kwargs.get("tool_name") or "unknown")
        self.session.on_tool_start(name, kwargs.get("args"), tool_id=str(kwargs.get("tool_call_id") or name), tool_type="hermes")

    def _observer_post_tool_call(self, **kwargs: Any) -> None:
        if self.session.run is None:
            return
        name = str(kwargs.get("tool_name") or "unknown")
        status = str(kwargs.get("status") or "ok")
        success = status == "ok"
        error = str(kwargs.get("error_message") or kwargs.get("error_type") or "")
        args = kwargs.get("args")
        result = kwargs.get("result")
        self.session.on_tool_finish(
            name,
            result,
            tool_id=str(kwargs.get("tool_call_id") or name),
            success=success,
            error=error,
            duration_s=(float(kwargs["duration_ms"]) / 1000) if kwargs.get("duration_ms") is not None else None,
        )
        self._record_tool_semantics(name, args, result, success)
        if status == "blocked":
            self.session.on_guardrail("tool_policy", error or "tool blocked", arguments={"tool": name, "status": status}, blocked=True, error=error)

    def _observer_pre_approval_request(self, **kwargs: Any) -> None:
        if self.session.run is not None:
            self.session.on_guardrail("approval_request", "approval required", arguments=kwargs, blocked=False)

    def _observer_post_approval_response(self, **kwargs: Any) -> None:
        if self.session.run is not None:
            choice = str(kwargs.get("choice") or "")
            self.session.on_guardrail("approval_response", choice, arguments=kwargs, blocked=choice in {"deny", "timeout"})

    @staticmethod
    def _payload_summary(value: Any) -> str:
        try:
            return json.dumps(value, ensure_ascii=False, sort_keys=True)[:4096]
        except (TypeError, ValueError):
            return str(value)[:4096]

    @staticmethod
    def _token_count(usage: Any, *keys: str) -> int | None:
        if not isinstance(usage, dict):
            return None
        for key in keys:
            value = usage.get(key)
            if isinstance(value, int):
                return value
        return None

    def _finish_llm(self, output: str, *, success: bool = True, error: str = "") -> None:
        if self._llm_call_name:
            self.session.on_llm_finish(self._llm_call_name, output_summary=output, success=success, error=error)
            self._llm_call_name = ""

    def _final_output(self, result: Any) -> str:
        if isinstance(result, dict):
            return str(result.get("final_response") or result.get("response") or "".join(self._streamed_output))
        return str(result or "".join(self._streamed_output))
