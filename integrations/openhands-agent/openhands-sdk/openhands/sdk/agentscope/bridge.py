"""Thin, optional native bridge from OpenHands execution points to AgentScope."""

from __future__ import annotations

import hashlib
import logging
import threading
from collections.abc import Callable
from typing import Any

from .config import AgentScopeSettings, load_agentscope_settings
from .noop import NoopTraceSession
from .session_registry import SessionRegistry


_logger = logging.getLogger(__name__)
_OPENHANDS_UPSTREAM_COMMIT = "a375c66aaf29c8d3b6553c9164e65ed917149e81"


class AgentScopeBridge:
    """Fail-open native bridge, called only from real OpenHands execution points.

    The bridge owns neither OpenHands scheduling nor its exception behaviour. SDK
    imports are lazy, so a normal OpenHands installation has no AgentScope import
    or network activity unless observability is explicitly enabled.
    """

    def __init__(
        self,
        *,
        settings: AgentScopeSettings | None = None,
        agent_name: str = "openhands-agent",
        agent_version: str = "",
        session_factory: Callable[[], Any] | None = None,
    ) -> None:
        self.settings = settings or load_agentscope_settings()
        self.agent_name = agent_name
        self.agent_version = agent_version
        self._session_factory = session_factory
        self._registry = SessionRegistry()
        self._completed_run_ids: dict[str, str] = {}

    @property
    def enabled(self) -> bool:
        return self.settings.enabled

    @property
    def active_session_count(self) -> int:
        return len(self._registry)

    def last_run_id(self, conversation_id: str) -> str:
        """Return the last native AgentScope Run ID for diagnostics/E2E only."""
        return self._completed_run_ids.get(conversation_id, "")

    def on_run_started(
        self,
        conversation_id: str,
        input_text: str,
        *,
        task_id: str = "",
        metadata: dict[str, Any] | None = None,
    ) -> str:
        """Create one Run for the user task currently consumed by Conversation."""
        if not conversation_id or not self.enabled:
            return ""
        existing = self._registry.active_task(conversation_id)
        task = existing or self._registry.start(conversation_id, self._create_session)
        session = self._session_for(conversation_id)
        if existing is None:
            self._safe(
                session.start_run,
                self._content(input_text),
                session_id=conversation_id,
                conversation_id=conversation_id,
                task_id=task_id or str(task.task_index),
                metadata={
                    "framework": "openhands",
                    "integration": "native",
                    "openhands.upstream_commit": _OPENHANDS_UPSTREAM_COMMIT,
                    "openhands.task_index": task.task_index,
                    "agentscope.capture_level": self.settings.capture_level,
                    **(metadata or {}),
                },
            )
        self._safe(session.update_run_status, "running")
        run = getattr(session, "run", None)
        run_id = str(getattr(run, "id", ""))
        if run_id:
            self._completed_run_ids[conversation_id] = run_id
        return task.session_key

    def on_turn_started(
        self, conversation_id: str, *, turn_id: str = "", turn_index: int | None = None
    ) -> None:
        self._safe(
            self._session_for(conversation_id).start_turn,
            turn_id,
            turn_index=turn_index,
        )

    def on_turn_finished(
        self,
        conversation_id: str,
        outcome: str = "continue",
        *,
        error: str = "",
        attributes: dict[str, Any] | None = None,
    ) -> None:
        self._safe(
            self._session_for(conversation_id).finish_turn,
            outcome,
            error=self._content(error),
            attributes=attributes,
        )

    def on_llm_started(self, conversation_id: str, name: str, **kwargs: Any) -> None:
        kwargs["input_summary"] = self._content(kwargs.get("input_summary", ""))
        self._safe(self._session_for(conversation_id).start_llm, name, **kwargs)

    def on_llm_finished(self, conversation_id: str, name: str, **kwargs: Any) -> None:
        kwargs["output_summary"] = self._content(kwargs.get("output_summary", ""))
        kwargs["error"] = self._content(kwargs.get("error", ""))
        self._safe(self._session_for(conversation_id).finish_llm, name, **kwargs)

    def on_tool_started(
        self, conversation_id: str, name: str, arguments: Any = None, **kwargs: Any
    ) -> None:
        self._safe(
            self._session_for(conversation_id).start_tool,
            name,
            self._content(arguments),
            **kwargs,
        )

    def on_tool_finished(
        self, conversation_id: str, name: str, result: Any = None, **kwargs: Any
    ) -> None:
        kwargs["error"] = self._content(kwargs.get("error", ""))
        self._safe(
            self._session_for(conversation_id).finish_tool,
            name,
            self._content(result),
            **kwargs,
        )

    def on_semantic_event(
        self, conversation_id: str, kind: str, name: str, **kwargs: Any
    ) -> None:
        # Reasoning is opt-in at L2 and callers must pass only user-visible text.
        if kind == "THOUGHT" and self.settings.capture_level != "L2":
            return
        self._safe(
            self._session_for(conversation_id).record_semantic_event,
            kind=kind,
            name=name,
            input_summary=self._content(kwargs.pop("input_summary", "")),
            output_summary=self._content(kwargs.pop("output_summary", "")),
            **kwargs,
        )

    def on_final_answer(
        self,
        conversation_id: str,
        output: str,
        *,
        attributes: dict[str, Any] | None = None,
    ) -> None:
        _task, session = self._registry.pop(conversation_id)
        session = session or NoopTraceSession()
        safe_output = self._content(output)
        self._safe(session.record_final_answer, safe_output, attributes=attributes)
        self._safe(session.finish_turn, "final")
        self._safe(session.finish_run, safe_output)

    def on_run_failed(
        self,
        conversation_id: str,
        error: str,
        *,
        attributes: dict[str, Any] | None = None,
    ) -> None:
        _task, session = self._registry.pop(conversation_id)
        session = session or NoopTraceSession()
        self._safe(session.finish_turn, "error", error=self._content(error))
        self._safe(session.fail_run, self._content(error), attributes=attributes)

    def on_run_waiting(
        self, conversation_id: str, status: str = "waiting_user"
    ) -> None:
        self._safe(self._session_for(conversation_id).update_run_status, status)
        self._safe(self._session_for(conversation_id).finish_turn, status)

    def _session_for(self, conversation_id: str) -> Any:
        if not self.enabled:
            return NoopTraceSession()
        return self._registry.session_for(conversation_id) or NoopTraceSession()

    def _create_session(self) -> Any:
        if self._session_factory is not None:
            return self._safe(self._session_factory, default=NoopTraceSession())
        try:
            from agentscope_sdk import (
                RuntimeTraceSession,
                configure_tracing,
                get_runtime,
                init,
            )

            tracer = configure_tracing(
                self.settings.server_url,
                self.settings.project_token,
                service_name=self.agent_name,
                capture_content=self.settings.capture_level != "L0",
                enable_redaction=True,
            )
            runtime = get_runtime()
            if runtime is None:
                runtime = init(
                    self.settings.server_url,
                    self.settings.project_token,
                    self.agent_name,
                    agent_version=self.agent_version,
                    framework="openhands",
                    runtime="python",
                    source="sdk",
                    environment=self.settings.environment,
                )
            return RuntimeTraceSession(
                runtime,
                tracer,
                metadata={"framework": "openhands", "integration": "native"},
                flush_on_run_finish=False,
                best_effort=True,
            )
        except Exception:
            _logger.debug(
                "AgentScope bridge startup failed; "
                "observability disabled for this task",
                exc_info=True,
            )
            return NoopTraceSession()

    def _content(self, value: Any) -> Any:
        if self.settings.capture_level != "L0":
            return value
        if value is None:
            return ""
        text = value if isinstance(value, str) else repr(value)
        digest = hashlib.sha256(text.encode("utf-8", errors="replace")).hexdigest()
        return f"[redacted length={len(text)} sha256={digest}]"

    @staticmethod
    def _safe(
        callback: Callable[..., Any], *args: Any, default: Any = None, **kwargs: Any
    ) -> Any:
        try:
            return callback(*args, **kwargs)
        except Exception:
            _logger.debug("AgentScope bridge callback failed", exc_info=True)
            return default


_default_bridge: AgentScopeBridge | None = None
_default_bridge_lock = threading.Lock()


def get_agentscope_bridge() -> AgentScopeBridge:
    """Return the process-wide optional bridge without importing AgentScope SDK."""
    global _default_bridge
    if _default_bridge is None:
        with _default_bridge_lock:
            if _default_bridge is None:
                _default_bridge = AgentScopeBridge()
    return _default_bridge


def reset_agentscope_bridge_for_tests() -> None:
    """Reset the lazily-created bridge; intentionally only for fork regressions."""
    global _default_bridge
    with _default_bridge_lock:
        _default_bridge = None
