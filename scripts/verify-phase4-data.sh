#!/usr/bin/env bash
# 使用本次统一 Demo 的精确 ID 校验全部持久化与实时证据。
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
RESULT_FILE=""
POSTGRES_SERVICE="${POSTGRES_SERVICE:-postgres}"
CLICKHOUSE_SERVICE="${CLICKHOUSE_SERVICE:-clickhouse}"
REDIS_SERVICE="${REDIS_SERVICE:-redis}"
POSTGRES_USER="${AGENTSCOPE_POSTGRES_USER:-agentscope}"
POSTGRES_DATABASE="${AGENTSCOPE_POSTGRES_DATABASE:-agentscope_test}"
CLICKHOUSE_DATABASE="${AGENTSCOPE_CLICKHOUSE_DATABASE:-agentscope_traces}"
OPENHANDS_DIR="${OPENHANDS_DIR:-${ROOT_DIR}/.external/openhands}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --result-file) RESULT_FILE="$2"; shift 2 ;;
    *) printf '未知参数: %s\n' "$1" >&2; exit 1 ;;
  esac
done
[[ -f "${RESULT_FILE}" ]] || {
  printf '结果文件不存在: %s\n' "${RESULT_FILE}" >&2
  exit 1
}

json_value() {
  python3 - "${RESULT_FILE}" "$1" <<'PY'
import json
import sys

value = json.load(open(sys.argv[1], encoding="utf-8"))
for part in sys.argv[2].split("."):
    value = value[int(part)] if isinstance(value, list) else value[part]
print(str(value).lower() if isinstance(value, bool) else value)
PY
}

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
  compose exec -T "${CLICKHOUSE_SERVICE}" \
    clickhouse-client --database "${CLICKHOUSE_DATABASE}" \
    --format TSVRaw --query "$1"
}
redis_query() {
  compose exec -T "${REDIS_SERVICE}" redis-cli --raw "$@"
}
assert_eq() {
  local actual="$1" expected="$2" label="$3"
  [[ "${actual}" == "${expected}" ]] || {
    printf '验证失败: %s，实际=%s，期望=%s\n' \
      "${label}" "${actual}" "${expected}" >&2
    exit 1
  }
  printf '  ✓ %s\n' "${label}"
}
assert_positive() {
  local actual="$1" label="$2"
  [[ "${actual}" =~ ^[0-9]+$ ]] && (( actual > 0 )) || {
    printf '验证失败: %s，实际=%s\n' "${label}" "${actual}" >&2
    exit 1
  }
  printf '  ✓ %s: %s\n' "${label}" "${actual}"
}

PROJECT_ID="$(json_value project_id)"
OPENHANDS_RUN_ID="$(json_value run_id)"
OPENHANDS_TRACE_ID="$(json_value trace_id)"
RUBRIC_TEMPLATE_ID="$(json_value rubric_template_id)"
RUBRIC_VERSION_ID="$(json_value rubric_version_id)"
BASELINE_EXPERIMENT_ID="$(json_value baseline_experiment_id)"
CANDIDATE_EXPERIMENT_ID="$(json_value candidate_experiment_id)"
CALIBRATION_ID="$(json_value calibration_id)"
COMPARISON_ID="$(json_value comparison_id)"
ANOMALY_ID="$(json_value anomaly_id)"
DROPPED="$(json_value sdk_spans_dropped)"

printf '\nPhase 4 企业 AgentOps 数据验收\n'
printf '\nPostgreSQL\n'
assert_eq \
  "$(pg_query "SELECT count(*) FROM runs WHERE id='${OPENHANDS_RUN_ID}' AND project_id='${PROJECT_ID}' AND trace_id='${OPENHANDS_TRACE_ID}' AND status='completed'")" \
  "1" "真实 OpenHands Run 完成"
assert_positive \
  "$(pg_query "SELECT count(*) FROM run_state_events WHERE run_id='${OPENHANDS_RUN_ID}'")" \
  "OpenHands 状态历史"
assert_eq \
  "$(pg_query "SELECT count(*) FROM run_snapshots WHERE run_id='${OPENHANDS_RUN_ID}' AND project_id='${PROJECT_ID}' AND length(payload_hash)=64")" \
  "1" "不可变配置快照及 SHA-256"
assert_positive \
  "$(pg_query "SELECT count(*) FROM realtime_outbox WHERE project_id='${PROJECT_ID}' AND run_id='${OPENHANDS_RUN_ID}' AND event_type='run.semantic.appended' AND published_at IS NOT NULL")" \
  "已发布实时语义事件"

for _ in $(seq 1 20); do
  pending="$(pg_query "SELECT count(*) FROM realtime_outbox WHERE project_id='${PROJECT_ID}' AND published_at IS NULL")"
  [[ "${pending}" == "0" ]] && break
  sleep 0.5
done
assert_eq "${pending}" "0" "Outbox 无积压"

assert_eq \
  "$(pg_query "SELECT count(*) FROM rubric_templates WHERE id='${RUBRIC_TEMPLATE_ID}' AND project_id='${PROJECT_ID}'")" \
  "1" "Rubric 模板"
assert_eq \
  "$(pg_query "SELECT count(*) FROM rubric_template_versions WHERE id='${RUBRIC_VERSION_ID}' AND template_id='${RUBRIC_TEMPLATE_ID}' AND status='published'")" \
  "1" "已发布且不可变的 Rubric 版本"
assert_eq \
  "$(pg_query "SELECT count(*) FROM eval_judge_votes WHERE project_id='${PROJECT_ID}' AND rubric_version_id='${RUBRIC_VERSION_ID}'")" \
  "6" "三用例双 Judge 投票均可审计"
assert_eq \
  "$(pg_query "SELECT count(*) FROM eval_score_aggregations WHERE project_id='${PROJECT_ID}' AND rubric_version_id='${RUBRIC_VERSION_ID}' AND valid_vote_count + error_vote_count=2 AND confidence BETWEEN 0 AND 1")" \
  "3" "Judge 聚合、错误票与置信度完整"
assert_eq \
  "$(pg_query "SELECT count(*) FROM annotation_items WHERE project_id='${PROJECT_ID}' AND is_gold=true AND gold_scores ? 'quality' AND run_id IN (SELECT result.run_id FROM eval_case_results result JOIN eval_jobs job ON job.id=result.eval_job_id WHERE job.experiment_id='${CANDIDATE_EXPERIMENT_ID}')")" \
  "3" "人工 Gold 标注"
assert_eq \
  "$(pg_query "SELECT count(*) FROM eval_calibration_runs WHERE id='${CALIBRATION_ID}' AND project_id='${PROJECT_ID}' AND status='completed' AND (report->>'sample_count')::int=3")" \
  "1" "Judge 校准报告"
assert_eq \
  "$(pg_query "SELECT count(*) FROM experiment_comparisons WHERE id='${COMPARISON_ID}' AND project_id='${PROJECT_ID}' AND baseline_experiment_id='${BASELINE_EXPERIMENT_ID}' AND candidate_experiment_id='${CANDIDATE_EXPERIMENT_ID}' AND status='completed'")" \
  "1" "实验对比与显著性计算"
assert_eq \
  "$(pg_query "SELECT count(*) FROM project_quotas WHERE project_id='${PROJECT_ID}'")" \
  "1" "项目配额策略"
assert_eq \
  "$(pg_query "SELECT count(*) FROM operational_anomalies WHERE id='${ANOMALY_ID}' AND project_id='${PROJECT_ID}' AND status='anomaly'")" \
  "1" "运行延迟异常记录"

printf '\nClickHouse\n'
assert_positive \
  "$(ch_query "SELECT count() FROM trace_spans WHERE project_id='${PROJECT_ID}' AND run_id='${OPENHANDS_RUN_ID}'")" \
  "OpenHands Span"
assert_positive \
  "$(ch_query "SELECT count() FROM trace_thoughts WHERE project_id='${PROJECT_ID}' AND run_id='${OPENHANDS_RUN_ID}'")" \
  "OpenHands 思考"
assert_positive \
  "$(ch_query "SELECT count() FROM trace_llm_calls WHERE project_id='${PROJECT_ID}' AND run_id='${OPENHANDS_RUN_ID}'")" \
  "OpenHands LLM"
assert_eq \
  "$(ch_query "SELECT count() FROM trace_llm_calls WHERE project_id='${PROJECT_ID}' AND run_id='${OPENHANDS_RUN_ID}' AND model != '' AND duration_ms > 0 AND input_tokens IS NOT NULL AND output_tokens IS NOT NULL AND total_tokens > 0 AND estimated_cost > 0 AND cost_source='estimated'")" \
  "$(ch_query "SELECT count() FROM trace_llm_calls WHERE project_id='${PROJECT_ID}' AND run_id='${OPENHANDS_RUN_ID}'")" \
  "OpenHands LLM 模型、耗时、Token 与成本完整"
assert_positive \
  "$(ch_query "SELECT count() FROM trace_llm_calls WHERE project_id='${PROJECT_ID}' AND run_id='${OPENHANDS_RUN_ID}' AND ttft_ms IS NOT NULL AND ttft_ms > 0")" \
  "OpenHands TTFT"
assert_positive \
  "$(ch_query "SELECT count() FROM trace_tool_calls WHERE project_id='${PROJECT_ID}' AND run_id='${OPENHANDS_RUN_ID}'")" \
  "OpenHands 工具"
assert_positive \
  "$(ch_query "SELECT count() FROM trace_final_answers WHERE project_id='${PROJECT_ID}' AND run_id='${OPENHANDS_RUN_ID}'")" \
  "OpenHands 最终答案"
assert_positive \
  "$(ch_query "SELECT count() FROM trace_governance_rollup_hourly WHERE project_id='${PROJECT_ID}' AND metric='run_latency_ms'")" \
  "小时成本性能 Rollup"
assert_positive "${DROPPED}" "SDK 本地背压丢弃计数"
assert_positive \
  "$(ch_query "SELECT count() FROM trace_ingest_health_events WHERE project_id='${PROJECT_ID}' AND sdk_spans_dropped > 0")" \
  "SDK 背压健康事件"

printf '\nRedis 与上游源码\n'
REDIS_EVENTS="$(redis_query XRANGE "agentscope:events:${PROJECT_ID}" - +)"
grep -q "${OPENHANDS_RUN_ID}" <<<"${REDIS_EVENTS}" || {
  printf '验证失败: Redis Stream 不包含 OpenHands Run\n' >&2
  exit 1
}
printf '  ✓ Redis Stream 包含真实 Run\n'
assert_eq "$(git -C "${OPENHANDS_DIR}" rev-parse HEAD)" \
  "a375c66aaf29c8d3b6553c9164e65ed917149e81" "OpenHands 固定提交"
assert_eq "$(git -C "${OPENHANDS_DIR}" status --porcelain)" "" \
  "OpenHands 上游源码未修改"

printf '\nPhase 4 企业 AgentOps 数据验收通过\n\n'
