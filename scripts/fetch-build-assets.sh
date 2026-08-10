#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
ASSET_DIR="$ROOT/benchmarks/docker/assets"
ASSET="$ASSET_DIR/docker-27.5.1.tgz"
URL="https://download.docker.com/linux/static/stable/x86_64/docker-27.5.1.tgz"
SHA256="4f798b3ee1e0140eab5bf30b0edc4e84f4cdb53255a429dc3bbae9524845d640"

mkdir -p "$ASSET_DIR"
if [[ -f "$ASSET" ]] && [[ "$(sha256sum "$ASSET" | awk '{print $1}')" == "$SHA256" ]]; then
  echo "Verified existing ${ASSET}."
else
  temp_asset="${ASSET}.tmp.$$"
  trap 'rm -f "${temp_asset}"' EXIT
  curl_args=(--fail --silent --show-error --location --connect-timeout 10 --max-time 300 --output "${temp_asset}" "${URL}")
  if [[ -n "${HTTPS_PROXY:-${https_proxy:-}}" ]]; then
    if ! curl --proxy "${HTTPS_PROXY:-${https_proxy:-}}" "${curl_args[@]}"; then
      # A local proxy is optional for this developer workflow. Fall back to a
      # direct, still checksum-verified request if it is temporarily unhealthy.
      echo "Proxy download failed; retrying the pinned Docker CLI asset directly." >&2
      curl --noproxy "*" "${curl_args[@]}"
    fi
  else
    curl "${curl_args[@]}"
  fi
  if [[ "$(sha256sum "$temp_asset" | awk '{print $1}')" != "$SHA256" ]]; then
    echo "Docker CLI build asset checksum mismatch" >&2
    exit 1
  fi
  mv "$temp_asset" "$ASSET"
  trap - EXIT
fi

# Every image build needs all three assets. Do not return early after only the
# Docker CLI is available, or Docker will fail later on a missing Compose/SDK.
"$ROOT/benchmarks/scripts/bootstrap-docker-compose-build-asset.sh"
"$ROOT/openhands/scripts/bootstrap-docker-build-assets.sh"
