#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_URL="${AGENTSCOPE_SERVER_URL:-http://127.0.0.1:8080}"
export NO_PROXY="${NO_PROXY:-127.0.0.1,localhost}"
export no_proxy="${no_proxy:-${NO_PROXY}}"
TARGETS_FILE="${AGENTSCOPE_LOAD_TARGETS_FILE:-}"
TARGETS_OUTPUT="${AGENTSCOPE_LOAD_TARGETS_OUTPUT:-}"
REPORT_FILE="${AGENTSCOPE_LOAD_REPORT:-${ROOT_DIR}/.tmp/phase4-load-report.json}"
REQUESTS="${AGENTSCOPE_LOAD_REQUESTS:-50}"
CONCURRENCY="${AGENTSCOPE_LOAD_CONCURRENCY:-4}"
BATCH_SIZE="${AGENTSCOPE_LOAD_BATCH_SIZE:-10}"
MAX_P95="${AGENTSCOPE_LOAD_MAX_P95:-2s}"
MAX_ERROR_RATE="${AGENTSCOPE_LOAD_MAX_ERROR_RATE:-0.01}"
MAX_DROP_RATE="${AGENTSCOPE_LOAD_MAX_DROP_RATE:-0.01}"

temporary_targets=""

log() {
  printf '[phase4-load] %s\n' "$*" >&2
}

cleanup() {
  if [[ -n "${temporary_targets}" ]]; then
    rm -f "${temporary_targets}"
  fi
}
trap cleanup EXIT

mkdir -p "$(dirname "${REPORT_FILE}")"

if ! curl --silent --show-error --fail "${SERVER_URL}/healthz" >/dev/null; then
  log "backend is not ready at ${SERVER_URL}"
  exit 1
fi

if [[ -z "${TARGETS_FILE}" ]]; then
  if [[ -n "${TARGETS_OUTPUT}" ]]; then
    TARGETS_FILE="${TARGETS_OUTPUT}"
    mkdir -p "$(dirname "${TARGETS_FILE}")"
  else
    TARGETS_FILE="$(mktemp)"
    temporary_targets="${TARGETS_FILE}"
  fi
  log "provisioning isolated load-test project over HTTP"
  SERVER_URL="${SERVER_URL}" TARGETS_FILE="${TARGETS_FILE}" python3 - <<'PY'
import http.cookiejar
import json
import os
import secrets
import urllib.request

base = os.environ["SERVER_URL"].rstrip("/")
target_path = os.environ["TARGETS_FILE"]
cookies = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookies))

def api(method, path, body=None, token=None):
    data = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(base + path, data=data, method=method)
    request.add_header("Content-Type", "application/json")
    if token:
        request.add_header("Authorization", "Bearer " + token)
    with opener.open(request, timeout=15) as response:
        return json.load(response)

suffix = secrets.token_hex(5)
email = f"phase4-load-{suffix}@example.com"
api("POST", "/api/auth/register", {
    "email": email,
    "password": "phase4-load-password",
    "display_name": "Phase 4 Load Test",
})
project = api("POST", "/api/projects", {
    "name": f"phase4-load-{suffix}",
    "description": "Automated capacity and recovery acceptance",
})
token_response = api("POST", f"/api/projects/{project['id']}/tokens", {
    "description": "phase4 load test",
    "scopes": ["otlp:write", "agent:register", "agent:runtime"],
})
token = token_response["plain_token"]
agent = api("POST", "/api/v1/agents/register", {
    "name": "phase4-load-agent",
    "description": "Capacity test traffic generator",
    "metadata": {
        "agent_version": "load-v1",
        "environment": "load",
        "source": "agentscope-loadtest",
    },
}, token)
instance_id = "phase4-load-instance-" + suffix
api("POST", f"/api/v1/agents/{agent['id']}/heartbeat", {
    "instance_id": instance_id,
    "instance_name": instance_id,
    "status": "active",
    "payload": {"source": "agentscope-loadtest"},
}, token)
session = next(cookie.value for cookie in cookies if cookie.name == "session")
targets = [{
    "project_id": project["id"],
    "token": token,
    "session_cookie": session,
    "agent_id": agent["id"],
    "instance_id": instance_id,
    "agent_name": "phase4-load-agent",
    "agent_version": "load-v1",
    "environment": "load",
}]
with open(target_path, "w", encoding="utf-8") as stream:
    json.dump(targets, stream)
os.chmod(target_path, 0o600)
print(project["id"])
PY
fi

log "running HTTP OTLP + Runtime + SSE load"
(
  cd "${ROOT_DIR}"
  go run ./cmd/agentscope-loadtest \
    --server-url "${SERVER_URL}" \
    --targets "${TARGETS_FILE}" \
    --requests "${REQUESTS}" \
    --concurrency "${CONCURRENCY}" \
    --batch-size "${BATCH_SIZE}" \
    --seed 4242 \
    --sse \
    --runtime \
    --max-p95 "${MAX_P95}" \
    --max-error-rate "${MAX_ERROR_RATE}" \
    --max-drop-rate "${MAX_DROP_RATE}" \
    --output json > "${REPORT_FILE}"
)

python3 - "${REPORT_FILE}" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as stream:
    report = json.load(stream)
assert report["schema_version"] == "agentscope-load-report-v1"
assert report["passed"] is True
assert report["requests"] > 0
assert report["spans"] > 0
assert report["sse"]["connections"] > 0
print(
    "PASS requests={requests} spans={spans} p95={p95:.2f}ms "
    "error_rate={error_rate:.4f} drop_rate={drop_rate:.4f}".format(
        requests=report["requests"],
        spans=report["spans"],
        p95=report["latency"]["p95_ms"],
        error_rate=report["error_rate"],
        drop_rate=report["drop_rate"],
    )
)
PY

log "report written to ${REPORT_FILE}"
