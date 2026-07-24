#!/usr/bin/env bash
# 检出固定版本的 OpenHands Software Agent SDK，并安装到隔离虚拟环境。
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OPENHANDS_DIR="${OPENHANDS_DIR:-${ROOT_DIR}/.external/openhands}"
OPENHANDS_VENV="${OPENHANDS_VENV:-${ROOT_DIR}/.external/openhands-venv}"
OPENHANDS_COMMIT="a375c66aaf29c8d3b6553c9164e65ed917149e81"
PYTHON_BIN="${PYTHON_BIN:-python3.12}"
DOWNLOAD_PROXY="${AGENTSCOPE_DOWNLOAD_PROXY:-}"

log() { printf '[setup-openhands-agent] %s\n' "$*" >&2; }
die() { log "错误: $*"; exit 1; }

command -v "${PYTHON_BIN}" >/dev/null 2>&1 ||
  die "需要 Python 3.12+，未找到 ${PYTHON_BIN}"

if [ ! -d "${OPENHANDS_DIR}/.git" ]; then
  mkdir -p "$(dirname "${OPENHANDS_DIR}")"
  log "检出 OpenHands 官方源码"
  if ! git clone --filter=blob:none --no-checkout \
    https://github.com/OpenHands/software-agent-sdk.git "${OPENHANDS_DIR}"; then
    rm -rf "${OPENHANDS_DIR}"
    log "代理访问 GitHub 失败，改用直连"
    env -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY \
      -u http_proxy -u https_proxy -u all_proxy \
      git clone --filter=blob:none --no-checkout \
      https://github.com/OpenHands/software-agent-sdk.git "${OPENHANDS_DIR}"
  fi
fi

if ! git -C "${OPENHANDS_DIR}" fetch --depth 1 origin "${OPENHANDS_COMMIT}"; then
  log "代理访问 GitHub 失败，改用直连"
  env -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY \
    -u http_proxy -u https_proxy -u all_proxy \
    git -C "${OPENHANDS_DIR}" fetch --depth 1 origin "${OPENHANDS_COMMIT}"
fi
git -C "${OPENHANDS_DIR}" checkout --detach "${OPENHANDS_COMMIT}"
ACTUAL_COMMIT="$(git -C "${OPENHANDS_DIR}" rev-parse HEAD)"
[ "${ACTUAL_COMMIT}" = "${OPENHANDS_COMMIT}" ] ||
  die "OpenHands 提交不匹配: ${ACTUAL_COMMIT}"
[ -z "$(git -C "${OPENHANDS_DIR}" status --porcelain)" ] ||
  die "OpenHands 上游工作区存在修改"

if [ -z "${DOWNLOAD_PROXY}" ] &&
  ss -ltn 2>/dev/null | grep -qE '127\.0\.0\.1:7897|0\.0\.0\.0:7897|\*:7897'; then
  DOWNLOAD_PROXY="http://127.0.0.1:7897"
fi
if [ -n "${DOWNLOAD_PROXY}" ]; then
  export HTTP_PROXY="${HTTP_PROXY:-${DOWNLOAD_PROXY}}"
  export HTTPS_PROXY="${HTTPS_PROXY:-${DOWNLOAD_PROXY}}"
  export http_proxy="${http_proxy:-${DOWNLOAD_PROXY}}"
  export https_proxy="${https_proxy:-${DOWNLOAD_PROXY}}"
  log "依赖下载使用代理 ${DOWNLOAD_PROXY}"
fi

if [ ! -x "${OPENHANDS_VENV}/bin/python" ]; then
  rm -rf "${OPENHANDS_VENV}"
  if command -v uv >/dev/null 2>&1; then
    uv venv --quiet --python "${PYTHON_BIN}" "${OPENHANDS_VENV}"
  else
    "${PYTHON_BIN}" -m venv "${OPENHANDS_VENV}"
  fi
fi

if command -v uv >/dev/null 2>&1; then
  install_with_uv() {
    UV_HTTP_TIMEOUT=300 uv pip install --quiet \
      --python "${OPENHANDS_VENV}/bin/python" \
      --constraint <(printf 'litellm==1.84.1\n') \
      -e "${OPENHANDS_DIR}/openhands-sdk" \
      -e "${OPENHANDS_DIR}/openhands-tools" \
      -e "${ROOT_DIR}/python"
  }
  if ! install_with_uv; then
    log "代理依赖下载失败，使用已有缓存并改为直连重试"
    if ! (
      unset HTTP_PROXY HTTPS_PROXY ALL_PROXY
      unset http_proxy https_proxy all_proxy
      install_with_uv
    ); then
      log "直连依赖下载失败，再次使用代理重试"
      install_with_uv
    fi
  fi
else
  "${OPENHANDS_VENV}/bin/python" -m pip install --quiet --upgrade pip
  "${OPENHANDS_VENV}/bin/python" -m pip install --quiet \
    -e "${OPENHANDS_DIR}/openhands-sdk" \
    -e "${OPENHANDS_DIR}/openhands-tools" \
    -e "${ROOT_DIR}/python"
fi

log "OpenHands 已就绪: ${OPENHANDS_DIR} @ ${ACTUAL_COMMIT}"
printf 'OPENHANDS_DIR=%s\nOPENHANDS_VENV=%s\n' \
  "${OPENHANDS_DIR}" "${OPENHANDS_VENV}"
