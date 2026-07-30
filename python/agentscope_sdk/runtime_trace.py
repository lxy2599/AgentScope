"""Framework-neutral, fail-open Run/Turn tracing session.

``RuntimeTraceSession`` is the stable surface for instrumenting a framework at
its real execution points.  It intentionally keeps AgentScope failures out of
the host agent's control flow: when setup or an individual telemetry operation
fails, the affected session becomes a no-op session instead of raising into the
agent loop.
"""

from __future__ import annotations

import logging
from typing import Any

from opentelemetry import trace

from agentscope_sdk.adapters.base import AdapterSession
from agentscope_sdk.runtime import RuntimeRun

_logger = logging.getLogger(__name__)


class RuntimeTraceSession:
    """Own a single framework Run and its explicit Turn hierarchy.

    This public API is suitable for native framework integrations such as the
    controlled Hermes fork.  It delegates semantic span construction,
    redaction, OTLP export, and runtime lifecycle calls to the existing SDK
    implementation, while adding three guarantees needed by long-lived hosts:

    * every method is best-effort and fail-open by default;
    * each session uses an explicit RuntimeRun handle, rather than Runtime's
      legacy implicit current-run state;
    * run completion can avoid synchronous trace flushing.
    """

    def __init__(
        self,
        runtime: Any,
        tracer: trace.Tracer,
        *,
        metadata: dict[str, Any] | None = None,
        flush_on_run_finish: bool = False,
        best_effort: bool = True,
    ) -> None:
        self._best_effort = best_effort
        self._delegate = AdapterSession(
            runtime,
            tracer,
            metadata=metadata,
            flush_on_run_finish=flush_on_run_finish,
        )
        self._disabled = False
        self.last_error: Exception | None = None

    @property
    def enabled(self) -> bool:
        """Whether this session can still emit lifecycle and trace data."""
        return not self._disabled

    @property
    def run(self) -> RuntimeRun | None:
        """The server-backed Run, or ``None`` for an inactive/no-op session."""
        return self._delegate.run

    @property
    def has_active_turn(self) -> bool:
        return self.enabled and self._delegate.has_active_turn

    def start_run(self, input: str, **kwargs: Any) -> RuntimeRun | None:
        result = self._call(None, self._delegate.start_run, input, **kwargs)
        return result if isinstance(result, RuntimeRun) else None

    def start_turn(
        self,
        turn_id: str = "",
        *,
        turn_index: int | None = None,
        attributes: dict[str, Any] | None = None,
    ) -> tuple[str, int] | None:
        result = self._call(
            None,
            self._delegate.start_turn,
            turn_id,
            turn_index=turn_index,
            attributes=attributes,
        )
        return result if isinstance(result, tuple) else None

    def finish_turn(self, outcome: str = "continue", **kwargs: Any) -> bool:
        return bool(self._call(False, self._delegate.finish_turn, outcome, **kwargs))

    def start_llm(self, name: str, **kwargs: Any) -> None:
        self._call(None, self._delegate.on_llm_start, name, **kwargs)

    def finish_llm(self, name: str, **kwargs: Any) -> None:
        self._call(None, self._delegate.on_llm_finish, name, **kwargs)

    def record_llm(self, name: str, **kwargs: Any) -> None:
        self._call(None, self._delegate.on_llm_call, name, **kwargs)

    def start_tool(self, name: str, arguments: Any = None, **kwargs: Any) -> None:
        self._call(None, self._delegate.on_tool_start, name, arguments, **kwargs)

    def finish_tool(self, name: str, result: Any = None, **kwargs: Any) -> None:
        self._call(None, self._delegate.on_tool_finish, name, result, **kwargs)

    def record_tool(self, name: str, arguments: Any = None, result: Any = None, **kwargs: Any) -> None:
        """Record a synchronous Tool call using the paired lifecycle API."""
        tool_id = str(kwargs.pop("tool_id", name))
        start_kwargs = {
            key: kwargs.pop(key)
            for key in ("tool_type", "side_effect_risk")
            if key in kwargs
        }
        self.start_tool(name, arguments, tool_id=tool_id, **start_kwargs)
        self.finish_tool(name, result, tool_id=tool_id, **kwargs)

    def record_final_answer(
        self,
        output: str,
        *,
        attributes: dict[str, Any] | None = None,
    ) -> bool:
        return bool(
            self._call(
                False,
                self._delegate.record_final_answer,
                output,
                attributes=attributes,
            )
        )

    def update_run_status(self, status: str, **kwargs: Any) -> bool:
        return bool(self._call(False, self._delegate.on_state_update, status, **kwargs))

    def finish_run(self, output: str, **kwargs: Any) -> bool:
        return bool(self._call(False, self._delegate.finish_run, output, **kwargs))

    def fail_run(self, error: str, **kwargs: Any) -> bool:
        return bool(self._call(False, self._delegate.fail_run, error, **kwargs))

    def _call(self, default: Any, callback: Any, *args: Any, **kwargs: Any) -> Any:
        if self._disabled:
            return default
        try:
            return callback(*args, **kwargs)
        except Exception as exc:
            self.last_error = exc
            if not self._best_effort:
                raise
            self._disabled = True
            _logger.debug("AgentScope trace session disabled after telemetry failure", exc_info=True)
            return default
