#!/usr/bin/env bash
# AgentScope 企业级 AgentOps 一次串联闭环演示
#
# 本演示统一展示 AgentScope 全部核心能力：
# - Agent 注册/心跳/在线状态
# - 完整可观测（thought/LLM/tool/retrieval/skill/final_answer）
# - 数据集、实验创建
# - Runner 执行多维评测
# - 聚合评分与报告
# - 数据库和 API 完整性验证
#
# 使用方法:
#   1. 确保后端运行: make dev
#   2. 运行: make demo-enterprise-agentops
#     或: ./scripts/demo-enterprise-agentops.sh
#
# 环境变量:
#   AGENTSCOPE_LLM_API_KEY  — 真实 LLM 密钥（可选，默认 deterministic 模式）
#   AGENTSCOPE_LLM_PROVIDER — LLM 提供商
#   AGENTSCOPE_LLM_MODEL    — LLM 模型名

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_URL="${AGENTSCOPE_SERVER_URL:-http://127.0.0.1:8080}"

log() { printf '[demo-enterprise-agentops] %s\n' "$*" >&2; }
die() { log "错误: $*"; exit 1; }

wait_for_server() {
  local url="${1}"
  local max_attempts=30
  log "等待后端启动: ${url}/healthz ..."
  for _ in $(seq 1 "${max_attempts}"); do
    if curl -sf "${url}/healthz" >/dev/null 2>&1; then
      log "后端就绪"
      return 0
    fi
    sleep 1
  done
  die "后端 ${max_attempts}s 内未就绪，请先运行 make dev"
}

install_sdk() {
  log "安装 AgentScope Python SDK..."
  pip install -q -e "${ROOT_DIR}/python"
  log "SDK 安装完成"
}

check_llm_config() {
  if [ -n "${AGENTSCOPE_LLM_API_KEY:-}" ]; then
    log "LLM 模式: 真实 LLM (${AGENTSCOPE_LLM_PROVIDER:-unknown}/${AGENTSCOPE_LLM_MODEL:-unknown})"
  else
    log "LLM 模式: 确定性 Mock（无需 API key）"
    log "如需真实 LLM，设置: AGENTSCOPE_LLM_PROVIDER + AGENTSCOPE_LLM_API_KEY + AGENTSCOPE_LLM_MODEL"
  fi
}

main() {
  log "AgentScope 企业级 AgentOps 闭环演示"
  echo ""

  # 1. 等待后端
  wait_for_server "${SERVER_URL}"

  # 2. 检查 LLM 配置
  check_llm_config

  # 3. 安装 SDK
  install_sdk

  # 4. 运行演示
  log "运行企业级演示..."
  echo ""

  python3 "${ROOT_DIR}/python/examples/enterprise_agentops_demo.py" \
    --server "${SERVER_URL}"
}

main "$@"
