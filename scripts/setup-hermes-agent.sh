#!/usr/bin/env bash
# 检出固定版本的 Hermes，并把 AgentScope SDK 安装到同一虚拟环境。
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
HERMES_DIR="${HERMES_DIR:-${ROOT_DIR}/.cache/hermes-agent}"
HERMES_VENV="${HERMES_VENV:-${ROOT_DIR}/.cache/hermes-venv}"
HERMES_COMMIT="d9165d7a678d4105f42921a7fc1886df3804531b"
PYTHON_BIN="${PYTHON_BIN:-python3}"

log() { printf '[setup-hermes-agent] %s\n' "$*" >&2; }
die() { log "错误: $*"; exit 1; }

if [ ! -d "${HERMES_DIR}/.git" ]; then
  log "检出 Hermes 上游源码"
  git clone https://github.com/NousResearch/hermes-agent.git "${HERMES_DIR}"
fi

git -C "${HERMES_DIR}" fetch --depth 1 origin "${HERMES_COMMIT}"
git -C "${HERMES_DIR}" checkout --detach "${HERMES_COMMIT}"
ACTUAL_COMMIT="$(git -C "${HERMES_DIR}" rev-parse HEAD)"
[ "${ACTUAL_COMMIT}" = "${HERMES_COMMIT}" ] || die "Hermes 提交不匹配: ${ACTUAL_COMMIT}"

if [ ! -x "${HERMES_VENV}/bin/python" ]; then
  "${PYTHON_BIN}" -m venv "${HERMES_VENV}"
fi

"${HERMES_VENV}/bin/python" -m pip install --quiet --upgrade pip
"${HERMES_VENV}/bin/python" -m pip install --quiet -e "${HERMES_DIR}"
"${HERMES_VENV}/bin/python" -m pip install --quiet -e "${ROOT_DIR}/python"

log "Hermes 已就绪: ${HERMES_DIR} @ ${ACTUAL_COMMIT}"
printf 'HERMES_DIR=%s\nHERMES_VENV=%s\n' "${HERMES_DIR}" "${HERMES_VENV}"
