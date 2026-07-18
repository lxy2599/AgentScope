"""Agent Runtime — Agent registration, heartbeat, and lifecycle management."""

from __future__ import annotations

import logging
import threading
import time
import uuid
from dataclasses import dataclass
from typing import Any

from agentscope_sdk.client import AgentClient, AgentInfo

logger = logging.getLogger(__name__)

# ---- Runtime state ----

_runtime: Runtime | None = None
_lock = threading.Lock()


@dataclass
class Runtime:
    """Runtime context for a registered agent.

    Attributes:
        agent_id: The server-assigned agent ID.
        agent_name: The agent name used during registration.
        instance_id: Unique identifier for this runtime instance.
        client: The AgentClient used for HTTP communication.
        _heartbeat_thread: Background thread for periodic heartbeats.
        _stop_event: Event to signal the heartbeat thread to stop.
    """

    agent_id: str
    agent_name: str
    instance_id: str
    client: AgentClient

    _heartbeat_thread: threading.Thread | None = None
    _stop_event: threading.Event | None = None

    def _start_heartbeat(self, interval: float) -> None:
        """Start a background thread that sends heartbeats periodically."""
        self._stop_event = threading.Event()
        self._heartbeat_thread = threading.Thread(
            target=_heartbeat_loop,
            args=(self, interval),
            daemon=True,
            name=f"agentscope-hb-{self.agent_name}",
        )
        self._heartbeat_thread.start()

    def _stop_heartbeat(self) -> None:
        """Stop the heartbeat thread and wait for it to finish."""
        if self._stop_event is not None:
            self._stop_event.set()
        if self._heartbeat_thread is not None and self._heartbeat_thread.is_alive():
            self._heartbeat_thread.join(timeout=5)
        self._heartbeat_thread = None
        self._stop_event = None

    def send_heartbeat(self, status: str = "online", payload: dict[str, Any] | None = None) -> None:
        """Send a single heartbeat for this runtime instance."""
        self.client.send_heartbeat(
            agent_id=self.agent_id,
            instance_id=self.instance_id,
            status=status,
            payload=payload or {},
        )


def _heartbeat_loop(rt: Runtime, interval: float) -> None:
    """Background loop that sends heartbeats at a fixed interval."""
    while not rt._stop_event.is_set():
        try:
            rt.send_heartbeat()
        except Exception:
            logger.warning("Heartbeat failed for agent %s/%s", rt.agent_name, rt.instance_id, exc_info=True)
        rt._stop_event.wait(interval)


def init(
    server_url: str,
    project_token: str,
    agent_name: str,
    *,
    description: str = "",
    metadata: dict[str, Any] | None = None,
    instance_id: str | None = None,
    heartbeat_interval: float = 30.0,
) -> Runtime:
    """Initialize the AgentScope runtime for the current process.

    This registers the agent with the control center and starts a background
    heartbeat thread.

    Args:
        server_url: Base URL of the AgentScope server.
        project_token: Project token for authentication.
        agent_name: Name of the agent to register.
        description: Optional agent description.
        metadata: Optional agent metadata.
        instance_id: Unique instance ID. Auto-generated if not provided.
        heartbeat_interval: Seconds between heartbeats (default 30).

    Returns:
        The initialized Runtime instance.

    Raises:
        RuntimeError: If init has already been called in this process.
    """
    global _runtime

    with _lock:
        if _runtime is not None:
            raise RuntimeError(
                f"Runtime already initialized for agent '{_runtime.agent_name}'. "
                "Call shutdown_runtime() first."
            )

        client = AgentClient(server_url=server_url, project_token=project_token)

        # Register agent
        agent_info: AgentInfo = client.register_agent(
            name=agent_name,
            description=description,
            metadata=metadata or {},
        )

        # Create runtime
        resolved_instance_id = instance_id or f"instance-{uuid.uuid4().hex[:12]}"
        rt = Runtime(
            agent_id=agent_info.id,
            agent_name=agent_name,
            instance_id=resolved_instance_id,
            client=client,
        )

        # Send initial heartbeat to register the instance
        rt.send_heartbeat()

        # Start background heartbeat
        rt._start_heartbeat(heartbeat_interval)

        _runtime = rt
        logger.info(
            "Agent runtime initialized: %s (id=%s, instance=%s)",
            agent_name,
            agent_info.id,
            resolved_instance_id,
        )
        return rt


def get_runtime() -> Runtime | None:
    """Get the current runtime instance.

    Returns:
        The current Runtime, or None if not initialized.
    """
    return _runtime


def shutdown_runtime() -> None:
    """Shut down the runtime, stopping the heartbeat thread.

    This is safe to call even if the runtime was not initialized.
    """
    global _runtime

    with _lock:
        if _runtime is None:
            return

        rt = _runtime
        _runtime = None

    # Stop heartbeat outside the lock to avoid deadlock
    rt._stop_heartbeat()
    logger.info(
        "Agent runtime shut down: %s (instance=%s)",
        rt.agent_name,
        rt.instance_id,
    )
