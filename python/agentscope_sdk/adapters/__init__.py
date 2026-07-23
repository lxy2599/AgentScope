"""Stable integration API for external Agent frameworks."""

from .base import AdapterSession
from .events import NormalizedEvent, normalize_event
from .hermes import HERMES_UPSTREAM_COMMIT, HermesAdapter

__all__ = ["AdapterSession", "NormalizedEvent", "normalize_event", "HermesAdapter", "HERMES_UPSTREAM_COMMIT"]
