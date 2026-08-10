"""Explicit per-user-task native tracing session registry."""

from __future__ import annotations

import threading
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any


@dataclass(frozen=True)
class ActiveTask:
    session_key: str
    conversation_id: str
    task_index: int


class SessionRegistry:
    """Map a conversation's active user task to one isolated SDK session."""

    def __init__(self) -> None:
        self._lock = threading.RLock()
        self._tasks: dict[str, ActiveTask] = {}
        self._sessions: dict[str, Any] = {}
        self._next_task_index: dict[str, int] = {}

    def start(self, conversation_id: str, factory: Callable[[], Any]) -> ActiveTask:
        with self._lock:
            existing = self._tasks.get(conversation_id)
            if existing is not None:
                return existing
            task_index = self._next_task_index.get(conversation_id, 0) + 1
            self._next_task_index[conversation_id] = task_index
            task = ActiveTask(
                session_key=f"{conversation_id}:task:{task_index}",
                conversation_id=conversation_id,
                task_index=task_index,
            )
            self._tasks[conversation_id] = task
            self._sessions[task.session_key] = factory()
            return task

    def active_task(self, conversation_id: str) -> ActiveTask | None:
        with self._lock:
            return self._tasks.get(conversation_id)

    def session_for(self, conversation_id: str) -> Any | None:
        with self._lock:
            task = self._tasks.get(conversation_id)
            return self._sessions.get(task.session_key) if task is not None else None

    def pop(self, conversation_id: str) -> tuple[ActiveTask | None, Any | None]:
        with self._lock:
            task = self._tasks.pop(conversation_id, None)
            return task, self._sessions.pop(
                task.session_key, None
            ) if task is not None else None

    def __len__(self) -> int:
        with self._lock:
            return len(self._tasks)
