#!/usr/bin/env python3
"""Create an ingest-health project and verify API, run creation and ClickHouse evidence."""
from __future__ import annotations

import argparse
import subprocess
import sys
import uuid

import httpx
from agentscope_sdk.tracing import configure_tracing, force_flush, shutdown_tracing

PASSWORD = "demo-password-123"


def fail(message: str) -> None:
    print(f"[verify] FAIL: {message}", file=sys.stderr)
    raise SystemExit(1)


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--server-url", default="http://127.0.0.1:8080")
    args = parser.parse_args()
    client = httpx.Client(base_url=args.server_url, timeout=20)

    email = f"ingest-health-{uuid.uuid4().hex[:8]}@example.com"
    response = client.post("/api/auth/register", json={"email": email, "password": PASSWORD, "display_name": "Ingest Health Demo"})
    if response.status_code != 201:
        fail(f"register: {response.status_code} {response.text}")
    response = client.post("/api/projects", json={"name": f"Ingest Health {uuid.uuid4().hex[:8]}"})
    if response.status_code != 201:
        fail(f"project: {response.status_code} {response.text}")
    project_id = response.json()["id"]
    response = client.post(f"/api/projects/{project_id}/tokens", json={"description": "ingest-health-demo", "scopes": ["otlp:write"]})
    if response.status_code != 201:
        fail(f"token: {response.status_code} {response.text}")
    token = response.json()["plain_token"]

    tracer = configure_tracing(args.server_url, token, service_name="ingest-health-demo", export_batch_size=1)
    with tracer.start_as_current_span("agent.run") as span:
        span.set_attribute("agentscope.agent.name", "ingest-health-demo")
    force_flush()
    shutdown_tracing()

    # Authenticated malformed OTLP creates a rejected health event without exposing content.
    response = client.post("/v1/traces", content=b"not-otlp", headers={"Authorization": f"Bearer {token}", "Content-Type": "application/x-protobuf"})
    if response.status_code != 400:
        fail(f"expected rejected OTLP payload, got {response.status_code}")

    response = client.get(f"/api/projects/{project_id}/ingest-stats?minutes=15")
    if response.status_code != 200:
        fail(f"stats: {response.status_code} {response.text}")
    stats = response.json()
    if stats.get("accepted", 0) < 1 or stats.get("rejected", 0) < 1 or stats.get("missing_run_id", 0) < 1:
        fail(f"unexpected stats: {stats}")
    events = client.get(f"/api/projects/{project_id}/ingest-stats/events?minutes=15").json()
    if not any(event["outcome"] == "accepted" for event in events) or not any(event["outcome"] == "rejected_decode" for event in events):
        fail(f"missing expected health events: {events}")

    pg_query = f"SELECT count(*) FROM runs WHERE project_id = '{project_id}' AND source = 'live'"
    result = subprocess.run(["docker", "compose", "exec", "-T", "postgres", "psql", "-U", "agentscope", "-d", "agentscope_test", "-tAc", pg_query], capture_output=True, text=True)
    if result.returncode != 0 or int(result.stdout.strip() or "0") < 1:
        fail(f"PostgreSQL run association: {result.stderr.strip() or result.stdout.strip()}")

    query = f"SELECT count(), countIf(outcome='accepted'), countIf(outcome='rejected_decode') FROM trace_ingest_health_events WHERE project_id = '{project_id}'"
    result = subprocess.run(["docker", "compose", "exec", "-T", "clickhouse", "clickhouse-client", "--database", "agentscope_traces", "--query", query], capture_output=True, text=True)
    if result.returncode != 0:
        fail(f"ClickHouse query: {result.stderr.strip()}")
    total, accepted, rejected = (int(value) for value in result.stdout.strip().split("\t"))
    if total < 2 or accepted < 1 or rejected < 1:
        fail(f"ClickHouse health evidence: {result.stdout.strip()}")

    print("[verify] API + PostgreSQL run association + ClickHouse health evidence passed")
    print(f"[verify] project_id={project_id} email={email} password={PASSWORD}")


if __name__ == "__main__":
    main()
