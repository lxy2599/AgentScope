#!/bin/sh
# Harbor executes this script as the official verifier in the task sandbox.
# It deliberately performs no package installation or network access.
set -eu

expected='agentlab-harbor-sentinel-v1'
actual=''
if [ -f /app/.agentlab-harbor-sentinel ]; then
  actual="$(cat /app/.agentlab-harbor-sentinel)"
fi

if [ "$actual" = "$expected" ]; then
  printf '1\n' > /logs/verifier/reward.txt
  printf '{"schema_version":"agentlab.harbor-sentinel-evidence.v1","task_sandbox_file":true,"content_matches":true}\n' > /logs/verifier/sentinel-evidence.json
else
  printf '0\n' > /logs/verifier/reward.txt
  printf '{"schema_version":"agentlab.harbor-sentinel-evidence.v1","task_sandbox_file":false,"content_matches":false}\n' > /logs/verifier/sentinel-evidence.json
fi
