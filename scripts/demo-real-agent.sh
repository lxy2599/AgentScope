#!/usr/bin/env bash
# AgentScope 真实 LLM Agent 端到端评测演示
#
# 使用方法:
#   1. 确保后端运行: make dev
#   2. 配置 LLM（DeepSeek 示例）:
#        export AGENTSCOPE_LLM_PROVIDER=deepseek
#        export AGENTSCOPE_LLM_BASE_URL=https://api.deepseek.com/v1
#        export AGENTSCOPE_LLM_API_KEY=sk-xxxx
#        export AGENTSCOPE_LLM_MODEL=deepseek-chat
#   3. 运行: ./scripts/demo-real-agent.sh
#
# 缺失 LLM key 时会直接失败，没有 mock fallback。

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_URL="${SERVER_URL:-http://127.0.0.1:8080}"

log() { printf '[demo-real-agent] %s\n' "$*" >&2; }
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
  log "检查 LLM 配置…"
  local missing=0
  if [ -z "${AGENTSCOPE_LLM_PROVIDER:-}" ]; then
    log "  MISSING: AGENTSCOPE_LLM_PROVIDER"
    missing=1
  fi
  if [ -z "${AGENTSCOPE_LLM_BASE_URL:-}" ]; then
    log "  MISSING: AGENTSCOPE_LLM_BASE_URL"
    missing=1
  fi
  if [ -z "${AGENTSCOPE_LLM_API_KEY:-}" ]; then
    log "  MISSING: AGENTSCOPE_LLM_API_KEY"
    missing=1
  fi
  if [ -z "${AGENTSCOPE_LLM_MODEL:-}" ]; then
    log "  MISSING: AGENTSCOPE_LLM_MODEL"
    missing=1
  fi

  if [ "${missing}" -eq 1 ]; then
    die "
LLM 环境变量未完全配置。请设置：

  export AGENTSCOPE_LLM_PROVIDER=deepseek
  export AGENTSCOPE_LLM_BASE_URL=https://api.deepseek.com/v1
  export AGENTSCOPE_LLM_API_KEY=你的API密钥
  export AGENTSCOPE_LLM_MODEL=deepseek-chat

或使用 GLM：

  export AGENTSCOPE_LLM_PROVIDER=glm
  export AGENTSCOPE_LLM_BASE_URL=https://open.bigmodel.cn/api/paas/v4
  export AGENTSCOPE_LLM_API_KEY=你的API密钥
  export AGENTSCOPE_LLM_MODEL=glm-4.7-flash

注意：本演示不提供 mock fallback，缺失 API key 将直接失败。
"
  fi
  log "LLM 配置: provider=${AGENTSCOPE_LLM_PROVIDER} model=${AGENTSCOPE_LLM_MODEL}"
  log "LLM 配置通过 ✓"
}

main() {
  log "AgentScope 真实 LLM Agent 演示"
  echo ""

  # 1. Check server
  wait_for_server "${SERVER_URL}"

  # 2. Validate LLM env vars (fail early, no mock fallback)
  check_llm_config

  # 3. Install Python SDK in editable mode
  log "安装 AgentScope Python SDK..."
  pip install -q -e "${ROOT_DIR}/python"
  log "SDK 安装完成"

  # 4. Run the real agent demo
  log "运行真实 LLM Agent 演示..."
  echo ""
  python3 "${ROOT_DIR}/python/examples/real_agent_demo.py" --server "${SERVER_URL}"
}

main "$@"
