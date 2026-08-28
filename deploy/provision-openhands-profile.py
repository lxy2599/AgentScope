#!/usr/bin/env python3
"""Persist the default OpenHands model profile without logging secrets."""

import json
import os
import sys
import urllib.error
import urllib.request


def required(name: str) -> str:
    value = os.environ.get(name, "")
    if not value:
        raise RuntimeError(f"missing {name}")
    return value


payload = {
    "llm": {
        "model": required("OPENHANDS_PROFILE_MODEL"),
        "api_key": required("OPENHANDS_PROFILE_API_KEY"),
        "base_url": required("OPENHANDS_PROFILE_BASE_URL"),
        "usage_id": "agentlab-openhands-interactive",
        "stream": True,
        "num_retries": 3,
    },
    "include_secrets": True,
}
request = urllib.request.Request(
    required("OPENHANDS_PROFILE_SERVER_URL") + "/api/profiles/default",
    data=json.dumps(payload).encode("utf-8"),
    headers={"Content-Type": "application/json"},
    method="POST",
)
try:
    with urllib.request.urlopen(request, timeout=30) as response:
        result = json.load(response)
except urllib.error.HTTPError as error:
    print(f"OpenHands profile provisioning failed: HTTP {error.code}", file=sys.stderr)
    raise SystemExit(1)
except Exception as error:
    print(f"OpenHands profile provisioning failed: {type(error).__name__}", file=sys.stderr)
    raise SystemExit(1)
if result.get("name") != "default":
    print("OpenHands profile provisioning returned an unexpected response", file=sys.stderr)
    raise SystemExit(1)
print("OpenHands default model profile provisioned.")
