#!/usr/bin/env bash
# Materialize the pinned Docker Compose plugin used by Harbor's Docker
# environment. This runs before an image build, never while a task runs.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
asset="$root/docker/assets/docker-compose-5.3.1-linux-x86_64"
expected='f9ebc6ebdb19d769b793c245a736caaeb198c62587f13b25c660c13b4987f959'
source_plugin="${AGENTLAB_DOCKER_COMPOSE_PLUGIN:-/usr/libexec/docker/cli-plugins/docker-compose}"

if [[ -f "$asset" ]] && [[ "$(sha256sum "$asset" | awk '{print $1}')" == "$expected" ]]; then
  exit 0
fi
if [[ ! -x "$source_plugin" ]]; then
  echo 'pinned Docker Compose build asset is unavailable; set AGENTLAB_DOCKER_COMPOSE_PLUGIN to a verified plugin path' >&2
  exit 2
fi
if [[ "$(sha256sum "$source_plugin" | awk '{print $1}')" != "$expected" ]]; then
  echo 'Docker Compose host plugin does not match the pinned 5.3.1 build asset' >&2
  exit 2
fi
mkdir -p "$(dirname "$asset")"
install -m 0755 "$source_plugin" "$asset"
