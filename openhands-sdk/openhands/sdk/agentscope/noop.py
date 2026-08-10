"""No-op session used when AgentScope is disabled or unavailable."""

from __future__ import annotations

from typing import Any


class NoopTraceSession:
    """Same lifecycle surface as RuntimeTraceSession, with no side effects."""

    enabled = False
    run = None
    has_active_turn = False

    def __getattr__(self, _name: str):
        def _noop(*_args: Any, **_kwargs: Any) -> Any:
            return False

        return _noop
