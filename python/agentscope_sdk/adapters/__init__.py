"""Stable integration API for external Agent frameworks."""

from .base import AdapterSession
from .events import NormalizedEvent, normalize_event

__all__ = ["AdapterSession", "NormalizedEvent", "normalize_event"]
