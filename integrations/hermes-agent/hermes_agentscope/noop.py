"""No-op tracing objects used when AgentScope is unavailable."""

from __future__ import annotations

from typing import Any


class NoopTraceSession:
    """Match the bridge-facing session API without changing Hermes behavior."""

    enabled = False
    run = None
    has_active_turn = False

    def start_run(self, *args: Any, **kwargs: Any) -> None:
        return None

    def start_turn(self, *args: Any, **kwargs: Any) -> None:
        return None

    def finish_turn(self, *args: Any, **kwargs: Any) -> bool:
        return False

    def start_llm(self, *args: Any, **kwargs: Any) -> None:
        return None

    def finish_llm(self, *args: Any, **kwargs: Any) -> None:
        return None

    def record_llm(self, *args: Any, **kwargs: Any) -> None:
        return None

    def start_tool(self, *args: Any, **kwargs: Any) -> None:
        return None

    def finish_tool(self, *args: Any, **kwargs: Any) -> None:
        return None

    def record_tool(self, *args: Any, **kwargs: Any) -> None:
        return None

    def record_final_answer(self, *args: Any, **kwargs: Any) -> bool:
        return False

    def update_run_status(self, *args: Any, **kwargs: Any) -> bool:
        return False

    def finish_run(self, *args: Any, **kwargs: Any) -> bool:
        return False

    def fail_run(self, *args: Any, **kwargs: Any) -> bool:
        return False
