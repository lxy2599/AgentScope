#!/usr/bin/env bash
# 真实 Hermes Agent 到 AgentScope 的完整可观测闭环。
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_URL="${AGENTSCOPE_SERVER_URL:-http://127.0.0.1:8080}"
export HERMES_LLM_BASE_URL="${HERMES_LLM_BASE_URL:-${CCSWITCH_OPENAI_BASE_URL:-}}"
export HERMES_LLM_API_KEY="${HERMES_LLM_API_KEY:-${CCSWITCH_OPENAI_API_KEY:-}}"
export HERMES_LLM_MODEL="${HERMES_LLM_MODEL:-${CCSWITCH_OPENAI_MODEL:-}}"
export HERMES_LLM_PROVIDER="${HERMES_LLM_PROVIDER:-${CCSWITCH_OPENAI_PROVIDER:-openai-compatible}}"
export HERMES_RESULT_FILE="${HERMES_RESULT_FILE:-${ROOT_DIR}/.cache/hermes-agentops-result.json}"
export AGENTSCOPE_COMPOSE_DIR="${AGENTSCOPE_COMPOSE_DIR:-${ROOT_DIR}}"
export HERMES_HOME="${HERMES_HOME:-${ROOT_DIR}/.cache/hermes-home}"

log() { printf '[demo-hermes-agentops] %s\n' "$*" >&2; }
die() { log "错误: $*"; exit 1; }

for key in HERMES_LLM_BASE_URL HERMES_LLM_API_KEY HERMES_LLM_MODEL; do
  [ -n "${!key:-}" ] || die "缺少 ${key}。请从 ccswitch 导出 OpenAI 兼容地址、密钥和模型。"
done

for _ in $(seq 1 30); do
  curl -sf "${SERVER_URL}/healthz" >/dev/null && break
  sleep 1
done
curl -sf "${SERVER_URL}/healthz" >/dev/null || die "后端未就绪: ${SERVER_URL}"

"${ROOT_DIR}/scripts/setup-hermes-agent.sh" >/dev/null
HERMES_DIR="${HERMES_DIR:-${ROOT_DIR}/.cache/hermes-agent}"
HERMES_VENV="${HERMES_VENV:-${ROOT_DIR}/.cache/hermes-venv}"
mkdir -p "$(dirname "${HERMES_RESULT_FILE}")" "${HERMES_HOME}"

log "运行真实 Hermes Agent，可观测结果写入单一项目"
PYTHONPATH="${HERMES_DIR}:${ROOT_DIR}/python${PYTHONPATH:+:${PYTHONPATH}}" \
  "${HERMES_VENV}/bin/python" "${ROOT_DIR}/python/examples/hermes_agentops_demo.py" \
  --server-url "${SERVER_URL}" --result-file "${HERMES_RESULT_FILE}"

log "结果文件: ${HERMES_RESULT_FILE}"
