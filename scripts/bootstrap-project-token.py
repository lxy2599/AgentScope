#!/usr/bin/env python3
"""Create a local AgentScope project token without printing its plaintext."""

from __future__ import annotations

import argparse
import json
import os
import secrets
import stat
import urllib.error
import urllib.request
from http.cookiejar import CookieJar
from pathlib import Path
from typing import Any


class BootstrapError(RuntimeError):
    pass


class API:
    def __init__(self, server_url: str) -> None:
        cookies = CookieJar()
        self._base_url = server_url.rstrip("/")
        self._opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookies))

    def request(self, method: str, path: str, body: dict[str, Any] | None = None) -> dict[str, Any]:
        payload = json.dumps(body, sort_keys=True, separators=(",", ":")).encode() if body is not None else None
        headers = {"Accept": "application/json"}
        if payload is not None:
            headers["Content-Type"] = "application/json"
        request = urllib.request.Request(f"{self._base_url}{path}", data=payload, headers=headers, method=method)
        try:
            with self._opener.open(request, timeout=20) as response:
                decoded = json.loads(response.read().decode("utf-8"))
        except (urllib.error.HTTPError, urllib.error.URLError, UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise BootstrapError(f"local control-plane bootstrap failed at {method} {path}") from exc
        if not isinstance(decoded, dict):
            raise BootstrapError(f"local control-plane bootstrap returned an invalid response at {method} {path}")
        return decoded


def required_id(payload: dict[str, Any], label: str) -> str:
    value = payload.get("id")
    if not isinstance(value, str) or not value:
        raise BootstrapError(f"local control-plane bootstrap did not return a {label}")
    return value


def append_env(env_file: Path, values: dict[str, str]) -> None:
    existing = env_file.read_text(encoding="utf-8") if env_file.exists() else ""
    lines = [line for line in existing.splitlines() if not any(line.startswith(f"{key}=") for key in values)]
    lines.extend(f"{key}={value}" for key, value in values.items())
    env_file.write_text("\n".join(lines) + "\n", encoding="utf-8")
    env_file.chmod(stat.S_IRUSR | stat.S_IWUSR)


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--server-url", required=True)
    parser.add_argument("--env-file", type=Path, required=True)
    args = parser.parse_args()

    api = API(args.server_url)
    nonce = secrets.token_hex(12)
    api.request(
        "POST",
        "/api/auth/register",
        {
            "email": f"agentlab-local-{nonce}@example.invalid",
            "password": secrets.token_urlsafe(32),
            "display_name": "AgentLab Local Control Plane",
        },
    )
    project = api.request("POST", "/api/projects", {"name": f"AgentLab Local {nonce}"})
    project_id = required_id(project, "project id")
    created = api.request(
        "POST",
        f"/api/projects/{project_id}/tokens",
        {
            "description": "AgentLab local runtime",
            "scopes": ["runner:write", "agent:register", "agent:runtime", "otlp:write"],
            "environment": "development",
            "token_type": "custom",
        },
    )
    token = created.get("plain_token")
    if not isinstance(token, str) or not token:
        raise BootstrapError("local control-plane bootstrap did not return a project token")
    append_env(args.env_file, {"AGENTSCOPE_PROJECT_TOKEN": token, "AGENTLAB_PROJECT_ID": project_id})
    print("Created local AgentScope project token in .env.")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
