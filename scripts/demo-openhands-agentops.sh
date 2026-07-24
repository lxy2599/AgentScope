#!/usr/bin/env bash
# 真实 OpenHands Software Agent SDK 到 AgentScope 的完整可观测闭环。
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVER_URL="${AGENTSCOPE_SERVER_URL:-http://127.0.0.1:8080}"
OPENHANDS_RESULT_FILE="${OPENHANDS_RESULT_FILE:-${ROOT_DIR}/.external/openhands-agentops-result.json}"
OPENHANDS_WORKSPACE="${OPENHANDS_WORKSPACE:-${ROOT_DIR}/.external/openhands-demo-workspace}"
OPENHANDS_VENV="${OPENHANDS_VENV:-${ROOT_DIR}/.external/openhands-venv}"
export AGENTSCOPE_COMPOSE_DIR="${AGENTSCOPE_COMPOSE_DIR:-${ROOT_DIR}}"
export OPENHANDS_RESULT_FILE OPENHANDS_WORKSPACE
export NO_PROXY="${NO_PROXY:+${NO_PROXY},}127.0.0.1,localhost"
export no_proxy="${no_proxy:+${no_proxy},}127.0.0.1,localhost"

log() { printf '[demo-openhands-agentops] %s\n' "$*" >&2; }
die() { log "错误: $*"; exit 1; }

if [ -z "${OPENHANDS_LLM_API_KEY:-}" ]; then
  if [ -n "${GLM_API_KEY:-}" ]; then
    export OPENHANDS_LLM_PROVIDER="glm"
    export OPENHANDS_LLM_API_KEY="${GLM_API_KEY}"
    export OPENHANDS_LLM_BASE_URL="${GLM_BASE_URL:-https://open.bigmodel.cn/api/paas/v4}"
    export OPENHANDS_LLM_MODEL="${GLM_MODEL:-glm-4.7-flash}"
  elif [ -n "${DEEPSEEK_API_KEY:-}" ]; then
    export OPENHANDS_LLM_PROVIDER="deepseek"
    export OPENHANDS_LLM_API_KEY="${DEEPSEEK_API_KEY}"
    export OPENHANDS_LLM_BASE_URL="${DEEPSEEK_BASE_URL:-https://api.deepseek.com/v1}"
    export OPENHANDS_LLM_MODEL="${DEEPSEEK_MODEL:-deepseek-chat}"
  elif [ -n "${CCSWITCH_OPENAI_API_KEY:-}" ]; then
    export OPENHANDS_LLM_PROVIDER="${CCSWITCH_OPENAI_PROVIDER:-openai-compatible}"
    export OPENHANDS_LLM_API_KEY="${CCSWITCH_OPENAI_API_KEY}"
    export OPENHANDS_LLM_BASE_URL="${CCSWITCH_OPENAI_BASE_URL:-}"
    export OPENHANDS_LLM_MODEL="${CCSWITCH_OPENAI_MODEL:-}"
  fi
fi

for key in OPENHANDS_LLM_API_KEY OPENHANDS_LLM_BASE_URL OPENHANDS_LLM_MODEL; do
  [ -n "${!key:-}" ] ||
    die "缺少 ${key}。请配置 GLM、DeepSeek 或 ccswitch 的 OpenAI 兼容参数。"
done
export OPENHANDS_LLM_PROVIDER="${OPENHANDS_LLM_PROVIDER:-openai-compatible}"
if [[ "${OPENHANDS_LLM_MODEL}" != */* ]]; then
  export OPENHANDS_LLM_MODEL="openai/${OPENHANDS_LLM_MODEL}"
fi

for _ in $(seq 1 30); do
  curl -sf "${SERVER_URL}/healthz" >/dev/null && break
  sleep 1
done
curl -sf "${SERVER_URL}/healthz" >/dev/null ||
  die "后端未就绪: ${SERVER_URL}，请先运行 make dev"

"${ROOT_DIR}/scripts/setup-openhands-agent.sh" >/dev/null
mkdir -p "${OPENHANDS_WORKSPACE}" "$(dirname "${OPENHANDS_RESULT_FILE}")"
rm -rf "${OPENHANDS_WORKSPACE:?}"/*

log "运行 OpenHands ${OPENHANDS_LLM_PROVIDER}/${OPENHANDS_LLM_MODEL}"
PYTHONPATH="${ROOT_DIR}/python${PYTHONPATH:+:${PYTHONPATH}}" \
  "${OPENHANDS_VENV}/bin/python" \
  "${ROOT_DIR}/python/examples/openhands_agentops_demo.py" \
  --server-url "${SERVER_URL}" \
  --workspace "${OPENHANDS_WORKSPACE}" \
  --result-file "${OPENHANDS_RESULT_FILE}"

"${ROOT_DIR}/scripts/verify-openhands-data.sh" \
  --result-file "${OPENHANDS_RESULT_FILE}"

log "演示与数据库验收通过，结果文件: ${OPENHANDS_RESULT_FILE}"
