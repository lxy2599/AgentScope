#!/usr/bin/env bash
# Prepare the monorepo's OpenHands source for the optional direct-source probe.
# Normal use should prefer `scripts/agentlab.sh agents-up`, which runs the
# immutable Docker image. This helper is intentionally for source development.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OPENHANDS_DIR="${OPENHANDS_NATIVE_FORK_DIR:-${ROOT_DIR}/openhands}"
OPENHANDS_VENV="${OPENHANDS_VENV:-${ROOT_DIR}/.runtime/openhands-native-venv}"
PYTHON_BIN="${PYTHON_BIN:-python3}"

log() { printf '[setup-openhands-native] %s\n' "$*" >&2; }
die() { log "错误: $*"; exit 1; }

command -v "${PYTHON_BIN}" >/dev/null 2>&1 || die "需要 Python 3.12+，未找到 ${PYTHON_BIN}"
test -f "${OPENHANDS_DIR}/pyproject.toml" || die "缺少单仓库 OpenHands 源码: ${OPENHANDS_DIR}"
test -d "${OPENHANDS_DIR}/openhands-sdk/openhands/sdk/agentscope" || die "OpenHands 源码不包含 AgentScope 原生接入"
test -f "${ROOT_DIR}/python/pyproject.toml" || die "缺少根目录 AgentScope Python SDK"

if ! "${PYTHON_BIN}" - <<'PY' >/dev/null 2>&1
import sys
raise SystemExit(0 if sys.version_info >= (3, 12) else 1)
PY
then
  die "需要 Python 3.12+"
fi

if ! [ -x "${OPENHANDS_VENV}/bin/python" ] || ! OPENHANDS_SUPPRESS_BANNER=1 "${OPENHANDS_VENV}/bin/python" - <<'PY' >/dev/null 2>&1
import agentscope_sdk
import openhands.sdk
import openhands.tools
PY
then
  log "准备 monorepo OpenHands native 开发环境（首次安装需要联网）"
  "${PYTHON_BIN}" -m venv "${OPENHANDS_VENV}"
  "${OPENHANDS_VENV}/bin/python" -m pip install --upgrade pip >/dev/null
  "${OPENHANDS_VENV}/bin/python" -m pip install \
    -e "${ROOT_DIR}/python" \
    -e "${OPENHANDS_DIR}/openhands-sdk[agentscope]" \
    -e "${OPENHANDS_DIR}/openhands-tools" >/dev/null
else
  log "复用已就绪的 monorepo OpenHands native 开发环境"
fi

log "OpenHands 源码: ${OPENHANDS_DIR}"
log "虚拟环境: ${OPENHANDS_VENV}"
