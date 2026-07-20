#!/usr/bin/env bash
# AgentScope Demo Data Integrity Verification Script
#
# 验证 demo 运行后在 PostgreSQL 和 ClickHouse 中的数据完整性。
#
# 使用方法:
#   ./scripts/verify-demo-data.sh --project-id <project_id> [--experiment-id <experiment_id>]
#
# 环境变量:
#   PGHOST      PostgreSQL 主机 (默认: localhost)
#   PGPORT      PostgreSQL 端口 (默认: 5432)
#   PGDATABASE  PostgreSQL 数据库 (默认: agentscope_test)
#   PGUSER      PostgreSQL 用户 (默认: agentscope)
#   PGPASSWORD  PostgreSQL 密码 (默认: agentscope)
#
#   CLICKHOUSE_HOST     ClickHouse 主机 (默认: localhost)
#   CLICKHOUSE_PORT     ClickHouse HTTP 端口 (默认: 8123)
#   CLICKHOUSE_DATABASE ClickHouse 数据库 (默认: agentscope_traces)
#
# 退出码:
#   0 - 所有检查通过
#   1 - 至少一项检查失败

set -euo pipefail

# ---- 配置 ----
PGHOST="${PGHOST:-localhost}"
PGPORT="${PGPORT:-5432}"
PGDATABASE="${PGDATABASE:-agentscope_test}"
PGUSER="${PGUSER:-agentscope}"
PGPASSWORD="${PGPASSWORD:-agentscope}"

CLICKHOUSE_HOST="${CLICKHOUSE_HOST:-localhost}"
CLICKHOUSE_PORT="${CLICKHOUSE_PORT:-8123}"
CLICKHOUSE_DATABASE="${CLICKHOUSE_DATABASE:-agentscope_traces}"

# ---- 参数解析 ----
PROJECT_ID=""
EXPERIMENT_ID=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --project-id) PROJECT_ID="$2"; shift 2 ;;
    --experiment-id) EXPERIMENT_ID="$2"; shift 2 ;;
    --help|-h)
      echo "用法: $0 --project-id <project_id> [--experiment-id <experiment_id>]"
      exit 0
      ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

# ---- 辅助函数 ----
PASS=0
FAIL=0

pass() {
  PASS=$((PASS + 1))
  echo "  ✅ $1"
}

fail() {
  FAIL=$((FAIL + 1))
  echo "  ❌ $1"
}

info() {
  echo "  ℹ️  $1"
}

pg_query() {
  docker exec agentscope-postgres-1 psql -U "${PGUSER}" -d "${PGDATABASE}" -tA -c "$1" 2>/dev/null || echo ""
}

ch_query() {
  local query="$1"
  docker exec agentscope-clickhouse-1 clickhouse-client --database "${CLICKHOUSE_DATABASE}" --query "${query}" 2>/dev/null || echo ""
}

echo ""
echo "═══════════════════════════════════════════════"
echo "  AgentScope Demo 数据完整性验证"
echo "═══════════════════════════════════════════════"
echo ""

# ============================================================
# 1. 项目验证
# ============================================================
echo "━━━ [1] 项目验证 ━━━"

if [[ -z "${PROJECT_ID}" ]]; then
  info "未指定 --project-id，自动查找最新项目"
  PROJECT_ID=$(pg_query "SELECT id::text FROM projects ORDER BY created_at DESC LIMIT 1")
  if [[ -z "${PROJECT_ID}" ]]; then
    fail "数据库中无项目记录"
    echo ""
    echo "═══════════════════════════════════════════════"
    echo "  验证结果: ${PASS} 通过, ${FAIL} 失败"
    echo "═══════════════════════════════════════════════"
    exit 1
  fi
  info "使用最新项目: ${PROJECT_ID}"
fi

PROJECT_EXISTS=$(pg_query "SELECT count(*) FROM projects WHERE id='${PROJECT_ID}'")
if [[ "${PROJECT_EXISTS}" != "0" ]]; then
  PROJECT_NAME=$(pg_query "SELECT name FROM projects WHERE id='${PROJECT_ID}'")
  pass "项目存在: ${PROJECT_NAME} (${PROJECT_ID})"
else
  fail "项目 ${PROJECT_ID} 不存在"
fi

echo ""

# ============================================================
# 2. Agent 验证
# ============================================================
echo "━━━ [2] Agent 验证 ━━━"

AGENT_COUNT=$(pg_query "SELECT count(*) FROM agents WHERE project_id='${PROJECT_ID}'")
if [[ "${AGENT_COUNT}" -gt 0 ]]; then
  pass "Agent 注册存在 (${AGENT_COUNT} 个)"
  pg_query "SELECT name FROM agents WHERE project_id='${PROJECT_ID}'" | while read -r name; do
    info "  - ${name}"
  done
else
  info "无 Agent 注册记录（此 demo 可能未使用 Agent 运行时）"
fi

INSTANCE_COUNT=$(pg_query "SELECT count(*) FROM agent_instances WHERE project_id='${PROJECT_ID}'")
if [[ "${INSTANCE_COUNT}" -gt 0 ]]; then
  pass "Agent 实例存在 (${INSTANCE_COUNT} 个)"
  LAST_SEEN=$(pg_query "SELECT count(*) FROM agent_instances WHERE project_id='${PROJECT_ID}' AND updated_at IS NOT NULL")
  if [[ "${LAST_SEEN}" -gt 0 ]]; then
    pass "实例 last_seen_at 有值 (${LAST_SEEN}/${INSTANCE_COUNT})"
  else
    fail "实例 last_seen_at 全部为空"
  fi
else
  info "无 Agent 实例记录（demo 可能未完成心跳注册）"
fi

echo ""

# ============================================================
# 3. Run 验证
# ============================================================
echo "━━━ [3] Run/Trace 一致性验证 ━━━"

RUN_COUNT=$(pg_query "SELECT count(*) FROM runs WHERE project_id='${PROJECT_ID}'")
if [[ "${RUN_COUNT}" -gt 0 ]]; then
  pass "Run 记录存在 (${RUN_COUNT} 个)"
else
  fail "无 Run 记录"
fi

TERMINAL_COUNT=$(pg_query "SELECT count(*) FROM runs WHERE project_id='${PROJECT_ID}' AND status IN ('completed', 'failed', 'error')")
NON_TERMINAL=$(pg_query "SELECT count(*) FROM runs WHERE project_id='${PROJECT_ID}' AND status NOT IN ('completed', 'failed', 'error')")
if [[ "${NON_TERMINAL}" -eq 0 ]]; then
  pass "所有 Run 状态已关闭 (全部 terminal)"
else
  warn "存在 ${NON_TERMINAL} 个未关闭的 Run（状态: $(pg_query "SELECT DISTINCT status FROM runs WHERE project_id='${PROJECT_ID}' AND status NOT IN ('completed', 'failed', 'error')" | tr '\n' ' ')）"
fi

# Check runs with trace but no input/output
WITH_TRACE=$(pg_query "SELECT count(*) FROM runs WHERE project_id='${PROJECT_ID}' AND trace_id IS NOT NULL AND trace_id != ''")
WITH_INPUT=$(pg_query "SELECT count(*) FROM runs WHERE project_id='${PROJECT_ID}' AND trace_id IS NOT NULL AND trace_id != '' AND input_summary IS NOT NULL AND input_summary != ''")
WITH_OUTPUT=$(pg_query "SELECT count(*) FROM runs WHERE project_id='${PROJECT_ID}' AND trace_id IS NOT NULL AND trace_id != '' AND output_summary IS NOT NULL AND output_summary != ''")

if [[ "${WITH_TRACE}" -gt 0 ]]; then
  pass "有 trace 的 run: ${WITH_TRACE} 个"
  if [[ "${WITH_INPUT}" -eq "${WITH_TRACE}" ]]; then
    pass "所有有 trace 的 run 都有 input"
  else
    info "$((WITH_TRACE - WITH_INPUT)) 个有 trace 的 run 没有 input（可能来自 SDK runtime run）"
  fi
  if [[ "${WITH_OUTPUT}" -eq "${WITH_TRACE}" ]]; then
    pass "所有有 trace 的 run 都有 output"
  else
    info "$((WITH_TRACE - WITH_OUTPUT)) 个有 trace 的 run 没有 output（可能来自 SDK runtime run）"
  fi
else
  info "无带 trace 的 run"
fi

# Agent name check
UNKNOWN_AGENT=$(pg_query "SELECT count(*) FROM runs WHERE project_id='${PROJECT_ID}' AND agent_name='unknown'")
if [[ "${UNKNOWN_AGENT}" -eq 0 ]]; then
  pass "所有 run 的 agent_name 已正确设置"
else
  info "${UNKNOWN_AGENT} 个 run 的 agent_name 为 unknown（由 OTLP trace 创建，agent name 未关联）"
fi

echo ""

# ============================================================
# 4. 评测结果验证
# ============================================================
echo "━━━ [4] 评测结果验证 ━━━"

# Check eval case results
ECR_COUNT=$(pg_query "SELECT count(*) FROM eval_case_results WHERE run_id IN (SELECT id::text FROM runs WHERE project_id='${PROJECT_ID}')")
if [[ "${ECR_COUNT}" -gt 0 ]]; then
  pass "评测结果存在 (${ECR_COUNT} 条)"
  # Check run_id linkage
  LINKED=$(pg_query "
    SELECT count(*) FROM eval_case_results ecr
    JOIN runs r ON r.id::text = ecr.run_id::text
    WHERE r.project_id='${PROJECT_ID}'
  ")
  if [[ "${LINKED}" -eq "${ECR_COUNT}" ]]; then
    pass "所有 eval_case_results 都能关联到 runs"
  else
    fail "${LINKED}/${ECR_COUNT} 的 eval_case_results 能关联到 runs"
  fi
else
  info "无评测结果（此项目可能未运行评测）"
fi

# Check eval scores
ES_COUNT=$(pg_query "SELECT count(*) FROM eval_scores WHERE project_id='${PROJECT_ID}'")
if [[ "${ES_COUNT}" -gt 0 ]]; then
  pass "多维评测分数存在 (${ES_COUNT} 条)"
  INVALID_SCORE=$(pg_query "SELECT count(*) FROM eval_scores WHERE project_id='${PROJECT_ID}' AND (score < 0 OR score > 1)")
  if [[ "${INVALID_SCORE}" -eq 0 ]]; then
    pass "所有 eval_scores.score 在 [0, 1] 范围内"
  else
    fail "${INVALID_SCORE} 条 eval_scores.score 超出 [0, 1] 范围"
  fi
else
  info "无多维评测分数（此项目可能未配置多维评测）"
fi

echo ""

# ============================================================
# 5. ClickHouse Span 验证
# ============================================================
echo "━━━ [5] ClickHouse Span 验证 ━━━"

CH_CONNECTED=""
CH_SPAN_COUNT=$(ch_query "SELECT count() FROM trace_spans WHERE project_id='${PROJECT_ID}'" 2>/dev/null || echo "")

if [[ -n "${CH_SPAN_COUNT}" ]] && [[ "${CH_SPAN_COUNT}" -gt 0 ]]; then
  pass "ClickHouse 可访问，按 project_id 查到 ${CH_SPAN_COUNT} 条 span"
  CH_CONNECTED="true"

  # Check run-level spans
  CH_RUN_COUNT=$(ch_query "SELECT count(DISTINCT run_id) FROM trace_spans WHERE project_id='${PROJECT_ID}' AND run_id != '00000000-0000-0000-0000-000000000000'" 2>/dev/null || echo "0")
  info "涉及 ${CH_RUN_COUNT} 个不同的 run_id"

  # Check agent semantic spans
  for span_kind in "THOUGHT" "LLM" "TOOL" "RETRIEVER" "SKILL" "FINAL_ANSWER"; do
    local_count=$(ch_query "SELECT count() FROM trace_spans WHERE project_id='${PROJECT_ID}' AND attributes LIKE '%agentscope.span.kind%${span_kind}%' " 2>/dev/null || echo "0")
    if [[ "${local_count}" -gt 0 ]]; then
      info "  ${span_kind} span: ${local_count} 条"
    fi
  done

  # Check thought spans
  THOUGHT_COUNT=$(ch_query "SELECT count() FROM trace_thoughts WHERE project_id='${PROJECT_ID}'" 2>/dev/null || echo "0")
  if [[ "${THOUGHT_COUNT}" -gt 0 ]]; then
    pass "Thought span 存在 (${THOUGHT_COUNT} 条)"
  else
    info "Thought span 不存在（demo 可能未上报 thought）"
  fi

  # Check LLM calls in ClickHouse
  LLM_CH_COUNT=$(ch_query "SELECT count() FROM trace_llm_calls WHERE project_id='${PROJECT_ID}'" 2>/dev/null || echo "0")
  if [[ "${LLM_CH_COUNT}" -gt 0 ]]; then
    pass "LLM call 记录存在 (${LLM_CH_COUNT} 条)"
  else
    info "LLM call 记录不存在（demo 可能未使用 LLM 或未捕获）"
  fi

  # Check tool calls
  TOOL_CH_COUNT=$(ch_query "SELECT count() FROM trace_tool_calls WHERE project_id='${PROJECT_ID}'" 2>/dev/null || echo "0")
  if [[ "${TOOL_CH_COUNT}" -gt 0 ]]; then
    pass "Tool call 记录存在 (${TOOL_CH_COUNT} 条)"
  else
    info "Tool call 记录不存在（demo 可能未使用工具）"
  fi

  # Check retrieval calls
  RETRIEVAL_CH_COUNT=$(ch_query "SELECT count() FROM trace_retrieval_calls WHERE project_id='${PROJECT_ID}'" 2>/dev/null || echo "0")
  if [[ "${RETRIEVAL_CH_COUNT}" -gt 0 ]]; then
    pass "Retrieval call 记录存在 (${RETRIEVAL_CH_COUNT} 条)"
  else
    info "Retrieval call 记录不存在（demo 可能未使用知识库检索）"
  fi

  # Check skill calls
  SKILL_CH_COUNT=$(ch_query "SELECT count() FROM trace_skill_calls WHERE project_id='${PROJECT_ID}'" 2>/dev/null || echo "0")
  if [[ "${SKILL_CH_COUNT}" -gt 0 ]]; then
    pass "Skill call 记录存在 (${SKILL_CH_COUNT} 条)"
  else
    info "Skill call 记录不存在（demo 可能未使用 skill）"
  fi

  # Check final answers
  FA_CH_COUNT=$(ch_query "SELECT count() FROM trace_final_answers WHERE project_id='${PROJECT_ID}'" 2>/dev/null || echo "0")
  if [[ "${FA_CH_COUNT}" -gt 0 ]]; then
    pass "FinalAnswer 记录存在 (${FA_CH_COUNT} 条)"
  else
    info "FinalAnswer 记录不存在（demo 可能未上报 final answer）"
  fi

else
  info "ClickHouse 无数据（\"${CH_SPAN_COUNT}\"），跳过 span 验证"
fi

echo ""

# ============================================================
# 6. 实验验证（可选）
# ============================================================
if [[ -n "${EXPERIMENT_ID}" ]]; then
  echo "━━━ [6] 实验验证 ━━━"

  EXP_STATUS=$(pg_query "SELECT status FROM experiments WHERE id='${EXPERIMENT_ID}' AND project_id='${PROJECT_ID}'" 2>/dev/null || echo "")
  if [[ -n "${EXP_STATUS}" ]]; then
    pass "实验存在，状态: ${EXP_STATUS}"

    TOTAL_CASES=$(pg_query "SELECT total_cases FROM experiments WHERE id='${EXPERIMENT_ID}'")
    RESULTS_COUNT=$(pg_query "SELECT count(*) FROM eval_case_results WHERE eval_job_id IN (SELECT id FROM eval_jobs WHERE experiment_id='${EXPERIMENT_ID}')")

    if [[ "${EXP_STATUS}" == "completed" ]]; then
      pass "实验已完成"
      if [[ "${RESULTS_COUNT}" -ge "${TOTAL_CASES}" ]]; then
        pass "实验结果完整 (${RESULTS_COUNT}/${TOTAL_CASES})"
      else
        info "实验结果 ${RESULTS_COUNT}/${TOTAL_CASES}（可能某些 case 没有结果）"
      fi
    elif [[ "${EXP_STATUS}" == "failed" ]]; then
      info "实验状态为 failed，请检查 runner 日志"
    fi
  else
    fail "实验 ${EXPERIMENT_ID} 不存在"
  fi

  echo ""
fi

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
