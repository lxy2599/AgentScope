#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_URL="${AGENTSCOPE_SERVER_URL:-http://127.0.0.1:8080}"
export NO_PROXY="${NO_PROXY:-127.0.0.1,localhost}"
export no_proxy="${no_proxy:-${NO_PROXY}}"
COMPOSE=(docker compose -f docker-compose.yml -f docker-compose.phase4.yml)
TARGETS_FILE="$(mktemp)"
LOAD_REPORT="$(mktemp)"
REDIS_SERVICE="${REDIS_SERVICE:-redis}"
CLICKHOUSE_SERVICE="${CLICKHOUSE_SERVICE:-clickhouse}"
POSTGRES_DB="${AGENTSCOPE_POSTGRES_DB:-agentscope_test}"
redis_paused="false"
clickhouse_paused="false"

log() {
  printf '[phase4-recovery] %s\n' "$*" >&2
}

cleanup() {
  if [[ "${redis_paused}" == "true" ]]; then
    "${COMPOSE[@]}" unpause "${REDIS_SERVICE}" >/dev/null 2>&1 || true
  fi
  if [[ "${clickhouse_paused}" == "true" ]]; then
    "${COMPOSE[@]}" unpause "${CLICKHOUSE_SERVICE}" >/dev/null 2>&1 || true
  fi
  rm -f "${TARGETS_FILE}" "${LOAD_REPORT}"
}
trap cleanup EXIT

cd "${ROOT_DIR}"
if [[ -z "$("${COMPOSE[@]}" ps -q "${REDIS_SERVICE}")" ]] ||
   [[ -z "$("${COMPOSE[@]}" ps -q "${CLICKHOUSE_SERVICE}")" ]]; then
  log "Redis and ClickHouse must be running through the project compose stack"
  exit 1
fi

log "preparing one isolated project"
AGENTSCOPE_LOAD_TARGETS_OUTPUT="${TARGETS_FILE}" \
AGENTSCOPE_LOAD_REPORT="${LOAD_REPORT}" \
AGENTSCOPE_LOAD_REQUESTS=4 \
AGENTSCOPE_LOAD_CONCURRENCY=1 \
AGENTSCOPE_LOAD_BATCH_SIZE=2 \
  "${ROOT_DIR}/scripts/test-phase4-load.sh" >/dev/null

project_id="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))[0]["project_id"])' "${TARGETS_FILE}")"

log "starting sustained traffic and pausing Redis"
go run ./cmd/agentscope-loadtest \
  --server-url "${SERVER_URL}" \
  --targets "${TARGETS_FILE}" \
  --duration 10s \
  --rate 10 \
  --concurrency 2 \
  --batch-size 2 \
  --runtime \
  --output json > "${LOAD_REPORT}" &
load_pid="$!"
sleep 2
"${COMPOSE[@]}" pause "${REDIS_SERVICE}" >/dev/null
redis_paused="true"

log "verifying trace writes continue while Redis is unavailable"
go run ./cmd/agentscope-loadtest \
  --server-url "${SERVER_URL}" \
  --targets "${TARGETS_FILE}" \
  --requests 4 \
  --concurrency 1 \
  --batch-size 2 \
  --runtime \
  --max-error-rate 0.01 \
  --output json >/dev/null

pending="$("${COMPOSE[@]}" exec -T postgres psql -U agentscope -d "${POSTGRES_DB}" -tAc \
  "SELECT count(*) FROM realtime_outbox WHERE project_id='${project_id}' AND published_at IS NULL")"
if (( pending < 1 )); then
  log "expected pending Outbox rows while Redis is paused"
  exit 1
fi

"${COMPOSE[@]}" unpause "${REDIS_SERVICE}" >/dev/null
redis_paused="false"
recovery_started_ms="$(date +%s%3N)"
wait "${load_pid}"

log "waiting for Outbox relay to catch up"
for _ in {1..30}; do
  pending="$("${COMPOSE[@]}" exec -T postgres psql -U agentscope -d "${POSTGRES_DB}" -tAc \
    "SELECT count(*) FROM realtime_outbox WHERE project_id='${project_id}' AND published_at IS NULL")"
  if [[ "${pending}" == "0" ]]; then
    break
  fi
  sleep 1
done
if [[ "${pending}" != "0" ]]; then
  log "Outbox did not drain after Redis recovery: ${pending}"
  exit 1
fi
recovery_finished_ms="$(date +%s%3N)"
recovery_ms="$((recovery_finished_ms - recovery_started_ms))"
log "Redis recovered and Outbox drained in ${recovery_ms}ms"

log "verifying SSE reconnect"
go run ./cmd/agentscope-loadtest \
  --server-url "${SERVER_URL}" \
  --targets "${TARGETS_FILE}" \
  --requests 2 \
  --concurrency 1 \
  --batch-size 1 \
  --sse \
  --sse-after-id 1 \
  --max-error-rate 0.01 \
  --output json >"${LOAD_REPORT}"
python3 - "${LOAD_REPORT}" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as stream:
    report = json.load(stream)
if report["sse"]["resets"] < 1:
    raise SystemExit("SSE reconnect did not receive cursor_expired reset")
PY

log "pausing ClickHouse and requiring a diagnostic ingest failure"
"${COMPOSE[@]}" pause "${CLICKHOUSE_SERVICE}" >/dev/null
clickhouse_paused="true"
set +e
go run ./cmd/agentscope-loadtest \
  --server-url "${SERVER_URL}" \
  --targets "${TARGETS_FILE}" \
  --requests 1 \
  --concurrency 1 \
  --batch-size 1 \
  --http-timeout 3s \
  --max-error-rate 0.01 \
  --output json >/dev/null
clickhouse_exit="$?"
set -e
"${COMPOSE[@]}" unpause "${CLICKHOUSE_SERVICE}" >/dev/null
clickhouse_paused="false"
if [[ "${clickhouse_exit}" == "0" ]]; then
  log "ClickHouse outage was not surfaced by the ingest path"
  exit 1
fi

log "forcing SDK queue saturation and checking drop metrics"
(
  cd "${ROOT_DIR}/python"
  python -m pytest tests/test_export_metrics.py -q
)

log "PASS Redis recovery, Outbox replay, SSE reconnect, ClickHouse diagnosis, SDK drops"
