"""Runner HTTP client for communicating with the AgentScope server."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

import httpx
from pydantic import BaseModel


class RunnerAPIError(Exception):
    """Raised when the server returns a non-2xx response.

    Error messages are sanitized to not contain the project token.
    """


# ---- HTTP response models ----

class EvalJobModel(BaseModel):
    """Model for the job portion of a claim response."""
    id: str
    experiment_id: str
    dataset_case_id: str
    status: str
    runner_name: str | None = None
    lease_expires_at: str | None = None
    attempts: int = 0
    error: str | None = None
    created_at: str | None = None
    updated_at: str | None = None


class ClaimResponseModel(BaseModel):
    """Model for a successful claim response."""
    job: EvalJobModel
    status: str
    lease_duration: str | None = None
    case_input: str = ""
    case_expected: str | None = None
    case_metadata: dict[str, Any] | None = None
    target_name: str = ""
    target_version: str = ""
    eval_config: dict[str, Any] | None = None


# ---- Domain types ----

@dataclass
class ClaimedJob:
    """A job that has been claimed by this runner, with all execution context."""
    id: str
    experiment_id: str
    dataset_case_id: str
    case_input: str
    case_expected: str | None = None
    case_metadata: dict[str, Any] | None = None
    target_name: str = ""
    target_version: str = ""
    eval_config: dict[str, Any] | None = None
    lease_expires_at: str | None = None
    attempts: int = 0


@dataclass
class JobResultPayload:
    """Payload for submitting an evaluation result."""
    run_id: str = ""
    score: float = 0.0
    passed: bool = False
    reason: str = ""
    actual_output: str = ""
    latency_ms: int = 0
    cost: float = 0.0
    details: dict[str, Any] | None = None


# ---- Client ----

class RunnerClient:
    """HTTP client for the AgentScope runner API.

    Args:
        server_url: Base URL of the AgentScope server (with or without trailing slash).
        project_token: Project token for bearer authentication.
        runner_name: Name of this runner instance.
        timeout: HTTP request timeout in seconds.
    """

    def __init__(
        self,
        server_url: str,
        project_token: str,
        runner_name: str,
        timeout: float = 30.0,
    ) -> None:
        # Strip trailing slash to avoid double-slash in path joins
        self._base_url = server_url.rstrip("/")
        self._project_token = project_token
        self._runner_name = runner_name
        self._timeout = timeout
        self._headers = {
            "Authorization": f"Bearer {self._project_token}",
            "Content-Type": "application/json",
        }

    def _sanitize_error(self, response: httpx.Response) -> str:
        """Extract an error message from a response, without exposing the token."""
        try:
            body = response.json()
            detail = body.get("message", body.get("error", str(response.reason_phrase)))
        except Exception:
            detail = str(response.reason_phrase)
        return detail

    def claim_job(self) -> ClaimedJob | None:
        """Claim the next available job.

        Returns:
            A ClaimedJob if a job was claimed, or None if no jobs are available.

        Raises:
            RunnerAPIError: If the server returns a non-2xx response (except 404).
        """
        url = f"{self._base_url}/runner/jobs/claim?runner_name={self._runner_name}"
        response = self._client_request("GET", url)

        if response.status_code == 404:
            return None

        if not response.is_success:
            msg = self._sanitize_error(response)
            raise RunnerAPIError(msg)

        model = ClaimResponseModel.model_validate(response.json())
        return ClaimedJob(
            id=model.job.id,
            experiment_id=model.job.experiment_id,
            dataset_case_id=model.job.dataset_case_id,
            case_input=model.case_input,
            case_expected=model.case_expected,
            case_metadata=model.case_metadata,
            target_name=model.target_name,
            target_version=model.target_version,
            eval_config=model.eval_config,
            lease_expires_at=model.job.lease_expires_at,
            attempts=model.job.attempts,
        )

    def get_run_id_by_trace(self, trace_id: str) -> str | None:
        """Look up the PostgreSQL run ID for a given trace ID.

        After sending an OTLP trace, the backend creates a ``runs`` record
        keyed by the trace's ``trace_id``.  This method retrieves the
        auto-generated PostgreSQL ``run_id`` so it can be passed when
        submitting eval results.

        Args:
            trace_id: The hex-encoded OTLP trace ID to look up.

        Returns:
            The PostgreSQL run UUID, or ``None`` if no run is found for
            that trace ID.

        Raises:
            RunnerAPIError: If the server returns an unexpected error.
        """
        url = f"{self._base_url}/runner/runs/by-trace/{trace_id}"
        response = self._client_request("GET", url)

        if response.status_code == 404:
            return None

        if not response.is_success:
            msg = self._sanitize_error(response)
            raise RunnerAPIError(msg)

        data = response.json()
        return data.get("run_id")

    def heartbeat(self, job_id: str) -> None:
        """Send a heartbeat to extend the lease on a claimed job.

        Args:
            job_id: The ID of the job to heartbeat.

        Raises:
            RunnerAPIError: If the server returns a non-2xx response.
        """
        url = f"{self._base_url}/runner/jobs/{job_id}/heartbeat"
        response = self._client_request(
            "POST", url, json={"runner_name": self._runner_name}
        )
        if not response.is_success:
            msg = self._sanitize_error(response)
            raise RunnerAPIError(msg)

    def submit_result(self, job_id: str, result: JobResultPayload) -> None:
        """Submit the evaluation result for a job.

        Args:
            job_id: The ID of the job.
            result: The evaluation result payload.

        Raises:
            RunnerAPIError: If the server returns a non-2xx response.
        """
        payload: dict[str, Any] = {
            "runner_name": self._runner_name,
            "run_id": result.run_id,
            "score": result.score,
            "passed": result.passed,
            "reason": result.reason,
            "actual_output": result.actual_output,
            "latency_ms": result.latency_ms,
            "cost": result.cost,
        }
        if result.details:
            payload["details"] = result.details

        url = f"{self._base_url}/runner/jobs/{job_id}/results"
        response = self._client_request("POST", url, json=payload)
        if not response.is_success:
            msg = self._sanitize_error(response)
            raise RunnerAPIError(msg)

    def fail_job(
        self,
        job_id: str,
        error_type: str,
        message: str,
        details: dict[str, Any] | None = None,
    ) -> None:
        """Report that a job failed.

        Args:
            job_id: The ID of the job.
            error_type: A short error type identifier (e.g., 'target_error').
            message: A human-readable error message.
            details: Optional structured error details.

        Raises:
            RunnerAPIError: If the server returns a non-2xx response.
        """
        url = f"{self._base_url}/runner/jobs/{job_id}/fail"
        error_msg = f"{error_type}: {message}"
        payload: dict[str, Any] = {
            "runner_name": self._runner_name,
            "error": error_msg,
        }
        if details:
            payload["details"] = details

        response = self._client_request("POST", url, json=payload)
        if not response.is_success:
            msg = self._sanitize_error(response)
            raise RunnerAPIError(msg)

    def _client_request(self, method: str, url: str, **kwargs: Any) -> httpx.Response:
        """Make an HTTP request and return the response.

        Low-level HTTP exceptions are converted to RunnerAPIError.
        """
        try:
            response = httpx.request(
                method, url, headers=self._headers, timeout=self._timeout, **kwargs
            )
            return response
        except httpx.RequestError as e:
            raise RunnerAPIError(str(e)) from e


# ---- Agent Control Center Client ----


class AgentAPIError(Exception):
    """Raised when the Agent API returns a non-2xx response."""


@dataclass
class AgentInfo:
    """Information about a registered agent."""
    id: str = ""
    project_id: str = ""
    name: str = ""
    description: str = ""
    status: str = ""
    version: int = 0
    metadata: dict[str, Any] = field(default_factory=dict)
    created_at: str = ""
    updated_at: str = ""


class AgentClient:
    """HTTP client for the Agent Control Center API.

    Args:
        server_url: Base URL of the AgentScope server.
        project_token: Project token for bearer authentication (requires scope 'agent:register').
        timeout: HTTP request timeout in seconds.
    """

    def __init__(
        self,
        server_url: str,
        project_token: str,
        timeout: float = 30.0,
    ) -> None:
        self._base_url = server_url.rstrip("/")
        self._project_token = project_token
        self._timeout = timeout
        self._headers = {
            "Authorization": f"Bearer {self._project_token}",
            "Content-Type": "application/json",
        }

    def register_agent(
        self,
        name: str,
        description: str = "",
        metadata: dict[str, Any] | None = None,
    ) -> AgentInfo:
        """Register this agent with the control center.

        Args:
            name: Agent name (unique within project).
            description: Optional description.
            metadata: Optional metadata dict.

        Returns:
            AgentInfo with the server's response.

        Raises:
            AgentAPIError: If the server returns a non-2xx response.
        """
        payload: dict[str, Any] = {
            "name": name,
            "description": description,
        }
        if metadata:
            payload["metadata"] = metadata

        url = f"{self._base_url}/api/v1/agents/register"
        response = self._request("POST", url, json=payload)

        data = response.json()
        return AgentInfo(
            id=data.get("id", ""),
            project_id=data.get("project_id", ""),
            name=data.get("name", ""),
            description=data.get("description", ""),
            status=data.get("status", ""),
            version=data.get("version", 0),
            metadata=data.get("metadata", {}),
            created_at=data.get("created_at", ""),
            updated_at=data.get("updated_at", ""),
        )

    def send_heartbeat(
        self,
        agent_id: str,
        instance_id: str,
        status: str = "online",
        payload: dict[str, Any] | None = None,
    ) -> None:
        """Send a heartbeat for an agent instance.

        Args:
            agent_id: The agent's UUID.
            instance_id: The instance identifier.
            status: Instance status (default "online").
            payload: Optional heartbeat payload.

        Raises:
            AgentAPIError: If the server returns a non-2xx response.
        """
        body: dict[str, Any] = {
            "instance_id": instance_id,
            "status": status,
        }
        if payload:
            body["payload"] = payload

        url = f"{self._base_url}/api/v1/agents/{agent_id}/heartbeat"
        self._request("POST", url, json=body)

    def set_capability(
        self,
        agent_id: str,
        name: str,
        description: str = "",
        metadata: dict[str, Any] | None = None,
    ) -> None:
        """Set a capability for an agent.

        Note: This is a convenience method that calls the backend directly.
        The backend agent:register scope is required.

        Args:
            agent_id: The agent's UUID.
            name: Capability name (unique per agent).
            description: Optional description.
            metadata: Optional metadata dict.

        Raises:
            AgentAPIError: If the server returns a non-2xx response.
        """
        payload: dict[str, Any] = {
            "name": name,
            "description": description,
        }
        if metadata:
            payload["metadata"] = metadata

        # POST to a capability endpoint
        url = f"{self._base_url}/api/v1/agents/{agent_id}/capabilities"
        self._request("POST", url, json=payload)

    def _request(self, method: str, url: str, **kwargs: Any) -> httpx.Response:
        """Make an HTTP request, raising AgentAPIError on failure."""
        try:
            response = httpx.request(
                method, url, headers=self._headers, timeout=self._timeout, **kwargs
            )
            if not response.is_success:
                try:
                    detail = response.json().get("message", str(response.reason_phrase))
                except Exception:
                    detail = str(response.reason_phrase)
                raise AgentAPIError(detail)
            return response
        except httpx.RequestError as e:
            raise AgentAPIError(str(e)) from e
