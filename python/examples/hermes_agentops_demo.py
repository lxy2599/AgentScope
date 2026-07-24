#!/usr/bin/env python3
"""Run real upstream Hermes with AgentScope observability and verify the evidence."""

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


def register_project(session: requests.Session, base_url: str) -> tuple[str, str, str, str]:
    suffix = uuid.uuid4().hex[:8]
    email = f"hermes-agentops-{suffix}@example.com"
    password = "demo-password-123"
    user = api(
        session,
        "POST",
        base_url,
        "/api/auth/register",
        json={"email": email, "password": password, "display_name": "Hermes AgentOps"},
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
    return email, password, str(project["id"]), str(token["plain_token"])


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


def verify_evidence(
    session: requests.Session,
    base_url: str,
    project_id: str,
    agent_id: str,
    success_run_id: str,
    failed_run_id: str,
    annotation_item_id: str,
) -> dict[str, Any]:
    runs = api(session, "GET", base_url, f"/api/projects/{project_id}/runs")
    assert isinstance(runs, list)
    found = {str(item.get("id")): item for item in runs}
    if success_run_id not in found or failed_run_id not in found:
        raise RuntimeError("API verification failed: expected Hermes runs were not listed")
    if found[success_run_id].get("status") != "completed" or found[failed_run_id].get("status") != "failed":
        raise RuntimeError("API verification failed: Hermes success/failure statuses are incorrect")
    detail = api(session, "GET", base_url, f"/api/runs/{success_run_id}")
    assert isinstance(detail, dict)
    spans = detail.get("spans", [])
    if not spans:
        raise RuntimeError("API verification failed: successful Hermes run has no spans")
    agents = api(session, "GET", base_url, f"/api/v1/agents?project_id={project_id}")
    assert isinstance(agents, list)
    if not any(str(item.get("id")) == agent_id for item in agents):
        raise RuntimeError("API verification failed: Hermes agent is not online in project agent list")

    pg_runs = compose_query(
        "postgres",
        f"SELECT id || '|' || status || '|' || trace_id FROM runs WHERE id IN ('{success_run_id}', '{failed_run_id}') ORDER BY id;",
    )
    pg_annotation = compose_query(
        "postgres",
        f"SELECT id || '|' || run_id || '|' || trace_id FROM annotation_items WHERE id = '{annotation_item_id}';",
    )
    ch_spans = compose_query("clickhouse", f"SELECT count() FROM trace_spans WHERE run_id = '{success_run_id}';")
    ch_llm = compose_query("clickhouse", f"SELECT count() FROM trace_llm_calls WHERE run_id = '{success_run_id}';")
    ch_tools = compose_query("clickhouse", f"SELECT count() FROM trace_tool_calls WHERE run_id = '{success_run_id}';")
    ch_answers = compose_query(
        "clickhouse", f"SELECT count() FROM trace_final_answers WHERE run_id = '{success_run_id}';"
    )
    if (
        not pg_runs
        or not pg_annotation
        or int(ch_spans or "0") < 1
        or int(ch_llm or "0") < 1
        or int(ch_answers or "0") < 1
    ):
        raise RuntimeError(
            "database verification failed: missing Hermes run, annotation, span, LLM, or final answer evidence"
        )
    return {
        "postgres_runs": pg_runs,
        "postgres_annotation": pg_annotation,
        "trace_spans": int(ch_spans),
        "trace_llm_calls": int(ch_llm),
        "trace_tool_calls": int(ch_tools or "0"),
        "trace_final_answers": int(ch_answers),
    }


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--server-url", default="http://127.0.0.1:8080")
    parser.add_argument("--result-file", required=True)
    args = parser.parse_args()
    base_url = args.server_url.rstrip("/")
    base_llm_url = require_env("HERMES_LLM_BASE_URL")
    api_key = require_env("HERMES_LLM_API_KEY")
    model = require_env("HERMES_LLM_MODEL")
    provider = os.environ.get("HERMES_LLM_PROVIDER", "openai-compatible")

    from run_agent import AIAgent

    http = requests.Session()
    email, password, project_id, project_token = register_project(http, base_url)
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
    # Keep the process alive long enough to prove repeated heartbeat reporting.
    time.sleep(2.2)
    session = AdapterSession(
        runtime,
        tracer,
        metadata={"adapter_name": "hermes", "framework": "hermes", "hermes.upstream_commit": HERMES_UPSTREAM_COMMIT},
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
    prompt = "Use the memory tool to store that the demo customer prefers Chinese, read it back, list available skills, then answer in one concise Chinese sentence confirming the preference."
    success_output = adapter.run(agent, prompt, task_id="hermes-success")
    success_run_id = session.last_run_id
    if not success_run_id:
        raise RuntimeError("Hermes adapter did not retain the successful runtime run ID")

    failed_session = AdapterSession(
        runtime,
        tracer,
        metadata={"adapter_name": "hermes", "framework": "hermes", "hermes.upstream_commit": HERMES_UPSTREAM_COMMIT},
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
    time.sleep(2)
    failed_run_id = failed_session.last_run_id
    if not failed_run_id:
        raise RuntimeError("Hermes adapter did not retain the failed runtime run ID")
    queues = api(http, "GET", base_url, f"/api/projects/{project_id}/annotation-queues")
    assert isinstance(queues, list)
    queue = (
        api(
            http,
            "POST",
            base_url,
            f"/api/projects/{project_id}/annotation-queues",
            json={"name": "Hermes 失败待标注", "description": "真实 Hermes 失败 trace"},
        )
        if not queues
        else queues[0]
    )
    assert isinstance(queue, dict)
    annotation = api(
        http,
        "POST",
        base_url,
        f"/api/projects/{project_id}/runs/{failed_run_id}/annotation-items",
        json={"queue_id": queue["id"]},
    )
    assert isinstance(annotation, dict)
    evidence = verify_evidence(
        http, base_url, project_id, runtime.agent_id, success_run_id, failed_run_id, str(annotation["id"])
    )
    result = {
        "email": email,
        "password": password,
        "project_id": project_id,
        "agent_id": runtime.agent_id,
        "success_run_id": success_run_id,
        "failed_run_id": failed_run_id,
        "annotation_item_id": annotation["id"],
        "success_output": success_output,
        "evidence": evidence,
    }
    Path(args.result_file).write_text(json.dumps(result, ensure_ascii=False, indent=2))
    print(json.dumps(result, ensure_ascii=False, indent=2))
    shutdown_runtime()
    shutdown_tracing()


if __name__ == "__main__":
    main()
