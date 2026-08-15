#!/usr/bin/env python3
"""Provision a fixed local AgentScope account, project, and runtime token.

The account/project identity is persisted in the local .env file.  The token is
cryptographically random by design, but is created once and reused on later
runs.  Plaintext secrets are never printed by this program.
"""

from __future__ import annotations

import argparse
import getpass
import json
import os
import shlex
import stat
import sys
import urllib.error
import urllib.request
from http.cookiejar import CookieJar
from pathlib import Path
from typing import Any


RUNTIME_TOKEN_SCOPES = ["runner:write", "agent:register", "agent:runtime", "otlp:write"]
DEFAULT_DISPLAY_NAME = "AgentLab Model Test Account"
DEFAULT_PROJECT_NAME = "AgentLab Model Tests"
MANAGED_ENV_KEYS = (
    "AGENTLAB_ACCOUNT_EMAIL",
    "AGENTLAB_ACCOUNT_DISPLAY_NAME",
    "AGENTLAB_PROJECT_NAME",
    "AGENTLAB_PROJECT_ID",
    "AGENTSCOPE_PROJECT_TOKEN",
)


class ProvisionError(RuntimeError):
    """A safe, user-actionable provisioning failure."""


class APIError(ProvisionError):
    def __init__(self, method: str, path: str, status: int | None, code: str | None = None) -> None:
        self.method = method
        self.path = path
        self.status = status
        self.code = code
        detail = f"HTTP {status}" if status is not None else "network error"
        if code:
            detail = f"{detail} ({code})"
        super().__init__(f"AgentScope request failed at {method} {path}: {detail}")


class API:
    def __init__(self, server_url: str) -> None:
        cookies = CookieJar()
        self._base_url = server_url.rstrip("/")
        self._opener = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(cookies))

    def request(self, method: str, path: str, body: dict[str, Any] | None = None) -> dict[str, Any] | list[Any]:
        payload = json.dumps(body, sort_keys=True, separators=(",", ":")).encode() if body is not None else None
        headers = {"Accept": "application/json"}
        if payload is not None:
            headers["Content-Type"] = "application/json"
        request = urllib.request.Request(f"{self._base_url}{path}", data=payload, headers=headers, method=method)
        try:
            with self._opener.open(request, timeout=20) as response:
                decoded = json.loads(response.read().decode("utf-8"))
        except urllib.error.HTTPError as exc:
            code = None
            try:
                payload = json.loads(exc.read().decode("utf-8"))
                if isinstance(payload, dict) and isinstance(payload.get("code"), str):
                    code = payload["code"]
            except (UnicodeDecodeError, json.JSONDecodeError):
                pass
            raise APIError(method, path, exc.code, code) from exc
        except (urllib.error.URLError, UnicodeDecodeError, json.JSONDecodeError) as exc:
            raise APIError(method, path, None) from exc
        if not isinstance(decoded, (dict, list)):
            raise ProvisionError(f"AgentScope returned an invalid response at {method} {path}")
        return decoded


def parse_env(env_file: Path) -> dict[str, str]:
    if not env_file.exists():
        return {}
    values: dict[str, str] = {}
    for line in env_file.read_text(encoding="utf-8").splitlines():
        if not line or line.lstrip().startswith("#") or "=" not in line:
            continue
        key, raw_value = line.split("=", 1)
        if key not in MANAGED_ENV_KEYS:
            continue
        if not raw_value.strip():
            values[key] = ""
            continue
        try:
            parsed = shlex.split(raw_value, comments=False, posix=True)
        except ValueError as exc:
            raise ProvisionError(f"Invalid {key} entry in {env_file}") from exc
        if len(parsed) != 1:
            raise ProvisionError(f"Invalid {key} entry in {env_file}")
        values[key] = parsed[0]
    return values


def write_env(env_file: Path, values: dict[str, str]) -> None:
    existing = env_file.read_text(encoding="utf-8") if env_file.exists() else ""
    lines = [line for line in existing.splitlines() if not any(line.startswith(f"{key}=") for key in values)]
    lines.extend(f"{key}={shlex.quote(value)}" for key, value in values.items())
    env_file.write_text("\n".join(lines) + "\n", encoding="utf-8")
    env_file.chmod(stat.S_IRUSR | stat.S_IWUSR)


def required_string(payload: dict[str, Any], field: str, label: str) -> str:
    value = payload.get(field)
    if not isinstance(value, str) or not value:
        raise ProvisionError(f"AgentScope did not return a {label}")
    return value


def login_or_register(api: API, email: str, password: str, display_name: str) -> tuple[str, str]:
    try:
        response = api.request("POST", "/api/auth/login", {"email": email, "password": password})
    except APIError as login_error:
        if login_error.status != 401:
            raise
        try:
            response = api.request(
                "POST",
                "/api/auth/register",
                {"email": email, "password": password, "display_name": display_name},
            )
        except APIError as register_error:
            if register_error.status == 409:
                raise ProvisionError(
                    "The fixed AgentScope account already exists, but the provided password was not accepted. "
                    "Use its existing password."
                ) from register_error
            raise
        print("Created fixed local AgentScope account.")
    else:
        print("Reused fixed local AgentScope account.")
    if not isinstance(response, dict):
        raise ProvisionError("AgentScope returned an invalid account response")
    return (
        required_string(response, "id", "account id"),
        required_string(response, "display_name", "account display name"),
    )


def list_projects(api: API) -> list[dict[str, Any]]:
    response = api.request("GET", "/api/projects")
    if not isinstance(response, list) or not all(isinstance(item, dict) for item in response):
        raise ProvisionError("AgentScope returned an invalid project list")
    return response


def resolve_project(api: API, project_id: str, project_name: str, *, allow_create: bool) -> tuple[str, bool]:
    projects = list_projects(api)
    if project_id:
        for project in projects:
            if project.get("id") == project_id:
                if project.get("name") != project_name:
                    raise ProvisionError(
                        "AGENTLAB_PROJECT_ID does not match AGENTLAB_PROJECT_NAME; refusing to bind the runtime to a different project"
                    )
                return project_id, False
        raise ProvisionError("AGENTLAB_PROJECT_ID is not accessible to the fixed account; refusing to create a different project")
    matches = [project for project in projects if project.get("name") == project_name]
    if len(matches) == 1:
        return required_string(matches[0], "id", "project id"), False
    if len(matches) > 1:
        raise ProvisionError("More than one project has AGENTLAB_PROJECT_NAME; set AGENTLAB_PROJECT_ID explicitly")
    if not allow_create:
        raise ProvisionError(
            "AGENTSCOPE_PROJECT_TOKEN exists but AGENTLAB_PROJECT_ID cannot be resolved; refusing to create a different project"
        )
    created = api.request("POST", "/api/projects", {"name": project_name})
    if not isinstance(created, dict):
        raise ProvisionError("AgentScope returned an invalid project response")
    return required_string(created, "id", "project id"), True


def provision(api: API, env_file: Path, email: str, password: str, display_name: str, project_name: str) -> None:
    existing = parse_env(env_file)
    _, account_display_name = login_or_register(api, email, password, display_name)
    token = existing.get("AGENTSCOPE_PROJECT_TOKEN", "")
    project_id, project_created = resolve_project(
        api, existing.get("AGENTLAB_PROJECT_ID", ""), project_name, allow_create=not bool(token)
    )

    values = {
        "AGENTLAB_ACCOUNT_EMAIL": email,
        "AGENTLAB_ACCOUNT_DISPLAY_NAME": account_display_name,
        "AGENTLAB_PROJECT_NAME": project_name,
        "AGENTLAB_PROJECT_ID": project_id,
    }
    if token:
        write_env(env_file, {**values, "AGENTSCOPE_PROJECT_TOKEN": token})
        print("Reused fixed AgentScope project and existing runtime token.")
        return

    created = api.request(
        "POST",
        f"/api/projects/{project_id}/tokens",
        {
            "description": "AgentLab persistent local runtime",
            "scopes": RUNTIME_TOKEN_SCOPES,
            "environment": "development",
            "token_type": "custom",
        },
    )
    if not isinstance(created, dict):
        raise ProvisionError("AgentScope returned an invalid Project Token response")
    token = required_string(created, "plain_token", "Project Token")
    write_env(env_file, {**values, "AGENTSCOPE_PROJECT_TOKEN": token})
    if project_created:
        print("Created fixed AgentScope project and persistent runtime token in .env.")
    else:
        print("Reused fixed AgentScope project and created its persistent runtime token in .env.")


def main() -> int:
    parser = argparse.ArgumentParser(description="Provision a fixed local AgentScope account and AgentLab project")
    parser.add_argument("--server-url", required=True)
    parser.add_argument("--env-file", type=Path, required=True)
    parser.add_argument("--email", help="Fixed account email; persisted as AGENTLAB_ACCOUNT_EMAIL")
    parser.add_argument("--display-name", help="Fixed account display name")
    parser.add_argument("--project-name", help="Fixed AgentLab project name")
    args = parser.parse_args()

    try:
        existing = parse_env(args.env_file)
        email = args.email or existing.get("AGENTLAB_ACCOUNT_EMAIL", "")
        if not email and sys.stdin.isatty():
            email = input("Fixed AgentScope account email: ").strip()
        if not email:
            raise ProvisionError("Set AGENTLAB_ACCOUNT_EMAIL in .env or pass --email")
        display_name = args.display_name or existing.get("AGENTLAB_ACCOUNT_DISPLAY_NAME", DEFAULT_DISPLAY_NAME)
        project_name = args.project_name or existing.get("AGENTLAB_PROJECT_NAME", DEFAULT_PROJECT_NAME)
        password = os.environ.get("AGENTLAB_ACCOUNT_PASSWORD", "")
        if not password and sys.stdin.isatty():
            password = getpass.getpass("Fixed AgentScope account password: ")
        if not password:
            raise ProvisionError("Set AGENTLAB_ACCOUNT_PASSWORD for this command; it is not written to .env")
        provision(API(args.server_url), args.env_file, email, password, display_name, project_name)
        return 0
    except ProvisionError as exc:
        print(f"provision-account error: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
