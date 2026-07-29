#!/usr/bin/env python3
"""Run real upstream Hermes through AgentScope and verify the online-evaluation loop."""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import time
import uuid
from pathlib import Path
from typing import Any

import requests

from agentscope_sdk.adapters import AdapterSession, HermesAdapter
from agentscope_sdk.runtime import init as agent_init
from agentscope_sdk.runtime import shutdown_runtime
from agentscope_sdk.tracing import configure_tracing, force_flush, shutdown_tracing

HERMES_UPSTREAM_COMMIT = "d9165d7a678d4105f42921a7fc1886df3804531b"
ONLINE_EVAL_TIMEOUT_SECONDS = 90


def require_env(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise RuntimeError(f"missing required environment variable: {name}")
    return value


def api(session: requests.Session, method: str, base_url: str, path: str, **kwargs: Any) -> dict[str, Any] | list[Any]:
    response = session.request(method, base_url + path, timeout=30, **kwargs)
    if response.status_code >= 400:
        raise RuntimeError(f"{method} {path}: {response.status_code} {response.text[:300]}")
    return response.json() if response.content else {}


def write_private_json(path: Path, value: dict[str, Any]) -> None:
    """Atomically persist local demo state without allowing group/world reads."""
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp_path = path.with_name(f".{path.name}.{uuid.uuid4().hex}.tmp")
    try:
        fd = os.open(tmp_path, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
        with os.fdopen(fd, "w", encoding="utf-8") as output:
            json.dump(value, output, ensure_ascii=False, indent=2)
            output.write("\n")
        os.chmod(tmp_path, 0o600)
        os.replace(tmp_path, path)
        os.chmod(path, 0o600)
    finally:
        if tmp_path.exists():
            tmp_path.unlink()


def read_json_object(path: Path, description: str) -> dict[str, Any]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise RuntimeError(f"cannot read {description}: {path}") from exc
    if not isinstance(value, dict):
        raise RuntimeError(f"{description} must contain a JSON object: {path}")
    return value


def required_value(value: dict[str, Any], name: str) -> str:
    item = str(value.get(name, "")).strip()
    if not item:
        raise RuntimeError(f"missing {name} in result data")
    return item


def validated_uuid(value: str, name: str) -> str:
    try:
        return str(uuid.UUID(value))
    except (AttributeError, ValueError) as exc:
        raise RuntimeError(f"invalid UUID for {name}") from exc


def default_credentials_file(result_file: Path) -> Path:
    return result_file.with_name(f"{result_file.name}.credentials.json")


def register_project(session: requests.Session, base_url: str) -> tuple[dict[str, str], str, str]:
    suffix = uuid.uuid4().hex[:8]
    credentials = {
        "email": f"hermes-agentops-{suffix}@example.com",
        "password": "demo-password-123",
    }
    user = api(
        session,
        "POST",
        base_url,
        "/api/auth/register",
        json={"email": credentials["email"], "password": credentials["password"], "display_name": "Hermes AgentOps"},
    )
    assert isinstance(user, dict)
    project = api(
        session,
        "POST",
        base_url,
        "/api/projects",
        json={"name": f"Hermes AgentOps {suffix}", "description": "真实 Hermes 上游 Agent 可观测演示"},
    )
    assert isinstance(project, dict)
    token = api(
        session,
        "POST",
        base_url,
        f"/api/projects/{project['id']}/tokens",
        json={"description": "hermes-agentops", "scopes": ["otlp:write", "agent:register", "agent:runtime"]},
    )
    assert isinstance(token, dict)
    return credentials, str(project["id"]), str(token["plain_token"])


def login(session: requests.Session, base_url: str, credentials: dict[str, Any]) -> None:
    email = required_value(credentials, "email")
    password = required_value(credentials, "password")
    user = api(session, "POST", base_url, "/api/auth/login", json={"email": email, "password": password})
    assert isinstance(user, dict)


def compose_query(service: str, query: str) -> str:
    root = os.environ.get("AGENTSCOPE_COMPOSE_DIR", "")
    command = ["docker", "compose"]
    if root:
        command.extend(["--project-directory", root])
    if service == "postgres":
        command.extend(
            [
                "exec",
                "-T",
                "postgres",
                "psql",
                "-U",
                os.environ.get("AGENTSCOPE_POSTGRES_USER", "agentscope"),
                "-d",
                os.environ.get("AGENTSCOPE_POSTGRES_DATABASE", "agentscope_test"),
                "-At",
                "-c",
                query,
            ]
        )
    else:
        command.extend(
            [
                "exec",
                "-T",
                "clickhouse",
                "clickhouse-client",
                "--database",
                os.environ.get("AGENTSCOPE_CLICKHOUSE_DATABASE", "agentscope_traces"),
                "--format",
                "TSVRaw",
                "--query",
                query,
            ]
        )
    return subprocess.run(command, check=True, text=True, capture_output=True).stdout.strip()


def create_full_sampling_rule(session: requests.Session, base_url: str, project_id: str) -> str:
    # This short-lived demo project contains only Hermes. Leave agent_id empty: the current durable
    # worker selects rules from the Run's agent_name, while sampling rules are keyed by UUID agent_id.
    # A project-wide rule is therefore the precise, portable way to guarantee the demo is selected.
    rule = api(
        session,
        "POST",
        base_url,
        f"/api/projects/{project_id}/sampling-rules",
        json={
            "name": "Hermes 100% online evaluation",
            "sample_rate": 1.0,
            "filters": "{}",
            "strategy": "random",
            "daily_budget": 0,
            "stratification": "{}",
        },
    )
    assert isinstance(rule, dict)
    return required_value(rule, "id")


def wait_for_online_eval_jobs(
    session: requests.Session,
    base_url: str,
    project_id: str,
    success_run_id: str,
    failed_run_id: str,
    timeout_seconds: int = ONLINE_EVAL_TIMEOUT_SECONDS,
) -> dict[str, dict[str, Any]]:
    """Wait for durable jobs rather than assuming a request-scoped evaluator finished."""
    run_ids = {"success": success_run_id, "failed": failed_run_id}
    deadline = time.monotonic() + timeout_seconds
    last_jobs: dict[str, dict[str, Any]] = {}
    while time.monotonic() < deadline:
        complete = True
        for label, run_id in run_ids.items():
            response = api(session, "GET", base_url, f"/api/projects/{project_id}/online-eval-jobs?run_id={run_id}")
            assert isinstance(response, list)
            if not response:
                complete = False
                continue
            job = response[0]
            if not isinstance(job, dict):
                raise RuntimeError("online evaluation job API returned an invalid payload")
            last_jobs[label] = job
            if str(job.get("status", "")) not in {"succeeded", "skipped", "failed", "dead_letter"}:
                complete = False
        if complete and len(last_jobs) == len(run_ids):
            failures = {
                label: str(job.get("status", ""))
                for label, job in last_jobs.items()
                if str(job.get("status", "")) != "succeeded"
            }
            if failures:
                raise RuntimeError(f"online evaluation did not succeed: {failures}")
            failed_annotation_id = required_value(last_jobs["failed"], "annotation_item_id")
            if not failed_annotation_id:
                raise RuntimeError("failed Hermes run did not automatically return to the annotation queue")
            return last_jobs
        time.sleep(0.5)
    statuses = {label: job.get("status", "not_created") for label, job in last_jobs.items()}
    raise RuntimeError(f"timed out waiting for online evaluation jobs after {timeout_seconds}s: {statuses}")


def verify_evidence(
    session: requests.Session,
    base_url: str,
    project_id: str,
    agent_id: str,
    sampling_rule_id: str,
    success_run_id: str,
    failed_run_id: str,
    success_job_id: str,
    failed_job_id: str,
    annotation_item_id: str,
) -> dict[str, Any]:
    project_id = validated_uuid(project_id, "project_id")
    agent_id = validated_uuid(agent_id, "agent_id")
    sampling_rule_id = validated_uuid(sampling_rule_id, "sampling_rule_id")
    success_run_id = validated_uuid(success_run_id, "success_run_id")
    failed_run_id = validated_uuid(failed_run_id, "failed_run_id")
    success_job_id = validated_uuid(success_job_id, "success_online_eval_job_id")
    failed_job_id = validated_uuid(failed_job_id, "failed_online_eval_job_id")
    annotation_item_id = validated_uuid(annotation_item_id, "annotation_item_id")

    runs = api(session, "GET", base_url, f"/api/projects/{project_id}/runs")
    assert isinstance(runs, list)
    found = {str(item.get("id")): item for item in runs if isinstance(item, dict)}
    if success_run_id not in found or failed_run_id not in found:
        raise RuntimeError("API verification failed: expected Hermes runs were not listed")
    if found[success_run_id].get("status") != "completed" or found[failed_run_id].get("status") != "failed":
        raise RuntimeError("API verification failed: Hermes success/failure statuses are incorrect")
    detail = api(session, "GET", base_url, f"/api/runs/{success_run_id}")
    assert isinstance(detail, dict)
    if not detail.get("spans", []):
        raise RuntimeError("API verification failed: successful Hermes run has no spans")
    agents = api(session, "GET", base_url, f"/api/v1/agents?project_id={project_id}")
    assert isinstance(agents, list)
    if not any(str(item.get("id")) == agent_id for item in agents if isinstance(item, dict)):
        raise RuntimeError("API verification failed: Hermes agent is not online in project agent list")

    jobs = wait_for_online_eval_jobs(session, base_url, project_id, success_run_id, failed_run_id)
    if required_value(jobs["success"], "id") != success_job_id or required_value(jobs["failed"], "id") != failed_job_id:
        raise RuntimeError("API verification failed: online evaluation job IDs changed unexpectedly")
    if required_value(jobs["failed"], "annotation_item_id") != annotation_item_id:
        raise RuntimeError("API verification failed: failed job is not linked to its automatic annotation item")
    items = api(session, "GET", base_url, f"/api/projects/{project_id}/annotation-items")
    assert isinstance(items, list)
    annotation = next((item for item in items if isinstance(item, dict) and str(item.get("id")) == annotation_item_id), None)
    if annotation is None or str(annotation.get("run_id")) != failed_run_id:
        raise RuntimeError("API verification failed: automatic annotation item is missing or points at the wrong Run")

    pg_runs = compose_query(
        "postgres",
        f"SELECT id || '|' || status || '|' || trace_id FROM runs WHERE id IN ('{success_run_id}', '{failed_run_id}') ORDER BY id;",
    )
    pg_rule = compose_query(
        "postgres", f"SELECT id || '|' || sample_rate || '|' || daily_budget FROM trace_sampling_rules WHERE id = '{sampling_rule_id}';"
    )
    pg_jobs = compose_query(
        "postgres",
        "SELECT id || '|' || run_id || '|' || status || '|' || sampling_reason || '|' || COALESCE(annotation_item_id::text,'') "
        f"FROM online_eval_jobs WHERE id IN ('{success_job_id}', '{failed_job_id}') ORDER BY run_id;",
    )
    pg_annotation = compose_query(
        "postgres",
        f"SELECT id || '|' || run_id || '|' || trace_id || '|' || online_eval_job_id FROM annotation_items WHERE id = '{annotation_item_id}';",
    )
    pg_scores = compose_query(
        "postgres", f"SELECT count(*) FROM eval_scores WHERE online_eval_job_id IN ('{success_job_id}', '{failed_job_id}');"
    )
    ch_spans = compose_query("clickhouse", f"SELECT count() FROM trace_spans WHERE run_id = '{success_run_id}';")
    ch_thoughts = compose_query("clickhouse", f"SELECT count() FROM trace_thoughts WHERE run_id = '{success_run_id}';")
    ch_llm = compose_query("clickhouse", f"SELECT count() FROM trace_llm_calls WHERE run_id = '{success_run_id}';")
    ch_tools = compose_query("clickhouse", f"SELECT count() FROM trace_tool_calls WHERE run_id = '{success_run_id}';")
    ch_memory = compose_query(
        "clickhouse", f"SELECT count() FROM trace_tool_calls WHERE run_id = '{success_run_id}' AND tool_name = 'memory';"
    )
    ch_skills = compose_query("clickhouse", f"SELECT count() FROM trace_skill_calls WHERE run_id = '{success_run_id}';")
    ch_answers = compose_query(
        "clickhouse", f"SELECT count() FROM trace_final_answers WHERE run_id = '{success_run_id}';"
    )
    counts = {
        "trace_spans": int(ch_spans or "0"),
        "trace_thoughts": int(ch_thoughts or "0"),
        "trace_llm_calls": int(ch_llm or "0"),
        "trace_tool_calls": int(ch_tools or "0"),
        "memory_tool_calls": int(ch_memory or "0"),
        "trace_skill_calls": int(ch_skills or "0"),
        "trace_final_answers": int(ch_answers or "0"),
        "online_eval_scores": int(pg_scores or "0"),
    }
    if (
        not pg_runs
        or not pg_rule
        or not pg_jobs
        or not pg_annotation
        or any(count < 1 for count in counts.values())
    ):
        raise RuntimeError(
            "database verification failed: missing Hermes run, sampling, online-evaluation, automatic-annotation, or trace evidence"
        )
    return {
        "postgres_runs": pg_runs,
        "postgres_sampling_rule": pg_rule,
        "postgres_online_eval_jobs": pg_jobs,
        "postgres_annotation": pg_annotation,
        **counts,
    }


def verify_only(base_url: str, result_file: Path, credentials_file: Path) -> dict[str, Any]:
    result = read_json_object(result_file, "result file")
    credentials = read_json_object(credentials_file, "private credentials file")
    http = requests.Session()
    login(http, base_url, credentials)
    evidence = verify_evidence(
        http,
        base_url,
        required_value(result, "project_id"),
        required_value(result, "agent_id"),
        required_value(result, "sampling_rule_id"),
        required_value(result, "success_run_id"),
        required_value(result, "failed_run_id"),
        required_value(result, "success_online_eval_job_id"),
        required_value(result, "failed_online_eval_job_id"),
        required_value(result, "annotation_item_id"),
    )
    verified = {key: value for key, value in result.items() if key != "evidence"}
    verified["evidence"] = evidence
    write_private_json(result_file, verified)
    return verified


def run_demo(base_url: str, result_file: Path, credentials_file: Path) -> dict[str, Any]:
    base_llm_url = require_env("HERMES_LLM_BASE_URL")
    api_key = require_env("HERMES_LLM_API_KEY")
    model = require_env("HERMES_LLM_MODEL")
    provider = os.environ.get("HERMES_LLM_PROVIDER", "openai-compatible")

    from run_agent import AIAgent

    http = requests.Session()
    credentials, project_id, project_token = register_project(http, base_url)
    write_private_json(credentials_file, credentials)
    tracer = configure_tracing(
        base_url, project_token, service_name="hermes-agentops", capture_content=True, export_batch_size=1
    )
    runtime = agent_init(
        server_url=base_url,
        project_token=project_token,
        agent_name="hermes-agent",
        description="NousResearch Hermes Agent with AgentScope observer hooks",
        heartbeat_interval=1,
        agent_version=HERMES_UPSTREAM_COMMIT[:12],
        framework="hermes",
        runtime="python",
        source="adapter",
        model_provider=provider,
        model_name=model,
        capabilities=[
            {"name": "memory", "description": "Hermes persistent memory", "metadata": {"kind": "memory"}},
            {"name": "skills", "description": "Hermes skill system", "metadata": {"kind": "skill"}},
            {"name": "tools", "description": "Hermes tools", "metadata": {"kind": "tool"}},
            {"name": "approval", "description": "Hermes approval lifecycle", "metadata": {"kind": "guardrail"}},
        ],
    )
    try:
        # Keep the process alive long enough to prove repeated heartbeat reporting.
        time.sleep(2.2)
        sampling_rule_id = create_full_sampling_rule(http, base_url, project_id)
        session = AdapterSession(
            runtime,
            tracer,
            metadata={
                "adapter_name": "hermes",
                "framework": "hermes",
                "hermes.upstream_commit": HERMES_UPSTREAM_COMMIT,
            },
        )
        adapter = HermesAdapter(session, capture_reasoning=True)
        agent = AIAgent(
            base_url=base_llm_url,
            api_key=api_key,
            provider=provider,
            model=model,
            enabled_toolsets=["memory", "skills", "file"],
            max_iterations=6,
            quiet_mode=True,
            skip_context_files=True,
            session_id=f"agentscope-hermes-{uuid.uuid4().hex[:8]}",
        )
        prompt = (
            "Use the memory tool to store that the demo customer prefers Chinese, read it back, list available skills, "
            "then answer in one concise Chinese sentence confirming the preference."
        )
        adapter.run(agent, prompt, task_id="hermes-success")
        success_run_id = session.last_run_id
        if not success_run_id:
            raise RuntimeError("Hermes adapter did not retain the successful runtime run ID")

        failed_session = AdapterSession(
            runtime,
            tracer,
            metadata={
                "adapter_name": "hermes",
                "framework": "hermes",
                "hermes.upstream_commit": HERMES_UPSTREAM_COMMIT,
            },
        )
        failed_adapter = HermesAdapter(failed_session, capture_reasoning=True)
        bad_agent = AIAgent(
            base_url="http://127.0.0.1:1/v1",
            api_key="unused",
            provider=provider,
            model=model,
            enabled_toolsets=[],
            max_iterations=1,
            quiet_mode=True,
            skip_context_files=True,
            session_id=f"agentscope-hermes-failure-{uuid.uuid4().hex[:8]}",
        )
        try:
            failed_adapter.run(bad_agent, "Reply with one word.", task_id="hermes-failure")
        except Exception:
            pass
        else:
            raise RuntimeError("expected Hermes network failure did not occur")
        force_flush()
        failed_run_id = failed_session.last_run_id
        if not failed_run_id:
            raise RuntimeError("Hermes adapter did not retain the failed runtime run ID")

        jobs = wait_for_online_eval_jobs(http, base_url, project_id, success_run_id, failed_run_id)
        result = {
            "project_id": project_id,
            "agent_id": runtime.agent_id,
            "sampling_rule_id": sampling_rule_id,
            "success_run_id": success_run_id,
            "failed_run_id": failed_run_id,
            "success_online_eval_job_id": required_value(jobs["success"], "id"),
            "failed_online_eval_job_id": required_value(jobs["failed"], "id"),
            "annotation_item_id": required_value(jobs["failed"], "annotation_item_id"),
        }
        result["evidence"] = verify_evidence(
            http,
            base_url,
            project_id=project_id,
            agent_id=runtime.agent_id,
            sampling_rule_id=sampling_rule_id,
            success_run_id=success_run_id,
            failed_run_id=failed_run_id,
            success_job_id=result["success_online_eval_job_id"],
            failed_job_id=result["failed_online_eval_job_id"],
            annotation_item_id=result["annotation_item_id"],
        )
        write_private_json(result_file, result)
        return result
    finally:
        shutdown_runtime()
        shutdown_tracing()


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--server-url", default="http://127.0.0.1:8080")
    parser.add_argument("--result-file", help="Path for the non-secret run identifiers and verification evidence")
    parser.add_argument("--credentials-file", help="Private 0600 file containing only the local demo login credentials")
    parser.add_argument("--verify-only", metavar="RESULT_FILE", help="Re-verify an existing result without invoking Hermes or an LLM")
    args = parser.parse_args()
    base_url = args.server_url.rstrip("/")

    if args.verify_only:
        if args.result_file:
            parser.error("--result-file cannot be combined with --verify-only; pass the result path to --verify-only")
        result_file = Path(args.verify_only)
        credentials_file = Path(args.credentials_file) if args.credentials_file else default_credentials_file(result_file)
        result = verify_only(base_url, result_file, credentials_file)
    else:
        if not args.result_file:
            parser.error("--result-file is required unless --verify-only is used")
        result_file = Path(args.result_file)
        credentials_file = Path(args.credentials_file) if args.credentials_file else default_credentials_file(result_file)
        result = run_demo(base_url, result_file, credentials_file)

    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
