"""Thin native bridge from Hermes execution points to AgentScope SDK sessions."""

from __future__ import annotations

import logging
from collections.abc import Callable
from typing import Any

from .config import AgentScopeSettings, load_agentscope_settings
from .noop import NoopTraceSession
from .session_registry import SessionRegistry

_logger = logging.getLogger(__name__)


class AgentScopeBridge:
    """Fail-open bridge used only by native Hermes execution points.

    The bridge does not alter Hermes scheduling, callbacks, LLM transport, or
    tool dispatch.  It only translates already-observed lifecycle transitions
    into the public ``agentscope_sdk.RuntimeTraceSession`` API.  SDK imports
    are lazy so a normal Hermes installation has no AgentScope dependency.
    """

    def __init__(
        self,
        *,
        settings: AgentScopeSettings | None = None,
        agent_name: str = "hermes-agent",
        agent_version: str = "",
        session_factory: Callable[[], Any] | None = None,
    ) -> None:
        self.settings = settings or load_agentscope_settings()
        self.agent_name = agent_name
        self.agent_version = agent_version
        self._session_factory = session_factory
        self._registry = SessionRegistry()

    @property
    def enabled(self) -> bool:
        return self.settings.enabled

    @property
    def active_session_count(self) -> int:
        return len(self._registry)

    def on_user_turn_started(
        self,
        session_key: str,
        input_text: str,
        *,
        session_id: str = "",
        conversation_id: str = "",
        task_id: str = "",
        metadata: dict[str, Any] | None = None,
    ) -> Any:
        """Create a Run at Hermes' real user-input entry point."""
        session = self._session_for(session_key)
        self._safe(
            session.start_run,
            input_text,
            session_id=session_id,
            conversation_id=conversation_id,
            task_id=task_id,
            metadata={"framework": "hermes", "integration": "native", **(metadata or {})},
        )
        return session

    def on_decision_turn_started(
        self,
        session_key: str,
        turn_id: str = "",
        *,
        turn_index: int | None = None,
        attributes: dict[str, Any] | None = None,
    ) -> Any:
        """Start an explicit Hermes decision-loop Turn under the current Run."""
        session = self._session_for(session_key)
        return self._safe(
            session.start_turn,
            turn_id,
            turn_index=turn_index,
            attributes=attributes,
        )

    def on_decision_turn_finished(
        self,
        session_key: str,
        outcome: str = "continue",
        *,
        error: str = "",
        attributes: dict[str, Any] | None = None,
    ) -> bool:
        session = self._session_for(session_key)
        return bool(
            self._safe(
                session.finish_turn,
                outcome,
                error=error,
                attributes=attributes,
                default=False,
            )
        )

    def trace_llm(self, session_key: str, name: str, **kwargs: Any) -> None:
        """Record a completed LLM call at the actual Hermes transport boundary."""
        self._safe(self._session_for(session_key).record_llm, name, **kwargs)

    def on_llm_started(self, session_key: str, name: str, **kwargs: Any) -> None:
        self._safe(self._session_for(session_key).start_llm, name, **kwargs)

    def on_llm_finished(self, session_key: str, name: str, **kwargs: Any) -> None:
        self._safe(self._session_for(session_key).finish_llm, name, **kwargs)

    def trace_tool(self, session_key: str, name: str, arguments: Any = None, result: Any = None, **kwargs: Any) -> None:
        """Record a completed Tool call at the actual Hermes dispatch boundary."""
        self._safe(self._session_for(session_key).record_tool, name, arguments, result, **kwargs)

    def on_tool_started(self, session_key: str, name: str, arguments: Any = None, **kwargs: Any) -> None:
        self._safe(self._session_for(session_key).start_tool, name, arguments, **kwargs)

    def on_tool_finished(self, session_key: str, name: str, result: Any = None, **kwargs: Any) -> None:
        self._safe(self._session_for(session_key).finish_tool, name, result, **kwargs)

    def on_final_answer(
        self,
        session_key: str,
        output: str,
        *,
        answer_attributes: dict[str, Any] | None = None,
        turn_attributes: dict[str, Any] | None = None,
        metrics: dict[str, Any] | None = None,
    ) -> bool:
        """Attach the final answer to the active Turn and finish its Run."""
        session = self._registry.pop(session_key) or NoopTraceSession()
        self._safe(session.record_final_answer, output, attributes=answer_attributes)
        self._safe(session.finish_turn, "final", attributes=turn_attributes)
        return bool(self._safe(session.finish_run, output, metrics=metrics, default=False))

    def on_turn_failed(
        self,
        session_key: str,
        error: str,
        *,
        attributes: dict[str, Any] | None = None,
        turn_attributes: dict[str, Any] | None = None,
    ) -> bool:
        """Close an active Turn and fail the associated Run without masking Hermes errors."""
        session = self._registry.pop(session_key) or NoopTraceSession()
        self._safe(session.finish_turn, "error", error=error, attributes=turn_attributes)
        return bool(self._safe(session.fail_run, error, attributes=attributes, default=False))

    def _session_for(self, session_key: str) -> Any:
        if not session_key or not self.enabled:
            return NoopTraceSession()
        return self._registry.get_or_create(session_key, self._create_session)

    def _create_session(self) -> Any:
        if self._session_factory is not None:
            return self._safe(self._session_factory, default=NoopTraceSession())
        try:
            from agentscope_sdk import RuntimeTraceSession, configure_tracing, get_runtime, init

            tracer = configure_tracing(
                self.settings.server_url,
                self.settings.project_token,
                service_name="hermes-agent",
                capture_content=False,
            )
            runtime = get_runtime()
            if runtime is None:
                runtime = init(
                    self.settings.server_url,
                    self.settings.project_token,
                    self.agent_name,
                    agent_version=self.agent_version,
                    framework="hermes",
                    runtime="python",
                    source="sdk",
                    environment=self.settings.environment,
                )
            return RuntimeTraceSession(
                runtime,
                tracer,
                metadata={"framework": "hermes", "integration": "native"},
                flush_on_run_finish=False,
                best_effort=True,
            )
        except Exception:
            _logger.debug("AgentScope bridge startup failed; observability disabled for this session", exc_info=True)
            return NoopTraceSession()

    @staticmethod
    def _safe(callback: Callable[..., Any], *args: Any, default: Any = None, **kwargs: Any) -> Any:
        try:
            return callback(*args, **kwargs)
        except Exception:
            _logger.debug("AgentScope bridge callback failed", exc_info=True)
            return default
