"""AgentScope Python SDK."""

from agentscope_sdk.target import (
    RegisteredTarget,
    TargetNotFoundError,
    TargetRegistrationError,
    get_target,
    target,
)

__all__ = [
    "target",
    "get_target",
    "RegisteredTarget",
    "TargetRegistrationError",
    "TargetNotFoundError",
]

__version__ = "0.1.0"
