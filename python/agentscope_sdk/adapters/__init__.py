"""Stable integration API for external Agent frameworks."""

from .base import AdapterSession
from .events import NormalizedEvent, normalize_event
from .hermes import HERMES_UPSTREAM_COMMIT, HermesAdapter
from .openhands import OPENHANDS_UPSTREAM_COMMIT, OpenHandsAdapter

__all__ = [
    "HERMES_UPSTREAM_COMMIT",
    "OPENHANDS_UPSTREAM_COMMIT",
    "AdapterSession",
    "HermesAdapter",
    "NormalizedEvent",
    "OpenHandsAdapter",
    "normalize_event",
]
