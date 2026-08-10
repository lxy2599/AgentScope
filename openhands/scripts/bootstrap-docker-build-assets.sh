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
readonly UV_SHA256="12f9a192bb32d70470aa22cbd2a193d1323a3f58f6ac5f9e3866aaca760c98c6"
readonly UV_URL="https://github.com/astral-sh/uv/releases/download/${UV_VERSION}/${UV_ARCHIVE}"
readonly PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly ASSET_DIR="${ASSET_DIR:-${PROJECT_ROOT}/.docker-build-assets}"
readonly ASSET_PATH="${ASSET_DIR}/${UV_ARCHIVE}"
readonly AGENTSCOPE_SDK_SOURCE="${AGENTSCOPE_SDK_SOURCE:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../python" && pwd)}"
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
  # A version string alone does not identify a reproducible uv binary: local
  # installations may be rebuilt or wrapped. Download the official, pinned
  # archive whenever no already-verified archive exists, then verify it before
  # Docker ever consumes it. This remains build-time only and never installs
  # dependencies during a benchmark run.
  temp_asset="${ASSET_PATH}.tmp.$$"
  trap 'rm -f "${temp_asset}"' EXIT
  curl_args=(--fail --silent --show-error --location --connect-timeout 10 --max-time 300 --output "${temp_asset}" "${UV_URL}")
  if [[ -n "${HTTPS_PROXY:-${https_proxy:-}}" ]]; then
    if ! curl --proxy "${HTTPS_PROXY:-${https_proxy:-}}" "${curl_args[@]}"; then
      # The supplied localhost proxy can disappear during long local sessions.
      # Retry direct once; integrity verification below still rejects every
      # archive except the pinned upstream release.
      echo "Proxy download failed; retrying the pinned uv asset directly." >&2
      curl --noproxy "*" "${curl_args[@]}"
    fi
  else
    curl "${curl_args[@]}"
  fi
  printf '%s  %s\n' "${UV_SHA256}" "${temp_asset}" | sha256sum --check --strict
  mv "${temp_asset}" "${ASSET_PATH}"
  trap - EXIT
  echo "Downloaded and verified ${ASSET_PATH} (uv ${UV_VERSION})."
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
