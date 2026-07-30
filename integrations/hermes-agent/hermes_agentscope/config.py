"""Configuration for the optional AgentScope bridge.

The bridge is disabled unless explicitly requested.  Credentials remain in the
environment and are deliberately excluded from the dataclass repr.
"""

from __future__ import annotations

import os
from dataclasses import dataclass, field
from typing import Mapping


_TRUE_VALUES = frozenset({"1", "true", "yes", "on"})


@dataclass(frozen=True)
class AgentScopeSettings:
    """Resolved bridge settings without exposing the project token in logs."""

    enabled: bool = False
    server_url: str = ""
    project_token: str = field(default="", repr=False)
    environment: str = "dev"
    reason: str = "disabled"


def load_agentscope_settings(env: Mapping[str, str] | None = None) -> AgentScopeSettings:
    """Load bridge settings, returning disabled settings for invalid input.

    Missing configuration is intentionally not an error.  Hermes must keep its
    original execution path when the bridge is disabled, its SDK is absent, or
    AgentScope has not been configured.
    """
    values = os.environ if env is None else env
    requested = str(values.get("HERMES_AGENTSCOPE_ENABLED", "")).strip().lower() in _TRUE_VALUES
    if not requested:
        return AgentScopeSettings()

    server_url = str(values.get("AGENTSCOPE_SERVER_URL", "")).strip().rstrip("/")
    project_token = str(values.get("AGENTSCOPE_PROJECT_TOKEN", "")).strip()
    environment = str(values.get("AGENTSCOPE_ENVIRONMENT", "dev")).strip() or "dev"
    if not server_url:
        return AgentScopeSettings(reason="missing_server_url")
    if not project_token:
        return AgentScopeSettings(server_url=server_url, environment=environment, reason="missing_project_token")
    return AgentScopeSettings(
        enabled=True,
        server_url=server_url,
        project_token=project_token,
        environment=environment,
        reason="enabled",
    )
