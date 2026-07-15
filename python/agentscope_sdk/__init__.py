"""AgentScope Python SDK."""

from agentscope_sdk.evaluators import EvaluationResult, EvaluatorConfigError, evaluate_rule
from agentscope_sdk.judges import (
    JudgeProvider,
    JudgeRequest,
    JudgeResponseError,
    JudgeResult,
    parse_judge_response,
)
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
    "EvaluationResult",
    "evaluate_rule",
    "EvaluatorConfigError",
    "JudgeRequest",
    "JudgeResult",
    "JudgeProvider",
    "JudgeResponseError",
    "parse_judge_response",
]

__version__ = "0.1.0"
