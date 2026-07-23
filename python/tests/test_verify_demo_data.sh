#!/usr/bin/env bash
# 验证 verify-demo-data.sh 脚本的参数校验和失败处理。
# 这些测试不依赖数据库，只测试脚本的 CLI 行为。
##
set -euo pipefail

SCRIPT="$(cd "$(dirname "$0")/../.." && pwd)/scripts/verify-demo-data.sh"
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

echo "═══════════════════════════════════════════════"
echo "  verify-demo-data.sh 参数校验测试"
echo "═══════════════════════════════════════════════"
echo ""

# Test 1: missing --project-id
echo "━━━ [1] 缺少 --project-id ━━━"
if output=$("$SCRIPT" --run-id "run-abc" 2>&1); then
  fail "缺少 --project-id 时应退出非零"
else
  pass "缺少 --project-id 时正确退出非零"
fi
echo ""

# Test 2: missing --run-id
echo "━━━ [2] 缺少 --run-id ━━━"
if output=$("$SCRIPT" --project-id "proj-abc" 2>&1); then
  fail "缺少 --run-id 时应退出非零"
else
  pass "缺少 --run-id 时正确退出非零"
fi
echo ""

# Test 3: --help
echo "━━━ [3] --help ━━━"
if output=$("$SCRIPT" --help 2>&1); then
  if echo "$output" | grep -q "用法"; then
    pass "--help 正确显示用法"
  else
    fail "--help 输出缺少'用法'"
  fi
else
  fail "--help 应退出 0"
fi
echo ""

# Test 4: --skip-db-check exits 0
echo "━━━ [4] --skip-db-check ━━━"
if output=$("$SCRIPT" --project-id "proj-abc" --run-id "run-abc" --skip-db-check 2>&1); then
  if echo "$output" | grep -q "跳过"; then
    pass "--skip-db-check 正确跳过"
  else
    fail "--skip-db-check 输出缺少'跳过'"
  fi
else
  fail "--skip-db-check 应退出 0"
fi
echo ""

# Test 5: unknown argument
echo "━━━ [5] 未知参数 ━━━"
if output=$("$SCRIPT" --unknown-arg 2>&1); then
  fail "未知参数时应退出非零"
else
  pass "未知参数时正确退出非零"
fi
echo ""

# Test 6: all required params provided validates successfully
echo "━━━ [6] 完整参数解析 ━━━"
if output=$("$SCRIPT" --project-id "proj-abc" --run-id "run-abc" --annotation-item-id "item-abc" --dataset-case-id "case-abc" --experiment-id "exp-abc" --skip-db-check 2>&1); then
  pass "完整参数解析成功"
else
  fail "完整参数应退出 0（--skip-db-check 时）"
fi
echo ""

# 汇总
echo "═══════════════════════════════════════════════"
echo "  结果: ${PASS} 通过, ${FAIL} 失败"
echo "═══════════════════════════════════════════════"

if [[ "${FAIL}" -gt 0 ]]; then
  exit 1
fi
exit 0
