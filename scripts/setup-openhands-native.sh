#!/usr/bin/env bash
# Install the controlled OpenHands native fork for a project-level AgentScope demo.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FORK_DIR="${OPENHANDS_NATIVE_FORK_DIR:-${ROOT_DIR}/integrations/openhands-agent}"
OPENHANDS_VENV="${OPENHANDS_VENV:-${ROOT_DIR}/.external/openhands-native-venv}"
PYTHON_BIN="${PYTHON_BIN:-python3.12}"

log() { printf '[setup-openhands-native] %s\n' "$*" >&2; }
die() { log "错误: $*"; exit 1; }

command -v "${PYTHON_BIN}" >/dev/null 2>&1 || die "需要 Python 3.12+，未找到 ${PYTHON_BIN}"
test -f "${FORK_DIR}/AGENTSCOPE_FORK.md" || die "缺少受控 OpenHands fork: ${FORK_DIR}"
test -d "${FORK_DIR}/openhands-sdk/openhands/sdk/agentscope" || die "fork 不包含 AgentScope 原生 bridge"

"${PYTHON_BIN}" -m venv "${OPENHANDS_VENV}"
"${OPENHANDS_VENV}/bin/python" -m pip install --upgrade pip >/dev/null
"${OPENHANDS_VENV}/bin/python" -m pip install \
  -e "${ROOT_DIR}/python" \
  -e "${FORK_DIR}/openhands-sdk[agentscope]" \
  -e "${FORK_DIR}/openhands-tools" >/dev/null

log "已安装受控 OpenHands native fork: ${FORK_DIR}"
log "虚拟环境: ${OPENHANDS_VENV}"
