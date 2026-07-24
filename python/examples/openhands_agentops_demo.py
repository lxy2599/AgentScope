#!/usr/bin/env python3
"""Run the pinned upstream OpenHands SDK and prove AgentScope evidence end to end."""

from __future__ import annotations

import argparse
import json
import os
import threading
import time
import uuid
from pathlib import Path
from typing import Any

import httpx
from pydantic import SecretStr

from agentscope_sdk.adapters import AdapterSession, OpenHandsAdapter
from agentscope_sdk.runtime import init as agent_init
from agentscope_sdk.runtime import shutdown_runtime
from agentscope_sdk.tracing import (
    configure_tracing,
    force_flush,
    shutdown_tracing,
)


OPENHANDS_UPSTREAM_COMMIT = "a375c66aaf29c8d3b6553c9164e65ed917149e81"


def require_env(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise RuntimeError(f"missing required environment variable: {name}")
    return value


def api(
    client: httpx.Client,
    method: str,
    base_url: str,
    path: str,
    **kwargs: Any,
) -> dict[str, Any] | list[Any]:
    response = client.request(method, base_url + path, timeout=30, **kwargs)
    if response.status_code >= 400:
        raise RuntimeError(
            f"{method} {path}: {response.status_code} {response.text[:500]}"
        )
    return response.json() if response.content else {}


def register_project(
    client: httpx.Client,
    base_url: str,
) -> tuple[str, str, str, str]:
    suffix = uuid.uuid4().hex[:8]
    email = f"openhands-agentops-{suffix}@example.com"
    password = "demo-password-123"
    api(
        client,
        "POST",
        base_url,
        "/api/auth/register",
        json={
            "email": email,
            "password": password,
            "display_name": "OpenHands AgentOps",
        },
    )
    project = api(
        client,
        "POST",
        base_url,
        "/api/projects",
        json={
            "name": f"OpenHands AgentOps {suffix}",
            "description": "真实 OpenHands 官方 SDK 可观测演示",
        },
    )
    assert isinstance(project, dict)
    token = api(
        client,
        "POST",
        base_url,
        f"/api/projects/{project['id']}/tokens",
        json={
            "description": "openhands-agentops",
            "scopes": [
                "otlp:write",
                "agent:register",
                "agent:runtime",
            ],
        },
    )
    assert isinstance(token, dict)
    return email, password, str(project["id"]), str(token["plain_token"])


class SSEObserver:
    def __init__(
        self,
        base_url: str,
        project_id: str,
        cookies: httpx.Cookies,
    ) -> None:
        self.url = f"{base_url}/api/projects/{project_id}/events/stream"
        self.cookies = httpx.Cookies(cookies)
        self.events: list[dict[str, Any]] = []
        self.error = ""
        self._ready = threading.Event()
        self._stop = threading.Event()
        self._response: httpx.Response | None = None
        self._thread = threading.Thread(target=self._consume, daemon=True)

    def start(self) -> None:
        self._thread.start()
        if not self._ready.wait(timeout=10):
            raise RuntimeError("SSE connection did not become ready")
        if self.error:
            raise RuntimeError(f"SSE connection failed: {self.error}")

    def stop(self) -> None:
        self._stop.set()
        if self._response is not None:
            self._response.close()
        self._thread.join(timeout=5)

    def _consume(self) -> None:
        current: dict[str, str] = {}
        try:
            timeout = httpx.Timeout(connect=10, read=None, write=10, pool=10)
            with httpx.Client(cookies=self.cookies, timeout=timeout) as client:
                with client.stream("GET", self.url) as response:
                    self._response = response
                    response.raise_for_status()
                    self._ready.set()
                    for line in response.iter_lines():
                        if self._stop.is_set():
                            return
                        if not line:
                            self._append(current)
                            current = {}
                            continue
                        if line.startswith(":"):
                            continue
                        field, _, value = line.partition(":")
                        current[field] = value.lstrip()
        except Exception as exc:
            if not self._stop.is_set():
                self.error = str(exc)
                self._ready.set()

    def _append(self, fields: dict[str, str]) -> None:
        raw = fields.get("data")
        if not raw:
            return
        try:
            payload = json.loads(raw)
        except ValueError:
            return
        if not isinstance(payload, dict):
            return
        payload["received_at_unix"] = time.time()
        payload["sse_event"] = fields.get("event", "")
        self.events.append(payload)
        print(
            "  [SSE]"
            f" +{payload['received_at_unix']:.3f}"
            f" {payload.get('type') or payload.get('sse_event')}"
            f" run={payload.get('run_id') or '-'}"
        )


def wait_for_trace(
    client: httpx.Client,
    base_url: str,
    run_id: str,
    timeout: float = 30,
) -> dict[str, Any]:
    deadline = time.time() + timeout
    while time.time() < deadline:
        detail = api(client, "GET", base_url, f"/api/runs/{run_id}")
        assert isinstance(detail, dict)
        if detail.get("spans"):
            return detail
        time.sleep(0.5)
    raise RuntimeError("OpenHands trace did not become queryable before timeout")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--server-url", default="http://127.0.0.1:8080")
    parser.add_argument("--workspace", required=True)
    parser.add_argument("--result-file", required=True)
    args = parser.parse_args()

    base_url = args.server_url.rstrip("/")
    workspace = Path(args.workspace).resolve()
    workspace.mkdir(parents=True, exist_ok=True)
    api_key = require_env("OPENHANDS_LLM_API_KEY")
    base_llm_url = require_env("OPENHANDS_LLM_BASE_URL")
    model = require_env("OPENHANDS_LLM_MODEL")
    input_cost_per_million = float(
        require_env("OPENHANDS_LLM_INPUT_COST_PER_MILLION")
    )
    output_cost_per_million = float(
        require_env("OPENHANDS_LLM_OUTPUT_COST_PER_MILLION")
    )
    if input_cost_per_million <= 0 or output_cost_per_million <= 0:
        raise ValueError("OpenHands LLM token prices must be positive")
    provider = os.environ.get(
        "OPENHANDS_LLM_PROVIDER",
        "openai-compatible",
    )

    from openhands.sdk import Agent, Conversation, LLM, Tool
    from openhands.tools.file_editor import FileEditorTool
    from openhands.tools.terminal import TerminalTool

    print("=" * 64)
    print("AgentScope × OpenHands 真实 AgentOps 演示")
    print("=" * 64)
    client = httpx.Client()
    email, password, project_id, project_token = register_project(client, base_url)
    observer = SSEObserver(base_url, project_id, client.cookies)
    observer.start()

    tracer = configure_tracing(
        base_url,
        project_token,
        service_name="openhands-agentops",
        capture_content=True,
        export_batch_size=1,
    )
    runtime = agent_init(
        server_url=base_url,
        project_token=project_token,
        agent_name="openhands-software-agent",
        description="Pinned official OpenHands SDK with AgentScope callbacks",
        heartbeat_interval=1,
        agent_version=OPENHANDS_UPSTREAM_COMMIT[:12],
        framework="openhands",
        runtime="python",
        source="adapter",
        model_provider=provider,
        model_name=model,
        harness_version="1.37.0",
        capabilities=[
            {
                "name": "TerminalTool",
                "description": "OpenHands terminal execution",
                "metadata": {"kind": "tool", "side_effect_risk": "high"},
            },
            {
                "name": "FileEditorTool",
                "description": "OpenHands file editing",
                "metadata": {"kind": "tool", "side_effect_risk": "medium"},
            },
        ],
    )
    session = AdapterSession(
        runtime,
        tracer,
        metadata={
            "adapter_name": "openhands",
            "framework": "openhands",
            "openhands.upstream_commit": OPENHANDS_UPSTREAM_COMMIT,
            "model_provider": provider,
            "model_name": model,
        },
    )
    adapter = OpenHandsAdapter(
        session,
        capture_reasoning=True,
        capture_system_prompt=False,
        metrics_provider=lambda: llm.metrics,
        metrics_cost_is_estimate=True,
    )

    llm = LLM(
        model=model,
        api_key=SecretStr(api_key),
        base_url=base_llm_url,
        usage_id="agentscope-openhands-demo",
        stream=True,
        input_cost_per_token=input_cost_per_million / 1_000_000,
        output_cost_per_token=output_cost_per_million / 1_000_000,
    )
    agent = Agent(
        llm=llm,
        tools=[
            Tool(name=TerminalTool.name),
            Tool(name=FileEditorTool.name),
        ],
    )
    conversation = Conversation(
        agent=agent,
        workspace=str(workspace),
        callbacks=adapter.callbacks,
        token_callbacks=adapter.token_callbacks,
        visualizer=None,
        max_iteration_per_run=12,
    )
    prompt = """Complete this task in the given workspace using the provided tools.
1. Use the terminal to inspect the workspace with pwd and ls.
2. Deliberately run `test -f missing-demo-file.txt`; this must fail once and is an
   expected recoverable failure. Do not stop after that failure.
3. Use FileEditorTool to create `agentscope_openhands_demo.txt` with exactly:
   AgentScope observed OpenHands successfully.
4. Use the terminal to verify the file with cat and grep.
5. Finish with a concise Chinese summary mentioning the recovered failure and the
   successful file verification. Do not merely describe commands; execute them."""

    try:
        print("\n[执行] OpenHands 正在检查、编辑并验证工作区")
        output = adapter.run(
            conversation,
            prompt,
            task_id="openhands-real-agentops",
        )
        run_id = session.last_run_id
        finished_at = time.time()
        if not run_id:
            raise RuntimeError("OpenHands adapter did not retain run ID")
        force_flush()
        detail = wait_for_trace(client, base_url, run_id)
        run_detail = detail.get("run") or {}
        trace_id = str(
            run_detail.get("trace_id")
            if isinstance(run_detail, dict)
            else ""
        )
        if not trace_id:
            raise RuntimeError("OpenHands run has no persisted trace ID")
        snapshot = api(client, "GET", base_url, f"/api/runs/{run_id}/snapshot")
        assert isinstance(snapshot, dict)
        time.sleep(2)

        run_events = [
            event
            for event in observer.events
            if str(event.get("run_id") or "") == run_id
        ]
        semantic_events = [
            event
            for event in run_events
            if event.get("type") == "run.semantic.appended"
            and float(event.get("received_at_unix") or 0) < finished_at
        ]
        if not semantic_events:
            raise RuntimeError(
                "SSE did not deliver a semantic event before run completion"
            )

        result = {
            "email": email,
            "password": password,
            "project_id": project_id,
            "agent_id": runtime.agent_id,
            "run_id": run_id,
            "trace_id": trace_id,
            "snapshot_id": snapshot["id"],
            "output": output,
            "workspace": str(workspace),
            "sse_nonterminal_before_finish": True,
            "sse_semantic_before_finish": True,
            "sse_events": run_events,
            "span_count": len(detail.get("spans") or []),
            "project_url": f"http://127.0.0.1:5173/projects/{project_id}",
            "run_url": f"http://127.0.0.1:5173/runs/{run_id}",
        }
        Path(args.result_file).write_text(
            json.dumps(result, ensure_ascii=False, indent=2),
            encoding="utf-8",
        )
        print("\n[结果]")
        print(json.dumps(result, ensure_ascii=False, indent=2))
    finally:
        observer.stop()
        conversation.close()
        shutdown_runtime()
        shutdown_tracing()
        client.close()


if __name__ == "__main__":
    main()
