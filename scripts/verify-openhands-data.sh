#!/usr/bin/env bash
# 使用本次 Demo 的精确 ID 验证 PostgreSQL、ClickHouse、Redis、SSE 与上游源码。
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RESULT_FILE=""
OPENHANDS_DIR="${OPENHANDS_DIR:-${ROOT_DIR}/.external/openhands}"
POSTGRES_SERVICE="${POSTGRES_SERVICE:-postgres}"
CLICKHOUSE_SERVICE="${CLICKHOUSE_SERVICE:-clickhouse}"
REDIS_SERVICE="${REDIS_SERVICE:-redis}"
CLICKHOUSE_CONTAINER="${CLICKHOUSE_CONTAINER:-}"
POSTGRES_USER="${AGENTSCOPE_POSTGRES_USER:-agentscope}"
POSTGRES_DATABASE="${AGENTSCOPE_POSTGRES_DATABASE:-agentscope_test}"
CLICKHOUSE_DATABASE="${AGENTSCOPE_CLICKHOUSE_DATABASE:-agentscope_traces}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --result-file) RESULT_FILE="$2"; shift 2 ;;
    *) printf '未知参数: %s\n' "$1" >&2; exit 1 ;;
  esac
done

[ -f "${RESULT_FILE}" ] || {
  printf '结果文件不存在: %s\n' "${RESULT_FILE}" >&2
  exit 1
}

json_value() {
  python3 - "${RESULT_FILE}" "$1" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
for part in sys.argv[2].split("."):
    value = value[part]
if isinstance(value, bool):
    print(str(value).lower())
else:
    print(value)
PY
}

PROJECT_ID="$(json_value project_id)"
RUN_ID="$(json_value run_id)"
TRACE_ID="$(json_value trace_id)"
SSE_BEFORE_FINISH="$(json_value sse_nonterminal_before_finish)"

compose() {
  docker compose \
    -f "${ROOT_DIR}/docker-compose.yml" \
    -f "${ROOT_DIR}/docker-compose.phase4.yml" "$@"
}

pg_query() {
  compose exec -T "${POSTGRES_SERVICE}" \
    psql -U "${POSTGRES_USER}" -d "${POSTGRES_DATABASE}" -At -c "$1"
}

ch_query() {
  if [ -n "${CLICKHOUSE_CONTAINER}" ]; then
    docker exec "${CLICKHOUSE_CONTAINER}" \
      clickhouse-client --database "${CLICKHOUSE_DATABASE}" \
      --format TSVRaw --query "$1"
    return
  fi
  compose exec -T "${CLICKHOUSE_SERVICE}" \
    clickhouse-client --database "${CLICKHOUSE_DATABASE}" \
    --format TSVRaw --query "$1"
}

redis_query() {
  compose exec -T "${REDIS_SERVICE}" redis-cli --raw "$@"
}

assert_eq() {
  local actual="$1" expected="$2" label="$3"
  [ "${actual}" = "${expected}" ] || {
    printf '验证失败: %s，实际=%s，期望=%s\n' \
      "${label}" "${actual}" "${expected}" >&2
    exit 1
  }
  printf '  ✓ %s\n' "${label}"
}

assert_positive() {
  local actual="$1" label="$2"
  [[ "${actual}" =~ ^[0-9]+$ ]] && [ "${actual}" -gt 0 ] || {
    printf '验证失败: %s，实际=%s\n' "${label}" "${actual}" >&2
    exit 1
  }
  printf '  ✓ %s: %s\n' "${label}" "${actual}"
}

printf '\nOpenHands AgentOps 数据验收\n'
assert_eq \
  "$(pg_query "SELECT count(*) FROM runs WHERE id='${RUN_ID}' AND project_id='${PROJECT_ID}' AND status='completed' AND trace_id='${TRACE_ID}'")" \
  "1" "PostgreSQL Run 完成且 Trace 关联正确"
assert_positive \
  "$(pg_query "SELECT count(*) FROM run_state_events WHERE run_id='${RUN_ID}'")" \
  "PostgreSQL 状态事件"
assert_eq \
  "$(pg_query "SELECT count(*) FROM run_snapshots WHERE run_id='${RUN_ID}' AND project_id='${PROJECT_ID}'")" \
  "1" "不可变 Run Snapshot"
assert_positive \
  "$(pg_query "SELECT count(*) FROM realtime_outbox WHERE run_id='${RUN_ID}'")" \
  "PostgreSQL 实时 Outbox"

assert_positive \
  "$(ch_query "SELECT count() FROM trace_spans WHERE run_id='${RUN_ID}'")" \
  "ClickHouse trace_spans"
assert_positive \
  "$(ch_query "SELECT count() FROM trace_thoughts WHERE run_id='${RUN_ID}'")" \
  "ClickHouse trace_thoughts"
assert_positive \
  "$(ch_query "SELECT count() FROM trace_llm_calls WHERE run_id='${RUN_ID}'")" \
  "ClickHouse trace_llm_calls"
assert_positive \
  "$(ch_query "SELECT count() FROM trace_tool_calls WHERE run_id='${RUN_ID}'")" \
  "ClickHouse trace_tool_calls"
assert_positive \
  "$(ch_query "SELECT count() FROM trace_final_answers WHERE run_id='${RUN_ID}'")" \
  "ClickHouse trace_final_answers"
assert_positive \
  "$(ch_query "SELECT count() FROM trace_tool_calls WHERE run_id='${RUN_ID}' AND lower(tool_name) LIKE '%terminal%'")" \
  "Terminal 工具证据"
assert_positive \
  "$(ch_query "SELECT count() FROM trace_tool_calls WHERE run_id='${RUN_ID}' AND lower(tool_name) LIKE '%file%'")" \
  "File 工具证据"

REDIS_EVENTS="$(
  redis_query XRANGE "agentscope:events:${PROJECT_ID}" - +
)"
grep -q "${RUN_ID}" <<<"${REDIS_EVENTS}" || {
  printf '验证失败: Redis Stream 中没有本次 Run\n' >&2
  exit 1
}
printf '  ✓ Redis Stream 包含相同 Run ID\n'

assert_eq "${SSE_BEFORE_FINISH}" "true" "SSE 非终态事件在 Run 完成前到达"
assert_eq "$(git -C "${OPENHANDS_DIR}" rev-parse HEAD)" \
  "a375c66aaf29c8d3b6553c9164e65ed917149e81" "OpenHands 固定提交"
assert_eq "$(git -C "${OPENHANDS_DIR}" status --porcelain)" "" \
  "OpenHands 上游源码未修改"

printf 'OpenHands AgentOps 数据验收通过\n\n'
