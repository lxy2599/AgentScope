#!/usr/bin/env bash
# AgentScope Demo Data Integrity Verification Script
#
# 精确验证 demo 运行后在 PostgreSQL 和 ClickHouse 中的数据完整性。
# 与基于 project_id 聚合的通用验证不同，本脚本按传入的精确资源 ID
# 校验每条数据，确保本次 Demo 产生的数据全部正确落库。
#
# 使用方法:
#   ./scripts/verify-demo-data.sh \
#     --project-id <project_id> \
#     --run-id <run_id> \
#     [--annotation-item-id <annot_item_id>] \
#     [--dataset-case-id <case_id>] \
#     [--experiment-id <experiment_id>] \
#     [--skip-db-check]
#
# 环境变量（可选，默认走 docker compose 容器）:
#   PGHOST              PostgreSQL 主机  (默认: localhost)
#   PGPORT              PostgreSQL 端口  (默认: 5432)
#   PGDATABASE          PostgreSQL 数据库 (默认: agentscope_test)
#   PGUSER              PostgreSQL 用户  (默认: agentscope)
#   PGPASSWORD          PostgreSQL 密码  (默认: agentscope)
#
#   CLICKHOUSE_HOST     ClickHouse 主机      (默认: localhost)
#   CLICKHOUSE_PORT     ClickHouse 原生端口   (默认: 9000, HTTP: 8123)
#   CLICKHOUSE_DATABASE ClickHouse 数据库     (默认: agentscope_traces)
#   CLICKHOUSE_USE_HTTP 设为 true 使用 HTTP 协议 (默认: false，使用 clickhouse-client)
#
# 退出码:
#   0 - 所有检查通过
#   1 - 至少一项检查失败，或数据库连接失败
##
set -euo pipefail

# ---- 配置 ----
PGHOST="${PGHOST:-localhost}"
PGPORT="${PGPORT:-5432}"
PGDATABASE="${PGDATABASE:-agentscope_test}"
PGUSER="${PGUSER:-agentscope}"
PGPASSWORD="${PGPASSWORD:-agentscope}"

CLICKHOUSE_HOST="${CLICKHOUSE_HOST:-localhost}"
CLICKHOUSE_PORT="${CLICKHOUSE_PORT:-9000}"
CLICKHOUSE_DATABASE="${CLICKHOUSE_DATABASE:-agentscope_traces}"
CLICKHOUSE_USE_HTTP="${CLICKHOUSE_USE_HTTP:-false}"

# ---- 参数解析 ----
PROJECT_ID=""
RUN_ID=""
ANNOTATION_ITEM_ID=""
DATASET_CASE_ID=""
EXPERIMENT_ID=""
SKIP_DB_CHECK=false

while [[ $# -gt 0 ]]; do
  case "$1" in
    --project-id) PROJECT_ID="$2"; shift 2 ;;
    --run-id) RUN_ID="$2"; shift 2 ;;
    --annotation-item-id) ANNOTATION_ITEM_ID="$2"; shift 2 ;;
    --dataset-case-id) DATASET_CASE_ID="$2"; shift 2 ;;
    --experiment-id) EXPERIMENT_ID="$2"; shift 2 ;;
    --skip-db-check) SKIP_DB_CHECK=true; shift ;;
    --help|-h)
      echo "用法: $0 --project-id <project_id> --run-id <run_id> [--annotation-item-id <id>] [--dataset-case-id <id>] [--experiment-id <id>] [--skip-db-check]"
      exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

if [[ -z "${PROJECT_ID}" ]]; then
  echo "错误: --project-id 是必需的"
  exit 1
fi

if [[ "${SKIP_DB_CHECK}" != "true" && -z "${RUN_ID}" ]]; then
  echo "错误: --run-id 是必需的（使用 --skip-db-check 可跳过数据库验证）"
  exit 1
fi

# ---- 辅助函数 ----
PASS=0
FAIL=0

pass() {
  PASS=$((PASS + 1))
  echo "  ✅ $1"
}

fail_exit() {
  local msg="$1"
  FAIL=$((FAIL + 1))
  echo "  ❌ ${msg}"
  echo ""
  echo "═══════════════════════════════════════════════"
  echo "  验证结果: ${PASS} 通过, ${FAIL} 失败"
  echo "═══════════════════════════════════════════════"
  exit 1
}

fail() {
  FAIL=$((FAIL + 1))
  echo "  ❌ $1"
}

info() {
  echo "  ℹ️  $1"
}

# ---- 数据库连接函数（连接失败直接 exit，不吞错误）----

pg_query() {
  local sql="$1"
  local result

  if command -v psql &>/dev/null; then
    result=$(PGPASSWORD="${PGPASSWORD}" psql -h "${PGHOST}" -p "${PGPORT}" -U "${PGUSER}" -d "${PGDATABASE}" -tA -c "${sql}" 2>&1)
  else
    result=$(docker exec agentscope-postgres-1 psql -U "${PGUSER}" -d "${PGDATABASE}" -tA -c "${sql}" 2>&1)
  fi

  local rc=$?
  if [[ $rc -ne 0 ]]; then
    echo "错误: PostgreSQL 查询失败: ${result}" >&2
    exit 1
  fi
  echo "${result}"
}

ch_query() {
  local query="$1"
  local result=""

  if [[ "${CLICKHOUSE_USE_HTTP}" == "true" ]]; then
    local url="http://${CLICKHOUSE_HOST}:${CLICKHOUSE_PORT}/"
    result=$(curl -sfG "${url}" \
      --data-urlencode "query=${query}" \
      --data-urlencode "database=${CLICKHOUSE_DATABASE}" 2>&1)
  elif command -v clickhouse-client &>/dev/null; then
    result=$(clickhouse-client --host "${CLICKHOUSE_HOST}" --port "${CLICKHOUSE_PORT}" \
      --database "${CLICKHOUSE_DATABASE}" --query "${query}" 2>&1)
  else
    result=$(docker exec agentscope-clickhouse-1 clickhouse-client \
      --database "${CLICKHOUSE_DATABASE}" --query "${query}" 2>&1)
  fi

  local rc=$?
  if [[ $rc -ne 0 ]]; then
    echo "错误: ClickHouse 查询失败: ${result}" >&2
    exit 1
  fi
  echo "${result}"
}

echo ""
echo "═══════════════════════════════════════════════"
echo "  AgentScope Demo 数据完整性精确验证"
echo "═══════════════════════════════════════════════"
echo ""

if [[ "${SKIP_DB_CHECK}" == "true" ]]; then
  info "数据库验证已跳过（--skip-db-check）"
  echo ""
  echo "═══════════════════════════════════════════════"
  echo "  验证结果: 跳过"
  echo "═══════════════════════════════════════════════"
  exit 0
fi

# ============================================================
# 1. 项目验证
# ============================================================
echo "━━━ [1] 项目存在验证 ━━━"

PROJECT_EXISTS=$(pg_query "SELECT count(*) FROM projects WHERE id='${PROJECT_ID}'")
if [[ "${PROJECT_EXISTS}" != "0" ]]; then
  PROJECT_NAME=$(pg_query "SELECT name FROM projects WHERE id='${PROJECT_ID}'")
  pass "项目存在: ${PROJECT_NAME} (${PROJECT_ID})"
else
  fail "项目 ${PROJECT_ID} 不存在"
fi

echo ""

# ============================================================
# 2. Run 精确验证（按 run_id）
# ============================================================
echo "━━━ [2] Run 精确验证 ━━━"

RUN_EXISTS=$(pg_query "SELECT count(*) FROM runs WHERE id='${RUN_ID}' AND project_id='${PROJECT_ID}'")
if [[ "${RUN_EXISTS}" != "1" ]]; then
  fail_exit "Run ${RUN_ID} 不存在或不属于项目 ${PROJECT_ID}"
fi

RUN_STATUS=$(pg_query "SELECT status FROM runs WHERE id='${RUN_ID}'" | tr -d ' \n')
pass "Run 存在，状态: ${RUN_STATUS}"

RUN_TRACE=$(pg_query "SELECT COALESCE(trace_id, '') FROM runs WHERE id='${RUN_ID}'" | tr -d ' \n')
if [[ -n "${RUN_TRACE}" ]]; then
  pass "Run 有关联 trace_id: ${RUN_TRACE}"
else
  fail "Run ${RUN_ID} 缺少 trace_id"
fi

RUN_INPUT=$(pg_query "SELECT COALESCE(input_summary, '') FROM runs WHERE id='${RUN_ID}'" | tr -d ' \n')
if [[ -n "${RUN_INPUT}" ]]; then
  pass "Run 有 input_summary"
else
  fail "Run ${RUN_ID} 缺少 input_summary"
fi

RUN_OUTPUT=$(pg_query "SELECT COALESCE(output_summary, '') FROM runs WHERE id='${RUN_ID}'" | tr -d ' \n')
if [[ -n "${RUN_OUTPUT}" ]]; then
  pass "Run 有 output_summary"
else
  fail "Run ${RUN_ID} 缺少 output_summary"
fi

echo ""

# ============================================================
# 3. Annotation Item 精确验证（按 annotation_item_id）
# ============================================================
if [[ -n "${ANNOTATION_ITEM_ID}" ]]; then
  echo "━━━ [3] Annotation Item 精确验证 ━━━"

  ANNOT_EXISTS=$(pg_query "SELECT count(*) FROM annotation_items WHERE id='${ANNOTATION_ITEM_ID}' AND project_id='${PROJECT_ID}'")
  if [[ "${ANNOT_EXISTS}" != "1" ]]; then
    fail_exit "Annotation Item ${ANNOTATION_ITEM_ID} 不存在于项目 ${PROJECT_ID}"
  fi

  ANNOT_RUN_ID=$(pg_query "SELECT run_id FROM annotation_items WHERE id='${ANNOTATION_ITEM_ID}'" | tr -d ' \n')
  if [[ "${ANNOT_RUN_ID}" == "${RUN_ID}" ]]; then
    pass "annotation_item.run_id == ${RUN_ID}"
  else
    fail "annotation_item.run_id 为 ${ANNOT_RUN_ID}，期望 ${RUN_ID}"
  fi

  ANNOT_TRACE_ID=$(pg_query "SELECT COALESCE(trace_id, '') FROM annotation_items WHERE id='${ANNOTATION_ITEM_ID}'" | tr -d ' \n')
  # 严格校验：annotation_items.trace_id 必须等于 runs.trace_id，不得为空
  if [[ -z "${ANNOT_TRACE_ID}" ]]; then
    fail_exit "annotation_items.trace_id 为空，期望 ${RUN_TRACE}"
  elif [[ "${ANNOT_TRACE_ID}" == "${RUN_TRACE}" ]]; then
    pass "annotation_item.trace_id 匹配 runs.trace_id: ${ANNOT_TRACE_ID}"
  else
    fail_exit "annotation_item.trace_id 为 ${ANNOT_TRACE_ID}，期望 ${RUN_TRACE}"
  fi

  echo ""
fi

# ============================================================
# 4. Dataset Case 精确验证（按 dataset_case_id）
# ============================================================
if [[ -n "${DATASET_CASE_ID}" ]]; then
  echo "━━━ [4] Dataset Case 精确验证 ━━━"

  CASE_EXISTS=$(pg_query "SELECT count(*) FROM dataset_cases WHERE id='${DATASET_CASE_ID}'")
  if [[ "${CASE_EXISTS}" != "1" ]]; then
    fail_exit "Dataset Case ${DATASET_CASE_ID} 不存在"
  fi

  CASE_META_RAW=$(pg_query "SELECT metadata FROM dataset_cases WHERE id='${DATASET_CASE_ID}'")
  pass "Dataset Case 存在"

  # 校验 Dataset 的项目归属：dataset_cases -> datasets -> project_id
  DS_PROJECT_ID=$(pg_query "SELECT d.project_id FROM dataset_cases c JOIN datasets d ON d.id = c.dataset_id WHERE c.id='${DATASET_CASE_ID}'" | tr -d ' \n')
  if [[ "${DS_PROJECT_ID}" == "${PROJECT_ID}" ]]; then
    pass "Dataset Case 所属 Dataset 的项目 ID 为 ${PROJECT_ID}"
  else
    fail_exit "Dataset Case 归属项目 ${DS_PROJECT_ID}，期望 ${PROJECT_ID}"
  fi

  # 展开 metadata 为单行 JSON 用于 grep 匹配
  CASE_META_FLAT=$(echo "${CASE_META_RAW}" | tr -d '\n' | tr -d ' ')

  if echo "${CASE_META_FLAT}" | grep -q "\"source_run_id\":\"${RUN_ID}\""; then
    pass "metadata.source_run_id 匹配 ${RUN_ID}"
  else
    fail "metadata.source_run_id 不匹配（期望 ${RUN_ID}）"
  fi

  if echo "${CASE_META_FLAT}" | grep -q "\"source_trace_id\":\"${RUN_TRACE}\""; then
    pass "metadata.source_trace_id 匹配 ${RUN_TRACE}"
  else
    fail "metadata.source_trace_id 不匹配（期望 ${RUN_TRACE}）"
  fi

  if echo "${CASE_META_FLAT}" | grep -qE '"missing_trace_id":(false|false,)'; then
    pass "metadata.missing_trace_id 为 false"
  else
    fail "metadata.missing_trace_id 不是 false"
  fi

  if echo "${CASE_META_FLAT}" | grep -q '"source":"annotation"'; then
    pass "metadata.source 为 annotation"
  else
    fail "metadata.source 不是 annotation"
  fi

  if [[ -n "${ANNOTATION_ITEM_ID}" ]]; then
    if echo "${CASE_META_FLAT}" | grep -q "\"source_annotation_item_id\":\"${ANNOTATION_ITEM_ID}\""; then
      pass "metadata.source_annotation_item_id 匹配"
    else
      fail "metadata.source_annotation_item_id 不匹配（期望 ${ANNOTATION_ITEM_ID}）"
    fi
  fi

  echo ""
fi

# ============================================================
# 5. 实验精确验证（按 experiment_id）
# ============================================================
if [[ -n "${EXPERIMENT_ID}" ]]; then
  echo "━━━ [5] 实验精确验证 ━━━"

  EXP_EXISTS=$(pg_query "SELECT count(*) FROM experiments WHERE id='${EXPERIMENT_ID}' AND project_id='${PROJECT_ID}'")
  if [[ "${EXP_EXISTS}" != "1" ]]; then
    fail_exit "Experiment ${EXPERIMENT_ID} 不存在于项目 ${PROJECT_ID}"
  fi

  EXP_STATUS=$(pg_query "SELECT status FROM experiments WHERE id='${EXPERIMENT_ID}'" | tr -d ' \n')
  pass "实验存在，状态: ${EXP_STATUS}"

  if [[ "${EXP_STATUS}" != "completed" ]]; then
    fail_exit "实验状态不是 completed（当前: ${EXP_STATUS}）"
  fi
  pass "实验已完成（completed）"

  TOTAL_CASES=$(pg_query "SELECT total_cases FROM experiments WHERE id='${EXPERIMENT_ID}'" | tr -d ' \n')
  PASSED_CASES=$(pg_query "SELECT passed_cases FROM experiments WHERE id='${EXPERIMENT_ID}'" | tr -d ' \n')
  FAILED_CASES=$(pg_query "SELECT failed_cases FROM experiments WHERE id='${EXPERIMENT_ID}'" | tr -d ' \n')
  SUM=$((PASSED_CASES + FAILED_CASES))
  if [[ "${SUM}" -eq "${TOTAL_CASES}" ]]; then
    pass "passed_cases(${PASSED_CASES}) + failed_cases(${FAILED_CASES}) == total_cases(${TOTAL_CASES})"
  else
    fail "passed_cases(${PASSED_CASES}) + failed_cases(${FAILED_CASES}) != total_cases(${TOTAL_CASES})"
  fi

  ECR_COUNT=$(pg_query "SELECT count(*) FROM eval_case_results WHERE eval_job_id IN (SELECT id FROM eval_jobs WHERE experiment_id='${EXPERIMENT_ID}')" | tr -d ' \n')
  if [[ "${ECR_COUNT}" -eq "${TOTAL_CASES}" ]]; then
    pass "eval_case_results 数量（${ECR_COUNT}）等于 total_cases（${TOTAL_CASES}）"
  else
    fail "eval_case_results 数量（${ECR_COUNT}）不等于 total_cases（${TOTAL_CASES}）"
  fi

  echo ""
fi

# ============================================================
# 6. ClickHouse Span 精确验证（按 run_id）
# ============================================================
echo "━━━ [6] ClickHouse Span 精确验证 ━━━"

CH_SPAN_COUNT=$(ch_query "SELECT count() FROM trace_spans WHERE run_id='${RUN_ID}'" 2>&1 | tr -d ' \n')

if [[ -z "${CH_SPAN_COUNT}" ]] || [[ "${CH_SPAN_COUNT}" -eq 0 ]]; then
  fail_exit "trace_spans 中该 run_id（${RUN_ID}）记录数为 0"
fi

pass "trace_spans 中该 run_id 存在（${CH_SPAN_COUNT} 条）"

# 使用 name='agent.run' 而非 span_kind（trace_spans 无 span_kind 列）
ROOT_COUNT=$(ch_query "SELECT count() FROM trace_spans WHERE run_id='${RUN_ID}' AND name='agent.run'" | tr -d ' \n')
if [[ "${ROOT_COUNT}" -ge 1 ]]; then
  pass "包含 agent.run root span"
else
  fail "缺少 agent.run root span"
fi

THOUGHT_COUNT=$(ch_query "SELECT count() FROM trace_thoughts WHERE run_id='${RUN_ID}'" | tr -d ' \n')
if [[ "${THOUGHT_COUNT}" -ge 1 ]]; then
  pass "trace_thoughts 存在（${THOUGHT_COUNT} 条）"
fi

LLM_COUNT=$(ch_query "SELECT count() FROM trace_llm_calls WHERE run_id='${RUN_ID}'" | tr -d ' \n')
if [[ "${LLM_COUNT}" -ge 1 ]]; then
  pass "trace_llm_calls 存在（${LLM_COUNT} 条）"
  LLM_AGENT_EMPTY=$(ch_query "SELECT count() FROM trace_llm_calls WHERE run_id='${RUN_ID}' AND (agent_id IS NULL OR agent_id = '')" | tr -d ' \n')
  if [[ "${LLM_AGENT_EMPTY}" -eq 0 ]]; then
    pass "trace_llm_calls 所有记录均有非空 agent_id"
  else
    fail "trace_llm_calls 中有 ${LLM_AGENT_EMPTY} 条 agent_id 为空"
  fi
else
  fail "trace_llm_calls 中不存在该 run_id 的记录"
fi

TOOL_COUNT=$(ch_query "SELECT count() FROM trace_tool_calls WHERE run_id='${RUN_ID}'" | tr -d ' \n')
if [[ "${TOOL_COUNT}" -ge 1 ]]; then
  pass "trace_tool_calls 存在（${TOOL_COUNT} 条）"
  TOOL_INST_EMPTY=$(ch_query "SELECT count() FROM trace_tool_calls WHERE run_id='${RUN_ID}' AND (agent_instance_id IS NULL OR agent_instance_id = '')" | tr -d ' \n')
  if [[ "${TOOL_INST_EMPTY}" -eq 0 ]]; then
    pass "trace_tool_calls 所有记录均有非空 agent_instance_id"
  else
    fail "trace_tool_calls 中有 ${TOOL_INST_EMPTY} 条 agent_instance_id 为空"
  fi
else
  fail "trace_tool_calls 中不存在该 run_id 的记录"
fi

FINAL_COUNT=$(ch_query "SELECT count() FROM trace_final_answers WHERE run_id='${RUN_ID}'" | tr -d ' \n')
if [[ "${FINAL_COUNT}" -ge 1 ]]; then
  pass "trace_final_answers 存在（${FINAL_COUNT} 条）"
else
  fail "trace_final_answers 中不存在该 run_id 的记录"
fi

echo ""

# ============================================================
# 汇总
# ============================================================
echo "═══════════════════════════════════════════════"
echo "  验证结果: ${PASS} 通过, ${FAIL} 失败"
echo "═══════════════════════════════════════════════"

if [[ "${FAIL}" -gt 0 ]]; then
  exit 1
fi
exit 0
