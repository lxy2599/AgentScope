#!/bin/sh
# This is the sole pass/fail authority for the smoke trial. It reads the Harbor
# task sandbox, not the sidecar workspace or the Agent final answer.
set -eu

expected='agentlab-real-agent-smoke-v1'
actual=''
if [ -f /app/agentlab-real-agent-smoke.txt ]; then
  actual="$(cat /app/agentlab-real-agent-smoke.txt)"
fi

if [ "$actual" = "$expected" ]; then
  printf '1\n' > /logs/verifier/reward.txt
else
  printf '0\n' > /logs/verifier/reward.txt
fi
