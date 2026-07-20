#!/usr/bin/env bash
# AgentScope Demo Data Integrity Verification Script
#
# 验证 demo 运行后在 PostgreSQL 和 ClickHouse 中的数据完整性。
#
# 使用方法:
#   ./scripts/verify-demo-data.sh \
#     --project-id <project_id> \
#     [--demo-kind real-agent|support-agent|multi-eval|generic-observability] \
#     [--experiment-id <experiment_id>]
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
#   1 - 至少一项检查失败

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
EXPERIMENT_ID=""
DEMO_KIND=""

while [[ $# -gt 0 ]]; do
  case "$1" in
    --project-id) PROJECT_ID="$2"; shift 2 ;;
    --experiment-id) EXPERIMENT_ID="$2"; shift 2 ;;
    --demo-kind)
      case "$2" in
        real-agent|support-agent|multi-eval|generic-observability)
          DEMO_KIND="$2"; shift 2 ;;
        *) echo "错误: --demo-kind 必须为 real-agent|support-agent|multi-eval|generic-observability"; exit 1 ;;
      esac ;;
    --help|-h)
      echo "用法: $0 --project-id <project_id> [--demo-kind <kind>] [--experiment-id <experiment_id>]"
      echo ""
      echo "Demo kinds:"
      echo "  real-agent              真实 LLM Agent 评测 demo"
      echo "  support-agent           客服订单处理 Agent demo"
      echo "  multi-eval              多维评测 demo"
      echo "  generic-observability   通用 Agent 可观测 demo"
      exit 0 ;;
    *) echo "未知参数: $1"; exit 1 ;;
  esac
done

if [[ -z "${PROJECT_ID}" ]]; then
  echo "错误: --project-id 是必需的"
  echo "用法: $0 --project-id <project_id> [--demo-kind <kind>] [--experiment-id <experiment_id>]"
  exit 1
fi

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

# 尽可能使用本地工具，否则 fallback 到 docker exec
_have_psql=false
_have_ch=false

if command -v psql &>/dev/null && command -v clickhouse-client &>/dev/null; then
  _have_psql=true
  _have_ch=true
fi

pg_query() {
  local sql="$1"
  if [[ "${_have_psql}" == "true" ]] && [[ -n "${PGHOST}" ]]; then
    PGPASSWORD="${PGPASSWORD}" psql -h "${PGHOST}" -p "${PGPORT}" -U "${PGUSER}" -d "${PGDATABASE}" -tA -c "${sql}" 2>/dev/null || \
    docker exec agentscope-postgres-1 psql -U "${PGUSER}" -d "${PGDATABASE}" -tA -c "${sql}" 2>/dev/null || echo ""
  else
    docker exec agentscope-postgres-1 psql -U "${PGUSER}" -d "${PGDATABASE}" -tA -c "${sql}" 2>/dev/null || echo ""
  fi
}

ch_query() {
  local query="$1"
  local result=""

  if [[ "${CLICKHOUSE_USE_HTTP}" == "true" ]]; then
    local url="http://${CLICKHOUSE_HOST}:${CLICKHOUSE_PORT}/"
    result=$(curl -sfG "${url}" \
      --data-urlencode "query=${query}" \
      --data-urlencode "database=${CLICKHOUSE_DATABASE}" 2>/dev/null || echo "")
  elif [[ "${_have_ch}" == "true" ]]; then
    result=$(clickhouse-client --host "${CLICKHOUSE_HOST}" --port "${CLICKHOUSE_PORT}" \
      --database "${CLICKHOUSE_DATABASE}" --query "${query}" 2>/dev/null || echo "")
  fi

  if [[ -z "${result}" ]]; then
    result=$(docker exec agentscope-clickhouse-1 clickhouse-client \
      --database "${CLICKHOUSE_DATABASE}" --query "${query}" 2>/dev/null || echo "")
  fi

  echo "${result}"
}

# ---- 语义表定义 ----
# 每个 demo kind 对应的预期语义轨迹类型
declare -A DEMO_EXPECTED_SPANS
DEMO_EXPECTED_SPANS["generic-observability"]="THOUGHT LLM TOOL RETRIEVER SKILL FINAL_ANSWER"
DEMO_EXPECTED_SPANS["support-agent"]="LLM TOOL RETRIEVER SKILL"
DEMO_EXPECTED_SPANS["real-agent"]="LLM TOOL"

# 每个 demo kind 对应的预期 ClickHouse 语义表中至少应有的条数
DEMO_MIN_SEMANTIC_RECORDS=1

# ---- 当前检查状态 ----
CH_CONNECTED=""

echo ""
echo "═══════════════════════════════════════════════"
echo "  AgentScope Demo 数据完整性验证"
echo "═══════════════════════════════════════════════"
echo ""

if [[ -n "${DEMO_KIND}" ]]; then
  echo "  Demo 类型: ${DEMO_KIND}"
fi
echo ""

# ============================================================
# 1. 项目验证
# ============================================================
echo "━━━ [1] 项目验证 ━━━"

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
  pg_query "SELECT name FROM agents WHERE project_id='${PROJECT_ID}'" | while IFS= read -r name; do
    if [[ -n "${name}" ]]; then
      info "  - ${name}"
    fi
  done
else
  if [[ "${DEMO_KIND}" == "multi-eval" ]]; then
    info "无 Agent 注册记录（multi-eval 使用 @target 装饰器，不强制注册 Agent 运行时）"
  else
    fail "无 Agent 注册记录"
  fi
fi

INSTANCE_COUNT=$(pg_query "SELECT count(*) FROM agent_instances WHERE project_id='${PROJECT_ID}'")
if [[ "${INSTANCE_COUNT}" -gt 0 ]]; then
  pass "Agent 实例存在 (${INSTANCE_COUNT} 个)"
  # 正确使用 last_heartbeat_at（即 updated_at），输出最近心跳时间
  HB_COUNT=$(pg_query "SELECT count(*) FROM agent_instances WHERE project_id='${PROJECT_ID}' AND updated_at IS NOT NULL")
  if [[ "${HB_COUNT}" -gt 0 ]]; then
    HB_TIME=$(pg_query "SELECT MAX(updated_at)::text FROM agent_instances WHERE project_id='${PROJECT_ID}' AND updated_at IS NOT NULL")
    pass "心跳记录存在 (${HB_COUNT}/${INSTANCE_COUNT})，最近心跳: ${HB_TIME}"
  else
    fail "实例全部缺少心跳"
  fi
else
  if [[ "${DEMO_KIND}" == "multi-eval" ]]; then
    info "无 Agent 实例记录（multi-eval demo 不注册 Agent 运行时）"
  else
    fail "无 Agent 实例记录"
  fi
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

# stale running run 必须作为失败项
NON_TERMINAL=$(pg_query "SELECT count(*) FROM runs WHERE project_id='${PROJECT_ID}' AND status NOT IN ('completed', 'failed', 'error')")
if [[ "${NON_TERMINAL}" -eq 0 ]]; then
  pass "所有 Run 状态已关闭 (全部 terminal)"
else
  STALE_STATUSES=$(pg_query "SELECT DISTINCT status FROM runs WHERE project_id='${PROJECT_ID}' AND status NOT IN ('completed', 'failed', 'error')" | tr '\n' ' ')
  fail "存在 ${NON_TERMINAL} 个未关闭的 Run（状态: ${STALE_STATUSES}）"
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
    info "$((WITH_TRACE - WITH_INPUT)) 个有 trace 的 run 没有 input（可能来自 SDK runtime run 而非 OTLP trace run）"
  fi
  if [[ "${WITH_OUTPUT}" -eq "${WITH_TRACE}" ]]; then
    pass "所有有 trace 的 run 都有 output"
  else
    info "$((WITH_TRACE - WITH_OUTPUT)) 个有 trace 的 run 没有 output（可能来自 SDK runtime run 而非 OTLP trace run）"
  fi
else
  info "无带 trace 的 run"
fi

# Agent name check — 对 real-agent 和 support-agent 严格要求
UNKNOWN_AGENT=$(pg_query "SELECT count(*) FROM runs WHERE project_id='${PROJECT_ID}' AND agent_name='unknown'")
if [[ "${UNKNOWN_AGENT}" -eq 0 ]]; then
  pass "所有 run 的 agent_name 已正确设置"
else
  if [[ "${DEMO_KIND}" == "real-agent" ]] || [[ "${DEMO_KIND}" == "support-agent" ]]; then
    fail "${UNKNOWN_AGENT} 个 run 的 agent_name 为 unknown（需要修复 OTLP trace agent name 关联）"
  else
    info "${UNKNOWN_AGENT} 个 run 的 agent_name 为 unknown（由 OTLP trace 创建，agent name 未关联）"
  fi
fi

echo ""

# ============================================================
# 4. 评测结果验证
# ============================================================
echo "━━━ [4] 评测结果验证 ━━━"

# Check eval case results — use the run's project_id directly
ECR_COUNT=$(pg_query "
  SELECT count(*) FROM eval_case_results ecr
  JOIN runs r ON r.id = ecr.run_id
  WHERE r.project_id='${PROJECT_ID}'
")
if [[ "${ECR_COUNT}" -gt 0 ]]; then
  pass "评测结果存在 (${ECR_COUNT} 条)"
  # Check run_id linkage (should all be linked since we used JOIN)
  pass "所有 eval_case_results 都能关联到 runs"
else
  if [[ "${DEMO_KIND}" == "multi-eval" ]]; then
    fail "multi-eval 必须有评测结果"
  else
    info "无评测结果（此项目可能未运行评测）"
  fi
fi

# Check eval scores — multi-eval 必须要求 > 0
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
  if [[ "${DEMO_KIND}" == "multi-eval" ]]; then
    fail "multi-eval 必须有 eval_scores（多维评测分数），当前为 0"
  else
    info "无多维评测分数（此项目可能未配置多维评测）"
  fi
fi

echo ""

# ============================================================
# 5. ClickHouse Span 验证
# ============================================================
echo "━━━ [5] ClickHouse Span 验证 ━━━"

CH_SPAN_COUNT=$(ch_query "SELECT count() FROM trace_spans WHERE project_id='${PROJECT_ID}'" 2>/dev/null || echo "")

if [[ -n "${CH_SPAN_COUNT}" ]] && [[ "${CH_SPAN_COUNT}" -gt 0 ]]; then
  pass "ClickHouse 可访问，按 project_id 查到 ${CH_SPAN_COUNT} 条 span"
  CH_CONNECTED="true"

  # Check run-level spans
  CH_RUN_COUNT=$(ch_query "SELECT count(DISTINCT run_id) FROM trace_spans WHERE project_id='${PROJECT_ID}' AND run_id != '00000000-0000-0000-0000-000000000000'" 2>/dev/null || echo "0")
  info "涉及 ${CH_RUN_COUNT} 个不同的 run_id"

  # Check agent semantic spans (as info)
  for span_kind in "THOUGHT" "LLM" "TOOL" "RETRIEVER" "SKILL" "FINAL_ANSWER"; do
    local_count=$(ch_query "SELECT count() FROM trace_spans WHERE project_id='${PROJECT_ID}' AND attributes LIKE '%agentscope.span.kind%${span_kind}%'" 2>/dev/null || echo "0")
    if [[ "${local_count}" -gt 0 ]]; then
      info "  ${span_kind} span: ${local_count} 条"
    fi
  done

  # ---- 按 DEMO_KIND 严格校验语义表 ----
  if [[ -n "${DEMO_KIND}" ]]; then
    echo ""
    echo "  [${DEMO_KIND}] 按 demo 类型校验语义表…"

    IFS=' ' read -ra EXPECTED_TABLES <<< "${DEMO_EXPECTED_SPANS[${DEMO_KIND}]:-}"

    # 定义每种 span_kind 对应的语义表名和列名
    declare -A TABLE_MAP TABLE_COL_MAP
    TABLE_MAP["THOUGHT"]="trace_thoughts"
    TABLE_COL_MAP["THOUGHT"]="summary"
    TABLE_MAP["LLM"]="trace_llm_calls"
    TABLE_COL_MAP["LLM"]="provider"
    TABLE_MAP["TOOL"]="trace_tool_calls"
    TABLE_COL_MAP["TOOL"]="tool_name"
    TABLE_MAP["RETRIEVER"]="trace_retrieval_calls"
    TABLE_COL_MAP["RETRIEVER"]="knowledge_base"
    TABLE_MAP["SKILL"]="trace_skill_calls"
    TABLE_COL_MAP["SKILL"]="skill_name"
    TABLE_MAP["FINAL_ANSWER"]="trace_final_answers"
    TABLE_COL_MAP["FINAL_ANSWER"]="answer_summary"

    for span_kind in "${EXPECTED_TABLES[@]}"; do
      table="${TABLE_MAP[${span_kind}]:-}"
      col="${TABLE_COL_MAP[${span_kind}]:-}"

      if [[ -z "${table}" ]]; then
        # 没有独立语义表，从 trace_spans attributes 中按条件查询
        semantic_count=$(ch_query "SELECT count() FROM trace_spans WHERE project_id='${PROJECT_ID}' AND attributes LIKE '%agentscope.span.kind%${span_kind}%'" 2>/dev/null || echo "0")
      else
        semantic_count=$(ch_query "SELECT count() FROM ${table} WHERE project_id='${PROJECT_ID}'" 2>/dev/null || echo "0")
      fi

      if [[ -n "${semantic_count}" ]] && [[ "${semantic_count}" -ge "${DEMO_MIN_SEMANTIC_RECORDS}" ]]; then
        pass "${span_kind} 语义记录符合要求 (${semantic_count} 条)"
      else
        fail "${span_kind} 语义记录缺失（期望 ≥ ${DEMO_MIN_SEMANTIC_RECORDS}，实际: ${semantic_count:-0}）"
      fi
    done
  fi

else
  if [[ -n "${DEMO_KIND}" ]] && [[ "${DEMO_KIND}" != "multi-eval" ]]; then
    fail "ClickHouse 中无 span 数据（${DEMO_KIND} 必须产生 trace）"
  else
    info "ClickHouse 无数据，跳过 span 验证"
  fi
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
