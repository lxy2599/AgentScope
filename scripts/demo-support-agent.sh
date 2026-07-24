#!/usr/bin/env bash
# AgentScope 客服订单处理 Agent 演示
#
# 使用方法:
#   1. 确保后端运行: make dev
#   2. 配置 LLM（可选，不加则使用 Fake LLM 模式）:
#        export AGENTSCOPE_LLM_PROVIDER=deepseek
#        export AGENTSCOPE_LLM_BASE_URL=https://api.deepseek.com/v1
#        export AGENTSCOPE_LLM_API_KEY=sk-xxxx
#        export AGENTSCOPE_LLM_MODEL=deepseek-chat
#   3. 运行: ./scripts/demo-support-agent.sh
#
# 缺失 LLM key 时自动使用 Fake LLM 模式运行演示。

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_URL="${AGENTSCOPE_SERVER_URL:-http://127.0.0.1:8080}"

log() { printf '[demo-support-agent] %s\n' "$*" >&2; }
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

check_llm_config() {
  # LLM 是可选的，缺失时使用 Fake LLM 模式
  if [ -z "${AGENTSCOPE_LLM_API_KEY:-}" ]; then
    log "未检测到 LLM API Key，将使用 Fake LLM 模式运行。"
    log "如需真实 LLM，请设置环境变量："
    log ""
    log "  DeepSeek 示例:"
    log "    export AGENTSCOPE_LLM_PROVIDER=deepseek"
    log "    export AGENTSCOPE_LLM_BASE_URL=https://api.deepseek.com/v1"
    log "    export AGENTSCOPE_LLM_API_KEY=你的API密钥"
    log "    export AGENTSCOPE_LLM_MODEL=deepseek-chat"
    log ""
    log "  GLM 示例:"
    log "    export AGENTSCOPE_LLM_PROVIDER=glm"
    log "    export AGENTSCOPE_LLM_BASE_URL=https://open.bigmodel.cn/api/paas/v4"
    log "    export AGENTSCOPE_LLM_API_KEY=你的API密钥"
    log "    export AGENTSCOPE_LLM_MODEL=glm-4.7-flash"
    log ""
    log "继续使用 Fake LLM 模式…"
    return 1
  fi
  log "LLM 配置: provider=${AGENTSCOPE_LLM_PROVIDER} model=${AGENTSCOPE_LLM_MODEL}"
  log "LLM 配置通过 ✓"
  return 0
}

install_sdk() {
  log "安装 AgentScope Python SDK..."
  pip install -q -e "${ROOT_DIR}/python"
  log "SDK 安装完成"
}

detect_fake_flag() {
  # 如果没有 API key，自动传 --fake
  if [ -z "${AGENTSCOPE_LLM_API_KEY:-}" ]; then
    echo "--fake"
  else
    echo ""
  fi
}

main() {
  log "AgentScope 客服订单处理 Agent 演示"
  echo ""

  # 1. Check server
  wait_for_server "${SERVER_URL}"

  # 2. LLM 配置检查（可选）
  check_llm_config || true

  # 3. Install Python SDK
  install_sdk

  # 4. Run the demo
  FAKE_FLAG="$(detect_fake_flag)"
  log "运行客服 Agent 演示..."
  if [ -n "${FAKE_FLAG}" ]; then
    log "模式: Fake LLM（不调用真实模型）"
  else
    log "模式: 真实 LLM（${AGENTSCOPE_LLM_PROVIDER}/${AGENTSCOPE_LLM_MODEL}）"
  fi
  echo ""

  # shellcheck disable=SC2086
  python3 "${ROOT_DIR}/python/examples/customer_support_agent.py" \
    --server "${SERVER_URL}" ${FAKE_FLAG}
}

main "$@"
