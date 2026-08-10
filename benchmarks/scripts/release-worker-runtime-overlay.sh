#!/usr/bin/env bash
# Refresh the normal local benchmarks Worker from reviewed local source, with
# immutable base image and the pinned Docker Compose plugin required by Harbor.
# No package or Agent is downloaded here or while benchmark tasks execute.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
image="${AGENTLAB_WORKER_IMAGE:-agentlab/benchmarks-worker:local}"

"$root/scripts/bootstrap-docker-compose-build-asset.sh"
base_id="$(docker image inspect --format '{{.Id}}' "$image")"
case "$base_id" in
  sha256:[0-9a-f][0-9a-f]*) ;;
  *) echo "could not resolve immutable local base for $image" >&2; exit 2 ;;
esac

# Classic builder only consumes the local, digest-addressed base and avoids a
# BuildKit metadata lookup through the developer proxy.
DOCKER_BUILDKIT=0 docker build --quiet \
  --build-arg "BASE_IMAGE=$image@$base_id" \
  --file "$root/docker/Dockerfile.worker-runtime-overlay" \
  --tag "$image" \
  "$root"

new_id="$(docker image inspect --format '{{.Id}}' "$image")"
case "$new_id" in
  sha256:[0-9a-f][0-9a-f]*) ;;
  *) echo "could not resolve updated Worker digest" >&2; exit 2 ;;
esac
printf '%s\n' "$new_id"
