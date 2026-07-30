"""Framework-neutral event normalization for external agent adapters."""

from __future__ import annotations

import json
import re
from dataclasses import dataclass, field
from typing import Any

from agentscope_sdk.tracing_contract import SemanticKind


@dataclass(frozen=True)
class NormalizedEvent:
    """A provider-independent event safe to map to an AgentScope span."""

    semantic_kind: str
    name: str
    input_summary: str = ""
    output_summary: str = ""
    attributes: dict[str, Any] = field(default_factory=dict)
    success: bool = True
    error: str = ""


def _slug(value: str) -> str:
    result = re.sub(r"[^a-zA-Z0-9_.-]+", "_", str(value).strip()).strip("_.-")
    return result or "unknown"


def _summary(value: Any, limit: int = 1024) -> str:
    if value is None:
        return ""
    if isinstance(value, str):
        return value[:limit]
    try:
        return json.dumps(value, ensure_ascii=False, sort_keys=True)[:limit]
    except (TypeError, ValueError):
        return str(value)[:limit]


def normalize_event(
    event_type: str,
    name: str,
    *,
    arguments: Any = None,
    result: Any = None,
    success: bool = True,
    error: str = "",
    attributes: dict[str, Any] | None = None,
) -> NormalizedEvent:
    """Normalize a framework event into AgentScope's semantic vocabulary.

    Event names intentionally stay explicit (for example ``memory.read``)
    because the trace UI can render raw spans even when no dedicated storage
    table exists for that semantic type.
    """

    normalized_type = event_type.strip().lower().replace("_", ".")
    safe_name = _slug(name)
    attrs = dict(attributes or {})
    input_summary = _summary(arguments)
    output_summary = _summary(result)

    if normalized_type.startswith("memory."):
        operation = normalized_type.split(".", 1)[1] or "access"
        attrs.update({"type": "memory", "memory.operation": operation, "memory.name": name})
        return NormalizedEvent(
            SemanticKind.MEMORY, f"memory.{operation}.{safe_name}", input_summary, output_summary, attrs, success, error
        )

    if normalized_type.startswith("guardrail.") or normalized_type.startswith("safety."):
        blocked = normalized_type.endswith("blocked") or not success
        attrs.update({"type": "guardrail", "guardrail.name": name, "guardrail.blocked": blocked})
        return NormalizedEvent(
            SemanticKind.GUARDRAIL, f"guardrail.{safe_name}", input_summary, output_summary, attrs, success, error
        )

    if normalized_type.startswith("skill."):
        attrs.update({"skill.name": name})
        return NormalizedEvent(SemanticKind.SKILL, f"skill.{safe_name}", input_summary, output_summary, attrs, success, error)

    if normalized_type.startswith("thought.") or normalized_type in {"thought", "reasoning"}:
        attrs.update({"thought.stage": normalized_type.rsplit(".", 1)[-1]})
        return NormalizedEvent(SemanticKind.THOUGHT, f"agent.{safe_name}", input_summary, output_summary, attrs, success, error)

    if normalized_type.startswith("llm."):
        return NormalizedEvent(SemanticKind.LLM, f"llm.{safe_name}", input_summary, output_summary, attrs, success, error)

    attrs.update({"tool.name": name})
    return NormalizedEvent(SemanticKind.TOOL, f"tool.{safe_name}", input_summary, output_summary, attrs, success, error)
