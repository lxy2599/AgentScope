#!/usr/bin/env bash
# Build and run the offline real-Harbor sentinel E2E. No provider/project
# secret is read or passed. The Worker image already contains Harbor 0.20.0.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
sidecar_image="agentlab/harbor-sentinel-task-runner:local"
worker_image="agentlab/benchmarks-worker:harbor-sentinel-e2e"
work_root="$root/.runtime/harbor-sentinel-e2e"
report="$work_root/report.json"
network="agentlab-harbor-sentinel-$$"

cleanup() {
  docker network rm "$network" >/dev/null 2>&1 || true
}
trap cleanup EXIT

"$root/scripts/bootstrap-docker-compose-build-asset.sh"
# Harbor 0.20.0 invokes `docker compose`. Build a small offline overlay over
# the already-built Worker rather than rebuilding its Python dependency layer.
# The source remains mounted for this test, so the exact checked-out Worker code
# is what executes; no package/Agent installation occurs while the task runs.
worker_base="agentlab/benchmarks-worker:local@$(docker image inspect --format '{{.Id}}' agentlab/benchmarks-worker:local)"
DOCKER_BUILDKIT=0 docker build --quiet \
  --build-arg BASE_IMAGE="$worker_base" \
  --file "$root/docker/Dockerfile.worker-compose-overlay" \
  --tag "$worker_image" "$root" >/dev/null
# The pinned base is already local; classic builder avoids an unnecessary remote
# BuildKit metadata lookup through the developer proxy.
DOCKER_BUILDKIT=0 docker build --quiet --file "$root/docker/Dockerfile.harbor-sentinel-task-runner" --tag "$sidecar_image" "$root" >/dev/null
sidecar_digest="$(docker image inspect "$sidecar_image" --format '{{.Id}}')"
case "$sidecar_digest" in
  sha256:[0-9a-f][0-9a-f]*) ;;
  *) echo 'could not resolve immutable sentinel sidecar digest' >&2; exit 2 ;;
esac

docker network create "$network" >/dev/null
# The Worker needs Docker-socket privileges, so a previous run can leave a
# root-owned benchmark-local directory. Clean only this known local fixture
# path through an already-pulled Alpine image, never through host sudo.
docker run --rm \
  --mount "type=bind,src=$root,dst=/agentlab/benchmarks" \
  alpine:3.20 sh -c 'rm -rf /agentlab/benchmarks/.runtime/harbor-sentinel-e2e && mkdir -p /agentlab/benchmarks/.runtime/harbor-sentinel-e2e'

# Mount the benchmark source at the same absolute path: the Docker daemon uses
# these paths for the sidecar's inert workspace bind while the Worker uses the
# source-mounted modules and its prebuilt Harbor installation.
docker run --rm \
  --network "$network" \
  --network-alias agentlab-harbor-sentinel-worker \
  --add-host host.docker.internal:host-gateway \
  --mount type=bind,src=/var/run/docker.sock,dst=/var/run/docker.sock \
  --mount "type=bind,src=$root,dst=$root" \
  --workdir "$root" \
  --env "PYTHONPATH=$root/src" \
  --env "AGENTLAB_SIDECAR_NETWORK=$network" \
  --env 'AGENTLAB_TOOL_GATEWAY_HOST=agentlab-harbor-sentinel-worker' \
  --entrypoint python \
  "$worker_image" \
  -m benchmark_worker.sentinel_e2e \
  --catalog-root "$root" \
  --work-root "$work_root" \
  --sidecar-image "$sidecar_image" \
  --sidecar-digest "$sidecar_digest" \
  --report "$report"

# The trusted Worker runs as root because it owns the Docker socket; normalize
# ownership only for this known local fixture directory so the developer can
# inspect the allowlisted report without privileged host commands.
docker run --rm \
  --mount "type=bind,src=$work_root,dst=/work" \
  alpine:3.20 sh -c "chown -R $(id -u):$(id -g) /work"

# A zero exit means sentinel_e2e verified and wrote an allowlisted report. Its
# own JSON output carries the official-verifier result; raw Harbor logs, task
# text, capabilities, and credentials are never printed here.
printf '%s\n' '{"harbor_sentinel_wrapper":"passed"}'
