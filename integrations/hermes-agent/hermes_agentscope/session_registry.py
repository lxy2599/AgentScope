"""Concurrent session registry keyed by Hermes session/task identity."""

from __future__ import annotations

import threading
from collections.abc import Callable
from typing import Any


class SessionRegistry:
    """Keep AgentScope sessions local to a Hermes conversation or task.

    There is deliberately no process-global "current Run": a gateway can
    execute multiple conversations at once, and every caller must supply a
    Hermes-owned key.
    """

    def __init__(self) -> None:
        self._lock = threading.RLock()
        self._sessions: dict[str, Any] = {}

    def get_or_create(self, key: str, factory: Callable[[], Any]) -> Any:
        with self._lock:
            session = self._sessions.get(key)
            if session is None:
                session = factory()
                self._sessions[key] = session
            return session

    def get(self, key: str) -> Any | None:
        with self._lock:
            return self._sessions.get(key)

    def pop(self, key: str) -> Any | None:
        with self._lock:
            return self._sessions.pop(key, None)

    def __len__(self) -> int:
        with self._lock:
            return len(self._sessions)
