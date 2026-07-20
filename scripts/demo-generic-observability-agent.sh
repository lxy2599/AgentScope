#!/usr/bin/env bash
# AgentScope 通用 Agent 深度可观测演示
#
# 使用方法:
#   1. 确保后端运行: make dev
#   2. 运行: ./scripts/demo-generic-observability-agent.sh
#
# 本演示不要求 LLM API key，使用 mock 模式运行。
# 如设置 AGENTSCOPE_LLM_API_KEY，将使用真实 LLM。

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_URL="${SERVER_URL:-http://127.0.0.1:8080}"

log() { printf '[demo-generic-obsv] %s\n' "$*" >&2; }
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
  log "AgentScope 通用 Agent 深度可观测演示"
  echo ""

  # 1. Check server
  wait_for_server "${SERVER_URL}"

  # 2. Install Python SDK in editable mode
  log "安装 AgentScope Python SDK..."
  pip install -q -e "${ROOT_DIR}/python"
  log "SDK 安装完成"

  # 3. Run the generic observability demo
  log "运行通用 Agent 可观测演示..."
  echo ""
  python3 "${ROOT_DIR}/python/examples/generic_observability_agent.py" --server "${SERVER_URL}"
}

main "$@"
