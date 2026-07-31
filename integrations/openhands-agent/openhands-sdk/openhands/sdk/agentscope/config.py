"""Optional AgentScope configuration for the controlled OpenHands fork."""

from __future__ import annotations

import os
from collections.abc import Mapping
from dataclasses import dataclass, field


_TRUE_VALUES = frozenset({"1", "true", "yes", "on"})
_CAPTURE_LEVELS = frozenset({"L0", "L1", "L2"})


@dataclass(frozen=True)
class AgentScopeSettings:
    """Resolved settings; credentials are intentionally excluded from repr."""

    enabled: bool = False
    server_url: str = ""
    project_token: str = field(default="", repr=False)
    environment: str = "dev"
    capture_level: str = "L0"
    reason: str = "disabled"


def load_agentscope_settings(
    env: Mapping[str, str] | None = None,
) -> AgentScopeSettings:
    """Load opt-in settings without making invalid configuration fatal."""
    values = os.environ if env is None else env
    enabled = (
        str(values.get("OPENHANDS_AGENTSCOPE_ENABLED", "")).strip().lower()
        in _TRUE_VALUES
    )
    if not enabled:
        return AgentScopeSettings()

    server_url = str(values.get("AGENTSCOPE_SERVER_URL", "")).strip().rstrip("/")
    project_token = str(values.get("AGENTSCOPE_PROJECT_TOKEN", "")).strip()
    environment = str(values.get("AGENTSCOPE_ENVIRONMENT", "dev")).strip() or "dev"
    capture_level = (
        str(values.get("OPENHANDS_AGENTSCOPE_CAPTURE_LEVEL", "L0")).strip().upper()
    )
    if capture_level not in _CAPTURE_LEVELS:
        capture_level = "L0"
    if not server_url:
        return AgentScopeSettings(
            environment=environment,
            capture_level=capture_level,
            reason="missing_server_url",
        )
    if not project_token:
        return AgentScopeSettings(
            server_url=server_url,
            environment=environment,
            capture_level=capture_level,
            reason="missing_project_token",
        )
    return AgentScopeSettings(
        enabled=True,
        server_url=server_url,
        project_token=project_token,
        environment=environment,
        capture_level=capture_level,
        reason="enabled",
    )
