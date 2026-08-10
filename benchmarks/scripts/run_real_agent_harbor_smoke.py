#!/usr/bin/env python3
"""Run one real OpenHands/Hermes Harbor smoke trial through AgentScope APIs.

The script deliberately creates an isolated temporary project and a temporary
runner token. It starts a one-shot trusted Worker container which launches a
*digest-pinned* owned Agent sidecar. The only score read from the final report
is Harbor's official verifier reward. It never prints request credentials,
model credentials, task output, or Harbor logs.
"""

from __future__ import annotations

import argparse
import json
import os
import secrets
import shutil
import subprocess
import sys
import time
import urllib.error
import urllib.request
from http.cookiejar import CookieJar
from pathlib import Path
from typing import Any

from benchmark_worker.platform import catalog_sync_payload
from benchmarkctl.catalog import load_catalog


class SmokeError(RuntimeError):
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
        data = json.dumps(body, separators=(",", ":"), sort_keys=True).encode() if body is not None else None
        headers = {"Accept": "application/json"}
        if data is not None:
            headers["Content-Type"] = "application/json"
        if token:
            headers["Authorization"] = f"Bearer {token}"
        request = urllib.request.Request(f"{self.base_url}{path}", data=data, headers=headers, method=method)
        try:
            with self.opener.open(request, timeout=30) as response:
                payload = json.loads(response.read().decode("utf-8"))
        except urllib.error.HTTPError as exc:
            # Do not print a server response: it may contain user task data.
            code = "unknown"
            try:
                body_json = json.loads(exc.read().decode("utf-8"))
                candidate = body_json.get("error", {}).get("code") if isinstance(body_json, dict) else None
                if isinstance(candidate, str) and candidate.replace("_", "").isalnum():
                    code = candidate
            except (UnicodeDecodeError, json.JSONDecodeError):
                pass
            raise SmokeError(f"{method} {path} failed with HTTP {exc.code} ({code})") from exc
        except (urllib.error.URLError, UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise SmokeError(f"{method} {path} failed") from exc
        if expect_object and not isinstance(payload, dict):
            raise SmokeError(f"{method} {path} returned a non-object response")
        return payload


def _id(value: dict[str, Any], label: str) -> str:
    candidate = value.get("id")
    if not isinstance(candidate, str) or not candidate:
        raise SmokeError(f"{label} response is missing id")
    return candidate


def _env_value(*names: str) -> str:
    for name in names:
        value = os.environ.get(name, "").strip()
        if value:
            return value
    return ""


def _model_environment() -> dict[str, str]:
    api_key = _env_value("LLM_API_KEY", "ARK_API_KEY", "OPENAI_API_KEY")
    if not api_key:
        raise SmokeError("missing model credential: set LLM_API_KEY or ARK_API_KEY in .env")
    model = _env_value("LLM_MODEL", "BENCHMARK_MODEL")
    if not model:
        raise SmokeError("missing model identifier: set LLM_MODEL or BENCHMARK_MODEL in .env")
    values = {"LLM_API_KEY": api_key, "LLM_MODEL": model}
    base_url = _env_value("LLM_BASE_URL", "ARK_BASE_URL", "OPENAI_BASE_URL")
    if base_url:
        values["LLM_BASE_URL"] = base_url
    for name in ("HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY"):
        value = os.environ.get(name, "").strip()
        if value:
            values[name] = value
    return values


def _local_image_digest(image: str) -> str:
    try:
        completed = subprocess.run(
            ["docker", "image", "inspect", "--format", "{{.Id}}", image],
            check=False,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
            timeout=20,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise SmokeError("could not inspect local Agent image") from exc
    digest = completed.stdout.strip()
    if completed.returncode != 0 or not digest.startswith("sha256:") or len(digest) != 71:
        raise SmokeError("required local Agent image is unavailable")
    return digest


def _wait_for_report(api: API, project_id: str, experiment_id: str, deadline: float) -> dict[str, Any]:
    while time.monotonic() < deadline:
        report = api.request("GET", f"/api/projects/{project_id}/benchmark-experiments/{experiment_id}/report")
        quality = report.get("quality")
        if isinstance(quality, dict) and isinstance(quality.get("official_score"), (int, float)):
            return report
        execution = report.get("execution")
        if isinstance(execution, dict) and isinstance(execution.get("failed"), int) and execution["failed"] > 0:
            trials = report.get("trials")
            failure_codes = {
                trial.get("error_type")
                for trial in trials
                if isinstance(trial, dict)
                and isinstance(trial.get("error_type"), str)
                and trial["error_type"].replace("_", "").isalnum()
            } if isinstance(trials, list) else set()
            suffix = ",".join(sorted(failure_codes)) if failure_codes else "unclassified"
            raise SmokeError(f"Harbor job finished without an official verifier result ({suffix})")
        time.sleep(2)
    raise SmokeError("timed out waiting for Harbor official verifier result")


def _worker_failure_category(stdout: bytes, stderr: bytes) -> str:
    """Classify a one-shot Worker failure without exposing its raw output.

    Worker, Docker, Harbor and model-provider logs can contain task content or
    provider diagnostics. The smoke wrapper intentionally persists and prints
    only a bounded category, never those raw streams.
    """

    text = (stdout + b"\n" + stderr).decode("utf-8", errors="replace").lower()
    if "no module named" in text or "modulenotfounderror" in text:
        return "worker_runtime_dependency_missing"
    if "host.docker.internal" in text and ("network error" in text or "connection" in text):
        return "platform_connectivity_error"
    if "no_jobs" in text or "no claimable jobs" in text:
        return "job_claim_error"
    if "task runner" in text or "task_runner" in text:
        return "task_runner_contract_error"
    if "litellm" in text or "llm_api_key" in text or "authentication" in text or "unauthorized" in text:
        return "model_provider_error"
    if "sidecar" in text:
        return "agent_sidecar_error"
    if "timeout" in text or "timed out" in text:
        return "timeout"
    if "harbor" in text:
        return "harbor_runtime_error"
    return "worker_execution_error"


def _write_worker_failure_diagnostic(work_root: Path, returncode: int, stdout: bytes, stderr: bytes) -> str:
    """Store a non-sensitive failure classification in the protected run directory."""

    category = _worker_failure_category(stdout, stderr)
    diagnostic = {
        "schema_version": "agentlab.real-agent-smoke-worker-diagnostic.v1",
        "status": "failed",
        "returncode": returncode,
        "category": category,
    }
    path = work_root / "worker-diagnostic.json"
    path.write_text(json.dumps(diagnostic, sort_keys=True) + "\n")
    path.chmod(0o600)
    return category


def _worker_command(
    *,
    root: Path,
    work_root: Path,
    worker_image: str,
    runner_token: str,
    worker_name: str,
    model_env: dict[str, str],
) -> list[str]:
    alias = f"agentlab-real-smoke-worker-{worker_name.rsplit('-', 1)[-1]}"
    server_url = os.environ.get("AGENTSCOPE_SERVER_URL", "http://127.0.0.1:18080").rstrip("/")
    # The Worker and its nested sidecar live on the Compose control network.
    # Its task catalog and work paths are mounted at identical absolute host
    # paths because Harbor itself creates task Docker containers through socket.
    values = {
        "AGENTSCOPE_SERVER_URL": "http://host.docker.internal:18080",
        "AGENTSCOPE_PROJECT_TOKEN": runner_token,
        "HERMES_AGENTSCOPE_ENABLED": "true",
        "HERMES_AGENTSCOPE_AGENT_NAME": "hermes-benchmark",
        "AGENTLAB_BENCHMARK_CATALOG_ROOT": str(root),
        "AGENTLAB_SIDECAR_NETWORK": "agentlab_default",
        "AGENTLAB_TOOL_GATEWAY_HOST": alias,
        **model_env,
    }
    # The launcher runs on the host, but this one-shot Worker runs in Docker.
    # Use the Docker-reachable proxy endpoint when supplied; forwarding the
    # host-only 127.0.0.1 proxy endpoint makes Worker -> AgentScope requests
    # time out. Keep host.docker.internal out of NO_PROXY: in this deployment
    # the Worker must route that address through the host proxy.
    docker_proxy = _env_value("AGENTLAB_DOCKER_PROXY_URL")
    if docker_proxy:
        values["HTTP_PROXY"] = docker_proxy
        values["HTTPS_PROXY"] = docker_proxy
    # Support a nondefault local HTTP port without allowing catalog data to
    # influence this endpoint.
    if server_url.startswith("http://127.0.0.1:"):
        values["AGENTSCOPE_SERVER_URL"] = server_url.replace("127.0.0.1", "host.docker.internal", 1)
    command = [
        "docker", "run", "--rm", "--init",
        "--network", "agentlab_default",
        "--network-alias", alias,
        "--add-host", "host.docker.internal:host-gateway",
        "--mount", "type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock",
        "--mount", f"type=bind,src={root},dst={root},readonly",
        "--mount", f"type=bind,src={work_root},dst={work_root}",
    ]
    for name, value in sorted(values.items()):
        command.extend(["--env", f"{name}={value}"])
    command.extend([
        worker_image,
        "--server-url", values["AGENTSCOPE_SERVER_URL"],
        "--worker-id", worker_name,
        "--work-root", str(work_root),
        "--heartbeat-interval-s", "10",
        "--once",
    ])
    return command


def main() -> int:
    parser = argparse.ArgumentParser(description="run one real Harbor official-verifier smoke trial")
    parser.add_argument("--framework", choices=("openhands", "hermes"), required=True)
    parser.add_argument("--server-url", default=os.environ.get("AGENTSCOPE_SERVER_URL", "http://127.0.0.1:18080"))
    parser.add_argument("--catalog-root", type=Path, default=Path(__file__).resolve().parents[1])
    parser.add_argument("--worker-image", default="agentlab/benchmarks-worker:local")
    parser.add_argument("--timeout-s", type=int, default=420)
    args = parser.parse_args()
    if args.timeout_s < 60:
        raise SmokeError("--timeout-s must be at least 60")

    model_env = _model_environment()
    catalog_root = args.catalog_root.expanduser().resolve()
    if not catalog_root.is_dir():
        raise SmokeError("catalog root is unavailable")
    template_id = f"{args.framework}-harbor-agent-smoke"
    catalog = load_catalog(catalog_root)
    run = catalog.compile(template_id)
    # The immutable catalog profile is the benchmark authority. The model value
    # is passed through verbatim because provider/model spelling is framework
    # specific: Hermes with a caller-supplied Ark OpenAI-compatible endpoint
    # requires Ark's bare deployment id, while other runtimes can retain their
    # own provider-prefixed profile.
    model_env["LLM_MODEL"] = run.model_profile.model
    # The catalog digest is an immutable sidecar identity, not a tag-only claim.
    if _local_image_digest(run.agent_artifact.image) != run.agent_artifact.image_digest:
        raise SmokeError("local Task Runner image does not match the catalog-pinned digest")
    _local_image_digest(args.worker_image)

    nonce = secrets.token_hex(8)
    api = API(args.server_url)
    email = f"agentlab-real-smoke-{nonce}@example.invalid"
    password = secrets.token_urlsafe(32)
    api.request("POST", "/api/auth/register", {"email": email, "password": password, "display_name": "AgentLab real smoke"})
    project_id = _id(api.request("POST", "/api/projects", {"name": f"AgentLab real smoke {nonce}"}), "project")
    dataset_id = _id(api.request("POST", f"/api/projects/{project_id}/datasets", {"name": "AgentLab Harbor real-agent smoke"}), "dataset")
    case = api.request(
        "POST",
        f"/api/datasets/{dataset_id}/cases",
        {"input": {"instruction": "AgentLab Harbor real-agent smoke"}, "metadata": {"task_id": "harbor-agent-smoke"}},
    )
    case_id = _id(case, "dataset case")
    suite = api.request(
        "POST",
        f"/api/projects/{project_id}/benchmark-suites",
        {
            "dataset_id": dataset_id,
            "benchmark_key": "agentlab_harbor_smoke",
            "name": "AgentLab Harbor real-agent smoke",
            "version": "1.0",
            "harness_kind": "harbor",
            "execution_kind": "harbor",
            "required_capabilities": ["shell", "filesystem", "git"],
            "metadata": {"catalog_suite_id": "agentlab-harbor-smoke"},
        },
    )
    _id(suite, "benchmark suite")
    manifest = catalog.to_manifest()
    api.request("POST", f"/api/projects/{project_id}/benchmark-catalog/sync", catalog_sync_payload(manifest))
    preflight = api.request("POST", f"/api/projects/{project_id}/benchmark-preflight", {"run_template_id": template_id})
    if preflight.get("compatible") is not True:
        raise SmokeError("benchmark preflight is incompatible")
    experiment_id = _id(api.request("POST", f"/api/projects/{project_id}/benchmark-experiments", {"run_template_id": template_id}), "benchmark experiment")
    launch = api.request("POST", f"/api/projects/{project_id}/benchmark-experiments/{experiment_id}/launch", {})
    if launch.get("launch", {}).get("status") != "queued":
        raise SmokeError("benchmark experiment was not queued")
    token_result = api.request(
        "POST",
        f"/api/projects/{project_id}/tokens",
        {
            "description": "ephemeral AgentLab real smoke worker",
            "scopes": ["runner:write", "agent:register", "agent:runtime", "otlp:write"],
            "environment": "development",
            "token_type": "custom",
        },
    )
    runner_token = token_result.get("plain_token")
    if not isinstance(runner_token, str) or not runner_token:
        raise SmokeError("could not create temporary runner token")

    work_root = catalog_root / ".runtime" / "real-agent-harbor-smoke" / nonce
    work_root.mkdir(mode=0o700, parents=True)
    worker_name = f"agentlab-real-smoke-{nonce}"
    try:
        completed = subprocess.run(
            _worker_command(
                root=catalog_root,
                work_root=work_root,
                worker_image=args.worker_image,
                runner_token=runner_token,
                worker_name=worker_name,
                model_env=model_env,
            ),
            check=False,
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            timeout=args.timeout_s,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise SmokeError("one-shot Harbor Worker did not complete") from exc
    if completed.returncode != 0:
        category = _write_worker_failure_diagnostic(
            work_root,
            completed.returncode,
            completed.stdout,
            completed.stderr,
        )
        raise SmokeError(f"one-shot Harbor Worker failed before an official result was available ({category})")

    report = _wait_for_report(api, project_id, experiment_id, time.monotonic() + 30)
    quality = report.get("quality", {})
    score = quality.get("official_score") if isinstance(quality, dict) else None
    if not isinstance(score, (int, float)):
        raise SmokeError("AgentScope report has no Harbor official score")
    # A score of 0 is a valid real verifier outcome; make it visible rather
    # than turning command success into a false pass. The shell wrapper returns
    # nonzero for that real failed benchmark to support CI.
    result = {
        "framework": args.framework,
        "official_score": float(score),
        "official_verifier": "harbor",
        "experiment_id": experiment_id,
        "dataset_case_id": case_id,
        "status": "passed" if float(score) == 1.0 else "failed",
    }
    print(json.dumps(result, sort_keys=True))
    return 0 if float(score) == 1.0 else 1


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except SmokeError as exc:
        print(f"real-agent-harbor-smoke failed: {exc}", file=sys.stderr)
        raise SystemExit(2)
