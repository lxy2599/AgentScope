#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

POSTGRES_SERVICE="${POSTGRES_SERVICE:-postgres}"
CLICKHOUSE_SERVICE="${CLICKHOUSE_SERVICE:-clickhouse}"

AGENTSCOPE_HTTP_ADDR="${AGENTSCOPE_HTTP_ADDR:-:8080}"
AGENTSCOPE_SESSION_SECRET="${AGENTSCOPE_SESSION_SECRET:-agentscope-dev-secret}"

AGENTSCOPE_POSTGRES_PORT="${AGENTSCOPE_POSTGRES_PORT:-}"
AGENTSCOPE_CLICKHOUSE_PORT="${AGENTSCOPE_CLICKHOUSE_PORT:-}"
AGENTSCOPE_DATABASE_URL="${AGENTSCOPE_DATABASE_URL:-}"
AGENTSCOPE_CLICKHOUSE_DSN="${AGENTSCOPE_CLICKHOUSE_DSN:-}"

backend_pid=""
frontend_pid=""
cleanup_done="false"

log() {
  printf '[dev] %s\n' "$*" >&2
}

port_is_free() {
  local port="$1"
  ! ss -ltn "( sport = :${port} )" | grep -q ":${port}"
}

choose_port() {
  local preferred="$1"
  local fallback_start="$2"
  local port

  if port_is_free "${preferred}"; then
    printf '%s' "${preferred}"
    return
  fi

  for ((port = fallback_start; port < fallback_start + 200; port++)); do
    if port_is_free "${port}"; then
      log "port ${preferred} is busy; using ${port} instead"
      printf '%s' "${port}"
      return
    fi
  done

  log "could not find a free port near ${fallback_start}"
  return 1
}

published_port() {
  local service="$1"
  local target_port="$2"
  local endpoint

  endpoint="$(docker compose port "${service}" "${target_port}" 2>/dev/null || true)"
  if [[ -z "${endpoint}" ]]; then
    return 1
  fi

  printf '%s' "${endpoint##*:}"
}

configure_ports_and_urls() {
  if [[ -z "${AGENTSCOPE_POSTGRES_PORT}" ]]; then
    AGENTSCOPE_POSTGRES_PORT="$(published_port "${POSTGRES_SERVICE}" 5432 || choose_port 5432 15432)"
  fi
  if [[ -z "${AGENTSCOPE_CLICKHOUSE_PORT}" ]]; then
    AGENTSCOPE_CLICKHOUSE_PORT="$(published_port "${CLICKHOUSE_SERVICE}" 9000 || choose_port 19000 19100)"
  fi

  if [[ -z "${AGENTSCOPE_DATABASE_URL}" ]]; then
    AGENTSCOPE_DATABASE_URL="postgres://agentscope:agentscope@localhost:${AGENTSCOPE_POSTGRES_PORT}/agentscope_test?sslmode=disable"
  fi
  if [[ -z "${AGENTSCOPE_CLICKHOUSE_DSN}" ]]; then
    AGENTSCOPE_CLICKHOUSE_DSN="clickhouse://localhost:${AGENTSCOPE_CLICKHOUSE_PORT}/agentscope_traces"
  fi

  export AGENTSCOPE_POSTGRES_PORT
  export AGENTSCOPE_CLICKHOUSE_PORT
}

backend_url() {
  if [[ "${AGENTSCOPE_HTTP_ADDR}" == :* ]]; then
    printf 'http://127.0.0.1%s' "${AGENTSCOPE_HTTP_ADDR}"
    return
  fi
  if [[ "${AGENTSCOPE_HTTP_ADDR}" == http://* || "${AGENTSCOPE_HTTP_ADDR}" == https://* ]]; then
    printf '%s' "${AGENTSCOPE_HTTP_ADDR}"
    return
  fi
  printf 'http://%s' "${AGENTSCOPE_HTTP_ADDR}"
}

cleanup() {
  if [[ "${cleanup_done}" == "true" ]]; then
    return
  fi
  cleanup_done="true"

  log "stopping frontend/backend processes"
  if [[ -n "${frontend_pid}" ]] && kill -0 "${frontend_pid}" 2>/dev/null; then
    kill "${frontend_pid}" 2>/dev/null || true
  fi
  if [[ -n "${backend_pid}" ]] && kill -0 "${backend_pid}" 2>/dev/null; then
    kill "${backend_pid}" 2>/dev/null || true
  fi
  wait "${frontend_pid}" 2>/dev/null || true
  wait "${backend_pid}" 2>/dev/null || true
}

wait_for_postgres() {
  log "waiting for PostgreSQL"
  for _ in {1..60}; do
    if docker compose exec -T "${POSTGRES_SERVICE}" pg_isready -U agentscope -d agentscope_test >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  log "PostgreSQL did not become ready in time"
  return 1
}

wait_for_clickhouse() {
  log "waiting for ClickHouse"
  for _ in {1..60}; do
    if docker compose exec -T "${CLICKHOUSE_SERVICE}" clickhouse-client --query "SELECT 1" >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  log "ClickHouse did not become ready in time"
  return 1
}

apply_postgres_migration() {
  local exists
  exists="$(
    docker compose exec -T "${POSTGRES_SERVICE}" \
      psql -U agentscope -d agentscope_test -tAc "SELECT to_regclass('public.users') IS NOT NULL"
  )"

  if [[ "${exists}" == "t" ]]; then
    log "PostgreSQL schema already exists"
    return 0
  fi

  log "applying PostgreSQL migration"
  docker compose exec -T "${POSTGRES_SERVICE}" \
    psql -U agentscope -d agentscope_test < "${ROOT_DIR}/migrations/00001_init.sql"
}

apply_clickhouse_migration() {
  log "applying ClickHouse migration"
  docker compose exec -T "${CLICKHOUSE_SERVICE}" \
    clickhouse-client --database agentscope_traces --multiquery < "${ROOT_DIR}/clickhouse/migrations/00001_trace.sql"
}

start_backend() {
  log "starting backend at $(backend_url)"
  (
    cd "${ROOT_DIR}"
    AGENTSCOPE_HTTP_ADDR="${AGENTSCOPE_HTTP_ADDR}" \
      AGENTSCOPE_DATABASE_URL="${AGENTSCOPE_DATABASE_URL}" \
      AGENTSCOPE_CLICKHOUSE_DSN="${AGENTSCOPE_CLICKHOUSE_DSN}" \
      AGENTSCOPE_SESSION_SECRET="${AGENTSCOPE_SESSION_SECRET}" \
      go run ./cmd/agentscope-server
  ) &
  backend_pid="$!"
}

start_frontend() {
  if [[ ! -d "${ROOT_DIR}/web/node_modules" ]]; then
    log "installing frontend dependencies"
    (cd "${ROOT_DIR}/web" && npm install)
  fi

  log "starting frontend at http://127.0.0.1:5173"
  (cd "${ROOT_DIR}/web" && npm run dev) &
  frontend_pid="$!"
}

main() {
  cd "${ROOT_DIR}"
  trap cleanup INT TERM EXIT

  configure_ports_and_urls

  log "starting Docker dependencies"
  docker compose up -d "${POSTGRES_SERVICE}" "${CLICKHOUSE_SERVICE}"

  wait_for_postgres
  wait_for_clickhouse
  apply_postgres_migration
  apply_clickhouse_migration

  start_backend
  start_frontend

  log "ready"
  log "frontend: http://127.0.0.1:5173"
  log "backend:  $(backend_url)"
  log "press Ctrl+C to stop frontend/backend; Docker services will keep running"

  wait -n "${backend_pid}" "${frontend_pid}"
}

main "$@"
