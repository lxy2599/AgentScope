#!/usr/bin/env bash
# Start one direct-source OpenHands probe and leave it alive for heartbeats.
# This is a development helper; production-like local use is `agentlab.sh`.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
STATE_DIR="${AGENTLAB_OBSERVED_OPENHANDS_DIR:-${ROOT_DIR}/.runtime/observed-openhands}"
LOG_DIR="${STATE_DIR}/logs"
PID_FILE="${STATE_DIR}/openhands.pid"
LOG_FILE="${LOG_DIR}/openhands.log"
OPENHANDS_VENV="${OPENHANDS_VENV:-${ROOT_DIR}/.runtime/openhands-native-venv}"

log() { printf '[observed-openhands] %s\n' "$*" >&2; }
die() { log "错误: $*"; exit 1; }

load_env() {
  if [[ -f "${ROOT_DIR}/.env" ]]; then
    set -a
    # shellcheck disable=SC1091
    source "${ROOT_DIR}/.env"
    set +a
  fi
  export AGENTSCOPE_SERVER_URL="${AGENTSCOPE_SERVER_URL:-http://127.0.0.1:${AGENTLAB_HTTP_PORT:-18080}}"
  export AGENTSCOPE_ENVIRONMENT="${AGENTSCOPE_ENVIRONMENT:-local}"
  export OPENHANDS_AGENTSCOPE_ENABLED=true
  export OPENHANDS_AGENTSCOPE_CAPTURE_LEVEL="${OPENHANDS_AGENTSCOPE_CAPTURE_LEVEL:-L0}"
  export OPENHANDS_LLM_API_KEY="${OPENHANDS_LLM_API_KEY:-${LLM_API_KEY:-${ARK_API_KEY:-}}}"
  export OPENHANDS_LLM_BASE_URL="${OPENHANDS_LLM_BASE_URL:-${LLM_BASE_URL:-${ARK_BASE_URL:-}}}"
  export OPENHANDS_LLM_MODEL="${OPENHANDS_LLM_MODEL:-${BENCHMARK_MODEL:-openai/deepseek-v4-flash}}"
  export OPENHANDS_OBSERVED_WORKSPACE="${OPENHANDS_OBSERVED_WORKSPACE:-${STATE_DIR}/workspace}"
}

require_env() {
  [[ -n "${!1:-}" ]] || die "缺少 ${1}；请仅在 ${ROOT_DIR}/.env 中配置。"
}

pid_running() {
  [[ -f "${PID_FILE}" ]] && kill -0 "$(<"${PID_FILE}")" 2>/dev/null
}

stop() {
  if ! [[ -f "${PID_FILE}" ]]; then
    log "未运行"
    return
  fi
  local pid
  pid="$(<"${PID_FILE}")"
  if kill -0 "${pid}" 2>/dev/null; then
    kill "${pid}"
    for _ in $(seq 1 20); do
      kill -0 "${pid}" 2>/dev/null || break
      sleep 0.25
    done
  fi
  : > "${PID_FILE}"
  log "已停止"
}

status() {
  if pid_running; then
    printf 'openhands direct-source probe: running (pid %s)\n' "$(<"${PID_FILE}")"
  else
    printf 'openhands direct-source probe: stopped\n'
  fi
}

load_env
case "${1:-start}" in
  start)
    pid_running && { status; exit 0; }
    require_env AGENTSCOPE_PROJECT_TOKEN
    require_env OPENHANDS_LLM_API_KEY
    require_env OPENHANDS_LLM_BASE_URL
    curl --fail --silent --show-error --max-time 5 "${AGENTSCOPE_SERVER_URL}/healthz" >/dev/null \
      || die "AgentScope 服务不可用: ${AGENTSCOPE_SERVER_URL}"
    mkdir -p "${LOG_DIR}"
    "${ROOT_DIR}/scripts/setup-openhands-native.sh" >/dev/null
    nohup env \
      PYTHONPATH="${ROOT_DIR}/python:${ROOT_DIR}/openhands/openhands-sdk:${ROOT_DIR}/openhands/openhands-tools${PYTHONPATH:+:${PYTHONPATH}}" \
      "${OPENHANDS_VENV}/bin/python" "${ROOT_DIR}/scripts/observed_openhands_agent.py" \
      >"${LOG_FILE}" 2>&1 &
    echo "$!" > "${PID_FILE}"
    log "已启动（pid $!；日志: ${LOG_FILE}）"
    ;;
  stop) stop ;;
  status) status ;;
  *) die "用法: $0 [start|stop|status]" ;;
esac
