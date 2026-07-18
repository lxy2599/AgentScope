"""Agent Runtime — Agent registration, heartbeat, and lifecycle management."""

from __future__ import annotations

import contextlib
import logging
import threading
import time
import uuid
from dataclasses import dataclass, field
from typing import Any, Iterator

from agentscope_sdk.client import AgentClient, AgentInfo

logger = logging.getLogger(__name__)

# ---- Run state constants ----

RUN_STATUS_QUEUED = "queued"
RUN_STATUS_RUNNING = "running"
RUN_STATUS_WAITING_LLM = "waiting_llm"
RUN_STATUS_WAITING_TOOL = "waiting_tool"
RUN_STATUS_WAITING_RETRIEVAL = "waiting_retrieval"
RUN_STATUS_RETRYING = "retrying"
RUN_STATUS_COMPLETED = "completed"
RUN_STATUS_FAILED = "failed"
RUN_STATUS_TIMEOUT = "timeout"
RUN_STATUS_CANCELLED = "cancelled"

# ---- Run domain types ----

@dataclass
class RuntimeRun:
    """Represents a single run within an agent runtime."""
    id: str = ""
    status: str = RUN_STATUS_RUNNING
    output: str = ""
    trace_id: str = ""

    def set_output(self, output: str) -> None:
        self.output = output


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
        _current_run: The currently active run (set by start_run/context manager).
        _heartbeat_thread: Background thread for periodic heartbeats.
        _stop_event: Event to signal the heartbeat thread to stop.
        _base_url: The server base URL.
        _project_token: The project token for auth.
    """

    agent_id: str
    agent_name: str
    instance_id: str
    client: AgentClient

    _current_run: RuntimeRun | None = None
    _heartbeat_thread: threading.Thread | None = None
    _stop_event: threading.Event | None = None
    _base_url: str = ""
    _project_token: str = ""
    last_error: Exception | None = None

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

    # ---- Run lifecycle API ----

    def start_run(self, input: str = "", metadata: dict[str, Any] | None = None, trace_id: str = "") -> RuntimeRun | None:
        """Start a new run by posting to the SDK runtime API.

        Args:
            input: Optional input description for the run.
            metadata: Optional metadata dict.
            trace_id: Optional OTLP trace ID to link the run to a trace.

        Returns:
            The RuntimeRun if successful, or None on network error.
        """
        import httpx

        payload: dict[str, Any] = {
            "agent_id": self.agent_id,
            "agent_instance_id": self.instance_id,
            "input": input,
            "trace_id": trace_id,
        }
        if metadata:
            payload["metadata"] = metadata

        headers = {
            "Authorization": f"Bearer {self._project_token}",
            "Content-Type": "application/json",
        }

        try:
            with httpx.Client(timeout=10.0) as client:
                resp = client.post(
                    f"{self._base_url}/api/sdk/runs/start",
                    headers=headers,
                    json=payload,
                )
                resp.raise_for_status()
                data = resp.json()
        except Exception as e:
            self.last_error = e
            logger.warning("start_run failed: %s", e)
            return None

        run = RuntimeRun(
            id=data.get("id", ""),
            status=data.get("status", RUN_STATUS_RUNNING),
            trace_id=data.get("trace_id", trace_id),
        )
        self._current_run = run
        return run

    def update_run_status(self, status: str, message: str = "", attributes: dict[str, Any] | None = None) -> bool:
        """Update the current run's status.

        Args:
            status: The new status (e.g., 'waiting_llm', 'running').
            message: Optional human-readable message.
            attributes: Optional additional attributes.

        Returns:
            True if successful, False on network error.
        """
        if self._current_run is None:
            logger.warning("update_run_status called with no active run")
            return False

        import httpx

        payload: dict[str, Any] = {
            "status": status,
            "message": message,
            "agent_id": self.agent_id,
        }
        if attributes:
            payload["attributes"] = attributes

        headers = {
            "Authorization": f"Bearer {self._project_token}",
            "Content-Type": "application/json",
        }

        try:
            with httpx.Client(timeout=10.0) as client:
                resp = client.post(
                    f"{self._base_url}/api/sdk/runs/{self._current_run.id}/state",
                    headers=headers,
                    json=payload,
                )
                resp.raise_for_status()
                data = resp.json()
                self._current_run.status = data.get("status", status)
        except Exception as e:
            self.last_error = e
            logger.warning("update_run_status failed: %s", e)
            return False

        return True

    def finish_run(self, output: str = "", metrics: dict[str, Any] | None = None) -> bool:
        """Finish the current run with completed status.

        Args:
            output: Optional run output.
            metrics: Optional metrics dict.

        Returns:
            True if successful, False on network error.
        """
        if self._current_run is None:
            return False

        import httpx

        payload: dict[str, Any] = {
            "output": output,
            "agent_id": self.agent_id,
        }
        if metrics:
            payload["metrics"] = metrics

        headers = {
            "Authorization": f"Bearer {self._project_token}",
            "Content-Type": "application/json",
        }

        try:
            with httpx.Client(timeout=10.0) as client:
                resp = client.post(
                    f"{self._base_url}/api/sdk/runs/{self._current_run.id}/finish",
                    headers=headers,
                    json=payload,
                )
                resp.raise_for_status()
        except Exception as e:
            self.last_error = e
            logger.warning("finish_run failed: %s", e)
            # Still clear the current run
        finally:
            self._current_run = None

        return True

    def fail_run(self, error: str, attributes: dict[str, Any] | None = None) -> bool:
        """Fail the current run.

        Args:
            error: Error description.
            attributes: Optional additional attributes.

        Returns:
            True if successful, False on network error.
        """
        if self._current_run is None:
            return False

        import httpx

        payload: dict[str, Any] = {
            "error": error,
            "agent_id": self.agent_id,
        }
        if attributes:
            payload["attributes"] = attributes

        headers = {
            "Authorization": f"Bearer {self._project_token}",
            "Content-Type": "application/json",
        }

        try:
            with httpx.Client(timeout=10.0) as client:
                resp = client.post(
                    f"{self._base_url}/api/sdk/runs/{self._current_run.id}/fail",
                    headers=headers,
                    json=payload,
                )
                resp.raise_for_status()
        except Exception as e:
            self.last_error = e
            logger.warning("fail_run failed: %s", e)
        finally:
            self._current_run = None

        return True

    @contextlib.contextmanager
    def run(self, input: str = "", metadata: dict[str, Any] | None = None) -> Iterator[RuntimeRun]:
        """Context manager that wraps a run lifecycle.

        Usage:
            with runtime.run(input="hello") as run:
                run.set_output("world")

        On success, calls finish_run automatically.
        On exception, calls fail_run and re-raises.
        Network errors are swallowed but logged.
        """
        run_ref = self.start_run(input=input, metadata=metadata)
        try:
            yield run_ref or RuntimeRun()
        except Exception as exc:
            self.fail_run(error=str(exc))
            raise
        else:
            if run_ref is not None:
                self.finish_run(output=run_ref.output)


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
            _base_url=server_url.rstrip("/"),
            _project_token=project_token,
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


# ---- Module-level run lifecycle convenience API ----


def start_run(input: str = "", metadata: dict[str, Any] | None = None, trace_id: str = "") -> RuntimeRun | None:
    """Start a new run using the current runtime.

    Args:
        input: Optional input description.
        metadata: Optional metadata dict.
        trace_id: Optional OTLP trace ID.

    Returns:
        The RuntimeRun if successful, or None if no runtime or network error.
    """
    rt = get_runtime()
    if rt is None:
        logger.warning("start_run: runtime not initialized")
        return None
    return rt.start_run(input=input, metadata=metadata, trace_id=trace_id)


def update_run_status(status: str, message: str = "", attributes: dict[str, Any] | None = None) -> bool:
    """Update the current run's status.

    Args:
        status: The new status.
        message: Optional message.
        attributes: Optional attributes dict.

    Returns:
        True if successful.
    """
    rt = get_runtime()
    if rt is None:
        return False
    return rt.update_run_status(status, message=message, attributes=attributes)


def finish_run(output: str = "", metrics: dict[str, Any] | None = None) -> bool:
    """Finish the current run.

    Args:
        output: Optional output string.
        metrics: Optional metrics dict.

    Returns:
        True if successful.
    """
    rt = get_runtime()
    if rt is None:
        return False
    return rt.finish_run(output=output, metrics=metrics)


def fail_run(error: str, attributes: dict[str, Any] | None = None) -> bool:
    """Fail the current run.

    Args:
        error: Error description.
        attributes: Optional attributes dict.

    Returns:
        True if successful.
    """
    rt = get_runtime()
    if rt is None:
        return False
    return rt.fail_run(error=error, attributes=attributes)
