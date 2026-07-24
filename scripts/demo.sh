#!/usr/bin/env bash
# AgentScope 端到端评测演示
#
# 使用方法:
#   1. 启动服务: make dev
#   2. 运行演示: ./scripts/demo.sh
#
# 此脚本调用 Python 演示脚本，完成注册、创建项目、创建评测并执行。

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_URL="${SERVER_URL:-http://127.0.0.1:8080}"

log() { printf '[demo] %s\n' "$*" >&2; }

wait_for_server() {
  local url="${1}"
  local max_attempts=30
  log "waiting for server at ${url} ..."
  for _ in $(seq 1 "${max_attempts}"); do
    if curl -sf "${url}/healthz" >/dev/null 2>&1; then
      log "server is ready"
      return 0
    fi
    sleep 1
  done
  log "server did not become ready after ${max_attempts}s"
  return 1
}

main() {
  log "AgentScope 端到端演示"

  # Check server
  wait_for_server "${SERVER_URL}"

  # Install AgentScope Python SDK (editable, ensures all deps are available)
  log "installing AgentScope Python SDK..."
  python3 -m pip install -q -e "${ROOT_DIR}/python"

  log "running demo script..."
  python3 "${ROOT_DIR}/python/examples/demo_eval.py" --server "${SERVER_URL}"
}

main "$@"
