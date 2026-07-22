"""Agent Runtime — Agent registration, heartbeat, and lifecycle management."""

from __future__ import annotations

import contextlib
import logging
import os
import platform
import socket
import threading
import time
import uuid
from dataclasses import dataclass, field
from typing import Any, Iterator

from agentscope_sdk.client import AgentClient, AgentInfo

logger = logging.getLogger(__name__)

# SDK version string — avoids circular import of __init__
SDK_VERSION = "0.1.0"

# ---- Runtime Contract ----

CONTRACT_VERSION = "agent-runtime-v1"
CAPABILITY_KINDS = {
    "tool",
    "skill",
    "knowledge_base",
    "memory",
    "guardrail",
    "model",
}

RUN_STATUS_THINKING = "thinking"
RUN_STATUS_WAITING_MEMORY = "waiting_memory"
RUN_STATUS_WAITING_USER = "waiting_user"


def _merge_non_empty(base: dict[str, Any], extra: dict[str, Any] | None) -> dict[str, Any]:
    """Merge extra into base, skipping empty/None values in base (not extra).

    Empty dicts (``{}``) and empty lists (``[]``) are kept since they carry
    semantic meaning (e.g. ``feature_flags``, ``tags``).
    """
    result = {k: v for k, v in base.items() if v not in ("", None)}
    if extra:
        result.update(extra)
    return result


def build_agent_metadata(
    *,
    agent_version: str = "",
    framework: str = "custom-python",
    runtime: str = "python",
    source: str = "sdk",
    environment: str = "dev",
    model_provider: str = "",
    model_name: str = "",
    harness_version: str = "",
    prompt_version: str = "",
    tags: list[str] | None = None,
    extra: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """Build standardised agent metadata dict for Runtime Contract v1.

    Args:
        agent_version: User-defined agent version string.
        framework: Agent framework (e.g. ``custom-python``, ``langgraph``, ``hermes``).
        runtime: Language runtime (e.g. ``python``, ``node``, ``go``).
        source: Origin (``sdk``, ``adapter``, ``runner``, ``demo``).
        environment: Deployment environment (``dev``, ``staging``, ``prod``).
        model_provider: Default model provider (e.g. ``glm``, ``openai``).
        model_name: Default model name.
        harness_version: Harness/orchestration layer version.
        prompt_version: Default prompt or system prompt version.
        tags: Low-cardinality tags (e.g. ``["demo", "customer-support"]``).
        extra: Additional metadata to merge (overwrites defaults).

    Returns:
        A dict with ``contract_version=agent-runtime-v1``.
    """
    return _merge_non_empty(
        {
            "contract_version": CONTRACT_VERSION,
            "agent_version": agent_version,
            "framework": framework,
            "runtime": runtime,
            "source": source,
            "environment": environment,
            "model_provider": model_provider,
            "model_name": model_name,
            "harness_version": harness_version,
            "prompt_version": prompt_version,
            "tags": tags or [],
        },
        extra,
    )


def build_run_metadata(
    *,
    session_id: str = "",
    conversation_id: str = "",
    task_id: str = "",
    parent_run_id: str = "",
    agent_version: str = "",
    framework: str = "custom-python",
    runtime: str = "python",
    source: str = "sdk",
    environment: str = "dev",
    model_provider: str = "",
    model_name: str = "",
    harness_version: str = "",
    prompt_version: str = "",
    feature_flags: dict[str, Any] | None = None,
    ablation_flags: dict[str, Any] | None = None,
    extra: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """Build standardised run metadata dict for Runtime Contract v1.

    Args:
        session_id: Session identifier for grouping runs.
        conversation_id: Conversation identifier.
        task_id: Task identifier.
        parent_run_id: Parent run ID for nested runs.
        agent_version: Agent version string.
        framework: Agent framework.
        runtime: Language runtime.
        source: Origin.
        environment: Deployment environment.
        model_provider: Model provider.
        model_name: Model name.
        harness_version: Harness version.
        prompt_version: Prompt version.
        feature_flags: Feature flag map (for ablation/experiment metadata).
        ablation_flags: Ablation flag map.
        extra: Additional metadata to merge.

    Returns:
        A dict with ``contract_version=agent-runtime-v1``.
    """
    return _merge_non_empty(
        {
            "contract_version": CONTRACT_VERSION,
            "session_id": session_id,
            "conversation_id": conversation_id,
            "task_id": task_id,
            "parent_run_id": parent_run_id,
            "agent_version": agent_version,
            "framework": framework,
            "runtime": runtime,
            "source": source,
            "environment": environment,
            "model_provider": model_provider,
            "model_name": model_name,
            "harness_version": harness_version,
            "prompt_version": prompt_version,
            "feature_flags": feature_flags or {},
            "ablation_flags": ablation_flags or {},
        },
        extra,
    )


def normalize_capability(
    *,
    kind: str,
    name: str,
    version: str = "",
    description: str = "",
    schema: dict[str, Any] | None = None,
    side_effect_risk: str = "",
    metadata: dict[str, Any] | None = None,
) -> dict[str, Any]:
    """Normalize a capability dict for Runtime Contract v1.

    Args:
        kind: One of ``tool``, ``skill``, ``knowledge_base``, ``memory``,
            ``guardrail``, ``model``.
        name: Capability name (unique per agent).
        version: Capability version string.
        description: Human-readable description.
        schema: JSON Schema for the capability input/output.
        side_effect_risk: ``"low"``, ``"medium"``, or ``"high"``.
        metadata: Extra metadata to include inside the capability ``metadata`` field.

    Returns:
        A dict with ``name``, ``description``, and ``metadata`` fields.

    Raises:
        ValueError: If ``kind`` is not in ``CAPABILITY_KINDS`` or ``name`` is empty.
    """
    if kind not in CAPABILITY_KINDS:
        raise ValueError(f"unsupported capability kind: {kind}")
    if not name:
        raise ValueError("capability name is required")

    cap_meta = {
        "contract_version": CONTRACT_VERSION,
        "kind": kind,
        "version": version,
        "schema": schema or {},
        "side_effect_risk": side_effect_risk,
        "metadata": metadata or {},
    }
    return {
        "name": name,
        "description": description,
        "metadata": _merge_non_empty(cap_meta, None),
    }


def build_run_resource_attributes(*, run_id: str, trace_id: str, agent_name: str) -> dict[str, str]:
    """Build resource attributes that cleanly separate Run ID from Trace ID.

    Args:
        run_id: The business-level PostgreSQL run UUID (e.g. from RuntimeRun.id).
        trace_id: The OTLP hex-encoded trace ID (e.g. from span_context.trace_id).
        agent_name: The agent name.

    Returns:
        A dict with ``agentscope.run.id``, ``agentscope.trace.id``, and
        ``agentscope.agent.name`` keys, ready to be passed to span.set_attribute().
    """
    return {
        "agentscope.run.id": run_id,
        "agentscope.trace.id": trace_id,
        "agentscope.agent.name": agent_name,
    }


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

        Runtime Contract defaults (set by init, used by start_run):
        _agent_version: User-defined agent version.
        _framework: Agent framework.
        _runtime: Language runtime.
        _source: Origin.
        _environment: Deployment environment.
        _model_provider: Default model provider.
        _model_name: Default model name.
        _harness_version: Harness version.
        _prompt_version: Default prompt version.
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

    # Runtime Contract defaults (populated by init, used by start_run and heartbeat)
    _agent_version: str = ""
    _framework: str = "custom-python"
    _runtime: str = "python"
    _source: str = "sdk"
    _environment: str = "dev"
    _model_provider: str = ""
    _model_name: str = ""
    _harness_version: str = ""
    _prompt_version: str = ""

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

    _model_provider: str = ""
    _model_name: str = ""
    _harness_version: str = ""
    _prompt_version: str = ""

    # Instance metadata (set at initialization)
    _process_start_time: str = ""
    _capacity: int = 1

    def _build_instance_payload(self) -> dict[str, Any]:
        """Build standard instance metadata payload for heartbeats."""
        hostname = socket.gethostname()
        pid = os.getpid()
        return _merge_non_empty(
            {
                "contract_version": CONTRACT_VERSION,
                "runtime": self._runtime,
                "framework": self._framework,
                "environment": self._environment,
                "hostname": hostname,
                "pid": pid,
                "process_start_time": self._process_start_time,
                "sdk_version": SDK_VERSION,
                "capacity": self._capacity,
                "inflight_runs": 1 if self._current_run is not None else 0,
            },
            None,
        )

    def send_heartbeat(self, status: str = "online", payload: dict[str, Any] | None = None) -> None:
        """Send a single heartbeat for this runtime instance.

        The heartbeat payload includes standard instance metadata
        (contract_version, runtime, framework, environment, hostname, pid,
        process_start_time, sdk_version, capacity, inflight_runs) merged with
        any caller-provided payload.
        """
        merged = self._build_instance_payload()
        if payload:
            merged.update(payload)
        self.client.send_heartbeat(
            agent_id=self.agent_id,
            instance_id=self.instance_id,
            status=status,
            payload=merged,
        )

    # ---- Run lifecycle API ----

    def start_run(
        self,
        input: str = "",
        metadata: dict[str, Any] | None = None,
        trace_id: str = "",
        *,
        session_id: str = "",
        conversation_id: str = "",
        task_id: str = "",
        parent_run_id: str = "",
        feature_flags: dict[str, Any] | None = None,
        ablation_flags: dict[str, Any] | None = None,
    ) -> RuntimeRun | None:
        """Start a new run by posting to the SDK runtime API.

        Args:
            input: Optional input description for the run.
            metadata: Optional metadata dict (merged with contract fields).
            trace_id: Optional OTLP trace ID to link the run to a trace.

            Runtime Contract parameters:
            session_id: Session identifier for grouping runs.
            conversation_id: Conversation identifier.
            task_id: Task identifier.
            parent_run_id: Parent run ID for nested runs.
            feature_flags: Feature flag map.
            ablation_flags: Ablation flag map.

        Returns:
            The RuntimeRun if successful, or None on network error.
        """
        import httpx

        run_meta = build_run_metadata(
            session_id=session_id,
            conversation_id=conversation_id,
            task_id=task_id,
            parent_run_id=parent_run_id,
            agent_version=self._agent_version,
            framework=self._framework,
            runtime=self._runtime,
            source=self._source,
            environment=self._environment,
            model_provider=self._model_provider,
            model_name=self._model_name,
            harness_version=self._harness_version,
            prompt_version=self._prompt_version,
            extra=metadata,
            feature_flags=feature_flags,
            ablation_flags=ablation_flags,
        )

        payload: dict[str, Any] = {
            "agent_id": self.agent_id,
            "instance_id": self.instance_id,
            "agent_instance_id": self.instance_id,
            "input": input,
            "trace_id": trace_id,
        }
        if run_meta:
            payload["metadata"] = run_meta

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
            "instance_id": self.instance_id,
            "agent_instance_id": self.instance_id,
            "trace_id": self._current_run.trace_id if self._current_run else "",
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

    def finish_run(self, output: str = "", metrics: dict[str, Any] | None = None, latency_ms: int = 0) -> bool:
        """Finish the current run with completed status.

        Args:
            output: Optional run output.
            metrics: Optional metrics dict.
            latency_ms: Run duration in milliseconds.

        Returns:
            True if successful, False on network error.
        """
        if self._current_run is None:
            return False

        import httpx

        payload: dict[str, Any] = {
            "output": output,
            "agent_id": self.agent_id,
            "instance_id": self.instance_id,
            "agent_instance_id": self.instance_id,
            "trace_id": self._current_run.trace_id if self._current_run else "",
            "latency_ms": latency_ms,
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
            "instance_id": self.instance_id,
            "agent_instance_id": self.instance_id,
            "trace_id": self._current_run.trace_id if self._current_run else "",
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
    # Runtime Contract parameters
    agent_version: str = "",
    framework: str = "custom-python",
    runtime: str = "python",
    source: str = "sdk",
    environment: str = "dev",
    model_provider: str = "",
    model_name: str = "",
    harness_version: str = "",
    prompt_version: str = "",
    tags: list[str] | None = None,
    capabilities: list[dict[str, Any]] | None = None,
) -> Runtime:
    """Initialize the AgentScope runtime for the current process.

    This registers the agent with the control center and starts a background
    heartbeat thread.

    Args:
        server_url: Base URL of the AgentScope server.
        project_token: Project token for authentication.
        agent_name: Name of the agent to register.
        description: Optional agent description.
        metadata: Optional agent metadata (merged with contract fields).
        instance_id: Unique instance ID. Auto-generated if not provided.
        heartbeat_interval: Seconds between heartbeats (default 30).

        Agent Runtime Contract parameters:
        agent_version: User-defined agent version (e.g. ``"1.0.0"``).
        framework: Agent framework (e.g. ``"custom-python"``, ``"langgraph"``, ``"hermes"``).
        runtime: Language runtime (e.g. ``"python"``, ``"node"``, ``"go"``).
        source: Origin (``"sdk"``, ``"adapter"``, ``"runner"``, ``"demo"``).
        environment: Deployment environment (``"dev"``, ``"staging"``, ``"prod"``).
        model_provider: Default model provider (e.g. ``"glm"``, ``"openai"``).
        model_name: Default model name.
        harness_version: Harness/orchestration layer version.
        prompt_version: Default prompt or system prompt version.
        tags: Low-cardinality tags.
        capabilities: List of normalized capability dicts to register.

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

        # Build contract metadata
        agent_meta = build_agent_metadata(
            agent_version=agent_version,
            framework=framework,
            runtime=runtime,
            source=source,
            environment=environment,
            model_provider=model_provider,
            model_name=model_name,
            harness_version=harness_version,
            prompt_version=prompt_version,
            tags=tags,
            extra=metadata,
        )

        # Register agent
        agent_info: AgentInfo = client.register_agent(
            name=agent_name,
            description=description,
            metadata=agent_meta,
        )

        # Register capabilities
        for capability in capabilities or []:
            client.set_capability(
                agent_id=agent_info.id,
                name=capability["name"],
                description=capability.get("description", ""),
                metadata=capability.get("metadata", {}),
            )

        # Create runtime with contract defaults
        resolved_instance_id = instance_id or f"instance-{uuid.uuid4().hex[:12]}"
        rt = Runtime(
            agent_id=agent_info.id,
            agent_name=agent_name,
            instance_id=resolved_instance_id,
            client=client,
            _base_url=server_url.rstrip("/"),
            _project_token=project_token,
            _agent_version=agent_version,
            _framework=framework,
            _runtime=runtime,
            _source=source,
            _environment=environment,
            _model_provider=model_provider,
            _model_name=model_name,
            _harness_version=harness_version,
            _prompt_version=prompt_version,
            _process_start_time=time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
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


def start_run(input: str = "", metadata: dict[str, Any] | None = None, trace_id: str = "",
              *, session_id: str = "", conversation_id: str = "", task_id: str = "",
              parent_run_id: str = "", feature_flags: dict[str, Any] | None = None,
              ablation_flags: dict[str, Any] | None = None) -> RuntimeRun | None:
    """Start a new run using the current runtime.

    Args:
        input: Optional input description.
        metadata: Optional metadata dict.
        trace_id: Optional OTLP trace ID.
        session_id: Session identifier.
        conversation_id: Conversation identifier.
        task_id: Task identifier.
        parent_run_id: Parent run ID.
        feature_flags: Feature flag map.
        ablation_flags: Ablation flag map.

    Returns:
        The RuntimeRun if successful, or None if no runtime or network error.
    """
    rt = get_runtime()
    if rt is None:
        logger.warning("start_run: runtime not initialized")
        return None
    return rt.start_run(
        input=input, metadata=metadata, trace_id=trace_id,
        session_id=session_id, conversation_id=conversation_id,
        task_id=task_id, parent_run_id=parent_run_id,
        feature_flags=feature_flags, ablation_flags=ablation_flags,
    )


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


def finish_run(output: str = "", metrics: dict[str, Any] | None = None, latency_ms: int = 0) -> bool:
    """Finish the current run.

    Args:
        output: Optional output string.
        metrics: Optional metrics dict.
        latency_ms: Run duration in milliseconds.

    Returns:
        True if successful.
    """
    rt = get_runtime()
    if rt is None:
        return False
    return rt.finish_run(output=output, metrics=metrics, latency_ms=latency_ms)


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
