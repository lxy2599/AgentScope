#!/usr/bin/env bash
# 检出固定 OpenClaw 版本并构建其官方插件 SDK；不读取或打印模型密钥。
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OPENCLAW_DIR="${OPENCLAW_DIR:-${ROOT_DIR}/.cache/openclaw-agent}"
OPENCLAW_COMMIT="ee07bb8e66f86579ab113dfe4ba2b856b076c494"
NODE_BIN="${NODE_BIN:-node}"

log() { printf '[setup-openclaw-agent] %s\n' "$*" >&2; }
die() { log "错误: $*"; exit 1; }

NODE_VERSION="$(${NODE_BIN} -p 'process.versions.node')"
node -e 'const [major, minor, patch] = process.versions.node.split(".").map(Number); process.exit((major > 22 || (major === 22 && (minor > 22 || (minor === 22 && patch >= 3)))) ? 0 : 1)' \
  || die "OpenClaw ${OPENCLAW_COMMIT} 需要 Node >= 22.22.3；当前为 ${NODE_VERSION}"

if [ ! -d "${OPENCLAW_DIR}/.git" ]; then
  log "检出 OpenClaw 上游源码"
  git clone https://github.com/openclaw/openclaw.git "${OPENCLAW_DIR}"
fi
git -C "${OPENCLAW_DIR}" fetch --depth 1 origin "${OPENCLAW_COMMIT}"
git -C "${OPENCLAW_DIR}" checkout --detach "${OPENCLAW_COMMIT}"
[ "$(git -C "${OPENCLAW_DIR}" rev-parse HEAD)" = "${OPENCLAW_COMMIT}" ] || die "OpenClaw 提交不匹配"

(
  cd "${OPENCLAW_DIR}"
  corepack enable
  pnpm install --frozen-lockfile
  pnpm build
)

log "OpenClaw 已就绪: ${OPENCLAW_DIR} @ ${OPENCLAW_COMMIT}"
log "将 ${ROOT_DIR}/integrations/openclaw/agentscope-observability 配置为 OpenClaw 插件目录后即可启用。"
