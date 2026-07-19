#!/usr/bin/env bash
# AgentScope 多维评测演示脚本
#
# 展示如何配置和运行多维评测（deterministic evaluators + LLM-as-Judge）
#
# 使用方法:
#   1. 确保后端运行: make dev
#   2. 运行: ./scripts/demo-multi-eval.sh
#
# 可选的 LLM-as-Judge 配置:
#   export DEEPSEEK_API_KEY=sk-xxxx  (使用 DeepSeek 作为 LLM Judge)
#   export GLM_API_KEY=xxxx           (使用 GLM 作为 LLM Judge)
#   不配置则跳过 LLM Judge，只运行 deterministic evaluators。

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_URL="${AGENTSCOPE_SERVER_URL:-http://127.0.0.1:8080}"

log() { printf '[demo-multi-eval] %s\n' "$*" >&2; }
die() { log "错误: $*"; exit 1; }

wait_for_server() {
  local url="${1}"
  local max_attempts=30
  log "等待后端启动: ${url}/healthz ..."
  for i in $(seq 1 "${max_attempts}"); do
    if curl -sf "${url}/healthz" >/dev/null 2>&1; then
      log "后端就绪"
      return 0
    fi
    sleep 1
  done
  die "后端 ${max_attempts}s 内未就绪，请先运行 make dev"
}

main() {
  log "AgentScope 多维评测演示"
  echo ""

  wait_for_server "${SERVER_URL}"

  # Install Python SDK
  log "安装 AgentScope Python SDK..."
  pip install -q -e "${ROOT_DIR}/python" 2>&1 || true
  log "SDK 安装完成"

  echo ""

  # Run the multi-dimensional eval demo
  log "运行多维评测演示..."
  log "模式: deterministic evaluators + LLM-as-Judge（如果配置了 API key）"
  if [ -n "${DEEPSEEK_API_KEY:-}" ]; then
    export AGENTSCOPE_LLM_PROVIDER=deepseek
    export AGENTSCOPE_LLM_BASE_URL=https://api.deepseek.com/v1
    export AGENTSCOPE_LLM_API_KEY="${DEEPSEEK_API_KEY}"
    export AGENTSCOPE_LLM_MODEL=deepseek-chat
    log "LLM Judge: DeepSeek (${AGENTSCOPE_LLM_MODEL})"
  elif [ -n "${GLM_API_KEY:-}" ]; then
    export AGENTSCOPE_LLM_PROVIDER=glm
    export AGENTSCOPE_LLM_BASE_URL=https://open.bigmodel.cn/api/paas/v4
    export AGENTSCOPE_LLM_API_KEY="${GLM_API_KEY}"
    export AGENTSCOPE_LLM_MODEL=glm-4.7-flash
    log "LLM Judge: GLM (${AGENTSCOPE_LLM_MODEL})"
  else
    log "未检测到 DEEPSEEK_API_KEY 或 GLM_API_KEY，将跳过 LLM Judge。"
    log "仅运行 deterministic evaluators。"
  fi

  python3 "${ROOT_DIR}/python/examples/demo_multi_eval.py" \
    --server "${SERVER_URL}"
}

main "$@"
