from __future__ import annotations

import json
import re
from dataclasses import dataclass
from typing import Any, Mapping, Protocol
from urllib.error import HTTPError, URLError
from urllib.parse import quote, urlencode
from urllib.request import Request, urlopen
from uuid import NAMESPACE_URL, uuid5

from .contracts import HARBOR_TOOL_GATEWAY_PROTOCOL, Correlation, JobResult, LeasedJob, TaskRun, TaskRunState, TaskSpec, WorkerBackend


class PlatformError(RuntimeError):
    """A safe error raised by the AgentScope HTTP client.

    Authorization and cookie values are deliberately never retained in the error.
    """

    def __init__(self, method: str, url: str, message: str, status_code: int | None = None) -> None:
        self.method = method
        self.url = url
        self.status_code = status_code
        super().__init__(f"{method} {url}: {message}")


class NoJobAvailable(PlatformError):
    """The runner API replied that no matching benchmark job can be claimed."""


@dataclass(frozen=True)
class HTTPResponse:
    status_code: int
    body: bytes


class HTTPTransport(Protocol):
    """Injectable HTTP boundary for deterministic client tests."""

    def request(self, method: str, url: str, headers: Mapping[str, str], body: bytes | None, timeout_s: float) -> HTTPResponse: ...


class UrllibTransport:
    def request(self, method: str, url: str, headers: Mapping[str, str], body: bytes | None, timeout_s: float) -> HTTPResponse:
        request = Request(url, data=body, headers=dict(headers), method=method)
        try:
            with urlopen(request, timeout=timeout_s) as response:  # noqa: S310 -- caller controls the AgentScope URL.
                return HTTPResponse(response.status, response.read())
        except HTTPError as exc:
            return HTTPResponse(exc.code, exc.read())
        except URLError as exc:
            raise PlatformError(method, url, f"network error: {exc.reason}") from exc


_SECRET_KEY = re.compile(r"(?:api[_-]?key|secret|token|password|credential|authorization|cookie)", re.IGNORECASE)


def redact(value: Any, known_secrets: tuple[str, ...] = ()) -> Any:
    """Return a log-safe copy; it never mutates an outgoing request payload."""

    if isinstance(value, Mapping):
        return {key: "[REDACTED]" if _SECRET_KEY.search(str(key)) else redact(child, known_secrets) for key, child in value.items()}
    if isinstance(value, list):
        return [redact(item, known_secrets) for item in value]
    if isinstance(value, tuple):
        return tuple(redact(item, known_secrets) for item in value)
    if isinstance(value, str):
        result = value
        for secret in known_secrets:
            if secret:
                result = result.replace(secret, "[REDACTED]")
        return result
    return value


def _manifest_object_list(objects: Mapping[str, Any], key: str) -> list[Mapping[str, Any]]:
    value = objects.get(key)
    if not isinstance(value, list) or not all(isinstance(item, Mapping) for item in value):
        raise ValueError(f"catalog manifest objects.{key} must be a list of objects")
    return list(value)


def catalog_sync_payload(manifest: Mapping[str, Any]) -> dict[str, Any]:
    """Convert the local catalog manifest into AgentScope's strict sync schema.

    The local compiler's `{schema_version, catalog_sha256, objects}` shape is an
    internal source representation and is never sent to AgentScope unchanged.
    """

    if manifest.get("schema_version") != "v1":
        raise ValueError("catalog manifest schema_version must be 'v1'")
    revision_hash = manifest.get("catalog_sha256")
    if not isinstance(revision_hash, str) or not revision_hash:
        raise ValueError("catalog manifest catalog_sha256 must be a non-empty string")
    objects = manifest.get("objects")
    if not isinstance(objects, Mapping):
        raise ValueError("catalog manifest objects must be an object")

    artifacts = []
    for artifact in _manifest_object_list(objects, "artifacts"):
        # Preserve immutable artifact capabilities in AgentScope's catalog: the
        # template preflight uses them to check compatibility with the selected
        # suite, and they become part of the experiment's immutable snapshot.
        artifacts.append(
            {
                "id": artifact["id"],
                "framework": artifact["framework"],
                "image": artifact["image"],
                "image_digest": artifact["image_digest"],
                "source_commit": artifact.get("source_commit", ""),
                "task_runner_protocol": artifact["task_runner_protocol"],
                "capabilities": artifact["capabilities"],
            }
        )
    profiles: list[dict[str, Any]] = []
    for model in _manifest_object_list(objects, "model_profiles"):
        profiles.append(
            {
                "id": model["id"],
                "kind": "model",
                "config": {
                    "provider": model["provider"],
                    "model": model["model"],
                    "parameters": model["parameters"],
                },
            }
        )
    for agent in _manifest_object_list(objects, "agent_profiles"):
        profiles.append(
            {
                "id": agent["id"],
                "kind": "agent",
                "framework": agent["framework"],
                "config": agent["config"],
            }
        )
    for execution in _manifest_object_list(objects, "execution_profiles"):
        profiles.append(
            {
                "id": execution["id"],
                "kind": "execution",
                "config": execution["execution"],
            }
        )

    run_templates = []
    for template in _manifest_object_list(objects, "run_templates"):
        # Execution overrides are versioned catalog content. AgentScope validates
        # and snapshots them with the resolved template, never as a mutable
        # request-time override.
        run_templates.append(
            {
                "id": template["id"],
                "suite_id": template["suite"],
                "agent_artifact_id": template["agent_artifact"],
                "model_profile_id": template["model_profile"],
                "agent_profile_id": template["agent_profile"],
                "execution_profile_id": template["execution_profile"],
                "task_selection": template["task_selection"],
                "execution_overrides": template.get("execution_overrides", {}),
                "trials_per_case": template["trials_per_case"],
            }
        )
    return {
        "revision": f"git:{revision_hash}",
        "artifacts": artifacts,
        "profiles": profiles,
        "run_templates": run_templates,
    }


def _json_response(method: str, url: str, response: HTTPResponse, known_secrets: tuple[str, ...]) -> dict[str, Any]:
    text = response.body.decode("utf-8", errors="replace")
    if not 200 <= response.status_code < 300:
        try:
            parsed: Any = json.loads(text) if text else {}
        except json.JSONDecodeError:
            parsed = text
        safe = redact(parsed, known_secrets)
        message = json.dumps(safe, sort_keys=True) if isinstance(safe, (dict, list)) else str(safe)
        if response.status_code == 404:
            raise NoJobAvailable(method, url, message, response.status_code)
        raise PlatformError(method, url, message, response.status_code)
    if not text:
        return {}
    try:
        parsed = json.loads(text)
    except json.JSONDecodeError as exc:
        raise PlatformError(method, url, "invalid JSON response", response.status_code) from exc
    if not isinstance(parsed, dict):
        raise PlatformError(method, url, "JSON response must be an object", response.status_code)
    return parsed


def _safe_latency_ms(value: object) -> int:
    if isinstance(value, bool) or not isinstance(value, int) or value < 0:
        raise ValueError("latency_ms must be a non-negative integer")
    return value


class PlatformClient:
    """HTTP client for catalog synchronization and runner APIs.

    `session_cookie` and `project_token` exist only for the duration of one request
    and are not included in exceptions, request logs, or CLI output.
    """

    def __init__(self, server_url: str, transport: HTTPTransport | None = None, timeout_s: float = 15.0) -> None:
        normalized = server_url.rstrip("/")
        if not normalized.startswith(("http://", "https://")):
            raise ValueError("server_url must start with http:// or https://")
        self.server_url = normalized
        self.transport = transport or UrllibTransport()
        self.timeout_s = timeout_s

    def _request_json(
        self,
        method: str,
        path: str,
        *,
        headers: Mapping[str, str],
        payload: Mapping[str, Any] | None = None,
        known_secrets: tuple[str, ...] = (),
    ) -> dict[str, Any]:
        url = f"{self.server_url}{path}"
        body = json.dumps(payload, sort_keys=True, separators=(",", ":")).encode("utf-8") if payload is not None else None
        request_headers = {"Accept": "application/json", **headers}
        if body is not None:
            request_headers["Content-Type"] = "application/json"
        response = self.transport.request(method, url, request_headers, body, self.timeout_s)
        return _json_response(method, url, response, known_secrets)

    def sync_catalog(self, project_id: str, manifest: Mapping[str, Any], session_cookie: str) -> dict[str, Any]:
        if not project_id:
            raise ValueError("project_id is required")
        if not session_cookie:
            raise ValueError("AGENTSCOPE_SESSION_COOKIE is required for remote catalog sync")
        encoded_project_id = quote(project_id, safe="")
        return self._request_json(
            "POST",
            f"/api/projects/{encoded_project_id}/benchmark-catalog/sync",
            headers={"Cookie": session_cookie},
            payload=catalog_sync_payload(manifest),
            known_secrets=(session_cookie,),
        )

    def register_benchmark_worker(
        self,
        project_token: str,
        runner_name: str,
        *,
        execution_kinds: tuple[str, ...] = ("harbor",),
        capacity: int = 1,
        version: str = "benchmark-worker/v1",
    ) -> dict[str, Any]:
        # This payload intentionally matches AgentScope's bounded registration
        # contract exactly. Runner-specific capabilities belong in the immutable
        # artifact catalog, not a worker liveness registration.
        return self._runner_post(
            project_token,
            "/runner/benchmark-workers/register",
            {
                "worker_name": runner_name,
                "execution_kinds": list(execution_kinds),
                "capacity": capacity,
                "version": version,
            },
        )

    def claim_harbor_job(self, project_token: str, runner_name: str) -> dict[str, Any] | None:
        query = urlencode({"runner_name": runner_name, "execution_kind": "harbor"})
        try:
            return self._runner_request(project_token, "GET", f"/runner/jobs/claim?{query}")
        except NoJobAvailable:
            return None

    def heartbeat_job(self, project_token: str, runner_name: str, job_id: str) -> dict[str, Any]:
        return self._runner_post(project_token, f"/runner/jobs/{quote(job_id, safe='')}/heartbeat", {"runner_name": runner_name})

    def submit_result(self, project_token: str, runner_name: str, result: JobResult) -> dict[str, Any]:
        if result.verifier_status not in {"passed", "failed"}:
            raise ValueError("a result without an official verifier verdict must not be submitted")
        task_run = result.task_run
        payload: dict[str, Any] = {
            "runner_name": runner_name,
            "run_id": task_run.run_id or "",
            "score": float(result.score if result.score is not None else 0.0),
            "passed": result.verifier_status == "passed",
            "reason": task_run.error_code or task_run.final_text or "official verifier completed",
            "actual_output": task_run.final_text or "",
            "latency_ms": _safe_latency_ms(result.latency_ms),
            "cost": 0.0,
            "benchmark_result": {
                "valid": True,
                "official_metrics": dict(result.official_metrics or {}),
                "artifact_manifest": [dict(artifact) for artifact in result.artifact_manifest],
            },
        }
        return self._runner_post(project_token, f"/runner/jobs/{quote(result.eval_job_id, safe='')}/results", payload)

    def fail_job(self, project_token: str, runner_name: str, eval_job_id: str, error_type: str) -> dict[str, Any]:
        base_categories = {"agent_error", "harness_error", "verifier_error", "timeout", "cancelled"}
        detailed_category = re.fullmatch(r"(?:agent|harness)_[a-z][a-z0-9_]{0,55}", error_type)
        if error_type not in base_categories and detailed_category is None:
            raise ValueError("error_type must be an allowlisted benchmark failure category")
        return self._runner_post(
            project_token,
            f"/runner/jobs/{quote(eval_job_id, safe='')}/fail",
            {
                "runner_name": runner_name,
                "error": "benchmark worker could not produce an official verifier result",
                "benchmark_result": {"valid": False, "error_type": error_type, "official_metrics": {}, "artifact_manifest": []},
            },
        )

    def _runner_post(self, project_token: str, path: str, payload: Mapping[str, Any]) -> dict[str, Any]:
        return self._runner_request(project_token, "POST", path, payload)

    def _runner_request(
        self,
        project_token: str,
        method: str,
        path: str,
        payload: Mapping[str, Any] | None = None,
    ) -> dict[str, Any]:
        if not project_token:
            raise ValueError("AGENTSCOPE_PROJECT_TOKEN is required")
        return self._request_json(
            method,
            path,
            headers={"Authorization": f"Bearer {project_token}"},
            payload=payload,
            known_secrets=(project_token,),
        )


class HTTPPlatformWorkerBackend(WorkerBackend):
    """Runner API adapter.

    It deliberately refuses submission before a future Harbor verifier produces an
    official passed/failed verdict. The CLI's contract mode only claims and
    heartbeats; it never calls `submit_result`.
    """

    def __init__(self, client: PlatformClient, project_token: str) -> None:
        self.client = client
        self.project_token = project_token

    def claim(self, worker_id: str) -> LeasedJob | None:
        claim = self.client.claim_harbor_job(self.project_token, worker_id)
        if claim is None:
            return None
        return self._leased_job(claim)

    def heartbeat(self, worker_id: str, eval_job_id: str) -> None:
        self.client.heartbeat_job(self.project_token, worker_id, eval_job_id)

    def submit_result(self, worker_id: str, result: JobResult) -> None:
        self.client.submit_result(self.project_token, worker_id, result)

    def fail(self, worker_id: str, eval_job_id: str, error_type: str) -> None:
        self.client.fail_job(self.project_token, worker_id, eval_job_id, error_type)

    @staticmethod
    def _leased_job(claim: Mapping[str, Any]) -> LeasedJob:
        job = claim.get("job")
        if not isinstance(job, Mapping) or not isinstance(job.get("id"), str) or not job["id"]:
            raise PlatformError("GET", "/runner/jobs/claim", "claim response is missing job.id")
        benchmark = claim.get("benchmark")
        if benchmark is not None and not isinstance(benchmark, Mapping):
            raise PlatformError("GET", "/runner/jobs/claim", "claim response benchmark must be an object")
        config = claim.get("eval_config")
        if not isinstance(config, Mapping):
            config = {}
        execution_config = job.get("execution_config")
        if isinstance(execution_config, Mapping):
            config = {**execution_config, **config}
        case_metadata = claim.get("case_metadata")
        if not isinstance(case_metadata, Mapping):
            case_metadata = {}
        task_id = case_metadata.get("task_id")
        if not isinstance(task_id, str) or not task_id:
            task_id = str(job.get("dataset_case_id", ""))
        correlation = Correlation(
            experiment_id=str(job.get("experiment_id", "")),
            eval_job_id=job["id"],
            trial_index=int(job.get("trial_index", 0)),
        )
        task = TaskSpec(
            task_run_id=str(uuid5(NAMESPACE_URL, f"agentlab:{job['id']}:{job.get('trial_index', 0)}")),
            task_id=task_id,
            instruction=str(claim.get("case_input", "")),
            workspace=str(config.get("workspace", "")),
            timeout_seconds=int(config.get("task_timeout_s", 0)),
            tool_gateway_endpoint=str(config.get("tool_gateway_endpoint", "")),
            tool_gateway_protocol=str(config.get("tool_gateway_protocol", HARBOR_TOOL_GATEWAY_PROTOCOL)),
            agent_profile_id=_profile_id(config, "agent_profile"),
            model_profile_id=_profile_id(config, "model_profile"),
            correlation=correlation,
            case_metadata=dict(case_metadata),
            execution_config=dict(config),
        )
        return LeasedJob(job["id"], task)


def _profile_id(config: Mapping[str, Any], key: str) -> str:
    value = config.get(key)
    if isinstance(value, Mapping):
        value = value.get("id")
    return value if isinstance(value, str) else ""
