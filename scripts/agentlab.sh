#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RUNTIME_DIR="$ROOT/.runtime"
ENV_FILE="$ROOT/.env"
COMPOSE=(docker compose --env-file "$ENV_FILE" -f "$ROOT/infra/compose.yaml")
SERVER_PID_FILE="$RUNTIME_DIR/agentscope-server.pid"
SERVER_LOG="$RUNTIME_DIR/agentscope-server.log"

load_env() {
  if [[ ! -f "$ENV_FILE" ]]; then
    echo "Missing $ENV_FILE. Run: $0 bootstrap" >&2
    exit 1
  fi
  set -a
  # shellcheck disable=SC1090
  source "$ENV_FILE"
  set +a
  export HTTP_PROXY="${HTTP_PROXY:-${AGENTLAB_PROXY_URL:-}}"
  export HTTPS_PROXY="${HTTPS_PROXY:-${AGENTLAB_PROXY_URL:-}}"
  export http_proxy="${http_proxy:-$HTTP_PROXY}"
  export https_proxy="${https_proxy:-$HTTPS_PROXY}"
  export NO_PROXY="${NO_PROXY:-localhost,127.0.0.1,::1,agentscope,postgres,clickhouse,openhands,hermes,benchmarks-worker}"
  export no_proxy="${no_proxy:-$NO_PROXY}"
  # Harbor is executed through the Worker's Docker socket. Local fixture paths
  # must remain identical on host and Worker so Harbor-created task containers
  # can read them; this is deployment configuration, never catalog input.
  if [[ -z "${AGENTLAB_BENCHMARK_CATALOG_ROOT:-}" ]]; then
    AGENTLAB_BENCHMARK_CATALOG_ROOT="$ROOT/benchmarks"
  fi
  AGENTLAB_BENCHMARK_CATALOG_ROOT="$(cd "$AGENTLAB_BENCHMARK_CATALOG_ROOT" && pwd -P)"
  if [[ ! -d "$AGENTLAB_BENCHMARK_CATALOG_ROOT" ]]; then
    echo "AGENTLAB_BENCHMARK_CATALOG_ROOT must be an existing directory" >&2
    exit 1
  fi
  export AGENTLAB_BENCHMARK_CATALOG_ROOT

}

bootstrap() {
  mkdir -p "$RUNTIME_DIR"
  if [[ ! -f "$ENV_FILE" ]]; then
    cp "$ROOT/.env.example" "$ENV_FILE"
  fi
  if ! grep -q '^AGENTSCOPE_SESSION_SECRET=.' "$ENV_FILE"; then
    local secret
    secret="$(python3 - <<'PY'
import secrets
print(secrets.token_urlsafe(48))
PY
)"
    printf '\nAGENTSCOPE_SESSION_SECRET=%s\n' "$secret" >> "$ENV_FILE"
    chmod 600 "$ENV_FILE"
  fi
  echo "Bootstrap complete. Set AGENTSCOPE_PROJECT_TOKEN and ARK_API_KEY in $ENV_FILE before running observed agents or benchmarks."
}

wait_for_url() {
  local url="$1"
  for _ in $(seq 1 60); do
    if curl --fail --silent --show-error "$url" >/dev/null 2>&1; then return 0; fi
    sleep 1
  done
  echo "Timed out waiting for $url" >&2
  return 1
}

apply_migrations() {
  local users_table traces_table migration migration_id applied_count baseline_version
  users_table="$("${COMPOSE[@]}" exec -T postgres psql -U agentscope -d agentlab -Atqc "SELECT to_regclass('public.users') IS NOT NULL" 2>/dev/null || true)"

  # Keep a durable migration ledger so adding a migration works on an already
  # initialized local AgentLab database. The old users-table sentinel skipped
  # every later migration once the first boot had completed.
  "${COMPOSE[@]}" exec -T postgres psql -v ON_ERROR_STOP=1 -U agentscope -d agentlab <<'SQL'
CREATE TABLE IF NOT EXISTS agentlab_schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
SQL

  applied_count="$("${COMPOSE[@]}" exec -T postgres psql -U agentscope -d agentlab -Atqc "SELECT count(*) FROM agentlab_schema_migrations" 2>/dev/null || true)"
  # Existing developer databases were created before the ledger existed. They
  # are known to have been initialized by the previous all-or-nothing bootstrap;
  # record their highest observable version once, then apply only newer files.
  if [[ "$users_table" == "t" && "$applied_count" == "0" ]]; then
    baseline_version=20
    if [[ "$("${COMPOSE[@]}" exec -T postgres psql -U agentscope -d agentlab -Atqc "SELECT to_regclass('public.benchmark_suites') IS NOT NULL" 2>/dev/null || true)" == "t" ]]; then
      baseline_version=21
    fi
    if [[ "$("${COMPOSE[@]}" exec -T postgres psql -U agentscope -d agentlab -Atqc "SELECT to_regclass('public.benchmark_catalog_revisions') IS NOT NULL" 2>/dev/null || true)" == "t" ]]; then
      baseline_version=22
    fi
    echo "Baselining existing PostgreSQL schema through migration $(printf '%05d' "$baseline_version")..."
    for migration in "$ROOT"/migrations/*.sql; do
      migration_id="${migration##*/}"
      migration_id="${migration_id%%_*}"
      if ((10#$migration_id <= baseline_version)); then
        "${COMPOSE[@]}" exec -T postgres psql -v ON_ERROR_STOP=1 -U agentscope -d agentlab \
          -c "INSERT INTO agentlab_schema_migrations (version) VALUES ('$migration_id') ON CONFLICT (version) DO NOTHING" >/dev/null
      fi
    done
  fi

  for migration in "$ROOT"/migrations/*.sql; do
    migration_id="${migration##*/}"
    migration_id="${migration_id%%_*}"
    if [[ "$("${COMPOSE[@]}" exec -T postgres psql -U agentscope -d agentlab -Atqc "SELECT EXISTS (SELECT 1 FROM agentlab_schema_migrations WHERE version = '$migration_id')" 2>/dev/null || true)" == "t" ]]; then
      continue
    fi
    echo "Applying PostgreSQL migration $migration_id..."
    "${COMPOSE[@]}" exec -T postgres psql -v ON_ERROR_STOP=1 -U agentscope -d agentlab < "$migration"
    "${COMPOSE[@]}" exec -T postgres psql -v ON_ERROR_STOP=1 -U agentscope -d agentlab \
      -c "INSERT INTO agentlab_schema_migrations (version) VALUES ('$migration_id')" >/dev/null
  done

  traces_table="$("${COMPOSE[@]}" exec -T clickhouse clickhouse-client --database agentscope_traces --query "EXISTS TABLE trace_spans" 2>/dev/null || true)"
  if [[ "$traces_table" != "1" ]]; then
    echo "Applying ClickHouse migrations..."
    for migration in "$ROOT"/clickhouse/migrations/*.sql; do
      "${COMPOSE[@]}" exec -T clickhouse clickhouse-client --database agentscope_traces --multiquery < "$migration"
    done
  fi
}

start_platform() {
  load_env
  mkdir -p "$RUNTIME_DIR"
  "${COMPOSE[@]}" up -d postgres clickhouse
  "${COMPOSE[@]}" up --wait postgres clickhouse
  apply_migrations

  if [[ -f "$SERVER_PID_FILE" ]] && kill -0 "$(cat "$SERVER_PID_FILE")" 2>/dev/null; then
    return 0
  fi

  local binary="$RUNTIME_DIR/agentscope-server"
  (
    cd "$ROOT"
    go build -o "$binary" ./cmd/agentscope-server
  )
  (
    export AGENTSCOPE_HTTP_ADDR=":${AGENTLAB_HTTP_PORT:-18080}"
    export AGENTSCOPE_DATABASE_URL="postgres://agentscope:agentscope@127.0.0.1:${AGENTLAB_POSTGRES_PORT:-15432}/agentlab?sslmode=disable"
    export AGENTSCOPE_CLICKHOUSE_DSN="clickhouse://127.0.0.1:${AGENTLAB_CLICKHOUSE_TCP_PORT:-19001}/agentscope_traces"
    export AGENTSCOPE_ENV=development
    export AGENTSCOPE_SECURE_COOKIES=false
    export AGENTSCOPE_REALTIME_ENABLED=false
    export AGENTSCOPE_ANOMALY_ENABLED=false
    export AGENTSCOPE_BENCHMARK_RUNNER_ENABLED=false
    setsid "$binary" >"$SERVER_LOG" 2>&1 < /dev/null &
    echo $! >"$SERVER_PID_FILE"
  )
  wait_for_url "http://127.0.0.1:${AGENTLAB_HTTP_PORT:-18080}/healthz"
}

ensure_project_token() {
  if [[ -n "${AGENTSCOPE_PROJECT_TOKEN:-}" ]]; then
    return 0
  fi
  "$ROOT/scripts/bootstrap-project-token.py" \
    --server-url "http://127.0.0.1:${AGENTLAB_HTTP_PORT:-18080}" \
    --env-file "$ENV_FILE"
  load_env
}

ensure_runtime_images() {
  local image
  local missing=0
  for image in agentlab/openhands:local agentlab/hermes:local agentlab/hermes-task-runner:local agentlab/benchmarks-worker:local; do
    if ! docker image inspect "$image" >/dev/null 2>&1; then
      missing=1
      break
    fi
  done
  # First use remains one command; routine starts reuse immutable local images
  # and never trigger a slow or proxy-sensitive rebuild just to restart agents.
  if [[ "$missing" == "1" ]]; then
    build_images
  fi
}

start_agents() {
  load_env
  ensure_project_token
  ensure_runtime_images
  "${COMPOSE[@]}" up -d --no-build openhands hermes benchmarks-worker
}

stop_agents() {
  load_env
  "${COMPOSE[@]}" stop openhands hermes benchmarks-worker || true
}

fetch_build_assets() {
  "$ROOT/scripts/fetch-build-assets.sh"
  "$ROOT/hermes/scripts/bootstrap-docker-build-assets.sh"
}

build_images() {
  load_env
  fetch_build_assets
  # Hermes' owned Dockerfile uses COPY --chmod and therefore requires
  # BuildKit. Keep every first-use build on the same builder so `agentlab.sh`
  # remains a reproducible one-command entrypoint.
  DOCKER_BUILDKIT=1 "${COMPOSE[@]}" build openhands hermes benchmarks-worker
  DOCKER_BUILDKIT=1 docker build \
    --build-arg HERMES_RUNTIME_IMAGE=agentlab/hermes:local \
    --target task-runner \
    -f "$ROOT/hermes/Dockerfile.task-runner" \
    -t agentlab/hermes-task-runner:local \
    "$ROOT/hermes"
}

stop_platform() {
  if [[ -f "$SERVER_PID_FILE" ]]; then
    local pid
    pid="$(cat "$SERVER_PID_FILE")"
    if kill -0 "$pid" 2>/dev/null; then
      kill "$pid"
      for _ in $(seq 1 20); do
        kill -0 "$pid" 2>/dev/null || break
        sleep 0.5
      done
    fi
    : > "$SERVER_PID_FILE"
  fi
}

case "${1:-}" in
  bootstrap) bootstrap ;;
  platform-up) start_platform ;;
  platform-down) load_env; stop_agents; stop_platform; "${COMPOSE[@]}" down ;;
  agents-up) start_platform; start_agents ;;
  agents-down) stop_agents ;;
  build) build_images ;;
  up) start_platform; start_agents ;;
  down) load_env; stop_agents; stop_platform; "${COMPOSE[@]}" down ;;
  status)
    load_env
    "${COMPOSE[@]}" ps
    if [[ -f "$SERVER_PID_FILE" ]] && kill -0 "$(cat "$SERVER_PID_FILE")" 2>/dev/null; then
      echo "agentscope-server: running (pid $(cat "$SERVER_PID_FILE"))"
    else
      echo "agentscope-server: stopped"
    fi
    curl --fail --silent "http://127.0.0.1:${AGENTLAB_HTTP_PORT:-18080}/healthz" || true
    echo
    ;;
  *)
    echo "Usage: $0 {bootstrap|platform-up|platform-down|agents-up|agents-down|build|up|down|status}" >&2
    exit 2
    ;;
esac
