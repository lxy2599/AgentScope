#!/usr/bin/env bash
# One command for an actual OpenHands or Hermes Harbor smoke benchmark.
# It uses runtime values from AgentLab/.env but never prints them.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
framework="${1:-openhands}"
case "$framework" in openhands|hermes) ;; *) echo 'usage: run-real-agent-harbor-smoke.sh [openhands|hermes]' >&2; exit 2;; esac

if [[ ! -f "$root/.env" ]]; then
  echo 'missing AgentLab/.env' >&2
  exit 2
fi
set -a
# shellcheck disable=SC1090
source "$root/.env"
set +a
export HTTP_PROXY="${HTTP_PROXY:-${AGENTLAB_PROXY_URL:-}}"
export HTTPS_PROXY="${HTTPS_PROXY:-${AGENTLAB_PROXY_URL:-}}"
export NO_PROXY="${NO_PROXY:-localhost,127.0.0.1,::1,agentscope,postgres,clickhouse,openhands,hermes,benchmarks-worker}"

# Fail before creating a project/job when local model credentials are not set.
if [[ -z "${LLM_API_KEY:-${ARK_API_KEY:-}}" ]]; then
  echo 'LLM_API_KEY or ARK_API_KEY must be set in AgentLab/.env before a real Agent benchmark can run' >&2
  exit 2
fi

exec env PYTHONPATH="$root/benchmarks/src${PYTHONPATH:+:$PYTHONPATH}" \
  python3 "$root/benchmarks/scripts/run_real_agent_harbor_smoke.py" \
  --framework "$framework" \
  --server-url "${AGENTSCOPE_SERVER_URL:-http://127.0.0.1:18080}" \
  --catalog-root "$root/benchmarks"
