#!/usr/bin/env python3
"""Exercise the AgentLab catalog → queue → official-result API path.

This is an API fixture, not a Terminal-Bench score run: it creates an isolated
project and submits a known synthetic *official verifier* payload only after
checking the same immutable snapshot consumed by the Harbor Worker.  It never
prints session cookies, project tokens, or generated credentials.
"""

from __future__ import annotations

import argparse
import json
import secrets
import subprocess
import sys
import urllib.error
import urllib.request
from http.cookiejar import CookieJar
from pathlib import Path
from typing import Any

from benchmark_worker.platform import catalog_sync_payload
from benchmarkctl.catalog import load_catalog


class APIError(RuntimeError):
    pass


class API:
    def __init__(self, base_url: str) -> None:
        self.base_url = base_url.rstrip("/")
        self.cookies = CookieJar()
        self.opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.cookies))

    def request(
        self,
        method: str,
        path: str,
        body: dict[str, Any] | None = None,
        *,
        token: str | None = None,
        expect_object: bool = True,
    ) -> Any:
        payload = json.dumps(body, separators=(",", ":"), sort_keys=True).encode() if body is not None else None
        headers = {"Accept": "application/json"}
        if payload is not None:
            headers["Content-Type"] = "application/json"
        if token:
            headers["Authorization"] = f"Bearer {token}"
        request = urllib.request.Request(f"{self.base_url}{path}", data=payload, headers=headers, method=method)
        try:
            with self.opener.open(request, timeout=20) as response:
                decoded = json.loads(response.read().decode("utf-8"))
        except urllib.error.HTTPError as exc:
            # Server error bodies are never relayed. Its allowlisted error code
            # is enough to make a local E2E failure actionable without exposing
            # a generated token/cookie or user-provided task data.
            error_code = "unknown"
            try:
                parsed = json.loads(exc.read().decode("utf-8"))
                if isinstance(parsed, dict) and isinstance(parsed.get("error"), dict):
                    candidate = parsed["error"].get("code")
                    if isinstance(candidate, str) and candidate.isascii() and candidate.replace("_", "").isalnum():
                        error_code = candidate
            except (UnicodeDecodeError, json.JSONDecodeError):
                pass
            raise APIError(f"{method} {path} failed with HTTP {exc.code} ({error_code})") from exc
        except (urllib.error.URLError, UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise APIError(f"{method} {path} failed") from exc
        if expect_object and not isinstance(decoded, dict):
            raise APIError(f"{method} {path} returned a non-object response")
        return decoded


def _verify_openhands_presence(
    openhands_python: Path,
    server_url: str,
    token: str,
    agentscope_python: Path,
) -> None:
    if not openhands_python.is_file() or not agentscope_python.is_dir():
        raise APIError("owned OpenHands Python or AgentScope SDK source is unavailable for presence verification")
    code = (
        "from openhands.agent_server.agentscope_presence import "
        "start_agentscope_presence, stop_agentscope_presence; "
        "ok = start_agentscope_presence(); stop_agentscope_presence(); "
        "raise SystemExit(0 if ok else 3)"
    )
    env = {
        "PATH": __import__("os").environ.get("PATH", ""),
        "HOME": __import__("os").environ.get("HOME", ""),
        "OPENHANDS_AGENTSCOPE_ENABLED": "true",
        "AGENTSCOPE_SERVER_URL": server_url,
        "AGENTSCOPE_PROJECT_TOKEN": token,
        # Local API E2E deliberately exercises the checked-out AgentScope SDK
        # contract. The production OpenHands Docker image installs its locked
        # agentscope-sdk dependency at image-build time instead.
        "PYTHONPATH": str(agentscope_python),
    }
    try:
        completed = subprocess.run(  # noqa: S603 -- owned interpreter and fixed inline source.
            [str(openhands_python), "-c", code],
            env=env,
            check=False,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            # Importing the owned OpenHands server package is intentionally part
            # of this source-level E2E and can be slow on a cold Python cache.
            # This is a hard bound, not a retry loop; normal SDK HTTP calls still
            # use their own bounded timeouts.
            timeout=60,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise APIError("OpenHands presence verification could not run") from exc
    if completed.returncode == 3:
        raise APIError("OpenHands presence registration returned disabled/fail-open")
    if completed.returncode != 0:
        raise APIError("OpenHands presence verification process failed")


def _verify_hermes_presence(
    hermes_root: Path,
    server_url: str,
    token: str,
    agent_name: str,
    agentscope_python: Path,
) -> None:
    """Construct the owned Hermes source and verify its native SDK presence.

    The child only constructs ``AIAgent``; its constructor is Hermes' genuine
    native initialization path and directly initializes the Python SDK. No
    plugin, bridge, observer process, or model request is involved here.
    AgentScope is checked separately through its authenticated API below.
    """

    hermes_python = hermes_root / ".venv" / "bin" / "python"
    if not hermes_python.is_file() or not (hermes_root / "run_agent.py").is_file() or not agentscope_python.is_dir():
        raise APIError("owned Hermes source, Python environment, or AgentScope SDK source is unavailable for presence verification")
    code = (
        "import time; "
        "from run_agent import AIAgent; "
        "from agentscope_sdk import shutdown_runtime, shutdown_tracing; "
        "agent = AIAgent(api_key='e2e-placeholder', base_url='http://127.0.0.1:9/v1', "
        "model='openai/e2e-presence', max_iterations=1, quiet_mode=True, "
        "skip_context_files=True, skip_memory=True, save_trajectories=False); "
        "time.sleep(0.5); shutdown_runtime(); shutdown_tracing(); del agent"
    )
    env = {
        "PATH": __import__("os").environ.get("PATH", ""),
        "HOME": __import__("os").environ.get("HOME", ""),
        "HERMES_AGENTSCOPE_ENABLED": "true",
        "HERMES_AGENTSCOPE_AGENT_NAME": agent_name,
        "AGENTSCOPE_SERVER_URL": server_url,
        "AGENTSCOPE_PROJECT_TOKEN": token,
        # Local API E2E directly loads the owned SDK source. Production images
        # install the same locked SDK during image construction instead.
        "PYTHONPATH": str(agentscope_python),
    }
    try:
        completed = subprocess.run(  # noqa: S603 -- owned interpreter and fixed inline source.
            [str(hermes_python), "-c", code],
            cwd=hermes_root,
            env=env,
            check=False,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
            timeout=60,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise APIError("Hermes presence verification could not run") from exc
    if completed.returncode != 0:
        raise APIError("Hermes native SDK presence verification process failed")


def obj_id(payload: dict[str, Any], name: str) -> str:
    value = payload.get("id")
    if not isinstance(value, str) or not value:
        raise APIError(f"{name} response is missing id")
    return value


def main() -> int:
    parser = argparse.ArgumentParser(description="Run AgentLab API queue/Harbor fixture E2E without exposing credentials.")
    parser.add_argument("--server-url", default="http://127.0.0.1:18080")
    parser.add_argument("--catalog-root", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument(
        "--openhands-python",
        type=Path,
        default=Path(__file__).resolve().parents[2] / "openhands" / ".venv" / "bin" / "python",
        help="owned OpenHands Python used to verify source-level presence registration",
    )
    parser.add_argument(
        "--hermes-root",
        type=Path,
        default=Path(__file__).resolve().parents[2] / "hermes",
        help="owned Hermes checkout used to verify native Python SDK presence registration",
    )
    args = parser.parse_args()
    catalog_root = args.catalog_root.expanduser().resolve()
    agentscope_python = catalog_root.parent / "agentscope" / "python"

    nonce = secrets.token_hex(8)
    api = API(args.server_url)
    email = f"agentlab-e2e-{nonce}@example.invalid"
    password = secrets.token_urlsafe(32)
    user = api.request("POST", "/api/auth/register", {"email": email, "password": password, "display_name": "AgentLab API E2E"})
    if obj_id(user, "register") == "":  # keeps static type/control-flow explicit.
        raise AssertionError("unreachable")
    project = api.request("POST", "/api/projects", {"name": f"AgentLab E2E {nonce}"})
    project_id = obj_id(project, "project")

    dataset = api.request("POST", f"/api/projects/{project_id}/datasets", {"name": "terminal-bench fixture"})
    dataset_id = obj_id(dataset, "dataset")
    case = api.request(
        "POST",
        f"/api/datasets/{dataset_id}/cases",
        {"input": {"instruction": "fixture task"}, "metadata": {"task_id": "smoke-task-001"}},
    )
    case_id = obj_id(case, "dataset case")
    suite = api.request(
        "POST",
        f"/api/projects/{project_id}/benchmark-suites",
        {
            "dataset_id": dataset_id,
            "benchmark_key": "terminal_bench_2_0",
            "name": "Terminal-Bench fixture",
            "version": "2.0",
            "harness_kind": "harbor",
            "execution_kind": "harbor",
            "required_capabilities": ["shell", "filesystem", "git"],
            "metadata": {"catalog_suite_id": "terminal-bench-2.0"},
        },
    )
    if obj_id(suite, "benchmark suite") == "":
        raise AssertionError("unreachable")

    catalog = load_catalog(catalog_root)
    effective_run = catalog.compile("hermes-terminal-bench-smoke")
    manifest = catalog.to_manifest()
    sync = api.request("POST", f"/api/projects/{project_id}/benchmark-catalog/sync", catalog_sync_payload(manifest))
    if sync.get("revision") != f"git:{manifest['catalog_sha256']}":
        raise APIError("server did not retain the catalog revision")
    preflight = api.request("POST", f"/api/projects/{project_id}/benchmark-preflight", {"run_template_id": "hermes-terminal-bench-smoke"})
    if preflight.get("compatible") is not True:
        raise APIError("template preflight is incompatible")
    experiment = api.request("POST", f"/api/projects/{project_id}/benchmark-experiments", {"run_template_id": "hermes-terminal-bench-smoke"})
    experiment_id = obj_id(experiment, "benchmark experiment")
    launch = api.request("POST", f"/api/projects/{project_id}/benchmark-experiments/{experiment_id}/launch", {})
    if launch.get("launch", {}).get("status") != "queued":
        raise APIError("benchmark launch was not queued")

    token_result = api.request(
        "POST",
        f"/api/projects/{project_id}/tokens",
        {
            "description": "ephemeral AgentLab E2E worker",
            "scopes": ["runner:write", "agent:register", "agent:runtime", "otlp:write"],
            "environment": "development",
            "token_type": "custom",
        },
    )
    token = token_result.get("plain_token")
    token_record = token_result.get("token")
    if not isinstance(token, str) or not token or not isinstance(token_record, dict):
        raise APIError("token creation did not produce a runner token")
    token_id = token_record.get("id")
    if not isinstance(token_id, str) or not token_id:
        raise APIError("token creation did not return token metadata")
    token_list = api.request("GET", f"/api/projects/{project_id}/tokens", expect_object=False)
    token_items = token_list.get("tokens", token_list) if isinstance(token_list, dict) else token_list
    if not isinstance(token_items, list):
        raise APIError("token list response is invalid")
    created = next((item for item in token_items if isinstance(item, dict) and item.get("id") == token_id), None)
    scopes = created.get("scopes") if isinstance(created, dict) else None
    required_scopes = {"runner:write", "agent:register", "agent:runtime", "otlp:write"}
    if not isinstance(scopes, list) or not required_scopes.issubset(set(scopes)):
        raise APIError("created E2E token does not retain its required scopes")
    raw_agent = api.request(
        "POST",
        "/api/v1/agents/register",
        {"name": f"agentlab-api-e2e-probe-{nonce}", "metadata": {"framework": "fixture"}},
        token=token,
    )
    raw_agent_id = obj_id(raw_agent, "agent registration probe")
    api.request(
        "POST",
        f"/api/v1/agents/{raw_agent_id}/heartbeat",
        {"instance_id": f"fixture-{nonce}", "status": "online", "payload": {"framework": "fixture"}},
        token=token,
    )
    _verify_openhands_presence(
        args.openhands_python,
        args.server_url,
        token,
        agentscope_python,
    )
    hermes_agent_name = f"hermes-api-e2e-{nonce}"
    _verify_hermes_presence(
        args.hermes_root,
        args.server_url,
        token,
        hermes_agent_name,
        agentscope_python,
    )
    agents = api.request("GET", f"/api/v1/agents?project_id={project_id}", expect_object=False)
    if not isinstance(agents, list):
        raise APIError("agent list response is invalid")
    hermes_agent = next((item for item in agents if isinstance(item, dict) and item.get("name") == hermes_agent_name), None)
    if not isinstance(hermes_agent, dict):
        raise APIError("Hermes native source did not register an agent")
    hermes_agent_id = obj_id(hermes_agent, "Hermes agent")
    hermes_detail = api.request("GET", f"/api/v1/agents/{hermes_agent_id}")
    runtime_summary = hermes_detail.get("runtime_summary")
    if not isinstance(runtime_summary, dict) or runtime_summary.get("total_instance_count", 0) < 1:
        raise APIError("Hermes native source did not record a heartbeat")
    worker = "agentlab-api-e2e"
    api.request("POST", "/runner/benchmark-workers/register", {"worker_name": worker, "execution_kinds": ["harbor"], "capacity": 1, "version": "benchmark-worker/v1"}, token=token)
    claim = api.request("GET", f"/runner/jobs/claim?runner_name={worker}&execution_kind=harbor", token=token)
    job = claim.get("job")
    if not isinstance(job, dict) or not isinstance(job.get("id"), str):
        raise APIError("worker claim is missing a job")
    job_id = job["id"]
    snapshot = job.get("execution_config")
    if not isinstance(snapshot, dict):
        raise APIError("claimed job has no immutable execution snapshot")
    harbor = snapshot.get("harbor")
    artifact = snapshot.get("agent_artifact")
    if not isinstance(harbor, dict) or harbor.get("version") != "0.20.0" or harbor.get("dataset") != "terminal-bench@2.0":
        raise APIError("Harbor execution configuration was not frozen into the claimed job")
    if not isinstance(artifact, dict) or artifact.get("image_digest") != effective_run.agent_artifact.image_digest:
        raise APIError("artifact digest was not frozen into the claimed job")
    if claim.get("case_metadata", {}).get("task_id") != "smoke-task-001" or job.get("dataset_case_id") != case_id:
        raise APIError("upstream task identity was not retained in the claim")
    api.request("POST", f"/runner/jobs/{job_id}/heartbeat", {"runner_name": worker}, token=token)
    api.request(
        "POST",
        f"/runner/jobs/{job_id}/results",
        {
            "runner_name": worker,
            "run_id": "",
            "score": 1.0,
            "passed": True,
            "reason": "fixture official verifier completed",
            "actual_output": "",
            "latency_ms": 0,
            "cost": 0,
            "benchmark_result": {
                "valid": True,
                "official_metrics": {"reward": 1.0},
                "artifact_manifest": [],
            },
        },
        token=token,
    )
    report = api.request("GET", f"/api/projects/{project_id}/benchmark-experiments/{experiment_id}/report")
    if report.get("quality", {}).get("official_score") != 1:
        raise APIError("official result did not appear in the benchmark report")
    reported_experiment = report.get("experiment")
    if not isinstance(reported_experiment, dict) or "benchmark_config" in reported_experiment:
        raise APIError("benchmark report exposed its raw execution configuration")
    config_snapshot = report.get("benchmark_config_snapshot")
    if not isinstance(config_snapshot, dict):
        raise APIError("benchmark report omitted its public immutable configuration snapshot")
    if config_snapshot.get("run_template_id") != effective_run.template_id:
        raise APIError("benchmark report did not retain its run template identity")
    if config_snapshot.get("catalog_revision") != f"git:{manifest['catalog_sha256']}":
        raise APIError("benchmark report did not retain its catalog revision")
    if config_snapshot.get("trials_per_case") != effective_run.trials_per_case:
        raise APIError("benchmark report did not retain its trial count")
    public_artifact = config_snapshot.get("agent_artifact")
    if not isinstance(public_artifact, dict) or public_artifact.get("image_digest") != effective_run.agent_artifact.image_digest:
        raise APIError("benchmark report did not retain its immutable agent artifact digest")
    public_profiles = config_snapshot.get("profiles")
    if not isinstance(public_profiles, dict):
        raise APIError("benchmark report omitted its public profile identities")
    public_model = public_profiles.get("model")
    public_agent = public_profiles.get("agent")
    public_execution = public_profiles.get("execution")
    if not all(isinstance(profile, dict) for profile in (public_model, public_agent, public_execution)):
        raise APIError("benchmark report profiles have an invalid public shape")
    expected_execution_profile = catalog.run_templates[effective_run.template_id].execution_profile
    if (
        public_model.get("id") != effective_run.model_profile.id
        or public_agent.get("id") != effective_run.agent_profile.id
        or public_execution.get("id") != expected_execution_profile
    ):
        raise APIError("benchmark report did not retain its profile identities")
    if set(public_profiles) - {"model", "agent", "execution"}:
        raise APIError("benchmark report exposed opaque profile configuration")
    allowed_profile_keys = {"id", "framework"}
    if any(set(profile) - allowed_profile_keys for profile in (public_model, public_agent, public_execution)):
        raise APIError("benchmark report exposed an opaque profile configuration field")
    public_selection = config_snapshot.get("task_selection")
    if not isinstance(public_selection, dict) or public_selection.get("include") != ["smoke-task-001"]:
        raise APIError("benchmark report did not retain its task selection")

    # A separate queued experiment exercises the durable, project-admin-only
    # cancellation path. It ensures this E2E never leaves fake immutable
    # snapshots behind for an actual Worker to claim on a later run.
    cancellable = api.request(
        "POST",
        f"/api/projects/{project_id}/benchmark-experiments",
        {"run_template_id": "hermes-terminal-bench-smoke"},
    )
    cancellable_experiment_id = obj_id(cancellable, "cancellable benchmark experiment")
    cancellation = api.request(
        "POST",
        f"/api/projects/{project_id}/benchmark-experiments/{cancellable_experiment_id}/cancel",
        {"reason": "API E2E fixture uses a synthetic official verifier result"},
    )
    if cancellation.get("status") != "cancelled" or cancellation.get("cancelled_jobs") != 1 or cancellation.get("audit_events") != 1:
        raise APIError("queued fixture benchmark was not durably cancelled")
    if "reason" in cancellation:
        raise APIError("cancellation response unexpectedly exposed audit reason")
    audit = api.request(
        "GET",
        f"/api/projects/{project_id}/benchmark-experiments/{cancellable_experiment_id}/cancellation-audit",
        expect_object=False,
    )
    if not isinstance(audit, list) or len(audit) != 1:
        raise APIError("cancellation audit is missing")
    event = audit[0]
    if not isinstance(event, dict) or event.get("previous_status") != "queued":
        raise APIError("cancellation audit did not retain the queued transition")
    repeated = api.request(
        "POST",
        f"/api/projects/{project_id}/benchmark-experiments/{cancellable_experiment_id}/cancel",
        {"reason": "API E2E fixture uses a synthetic official verifier result"},
    )
    if repeated.get("cancelled_jobs") != 0 or repeated.get("audit_events") != 0:
        raise APIError("repeated cancellation is not idempotent")

    print(json.dumps({
        "api_e2e": "passed",
        "catalog_revision": sync["revision"],
        "experiment_id": experiment_id,
        "harbor_version": harbor["version"],
        "openhands_presence": "passed",
        "hermes_presence": "passed",
        "task_selection_metadata_mapping": "passed",
        "worker_result": "official_verifier_fixture_passed",
        "cancellation_audit": "passed",
    }, sort_keys=True))
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except APIError as exc:
        print(f"api-e2e failed: {exc}", file=sys.stderr)
        raise SystemExit(2)
