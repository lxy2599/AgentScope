"""Process-level AgentScope presence for the owned OpenHands Agent Server.

Conversation-level tracing remains in ``openhands.sdk.agentscope``.  This
small lifecycle helper registers the interactive server itself at startup so
AgentScope can show an online OpenHands agent even before the first user task.
It is strictly fail-open and never serializes credentials or exceptions.
"""

from __future__ import annotations

import logging

from openhands.sdk.agentscope.config import load_agentscope_settings


logger = logging.getLogger(__name__)

_started = False


def start_agentscope_presence() -> bool:
    """Best-effort agent registration plus background heartbeat.

    ``agentscope_sdk.init`` is process-global; if the conversation bridge has
    already initialized it, this function simply reuses the existing runtime.
    """

    global _started
    if _started:
        return True
    settings = load_agentscope_settings()
    if not settings.enabled:
        return False
    try:
        from agentscope_sdk import get_runtime, init

        if get_runtime() is None:
            init(
                settings.server_url,
                settings.project_token,
                "openhands-agent",
                framework="openhands",
                runtime="python",
                source="agent-server",
                environment=settings.environment,
                heartbeat_interval=30,
            )
        _started = True
        return True
    except Exception:
        logger.debug(
            "AgentScope presence startup failed; continuing without observability",
            exc_info=True,
        )
        return False


def stop_agentscope_presence() -> None:
    """Stop the runtime heartbeat only when this process started it."""

    global _started
    if not _started:
        return
    try:
        from agentscope_sdk import shutdown_runtime

        shutdown_runtime()
    except Exception:
        logger.debug("AgentScope presence shutdown failed", exc_info=True)
    finally:
        _started = False


def reset_agentscope_presence_for_tests() -> None:
    """Reset only local lifecycle state; tests mock the SDK boundary."""

    global _started
    _started = False
