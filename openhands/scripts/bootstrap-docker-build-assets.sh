#!/usr/bin/env bash
# Prepare the *build-time only* uv archive for Dockerfile.task-runner.
#
# The owned AgentLab fork intentionally reuses a pinned host uv binary instead
# of resolving a GHCR image. The ignored archive is checked here and again
# inside Docker; no task-time dependency installation is involved.
set -euo pipefail

readonly UV_VERSION="0.11.4"
readonly UV_TARGET="x86_64-unknown-linux-gnu"
readonly UV_ARCHIVE="uv-${UV_TARGET}.tar.gz"
readonly UV_SHA256="9ec893bfa7f8c3bdae829196f4193e8554ee88cd05f77420b0e5fb841d8c95ad"
readonly ASSET_DIR="${ASSET_DIR:-.docker-build-assets}"
readonly ASSET_PATH="${ASSET_DIR}/${UV_ARCHIVE}"
readonly AGENTSCOPE_SDK_SOURCE="${AGENTSCOPE_SDK_SOURCE:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../agentscope/python" && pwd)}"
readonly AGENTSCOPE_SDK_ASSET_DIR="${ASSET_DIR}/agentscope_sdk"
readonly AGENTSCOPE_SDK_MANIFEST="${ASSET_DIR}/agentscope_sdk.sha256"

if [[ "$(uname -s)" != "Linux" || "$(uname -m)" != "x86_64" ]]; then
  echo "This pinned asset is only for Linux x86_64; add a separately pinned target." >&2
  exit 2
fi

mkdir -p "${ASSET_DIR}"
if [[ -f "${ASSET_PATH}" ]] && printf '%s  %s\n' "${UV_SHA256}" "${ASSET_PATH}" | sha256sum --check --status; then
  echo "Verified existing ${ASSET_PATH} (uv ${UV_VERSION})."
else
  uv_binary="$(command -v uv || true)"
  if [[ -z "${uv_binary}" ]] || ! "${uv_binary}" --version | grep -Fqx "uv ${UV_VERSION} (x86_64-unknown-linux-gnu)"; then
    echo "A host uv ${UV_VERSION} (x86_64-unknown-linux-gnu) is required to prepare the pinned build asset." >&2
    exit 2
  fi

  stage_dir="$(mktemp -d "${ASSET_DIR}/.uv-stage.XXXXXX")"
  trap 'rm -rf "${stage_dir}"' EXIT
  mkdir -p "${stage_dir}/uv-${UV_TARGET}"
  cp "$(readlink -f "${uv_binary}")" "${stage_dir}/uv-${UV_TARGET}/uv"
  ln -s uv "${stage_dir}/uv-${UV_TARGET}/uvx"
  tar -C "${stage_dir}" -czf "${ASSET_PATH}" "uv-${UV_TARGET}"
  printf '%s  %s\n' "${UV_SHA256}" "${ASSET_PATH}" | sha256sum --check --strict
  trap - EXIT
  echo "Prepared and verified ${ASSET_PATH} (uv ${UV_VERSION})."
fi

if [[ ! -f "${AGENTSCOPE_SDK_SOURCE}/pyproject.toml" ]] || [[ ! -f "${AGENTSCOPE_SDK_SOURCE}/agentscope_sdk/__init__.py" ]]; then
  echo "Missing local AgentScope SDK source at ${AGENTSCOPE_SDK_SOURCE}. Set AGENTSCOPE_SDK_SOURCE to the SDK package root." >&2
  exit 2
fi

# Bake the controlled local SDK into owned-agent images.  The images run from
# this copied code rather than importing the AgentScope checkout at runtime.
# The manifest is verified again by Docker before copy-in and never contains
# credentials.
rm -rf "${AGENTSCOPE_SDK_ASSET_DIR}"
mkdir -p "${AGENTSCOPE_SDK_ASSET_DIR}"
cp -a "${AGENTSCOPE_SDK_SOURCE}/agentscope_sdk/." "${AGENTSCOPE_SDK_ASSET_DIR}/"
# Source checkouts may contain local bytecode. Do not bake host-specific caches
# into an image or its integrity manifest.
find "${AGENTSCOPE_SDK_ASSET_DIR}" -type d -name __pycache__ -prune -exec rm -rf {} +
find "${AGENTSCOPE_SDK_ASSET_DIR}" -type f \( -name '*.pyc' -o -name '*.pyo' \) -delete
manifest_path="$(cd "$(dirname "${AGENTSCOPE_SDK_MANIFEST}")" && pwd)/$(basename "${AGENTSCOPE_SDK_MANIFEST}")"
(
  cd "${AGENTSCOPE_SDK_ASSET_DIR}"
  find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum > "${manifest_path}"
)
printf '%s\n' "Staged AgentScope SDK source for image build."
