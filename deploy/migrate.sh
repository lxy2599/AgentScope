#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE=(docker compose --env-file "$ROOT/deploy/.env" -f "$ROOT/deploy/compose.yaml")

"${COMPOSE[@]}" up -d postgres clickhouse redis
"${COMPOSE[@]}" up --wait postgres clickhouse redis

"${COMPOSE[@]}" exec -T postgres psql -v ON_ERROR_STOP=1 -U agentscope -d agentlab <<'SQL'
CREATE TABLE IF NOT EXISTS agentlab_schema_migrations (
    version TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
SQL

for migration in "$ROOT"/migrations/*.sql; do
  version="$(basename "$migration" | cut -d_ -f1)"
  applied="$("${COMPOSE[@]}" exec -T postgres psql -U agentscope -d agentlab -Atqc "SELECT EXISTS (SELECT 1 FROM agentlab_schema_migrations WHERE version = '$version')")"
  if [[ "$applied" == "t" ]]; then
    continue
  fi
  echo "Applying PostgreSQL migration $version"
  "${COMPOSE[@]}" exec -T postgres psql -v ON_ERROR_STOP=1 -U agentscope -d agentlab < "$migration"
  "${COMPOSE[@]}" exec -T postgres psql -v ON_ERROR_STOP=1 -U agentscope -d agentlab \
    -c "INSERT INTO agentlab_schema_migrations (version) VALUES ('$version')"
done

trace_table="$("${COMPOSE[@]}" exec -T clickhouse clickhouse-client --database agentscope_traces --query "EXISTS TABLE trace_spans")"
if [[ "$trace_table" != "1" ]]; then
  for migration in "$ROOT"/clickhouse/migrations/*.sql; do
    echo "Applying ClickHouse migration $(basename "$migration")"
    "${COMPOSE[@]}" exec -T clickhouse clickhouse-client --database agentscope_traces --multiquery < "$migration"
  done
fi
